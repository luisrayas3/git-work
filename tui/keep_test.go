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
		"query": `sort_by(.fields.title) | map(. + {key: (.id + "@x")}) + [{key: "none", fields: {title: "(none)", type: "task"}}]`,
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

// TestALevelIsAllKeyedRowsOrNone: a keyed row orders for this view only
// and a plain one by its rank, so a level of both is refused, naming a key
// and an id from it — the roots across their groups, and a row's listed
// children; on a refresh the refusal is the status line. A row keyed by its
// own id is plain (R2, settled 2026-10-08).
func TestALevelIsAllKeyedRowsOrNone(t *testing.T) {
	repo := testRepo(t)
	x := newIssue(t, repo, map[string]any{"title": "x", "status": "to-do"})
	y := newIssue(t, repo, map[string]any{"title": "y", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "z", "status": "in-progress"})

	// the roots, across their groups
	mixed := `sort_by(.fields.title) | map(if .fields.title == "z" then . + {key: "z@x"} else . end)`
	_, err := newListPage(repo, call(t, view.KindList, map[string]any{"query": mixed, "group_by": "status"}))
	require.ErrorContains(t, err, "the roots mix keyed and plain rows, key z@x and id "+x)
	_, err = newGanttPage(repo, call(t, view.KindGantt, map[string]any{"query": mixed, "start": "due", "stop": "due"}))
	require.ErrorContains(t, err, "key z@x")

	// a listed children array
	listed := `map(select(.fields.title == "x")) | map(. + {key: "parent", children: ["` + y + `", {key: "inline", fields: {title: "an inline row", type: "task"}}]})`
	_, err = newListPage(repo, call(t, view.KindList, map[string]any{"query": listed, "expand": map[string]any{}}))
	require.ErrorContains(t, err, "the rows under parent mix keyed and plain rows, key inline and id "+y)

	// a row keyed by its own id is plain, and drags writing its rank
	page := listCall(t, repo, map[string]any{"query": `sort_by(.fields.title) | map(. + {key: .id})`})
	cursorTo(t, page, y)
	page = send(page, "space", "up", "space").(*listPage)
	require.True(t, strings.HasPrefix(page.status, "rank set"), page.status)
	require.NotEqual(t, "", fieldOf(t, repo, y, "rank"))

	// a refresh that makes a level mixed says so on the status line
	page = listCall(t, repo, map[string]any{"query": `map(if .fields.title == "w" then . else . + {key: (.id + "@x")} end)`})
	require.Empty(t, page.status)
	newIssue(t, repo, map[string]any{"title": "w"})
	page.Update(refreshMsg{})
	require.Contains(t, page.status, "mix keyed and plain rows")
}

// TestARowWithNoIdNamesItsType: a row that stands for no issue has no issue
// to read a type off, so it names one the schema knows, or it is refused,
// naming its key (R2, settled 2026-10-08).
func TestARowWithNoIdNamesItsType(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	_, err := newListPage(repo, call(t, view.KindList, map[string]any{
		"query": `map(. + {key: (.id + "@x")}) + [{key: "none", fields: {title: "(none)"}}]`,
	}))
	require.ErrorContains(t, err, "row none has no id and no type")

	_, err = newListPage(repo, call(t, view.KindList, map[string]any{
		"query": `map(. + {key: (.id + "@x")}) + [{key: "none", fields: {title: "(none)", type: "nonsense"}}]`,
	}))
	require.ErrorContains(t, err, "row none has no id and type nonsense, which the schema does not know")
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
