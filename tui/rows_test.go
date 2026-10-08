package tui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/view"
)

// Rows a query makes: a key that is not the id, a row with no id, and the
// children a row lists (doc/design/query-rows.md).

// call parses a view call built as a Go value, which spares a test the
// quoting of a jq program inside a JSON string inside a Go string.
func call(t *testing.T, kind string, kwargs map[string]any) *view.Call {
	t.Helper()
	values := map[string]json.RawMessage{}
	for key, value := range kwargs {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		values[key] = raw
	}
	c, err := view.Parse(kind, values)
	require.NoError(t, err)
	return c
}

// listCall builds a list page from a Go value of its arguments.
func listCall(t *testing.T, repo *cache.RepoCache, kwargs map[string]any) *listPage {
	t.Helper()
	page, err := newListPage(repo, call(t, view.KindList, kwargs))
	require.NoError(t, err)
	page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return page
}

// cursorTo puts the cursor on the row with that key, on the id column.
func cursorTo(t *testing.T, p *listPage, key string) {
	t.Helper()
	for at, index := range p.order {
		if p.rows[index].key == key {
			p.cursor, p.column = at, 0
			return
		}
	}
	t.Fatalf("no row %s drawn", key)
}

// nodeOf is the tree row with that key.
func nodeOf(t *testing.T, p *listPage, key string) treeRow {
	t.Helper()
	for _, node := range p.nodes {
		if node.key == key {
			return node
		}
	}
	t.Fatalf("no row %s in the tree", key)
	return treeRow{}
}

// pushed is the issue a command opened in show, "" when it opened nothing.
func pushed(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	msg, ok := cmd().(pushMsg)
	if !ok {
		return ""
	}
	shown, ok := msg.page.(*showPage)
	if !ok {
		return ""
	}
	return shown.id
}

// TestAKeyDrawsAnIssueTwice: a key is a row's identity on the screen and the
// id is the issue it acts on (R1), so one issue keyed twice is two rows,
// each of which opens, copies and edits the one issue, and the cursor comes
// back to the row it was on, not to the first row of that issue.
func TestAKeyDrawsAnIssueTwice(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "the epic", "status": "to-do"})

	page := listCall(t, repo, map[string]any{
		"fields": []string{"title", "status"},
		"query": `map(. + {key: (.id + "@ada")})
			+ map(. + {key: (.id + "@grace"), fields: (.fields + {title: "the epic again"})})`,
	})
	drawn := plainView(page)
	require.Contains(t, drawn, "the epic")
	require.Contains(t, drawn, "the epic again", "the cells are the row as the query shaped it")
	require.Contains(t, drawn, "2 issues")

	cursorTo(t, page, id+"@grace")
	_, cmd := page.Update(press("enter"))
	require.Equal(t, id, pushed(cmd), "enter opens the issue")

	page.Update(press("ctrl+c"))
	require.Equal(t, "copied "+id[:7], page.status, "copy copies the id")

	page = send(page, "right", "right", "space").(*listPage)
	require.NotNil(t, page.editor, "space edits the issue's field")
	require.Equal(t, id, page.editor.issueId)
	page = send(page, "esc").(*listPage)

	// a refresh puts the cursor back on the row, by key
	page.Update(refreshMsg{})
	require.Equal(t, id+"@grace", page.currentKey())
}

// TestARepeatedKeyIsRefused: keys are unique among a view's rows, and none
// starts with the ghost's prefix; the refusal names the key (R1).
func TestARepeatedKeyIsRefused(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	for query, refusal := range map[string]string{
		`map(. + {key: "x"}) + map(. + {key: "x"})`: "key x is given twice",
		`map(. + {key: "+x"})`:                      "key +x starts with +, which is the ghost's",
	} {
		_, err := newListPage(repo, call(t, view.KindList, map[string]any{"query": query}))
		require.EqualError(t, err, refusal)
		_, err = newGanttPage(repo, call(t, view.KindGantt, map[string]any{"query": query, "start": "due", "stop": "due"}))
		require.EqualError(t, err, refusal)
	}
}

// TestARowWithNoIdStandsForNothing: a row with a key and no id draws its
// fields and is a row the cursor stands on, but enter and space on a cell
// ring and copy copies nothing; space on its id grabs it (R2).
func TestARowWithNoIdStandsForNothing(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := listCall(t, repo, map[string]any{
		"fields": []string{"title", "status"},
		"query":  `. + [{key: "none", fields: {title: "(none)"}}]`,
	})
	require.Contains(t, plainView(page), "(none)")
	cursorTo(t, page, "none")
	require.Equal(t, "", page.currentId())
	require.Contains(t, lastLine(plainView(page)), "space: grab")
	require.NotContains(t, lastLine(plainView(page)), "enter: open")

	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "enter rings")
	require.Equal(t, "", pushed(cmd))

	page.status = ""
	_, cmd = page.Update(press("ctrl+c"))
	require.NotNil(t, cmd, "copy rings")
	require.Equal(t, "", page.status, "and copies nothing")

	page = send(page, "right", "space").(*listPage)
	require.Nil(t, page.editor, "space on a cell rings")

	page = send(page, "left", "space").(*listPage)
	require.GreaterOrEqual(t, page.grabbed, 0, "space on the id grabs it")
	page = send(page, "esc").(*listPage)

	// the gantt draws it too, and opens nothing on it
	g, err := newGanttPage(repo, call(t, view.KindGantt, map[string]any{
		"query": `. + [{key: "none", fields: {title: "(none)"}}]`, "start": "due", "stop": "due",
	}))
	require.NoError(t, err)
	for at, index := range g.order {
		if g.bars[index].key == "none" {
			g.cursor = at
		}
	}
	require.Equal(t, "none", g.currentKey())
	_, cmd = g.Update(press("enter"))
	require.Equal(t, "", pushed(cmd))
}

