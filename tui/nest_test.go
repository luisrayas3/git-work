package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

// colOf is the column a text starts in on a drawn line, -1 when it is not
// there: columns, not bytes, because the marker and the arrows are one
// column of several bytes.
func colOf(line, text string) int {
	at := strings.Index(line, text)
	if at < 0 {
		return -1
	}
	return utf8.RuneCountInString(line[:at])
}

// startOf is the column a row's id is drawn in, -1 when the row is not
// drawn: the table's indent plus the cursor marker, so a root's id is at 1
// and a child's at 1 + indentWidth per level.
func startOf(p page, id string) int {
	return colOf(rowOf(p, id), id[:idWidth])
}

// tableHeaderAbove is the header line of the table a drawn row is in: the
// nearest line above it that starts, at the row's indent, with "id".
func tableHeaderAbove(p page, id string) string {
	lines := strings.Split(plainView(p), "\n")
	indent := startOf(p, id)
	for at := len(lines) - 1; at >= 0; at-- {
		if strings.Contains(lines[at], id[:idWidth]) {
			for above := at - 1; above >= 0; above-- {
				if strings.HasPrefix(lines[above], strings.Repeat(" ", indent)+"id ") {
					return lines[above]
				}
			}
		}
	}
	return ""
}

// kwargs reads a call's arguments, for the checks that go through view.Parse
// rather than through a page.
func kwargs(t *testing.T, text string) map[string]json.RawMessage {
	t.Helper()

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(text), &values))
	return values
}

// TestListNestsChildrenUnderTheirParent: `expand` names the relation whose
// targets nest under a row, and the derived side of a stored relation
// resolves through it: `children` is read off every `parent`. The tree
// opens folded, the arrow cell after the id says how many rows are hidden,
// space there unfolds, and up and down stay on the level while tab and
// shift-tab walk between them.
func TestListNestsChildrenUnderTheirParent(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "parent": story})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "parent": story})
	other := newTyped(t, repo, "story", map[string]any{"title": "the other story"})

	page := list(t, repo, `{"expand":"children","query":"map(select(.fields.type == \"story\")) | sort_by(.fields.title)"}`)
	drawn := plainView(page)
	require.Contains(t, drawn, "2 issues", "it opens folded: the roots are the list")
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▸ 2", "the arrow follows the id, with the count")
	require.Equal(t, "", rowOf(page, one))

	// space on the arrow cell unfolds, and the cursor gets there with →
	require.Equal(t, other, page.current().id)
	send(page, "down", "right", "space")
	drawn = plainView(page)
	require.Contains(t, drawn, "4 issues")
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▾")
	require.Equal(t, 1, startOf(page, story), "a root's table is flush")
	require.Equal(t, 1+indentWidth, startOf(page, one), "the child table is indented as a unit")
	require.Equal(t, 1+indentWidth, startOf(page, two))
	require.Contains(t, rowOf(page, other), other[:idWidth]+"   ", "a leaf's tree cell is empty")
	require.Less(t, indexOf(drawn, other[:idWidth]), indexOf(drawn, story[:idWidth]))
	require.Less(t, indexOf(drawn, story[:idWidth]), indexOf(drawn, one[:idWidth]))
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, two[:idWidth]))

	// up and down are between the stories; tab goes into the children
	send(page, "left")
	require.Equal(t, story, page.current().id)
	send(page, "down")
	require.True(t, page.node().ghost, "the last story: below it at this level is the ghost (ghost.go)")
	send(page, "up")
	require.Equal(t, story, page.current().id)
	send(page, "tab")
	require.Equal(t, one, page.current().id)
	send(page, "down")
	require.Equal(t, two, page.current().id)
	send(page, "up", "up")
	require.Equal(t, one, page.current().id, "the level's first")
	send(page, "shift+tab")
	require.Equal(t, story, page.current().id)

	// space on the arrow folds it again; on a leaf's tree cell it rings
	send(page, "right", "space")
	require.Contains(t, plainView(page), "2 issues")
	require.Contains(t, rowOf(page, story), "▸ 2")
	require.Equal(t, "", rowOf(page, one))
	send(page, "up", "space")
	require.Equal(t, "nothing to fold", page.status)
}

