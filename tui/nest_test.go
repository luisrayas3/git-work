package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
)

// TestListNestsChildrenUnderTheirParent: `expand` names the relation whose
// targets nest under a row, and the derived side of a stored relation
// resolves through it: `children` is read off every `parent`. A child the
// query did not match still shows under its parent, indented behind a
// fold marker; z folds it away; up and down stay on the level and tab
// and shift-tab walk between them.
func TestListNestsChildrenUnderTheirParent(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "parent": story})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "parent": story})
	other := newTyped(t, repo, "story", map[string]any{"title": "the other story"})

	page := list(t, repo, `{"expand":"children","query":"map(select(.fields.type == \"story\")) | sort_by(.fields.title)"}`)
	drawn := plainView(page)
	require.Contains(t, drawn, "4 issues")
	require.Contains(t, rowOf(page, story), "▾ "+story[:idWidth])
	require.Contains(t, rowOf(page, one), "    "+one[:idWidth], "a child is indented under its parent")
	require.Contains(t, rowOf(page, other), "  "+other[:idWidth], "a leaf has room for a marker and no marker")
	require.Less(t, indexOf(drawn, other[:idWidth]), indexOf(drawn, story[:idWidth]))
	require.Less(t, indexOf(drawn, story[:idWidth]), indexOf(drawn, one[:idWidth]))
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, two[:idWidth]))

	// up and down are between the stories; tab goes into the children
	require.Equal(t, other, page.current().id)
	send(page, "down")
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

	send(page, "z")
	drawn = plainView(page)
	require.Contains(t, drawn, "2 issues")
	require.Contains(t, rowOf(page, story), "▸ ")
	require.Equal(t, "", rowOf(page, one))
	send(page, "z")
	require.NotEqual(t, "", rowOf(page, one))
}

// TestListAMatchedChildShowsOnce: a matched issue that is another matched
// issue's child shows under it and not again as a root; a cycle is cut at
// the repeat; and `depth` is how far the tree goes.
func TestListAMatchedChildShowsOnce(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story})
	sub := newTyped(t, repo, "subtask", map[string]any{"title": "the subtask", "parent": task})

	page := list(t, repo, `{"expand":"children","query":"sort_by(.fields.title)"}`)
	drawn := plainView(page)
	require.Equal(t, 1, strings.Count(drawn, task[:idWidth]), "under its story, not again as a root")
	require.Equal(t, 1, strings.Count(drawn, sub[:idWidth]))
	require.Contains(t, drawn, "3 issues")
	// the task is a level under the story, and the subtask, past the
	// depth, is a root of its own
	require.Contains(t, rowOf(page, task), "    "+task[:idWidth])
	require.Contains(t, rowOf(page, sub), "  "+sub[:idWidth])

	page = list(t, repo, `{"expand":"children","depth":0,"query":"sort_by(.fields.title)"}`)
	require.Contains(t, rowOf(page, sub), "      "+sub[:idWidth], "unlimited: the subtask two levels down")

	// a cycle: each blocks the other
	a := newTyped(t, repo, "task", map[string]any{"title": "a"})
	b := newTyped(t, repo, "task", map[string]any{"title": "b", "blocks": []any{a}})
	_, err := host.IssueSet(repo, a, map[string]issue.Value{"blocks": issue.MustValue([]any{b})}, false)
	require.NoError(t, err)
	page = list(t, repo, `{"expand":"blocks","depth":0,"query":"map(select(.fields.title == \"a\" or .fields.title == \"b\")) | sort_by(.fields.title)"}`)
	drawn = plainView(page)
	require.Contains(t, drawn, "2 issues")
	require.Equal(t, 1, strings.Count(drawn, a[:idWidth]))
	require.Equal(t, 1, strings.Count(drawn, b[:idWidth]))
	require.Contains(t, rowOf(page, b), "    "+b[:idWidth], "b under a, and a not again under b")
}

// TestListNestedGrabMovesAmongSiblings: with a rank bound a grabbed row
// moves among its siblings only, its own subtree with it, and the drop
// writes a key between the siblings' ranks.
func TestListNestedGrabMovesAmongSiblings(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "rank": "m"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "parent": story, "rank": "a"})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "parent": story, "rank": "b"})
	other := newTyped(t, repo, "story", map[string]any{"title": "the other story", "rank": "z"})

	page := list(t, repo, `{"expand":"children","rank":"rank","query":"map(select(.fields.type == \"story\"))"}`)
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
