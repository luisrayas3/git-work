package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

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
	require.Contains(t, rowOf(page, one), one[:idWidth]+"   ", "a child is indented under its parent")
	require.Contains(t, rowOf(page, other), other[:idWidth]+"   ", "a leaf draws its indent and no arrow")
	require.Less(t, indexOf(drawn, other[:idWidth]), indexOf(drawn, story[:idWidth]))
	require.Less(t, indexOf(drawn, story[:idWidth]), indexOf(drawn, one[:idWidth]))
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, two[:idWidth]))

	// up and down are between the stories; tab goes into the children
	send(page, "left")
	require.Equal(t, story, page.current().id)
	send(page, "down")
	require.Equal(t, story, page.current().id, "the last story: nothing at this level below")
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
	require.Contains(t, rowOf(page, sub), sub[:idWidth]+"     ", "two levels down")

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
	require.Contains(t, rowOf(page, task), task[:idWidth]+"   ")
	require.Contains(t, rowOf(page, sub), sub[:idWidth]+" ")

	page = list(t, repo, `{"expand":{"relation":"children","expand":0},"query":"sort_by(.fields.title)"}`)
	send(page, "shift+z")
	require.Contains(t, rowOf(page, sub), sub[:idWidth]+"     ", "0: the subtask two levels down")

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
	require.Contains(t, rowOf(page, b), b[:idWidth]+"   ", "b under a, and a not again under b")
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
	require.Contains(t, rowOf(page, b), b[:idWidth]+"   ")
	require.Contains(t, rowOf(page, c), c[:idWidth]+"     ")
	require.Equal(t, "", rowOf(page, d), "past the count")

	page = list(t, repo, `{"expand":{"relation":"blocks","expand":0},"query":"map(select(.fields.title == \"a\"))"}`)
	send(page, "shift+z")
	require.Contains(t, plainView(page), "4 issues")
	require.Contains(t, rowOf(page, d), d[:idWidth]+"       ", "0: every level")
}

// TestListLayerQueryAndFields: a layer carries the list's own arguments for
// its own rows — a `query` over that row's children, and `fields` of its
// own, which the header describes as the cursor moves between the levels.
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

	// the header is the cursor's own layer's, and changes with the level
	header := func(p *listPage) string {
		return strings.Split(plainView(p), "\n")[1]
	}
	require.NotContains(t, header(page), "status")
	send(page, "tab")
	require.Equal(t, open, page.current().id)
	require.Contains(t, header(page), "status", "the child's layer draws its own columns")
	send(page, "shift+tab")
	require.NotContains(t, header(page), "status")
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