// TestListFoldsEveryRowAtOnce: Z folds every parent, and again where none
// is open, opens every one of them; enter on the arrow opens the row, as it
// does on the id.
func TestListFoldsEveryRowAtOnce(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story})
	sub := newTyped(t, repo, "subtask", map[string]any{"title": "the subtask", "parent": task})

	page := list(t, repo, `{"expand":{"relation":"children","expand":0},"query":"map(select(.fields.type == \"story\"))"}`)
	require.Contains(t, plainView(page), "1 issue")

	send(page, "shift+z")
	drawn := plainView(page)
	require.Contains(t, drawn, "3 issues", "every parent open, however deep")
	require.Equal(t, 1+2*indentWidth, startOf(page, sub), "two levels down")

	send(page, "shift+z")
	require.Contains(t, plainView(page), "1 issue")

	// enter on the arrow cell opens the row, the way it does on the id
	_, cmd := send(page, "right").(*listPage).act()
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
}

// TestListAMatchedChildShowsOnce: a matched issue that is another matched
// issue's child shows under it and not again as a root; a cycle is cut at
// the repeat; and a layer with no `expand` of its own is the last one.
func TestListAMatchedChildShowsOnce(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story})
	sub := newTyped(t, repo, "subtask", map[string]any{"title": "the subtask", "parent": task})

	page := list(t, repo, `{"expand":"children","query":"sort_by(.fields.title)"}`)
	send(page, "shift+z")
	drawn := plainView(page)
	require.Equal(t, 1, strings.Count(drawn, task[:idWidth]), "under its story, not again as a root")
	require.Equal(t, 1, strings.Count(drawn, sub[:idWidth]))
	require.Contains(t, drawn, "3 issues")
	// the task is a level under the story, and the subtask, past the one
	// layer the spec describes, is a root of its own
	require.Equal(t, 1+indentWidth, startOf(page, task))
	require.Equal(t, 1, startOf(page, sub))

	page = list(t, repo, `{"expand":{"relation":"children","expand":0},"query":"sort_by(.fields.title)"}`)
	send(page, "shift+z")
	require.Equal(t, 1+2*indentWidth, startOf(page, sub), "0: the subtask two levels down")

	// a cycle: each blocks the other
	a := newTyped(t, repo, "task", map[string]any{"title": "a"})
	b := newTyped(t, repo, "task", map[string]any{"title": "b", "blocks": []any{a}})
	_, err := host.IssueSet(repo, a, map[string]issue.Value{"blocks": issue.MustValue([]any{b})}, false)
	require.NoError(t, err)
	page = list(t, repo, `{"expand":{"relation":"blocks","expand":0},"query":"map(select(.fields.title == \"a\" or .fields.title == \"b\")) | sort_by(.fields.title)"}`)
	send(page, "shift+z")
	drawn = plainView(page)
	require.Contains(t, drawn, "2 issues")
	require.Equal(t, 1, strings.Count(drawn, a[:idWidth]))
	require.Equal(t, 1, strings.Count(drawn, b[:idWidth]))
	require.Equal(t, 1+indentWidth, startOf(page, b), "b under a, and a not again under b")
}

// TestListACountIsThatManyMoreLevels: `"expand": N` on a layer is the same
// layer N more levels down and no further, where 0 is every level.
func TestListACountIsThatManyMoreLevels(t *testing.T) {
	repo := testRepo(t)
	d := newTyped(t, repo, "task", map[string]any{"title": "d"})
	c := newTyped(t, repo, "task", map[string]any{"title": "c", "blocks": []any{d}})
	b := newTyped(t, repo, "task", map[string]any{"title": "b", "blocks": []any{c}})
	newTyped(t, repo, "task", map[string]any{"title": "a", "blocks": []any{b}})

	page := list(t, repo, `{"expand":{"relation":"blocks","expand":1},"query":"map(select(.fields.title == \"a\"))"}`)
	send(page, "shift+z")
	require.Contains(t, plainView(page), "3 issues", "a, b under it, c under b, and the layer stops")
	require.Equal(t, 1+indentWidth, startOf(page, b))
	require.Equal(t, 1+2*indentWidth, startOf(page, c))
	require.Equal(t, "", rowOf(page, d), "past the count")

	page = list(t, repo, `{"expand":{"relation":"blocks","expand":0},"query":"map(select(.fields.title == \"a\"))"}`)
	send(page, "shift+z")
	require.Contains(t, plainView(page), "4 issues")
	require.Equal(t, 1+3*indentWidth, startOf(page, d), "0: every level")
}

