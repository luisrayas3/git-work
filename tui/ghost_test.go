package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// opened is the creator a ghost's enter pushed, with the draft it opened on.
func opened(t *testing.T, p page, spelling string) *newPage {
	t.Helper()
	_, cmd := p.Update(press(spelling))
	require.NotNil(t, cmd, "enter on a ghost answers with the creator")
	msg, ok := cmd().(pushMsg)
	require.True(t, ok, "the creator is pushed over the view")
	created, ok := msg.page.(*newPage)
	require.True(t, ok)
	return created
}

// TestListGhostPerGroup: a dim + row at the foot of each group, reached
// with the cursor, opening the creator with the group's value and the
// type its rows share filled in; the count leaves it out (C6, C7).
func TestListGhostPerGroup(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "two", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "three", "status": "in-progress"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status"}`)
	drawn := plainView(page)
	require.Equal(t, 2, strings.Count(drawn, ghostLabel), "one ghost per group")
	require.Contains(t, drawn, "3 issues", "the ghosts are not issues")

	// the groups come in first-appearance order, last edited first: the
	// in-progress group with `three`, then to-do with `two` and `one`; a
	// ghost is the last row of each, before the next header
	lines := strings.Split(drawn, "\n")
	var ghosts []int
	threeAt, todoAt, oneAt := -1, -1, -1
	for at, line := range lines {
		switch {
		case strings.Contains(line, ghostLabel):
			ghosts = append(ghosts, at)
		case strings.Contains(line, "three"):
			threeAt = at
		case strings.Contains(line, "one"):
			oneAt = at
		case strings.TrimSpace(line) == "to-do":
			todoAt = at
		}
	}
	require.Len(t, ghosts, 2)
	require.Less(t, threeAt, ghosts[0])
	require.Less(t, ghosts[0], todoAt)
	require.Less(t, oneAt, ghosts[1])

	// end lands on the last row, which is the last group's ghost
	page = send(page, "end").(*listPage)
	require.True(t, page.node().ghost)
	require.Contains(t, plainView(page), "enter: new issue")

	created := opened(t, page, "enter")
	require.Equal(t, "task", created.typeKey(), "every row in the group is a task")
	require.Equal(t, `"to-do"`, string(created.fields["status"]), "the group's value is prefilled")
	require.NotNil(t, created.editor, "and the title is being typed")

	// space, copy and grab have nothing on a ghost
	_, cmd := page.Update(press("space"))
	require.NotNil(t, cmd)
	require.Contains(t, page.status, "enter adds")
	_, cmd = page.Update(press("alt+c"))
	require.NotNil(t, cmd)
}

// TestListGhostUngroupedAndFiltered: one ghost for an ungrouped list, and
// none while a filter is on.
func TestListGhostUngroupedAndFiltered(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	require.Equal(t, 1, strings.Count(plainView(page), ghostLabel))
	require.Equal(t, 2, len(page.order))

	page.filter = "one"
	page.reorder()
	require.Equal(t, 1, len(page.order), "the filter hides the ghost")
	require.NotContains(t, plainView(page), ghostLabel)

	// a store with nothing in it still has somewhere to add
	empty := list(t, testRepo(t), "")
	require.Equal(t, 1, strings.Count(plainView(empty), ghostLabel))
	created := opened(t, empty, "enter")
	require.Equal(t, "", created.typeKey(), "no rows, no type to share: the page opens on the type")
}

// TestListGhostIsNotARankSibling: a row dragged past the last of its group
// steps over the ghost into the next group, and the drop ranks the real
// rows only.
func TestListGhostIsNotARankSibling(t *testing.T) {
	repo := testRepo(t)
	one := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "two", "status": "in-progress"})

	// last edited first: in-progress's `two` heads the list, to-do's
	// `one` is in the last group, so up is the way into the other group
	page := list(t, repo, `{"fields":["title"],"group_by":"status"}`)
	page.putCursorOn(one)
	page = send(page, "space", "up").(*listPage)
	require.Equal(t, "in-progress", page.nodes[page.grabbed].group)
	// drawn under the in-progress header, above its ghost
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, "in-progress"), indexOf(drawn, "one"))
	require.Less(t, indexOf(drawn, "one"), indexOf(drawn, ghostLabel))

	page = send(page, "space").(*listPage)
	require.Contains(t, page.status, "moved to in-progress")
	require.Equal(t, `"in-progress"`, field(t, repo, one, "status"))
}

// TestBoardGhostPerColumn: a + card at the foot of every column, in every
// lane, opening the creator with the column's value, and the lane's, and
// the type the lane's cards share; counts leave it out.
func TestBoardGhostPerColumn(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do", "priority": "high"})
	newIssue(t, repo, map[string]any{"title": "two", "status": "to-do", "priority": "low"})

	page := board(t, repo, `{"columns":"status"}`)
	require.Equal(t, len(page.columns), strings.Count(plainView(page), ghostLabel), "a ghost in every column")
	require.Contains(t, header(page), "To Do (2)", "the header counts cards, not ghosts")
	require.Contains(t, plainView(page), "2 issues")
	require.False(t, page.current().ghost, "the cursor opens on a card")

	// down past the last card is the ghost
	page = send(page, "down", "down").(*boardPage)
	require.True(t, page.current().ghost)
	created := opened(t, page, "enter")
	require.Equal(t, "task", created.typeKey())
	require.Equal(t, `"to-do"`, string(created.fields["status"]))

	// an empty column's ghost is reachable, and prefills that column
	page = send(page, "right").(*boardPage)
	require.True(t, page.current().ghost)
	require.Equal(t, "in-progress", page.columns[page.col].value)
	created = opened(t, page, "enter")
	require.Equal(t, `"in-progress"`, string(created.fields["status"]))

	// a grab on a ghost rings
	_, cmd := page.Update(press("space"))
	require.NotNil(t, cmd)
	require.Equal(t, -1, page.grabbed)

	// with swimlanes, the lane's value comes too
	lanes := board(t, repo, `{"columns":"status","group_by":"priority"}`)
	for l, la := range lanes.lanes {
		if la.group == "low" {
			lanes.lane, lanes.col, lanes.row = l, 1, 0
		}
	}
	lanes = send(lanes, "end").(*boardPage)
	require.True(t, lanes.current().ghost)
	created = opened(t, lanes, "enter")
	require.Equal(t, `"to-do"`, string(created.fields["status"]))
	require.Equal(t, `"low"`, string(created.fields["priority"]))
}

// TestGanttGhostPerGroup: the chart has the list's ghosts, drawn with no
// bar, opening the creator with the group's value.
func TestGanttGhostPerGroup(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newTyped(t, repo, "task", map[string]any{"title": "one", "start": "2026-09-07", "stop": "2026-09-21", "status": "to-do"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","group_by":"status"}`)
	require.Equal(t, 1, strings.Count(plainView(page), ghostLabel))
	require.Contains(t, plainView(page), "1 issue")

	page = send(page, "end").(*ganttPage)
	require.True(t, page.node().ghost)
	created := opened(t, page, "enter")
	require.Equal(t, "task", created.typeKey())
	require.Equal(t, `"to-do"`, string(created.fields["status"]))

	_, cmd := page.Update(press("space"))
	require.NotNil(t, cmd)
	require.Equal(t, -1, page.grabbed)
}