// TestBoardAndMatrixDrawIssuesOnly: neither nests and the matrix sums, so
// both ignore key and children, drop a row with no id, and draw an issue
// two rows stand for once.
func TestBoardAndMatrixDrawIssuesOnly(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	query := `map(. + {key: (.id + "@1")}) + map(. + {key: (.id + "@2")}) + [{key: "none", fields: {title: "(none)", status: "to-do"}}]`

	b, err := newBoardPage(repo, call(t, view.KindBoard, map[string]any{"query": query, "columns": "status"}))
	require.NoError(t, err)
	require.Equal(t, 1, b.real)

	m, err := newMatrixPage(repo, call(t, view.KindMatrix, map[string]any{"query": query, "rows": "status", "columns": "type"}))
	require.NoError(t, err)
	require.Equal(t, 1, m.count)
}

// TestAListedChildIsTheRowsOwn: a row's children replace what the layer's
// relation reads off the store for it, so one epic drawn twice has two sets
// of children, and the layer below still applies its query and fields; a
// layer with no relation is fine where every row above lists its children
// (R3).
func TestAListedChildIsTheRowsOwn(t *testing.T) {
	repo := testRepo(t)
	ada := jiraUser(t, repo, "Ada Lovelace")
	grace := jiraUser(t, repo, "Grace Hopper")
	epic := newTyped(t, repo, "epic", map[string]any{"title": "Big epic"})
	hers := newTyped(t, repo, "story", map[string]any{"title": "Ada's story", "parent": epic, "assignee": ada, "status": "to-do"})
	done := newTyped(t, repo, "story", map[string]any{"title": "Ada's done story", "parent": epic, "assignee": ada, "status": "done"})
	his := newTyped(t, repo, "story", map[string]any{"title": "Grace's story", "parent": epic, "assignee": grace, "status": "to-do"})

	// one row per (person, epic): each lists that person's stories in it
	query := `. as $all
		| [$all[] | select(.fields.type == "story") | .fields.assignee] | unique
		| map(. as $who | $all[] | select(.fields.type == "epic") | . as $epic
			| $epic + {key: ($epic.id + "@" + $who),
			           children: [$all[] | select(.fields.parent == $epic.id and .fields.assignee == $who) | .id]})`

	page := listCall(t, repo, map[string]any{
		"query":  query,
		"fields": []string{"title"},
		"expand": map[string]any{"fields": []string{"status", "title"}, "query": `map(select(.fields.status != "done"))`},
	})
	require.Empty(t, page.status)
	page = send(page, "Z").(*listPage)

	require.Equal(t, 1, nodeOf(t, page, epic+"@"+ada).children, "the layer's query narrows the listed children")
	require.Equal(t, 1, nodeOf(t, page, epic+"@"+grace).children)
	require.Equal(t, epic+"@"+ada, nodeOf(t, page, hers).parent)
	require.Equal(t, epic+"@"+grace, nodeOf(t, page, his).parent)
	for _, node := range page.nodes {
		require.NotEqual(t, done, node.key, "the done story is narrowed away")
	}
	require.Contains(t, tableHeaderAbove(page, hers), "status", "the layer's fields")

	// a row that lists none, under a layer that names no relation, has
	// none, and the status line says why
	page = listCall(t, repo, map[string]any{
		"query":  `map(select(.fields.type == "epic"))`,
		"expand": map[string]any{"fields": []string{"title"}},
	})
	require.Equal(t, 0, nodeOf(t, page, epic).children)
	require.Contains(t, page.status, "names no relation")

	// a layer with a relation reads the store for a row that lists nothing,
	// and the listed children of one that does
	page = listCall(t, repo, map[string]any{
		"query":  `map(select(.fields.type == "epic")) | map(. , . + {key: "listed", children: [{key: "inline", fields: {title: "an inline row"}}]})`,
		"expand": "children",
	})
	require.Equal(t, 3, nodeOf(t, page, epic).children)
	require.Equal(t, 1, nodeOf(t, page, "listed").children)
	require.Equal(t, "listed", nodeOf(t, page, "inline").parent)
	require.Equal(t, "", nodeOf(t, page, "inline").id)

	// a key shows once: a story listed under two rows is under the first
	page = listCall(t, repo, map[string]any{
		"query":  `map(select(.fields.type == "epic")) | map(. + {key: "a", children: ["` + hers + `"]}, . + {key: "b", children: ["` + hers + `"]})`,
		"expand": map[string]any{},
	})
	require.Equal(t, "a", nodeOf(t, page, hers).parent)
	require.Equal(t, 0, nodeOf(t, page, "b").children)
}