// TestListLayerQueryAndFields: a layer carries the list's own arguments for
// its own rows — a `query` over that row's children, and `fields` of its
// own, which the child table's own header describes while the top header
// stays the roots'.
func TestListLayerQueryAndFields(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	open := newTyped(t, repo, "task", map[string]any{"title": "open one", "parent": story, "status": "to-do"})
	done := newTyped(t, repo, "task", map[string]any{"title": "done one", "parent": story, "status": "done"})

	spec := `{"relation":"children","query":"map(select(.fields.status != \"done\"))","fields":["status","title"]}`
	page := list(t, repo, `{"fields":["title"],"expand":`+spec+`,"query":"map(select(.fields.type == \"story\"))"}`)
	require.Contains(t, rowOf(page, story), "▸ 1", "the layer's query narrows the children it counts")

	send(page, "right", "space")
	drawn := plainView(page)
	require.Contains(t, drawn, "2 issues")
	require.NotEqual(t, "", rowOf(page, open))
	require.Equal(t, "", rowOf(page, done), "the layer's query left it out")

	// the top header is the roots' and stays so; the child table has its
	// own, with the layer's columns
	topHeader := func(p *listPage) string {
		return strings.Split(plainView(p), "\n")[1]
	}
	require.NotContains(t, topHeader(page), "status")
	require.Contains(t, tableHeaderAbove(page, open), "status", "the child's layer draws its own columns")
	send(page, "tab")
	require.Equal(t, open, page.current().id)
	require.NotContains(t, topHeader(page), "status", "the top header never follows the cursor")
	require.Equal(t, []string{"status", "title"}, page.cursorFields(), "the cursor walks the child's own cells")
	send(page, "right") // the column was on the tree cell; one step in is the first field
	require.Equal(t, "status", page.fieldKey())
	send(page, "shift+tab")
	require.NotContains(t, topHeader(page), "status")
}

// TestListChildTableHasItsOwnHeader: the rows under every opened parent are
// a table of their own — a header line above the first child at the table's
// indent, the same columns and widths under every parent — and a folded
// parent has no table and no header under it.
func TestListChildTableHasItsOwnHeader(t *testing.T) {
	repo := testRepo(t)
	first := newTyped(t, repo, "story", map[string]any{"title": "first story"})
	a := newTyped(t, repo, "task", map[string]any{"title": "a", "parent": first, "status": "to-do"})
	second := newTyped(t, repo, "story", map[string]any{"title": "second story"})
	b := newTyped(t, repo, "task", map[string]any{"title": "a much longer title", "parent": second, "status": "done"})

	page := list(t, repo, `{"fields":["title"],"expand":{"relation":"children","fields":["status","title"]},`+
		`"query":"map(select(.fields.type == \"story\")) | sort_by(.fields.title)"}`)
	lines := strings.Split(plainView(page), "\n")
	headers := 0
	for _, line := range lines[2:] {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "id ") {
			headers++
		}
	}
	require.Equal(t, 0, headers, "folded: no child table, no header")

	send(page, "shift+z")
	lines = strings.Split(plainView(page), "\n")
	require.True(t, strings.HasPrefix(lines[1], " id "), "the top header is the roots'")
	require.NotContains(t, lines[1], "status")

	under := func(parent string) string { return lines[indexOfLine(lines, parent[:idWidth])+1] }
	for _, parent := range []string{first, second} {
		header := under(parent)
		require.True(t, strings.HasPrefix(header, strings.Repeat(" ", indentWidth)+" id "),
			"a header line right under the parent, at the table's indent: %q", header)
		require.Contains(t, header, "status")
		require.Contains(t, header, "title")
	}
	require.Equal(t, under(first), under(second), "every table of one layer has the same header")

	// the columns sit under the header's cells in both tables
	header := under(first)
	require.Equal(t, colOf(header, "status"), colOf(rowOf(page, a), "to-do"))
	require.Equal(t, colOf(header, "status"), colOf(rowOf(page, b), "done"))
	require.Equal(t, colOf(header, "title"), colOf(rowOf(page, b), "a much"))
}

