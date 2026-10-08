package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/view"
)

// drawnKeys is the keys of the rows drawn, ghosts left out, in order.
func drawnKeys(p *listPage) []string {
	var keys []string
	for _, index := range p.order {
		if !p.nodes[index].ghost {
			keys = append(keys, p.nodes[index].key)
		}
	}
	return keys
}

// TestAKeyedRowReordersForThisViewOnly: a row whose key is not its id
// stands for a share of an issue, so its drop writes no rank; the view
// holds the order by key across a refresh, and a key it does not know keeps
// its (rank, id) place after the ones it does (R5).
func TestAKeyedRowReordersForThisViewOnly(t *testing.T) {
	repo := testRepo(t)
	a := newIssue(t, repo, map[string]any{"title": "a"})
	b := newIssue(t, repo, map[string]any{"title": "b"})

	page := listCall(t, repo, map[string]any{
		"query": `sort_by(.fields.title) | map(. + {key: (.id + "@x")})`,
	})
	require.Equal(t, []string{a + "@x", b + "@x"}, drawnKeys(page))

	cursorTo(t, page, b+"@x")
	page = send(page, "space", "up", "space").(*listPage)
	require.Equal(t, "order kept for this view", page.status)
	require.Equal(t, []string{b + "@x", a + "@x"}, drawnKeys(page))
	require.Equal(t, "", fieldOf(t, repo, a, "rank"), "nothing is written")
	require.Equal(t, "", fieldOf(t, repo, b, "rank"))
	require.Equal(t, b+"@x", page.currentKey())

	// a refresh keeps it, and a new row goes after the ones the view knows
	c := newIssue(t, repo, map[string]any{"title": "0 first by title"})
	page.Update(refreshMsg{})
	require.Equal(t, []string{b + "@x", a + "@x", c + "@x"}, drawnKeys(page))

	// a row with no id drags the same way
	page = listCall(t, repo, map[string]any{
		"query": `sort_by(.fields.title) | . + [{key: "none", fields: {title: "(none)"}}]`,
	})
	cursorTo(t, page, "none")
	page = send(page, "space", "up", "space").(*listPage)
	require.Equal(t, "order kept for this view", page.status)
	keys := drawnKeys(page)
	require.Equal(t, "none", keys[len(keys)-2])
}

// TestAKeyedRowStaysInItsGroup: dropping a share in another group would
// write the issue's field, so a keyed row carried past the edge of its
// group rings and stays (R5, Out of scope).
func TestAKeyedRowStaysInItsGroup(t *testing.T) {
	repo := testRepo(t)
	a := newIssue(t, repo, map[string]any{"title": "a", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "b", "status": "in-progress"})

	page := listCall(t, repo, map[string]any{
		"query":    `sort_by(.fields.title) | map(. + {key: (.id + "@x")})`,
		"group_by": "status",
	})
	cursorTo(t, page, a+"@x")
	group := page.node().group
	_, cmd := page.Update(press("space"))
	require.NotNil(t, cmd)
	_, cmd = page.Update(press("down"))
	require.Equal(t, "stays in its group", page.status)
	require.NotNil(t, cmd, "it rings")
	page = send(page, "space").(*listPage)
	require.Equal(t, group, nodeOf(t, page, a+"@x").group)
	require.Equal(t, "to-do", fieldOf(t, repo, a, "status"))
}

// TestAPlainDropBesideAKeyedRow: a row keyed by its own id drags and writes
// its rank as before, and no rank is written for a keyed sibling's issue;
// the view keeps the scope as it was drawn, since a keyed row's place is
// one no rank can hold (R5).
func TestAPlainDropBesideAKeyedRow(t *testing.T) {
	repo := testRepo(t)
	x := newIssue(t, repo, map[string]any{"title": "x"})
	y := newIssue(t, repo, map[string]any{"title": "y"})
	z := newIssue(t, repo, map[string]any{"title": "z"})

	page := listCall(t, repo, map[string]any{
		"query": `sort_by(.fields.title) | map(if .fields.title == "z" then . + {key: "z@x"} else . end)`,
	})
	require.Equal(t, []string{x, y, "z@x"}, drawnKeys(page))

	cursorTo(t, page, y)
	page = send(page, "space", "down", "space").(*listPage)
	require.True(t, strings.HasPrefix(page.status, "rank set"), page.status)
	require.NotEqual(t, "", fieldOf(t, repo, x, "rank"), "the row above is filled")
	require.NotEqual(t, "", fieldOf(t, repo, y, "rank"))
	require.Equal(t, "", fieldOf(t, repo, z, "rank"), "the keyed row's issue is not")
	require.Equal(t, []string{x, "z@x", y}, drawnKeys(page), "the drop reads as it was drawn")
}

// TestAGanttKeyedRowReordersForThisViewOnly: the gantt holds a keyed bar's
// order the way the list does, and writes none.
func TestAGanttKeyedRowReordersForThisViewOnly(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	a := newTyped(t, repo, "task", map[string]any{"title": "a", "start": "2026-09-28", "stop": "2026-10-02"})
	b := newTyped(t, repo, "task", map[string]any{"title": "b", "start": "2026-09-28", "stop": "2026-10-02"})

	g, err := newGanttPage(repo, call(t, view.KindGantt, map[string]any{
		"query": `sort_by(.fields.title) | map(. + {key: (.id + "@x")})`, "start": "start", "stop": "stop",
	}))
	require.NoError(t, err)
	for at, index := range g.order {
		if g.bars[index].key == b+"@x" {
			g.cursor = at
		}
	}
	g = send(g, "space", "up", "space").(*ganttPage)
	require.Equal(t, "order kept for this view", g.status)
	require.Equal(t, b+"@x", g.bars[g.order[0]].key)
	require.Equal(t, "", fieldOf(t, repo, a, "rank"))
	require.Equal(t, "", fieldOf(t, repo, b, "rank"))

	g.Update(refreshMsg{})
	require.Equal(t, b+"@x", g.bars[g.order[0]].key, "held across a refresh")
}
