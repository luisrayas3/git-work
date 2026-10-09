package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
)

// `empty_groups` on the board adds the swimlanes no card falls in
// (doc/design/empty-groups.md).

// TestBoardEmptyGroupsOfAnEnum: every value the schema lists, in schema
// order; off, the lanes the cards have, as before.
func TestBoardEmptyGroupsOfAnEnum(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "a", "status": "to-do", "priority": "low"})
	newIssue(t, repo, map[string]any{"title": "b", "status": "to-do", "priority": "high"})

	off := board(t, repo, `{"columns":"status","group_by":"priority"}`)
	require.Equal(t, []string{"high", "low"}, laneLabels(off))

	on := board(t, repo, `{"columns":"status","group_by":"priority","empty_groups":true}`)
	require.Equal(t, []string{"highest", "high", "medium", "low", "lowest"}, laneLabels(on),
		"(none) only where a card has no value")
	require.Contains(t, plainView(on), "2 issues", "an empty lane adds no card to the count")
	ghosts := 0
	for _, c := range on.cards {
		if c.ghost {
			ghosts++
		}
	}
	require.Equal(t, 5*len(on.columns), ghosts, "a ghost in every column of every lane")
}

// TestBoardEmptyGroupsNothingForText: a field with no set of values to
// draw changes nothing, and neither does the type.
func TestBoardEmptyGroupsNothingForText(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "a", "status": "to-do"})

	page := board(t, repo, `{"columns":"status","group_by":"title","empty_groups":true}`)
	require.Equal(t, []string{"a"}, laneLabels(page))
	page = board(t, repo, `{"columns":"status","group_by":"type","empty_groups":true}`)
	require.Equal(t, []string{"task"}, laneLabels(page))
}

// initiatives is the design's example: initiative X with epics A, B and C,
// initiative Y with epic D, an archived epic under X, and a task under each
// of A and B, titled so the query meets A's first.
func initiatives(t *testing.T) (repo *cache.RepoCache, a, b, c, d, gone string) {
	t.Helper()

	repo = testRepo(t)
	x := newTyped(t, repo, "initiative", map[string]any{"title": "X"})
	y := newTyped(t, repo, "initiative", map[string]any{"title": "Y"})
	a = newTyped(t, repo, "epic", map[string]any{"title": "epic A", "parent": x})
	b = newTyped(t, repo, "epic", map[string]any{"title": "epic B", "parent": x})
	c = newTyped(t, repo, "epic", map[string]any{"title": "epic C", "parent": x})
	d = newTyped(t, repo, "epic", map[string]any{"title": "epic D", "parent": y})
	gone = newTyped(t, repo, "epic", map[string]any{"title": "epic gone", "parent": x, "archived": true})
	newIssue(t, repo, map[string]any{"title": "a task", "status": "to-do", "parent": a})
	newIssue(t, repo, map[string]any{"title": "b task", "status": "to-do", "parent": b})
	return repo, a, b, c, d, gone
}

const xTasks = `"columns":"status","group_by":"parent",` +
	`"query":"map(select(.fields.type == \"task\")) | sort_by(.fields.title)"`

// TestBoardEmptyGroupsOfARelation: grouped by epic, the board of X's tasks
// draws X's epic with no task, never Y's, and never an archived one.
func TestBoardEmptyGroupsOfARelation(t *testing.T) {
	repo, a, b, c, _, gone := initiatives(t)

	off := board(t, repo, `{`+xTasks+`}`)
	require.Len(t, off.lanes, 2, "off, the lanes the cards have")

	page := board(t, repo, `{`+xTasks+`,"empty_groups":true}`)
	lanes := laneLabels(page)
	require.Len(t, lanes, 3)
	for at, title := range []string{"epic A", "epic B", "epic C"} {
		require.Contains(t, lanes[at], title, "lane %d", at)
	}
	require.Equal(t, []any{a, b, c}, []any{page.lanes[0].raw, page.lanes[1].raw, page.lanes[2].raw},
		"each lane carries the full id")

	archived := board(t, repo, `{`+xTasks+`,"empty_groups":true,"include_archive":true}`)
	var raws []any
	for _, la := range archived.lanes {
		raws = append(raws, la.raw)
	}
	require.Contains(t, raws, gone, "include_archive brings the archived sibling back")
}

// TestBoardEmptyGroupsAnchorOnNoValue: a lane issue with no value of its
// own anchors on "no value", so its siblings are the target issues with none
// either, of any type the field may point at (E3, Q1).
func TestBoardEmptyGroupsAnchorOnNoValue(t *testing.T) {
	repo := testRepo(t)
	x := newTyped(t, repo, "initiative", map[string]any{"title": "X"})
	loose := newTyped(t, repo, "epic", map[string]any{"title": "loose epic"})
	other := newTyped(t, repo, "epic", map[string]any{"title": "other loose epic"})
	filed := newTyped(t, repo, "epic", map[string]any{"title": "filed epic", "parent": x})
	story := newTyped(t, repo, "story", map[string]any{"title": "loose story"})
	newIssue(t, repo, map[string]any{"title": "a task", "status": "to-do", "parent": loose})

	page := board(t, repo, `{`+xTasks+`,"empty_groups":true}`)
	var raws []any
	for _, la := range page.lanes {
		raws = append(raws, la.raw)
	}
	require.Contains(t, raws, loose)
	require.Contains(t, raws, other)
	require.Contains(t, raws, story, "a task's parent may be a story")
	require.NotContains(t, raws, filed)
	require.NotContains(t, raws, x, "an initiative is not a task's parent")
}

// TestBoardEmptyLaneGhostAndDrop: the ghost of an empty lane is reached
// with down and prefilled with the lane's stored full id, and a card
// dropped into the lane writes that id (E5).
func TestBoardEmptyLaneGhostAndDrop(t *testing.T) {
	repo, _, b, c, _, _ := initiatives(t)

	page := board(t, repo, `{`+xTasks+`,"empty_groups":true}`)
	// a task, A's ghost, b task, B's ghost, C's ghost
	page = send(page, "down", "down", "down", "down").(*boardPage)
	require.True(t, page.current().ghost)
	require.Equal(t, 2, page.lane, "in C's lane")
	created := opened(t, page, "enter")
	require.Equal(t, `"`+c+`"`, string(created.fields["parent"]), "the full id, never the label")
	require.Equal(t, "task", created.typeKey(), "the type the board's cards share")

	page = board(t, repo, `{`+xTasks+`,"empty_groups":true}`)
	page = send(page, "down", "down").(*boardPage)
	moved := page.currentId()
	require.Equal(t, b, fieldOf(t, repo, moved, "parent"))
	page = send(page, "space", "j", "space").(*boardPage)
	require.Equal(t, c, fieldOf(t, repo, moved, "parent"))
	require.Contains(t, plainView(page), "moved to "+c[:idWidth])
	require.Len(t, page.lanes, 3, "the lane B emptied stays, C's sibling")
}