// TestListUnfoldingMovesNothing: the tree cell and a layer's columns are
// measured over every row, hidden or drawn, so opening a parent — even one
// whose children share its layer and carry a long title — moves no column of
// the rows already on the screen.
func TestListUnfoldingMovesNothing(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "short", "status": "to-do"})
	task := newTyped(t, repo, "task", map[string]any{"title": "a task with a title much longer than its parent's", "parent": story, "status": "in-progress"})
	sub := newTyped(t, repo, "subtask", map[string]any{"title": "deeper", "parent": task})
	for i := 0; i < 10; i++ {
		newTyped(t, repo, "subtask", map[string]any{"title": "s", "parent": task})
	}

	page := list(t, repo, `{"fields":["title","status"],"expand":{"relation":"children","expand":0},"query":"map(select(.fields.type == \"story\"))"}`)
	folded := rowOf(page, story)
	require.Contains(t, folded, "▸ 1")

	send(page, "shift+z")
	opened := rowOf(page, story)
	require.Contains(t, opened, "▾")
	require.Equal(t, colOf(folded, "short"), colOf(opened, "short"), "the title column stayed put")
	require.Equal(t, colOf(folded, "to-do"), colOf(opened, "to-do"), "and the status column")
	require.Equal(t, utf8.RuneCountInString(folded), utf8.RuneCountInString(opened))

	// the eleven-child task's count sized the tree cell before it was drawn
	require.Contains(t, rowOf(page, task), "▾")
	require.Equal(t, colOf(rowOf(page, task), "a task"), colOf(rowOf(page, sub), "deeper")-indentWidth,
		"the subtask's table is one indent further in, its title column with it")

	send(page, "shift+z")
	require.Equal(t, folded, rowOf(page, story), "folded again, drawn as before")
}

// TestListKeepsTheChildTableHeaderOnTop: a window too small for a child
// table still opens on that table's header when its first line is inside
// the table, whether the cursor is deep in it or on its first row, because a
// row read without its header is a row of numbers.
func TestListKeepsTheChildTableHeaderOnTop(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	var tasks []string
	for _, title := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		tasks = append(tasks, newTyped(t, repo, "task", map[string]any{"title": title, "parent": story, "status": "to-do"}))
	}

	page := list(t, repo, `{"fields":["title"],"expand":{"relation":"children","fields":["status","title"],"query":"sort_by(.fields.title)"},`+
		`"query":"map(select(.fields.type == \"story\"))"}`)
	page.Update(tea.WindowSizeMsg{Width: 100, Height: 9})
	// G is the roots' ghost, the last row; the last task is seven down
	send(page, "tab", "down", "down", "down", "down", "down", "down", "down")
	require.Equal(t, tasks[7], page.current().id)

	lines := strings.Split(plainView(page), "\n")
	require.True(t, strings.HasPrefix(lines[2], strings.Repeat(" ", indentWidth)+" id "), "the window opens on the table's header: %q", lines[2])
	require.Contains(t, lines[2], "status")
	require.NotContains(t, plainView(page), tasks[0][:idWidth], "and not on the table's first row")
	require.Contains(t, plainView(page), tasks[7][:idWidth], "the cursor's row is on screen")

	// back up onto the first task, the header is still the line above it
	for page.current().id != tasks[0] {
		send(page, "k")
	}
	lines = strings.Split(plainView(page), "\n")
	require.True(t, strings.HasPrefix(lines[2], strings.Repeat(" ", indentWidth)+" id "), "the table's header is the first body line: %q", lines[2])
	require.Contains(t, lines[3], tasks[0][:idWidth], "the cursor's row is right under it")
}

// indexOfLine is the first line containing a text, -1 when none does.
func indexOfLine(lines []string, text string) int {
	for at, line := range lines {
		if strings.Contains(line, text) {
			return at
		}
	}
	return -1
}

// TestListLayerGroupsItsOwnRows: a layer's `group_by` sections the children
// under one parent, under their own headers at the level's indent, while the
// roots keep the sections the call's own `group_by` gives them.
func TestListLayerGroupsItsOwnRows(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	newTyped(t, repo, "task", map[string]any{"title": "a", "parent": story, "status": "to-do"})
	newTyped(t, repo, "task", map[string]any{"title": "b", "parent": story, "status": "done"})
	newTyped(t, repo, "task", map[string]any{"title": "c", "parent": story, "status": "to-do"})

	page := list(t, repo, `{"fields":["title"],"expand":{"relation":"children","group_by":"status"},"query":"map(select(.fields.type == \"story\"))"}`)
	send(page, "shift+z")
	drawn := plainView(page)

	// the groups come in the order they first appear, each heading its own
	// run of siblings
	require.Less(t, indexOf(drawn, "to-do"), indexOf(drawn, "done"))
	lines := strings.Split(drawn, "\n")
	for at, line := range lines {
		if strings.Contains(line, "to-do") {
			require.Contains(t, lines[at+1], "a")
			require.Contains(t, lines[at+2], "c")
		}
		if strings.Contains(line, "done") {
			require.Contains(t, lines[at+1], "b")
		}
	}
}

// TestListNestedGrabMovesAmongSiblings: a grabbed row moves among its
// siblings only, its own subtree with it, and the drop writes a key between
// the siblings' ranks.
func TestListNestedGrabMovesAmongSiblings(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "rank": "m"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "parent": story, "rank": "a"})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "parent": story, "rank": "b"})
	other := newTyped(t, repo, "story", map[string]any{"title": "the other story", "rank": "z"})

	page := list(t, repo, `{"expand":"children","query":"map(select(.fields.type == \"story\"))"}`)
	require.Equal(t, story, page.current().id)
	send(page, "tab", "down")
	require.Equal(t, two, page.current().id)

	// two cannot leave its parent: down is the end of its siblings
	send(page, "space", "down")
	require.Equal(t, two, page.current().id)
	require.Equal(t, 2, page.cursor)
	send(page, "up")
	require.Equal(t, 1, page.cursor)
	send(page, "enter")
	require.Less(t, field(t, repo, two, "rank"), `"a"`)
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, two[:idWidth]), indexOf(drawn, one[:idWidth]))

	// a story moves with its children
	send(page, "shift+tab", "space", "down")
	require.Equal(t, story, page.current().id)
	require.Equal(t, 1, page.cursor)
	drawn = plainView(page)
	require.Less(t, indexOf(drawn, other[:idWidth]), indexOf(drawn, story[:idWidth]))
	require.Less(t, indexOf(drawn, story[:idWidth]), indexOf(drawn, two[:idWidth]))
	send(page, "enter")
	require.Greater(t, field(t, repo, story, "rank"), `"z"`)
}

// TestExpandIsCheckedAgainstTheSchema: a layer's relation and the keys it
// draws are checked before anything is drawn, as show's `children` are.
func TestExpandIsCheckedAgainstTheSchema(t *testing.T) {
	repo := testRepo(t)
	s, err := repo.LoadSchema()
	require.NoError(t, err)

	call, err := view.Parse(view.KindList, kwargs(t, `{"expand":"nephews"}`))
	require.NoError(t, err)
	require.ErrorContains(t, view.CheckSchema(call, s), "nephews")

	call, err = view.Parse(view.KindList, kwargs(t, `{"expand":{"relation":"children","fields":["nope"]}}`))
	require.NoError(t, err)
	require.ErrorContains(t, view.CheckSchema(call, s), "nope")

	call, err = view.Parse(view.KindList, kwargs(t, `{"expand":{"relation":"children","fields":["status"]}}`))
	require.NoError(t, err)
	require.NoError(t, view.CheckSchema(call, s))
}

// TestArchivedAreOutOfTheInputUnlessAsked: a view's query and a layer's run
// over the unarchived issues, and include_archive brings the archived back,
// on the call and on a layer alike (doc/design/include-archive.md, I1, I5).
func TestArchivedAreOutOfTheInputUnlessAsked(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	live := newTyped(t, repo, "task", map[string]any{"title": "live", "parent": story})
	gone := newTyped(t, repo, "task", map[string]any{"title": "gone", "parent": story})
	_, err := host.IssueSet(repo, gone, map[string]issue.Value{"archived": issue.MustValue(true)}, false)
	require.NoError(t, err)

	// `.` is the whole input, and the input has no archived issue
	page := list(t, repo, `{"query":"."}`)
	require.NotEqual(t, "", rowOf(page, live))
	require.Equal(t, "", rowOf(page, gone))

	page = list(t, repo, `{"query":".","include_archive":true}`)
	require.NotEqual(t, "", rowOf(page, gone))

	// a layer's children are the unarchived, unless the layer says otherwise
	roots := `"query":"map(select(.fields.type == \"story\"))"`
	page = list(t, repo, `{`+roots+`,"expand":"children"}`)
	require.Contains(t, rowOf(page, story), "▸ 1")

	// the call's switch is inherited by a layer that does not name it
	page = list(t, repo, `{`+roots+`,"include_archive":true,"expand":"children"}`)
	require.Contains(t, rowOf(page, story), "▸ 2", "the call's switch reaches the layers")
	send(page, "right", "space")
	require.NotEqual(t, "", rowOf(page, gone))

	// a layer that names it overrides it, either way
	page = list(t, repo, `{`+roots+`,"include_archive":true,"expand":{"relation":"children","include_archive":false}}`)
	require.Contains(t, rowOf(page, story), "▸ 1", "explicitly false under a call that is true")

	page = list(t, repo, `{`+roots+`,"expand":{"relation":"children","include_archive":true}}`)
	require.Contains(t, rowOf(page, story), "▸ 2")
	send(page, "right", "space")
	require.NotEqual(t, "", rowOf(page, gone))
}
