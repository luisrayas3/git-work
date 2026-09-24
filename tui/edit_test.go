package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/rank"
)

// fieldOf reads a field back out of the store, which is the only place a test
// should look: the page may say anything, the store is what was written.
func fieldOf(t *testing.T, repo *cache.RepoCache, id, key string) string {
	t.Helper()

	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)

	raw, ok := document.Fields[key]
	if !ok {
		return ""
	}
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	return plainValue(value)
}

// TestEditAnEnumPicksFromTheSchema is the whole point of taking the widget
// from the schema: the choices are the field's values, and nothing else is.
func TestEditAnEnumPicksFromTheSchema(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := list(t, repo, `{"fields":["title","status"]}`)

	// the column cursor moves across the field columns, and `e` edits the one
	// it is on
	page = send(page, "l", "e").(*listPage)
	require.NotNil(t, page.editor)

	drawn := plainView(page)
	require.Contains(t, drawn, "In Progress")
	require.Contains(t, drawn, "in-progress")
	require.Contains(t, drawn, "Canceled")
	// the value the issue has is where the picker opens
	require.Equal(t, "to-do", page.editor.picker.items[page.editor.picker.cursor].value)

	page = send(page, "j", "enter").(*listPage)
	require.Nil(t, page.editor)
	require.Equal(t, "in-progress", fieldOf(t, repo, id, "status"))
	require.Contains(t, plainView(page), "status set on")
}

func TestEditCanBeCancelled(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := list(t, repo, `{"fields":["title","status"]}`)
	page = send(page, "l", "e", "j", "esc").(*listPage)

	require.Nil(t, page.editor)
	require.Equal(t, "to-do", fieldOf(t, repo, id, "status"))
}

// TestEditATextFieldWritesIt covers the other widget, and the fact that the
// title is a field like any other (f4bac00).
func TestEditATextFieldWritesIt(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, `{"fields":["title"]}`)
	page = send(page, "e").(*listPage)
	require.NotNil(t, page.editor)

	page.editor.input.SetValue("a better title")
	page = send(page, "enter").(*listPage)

	require.Equal(t, "a better title", fieldOf(t, repo, id, "title"))
}

// TestASchemaRefusalIsAStatusLine is the contract between the renderer and
// the check: the store says no, the page says so, and nothing changed.
func TestASchemaRefusalIsAStatusLine(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "estimate": 3})

	page := list(t, repo, `{"fields":["estimate"]}`)
	page = send(page, "e").(*listPage)
	require.NotNil(t, page.editor)

	page.editor.input.SetValue("three")
	page = send(page, "enter").(*listPage)

	require.Contains(t, plainView(page), "is a number")
	require.Equal(t, "3", fieldOf(t, repo, id, "estimate"))
}

// TestASetValuedFieldSaysWhereToEditIt is the honest refusal: a picker that
// replaced a whole set would be a worse answer than the command that does not.
func TestASetValuedFieldSaysWhereToEditIt(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, `{"fields":["labels"]}`)
	page = send(page, "e").(*listPage)

	require.Nil(t, page.editor)
	require.Contains(t, plainView(page), "git work issue add/remove")
}

func TestCommentWritesOne(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	page = send(page, "c").(*listPage)
	require.NotNil(t, page.comment)

	page.comment.area.SetValue("a thought")
	page = send(page, "ctrl+s").(*listPage)

	require.Nil(t, page.comment)
	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, document.Comments, 2, "the body, and the one just written")
	require.Equal(t, "a thought", document.Comments[1].Message)
}

// TestGrabNeedsARank says why: without a rank field there is nowhere to write
// the new order to, so the drag would be a change that vanishes on reload.
func TestGrabNeedsARank(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	page = send(page, "space").(*listPage)

	require.Equal(t, -1, page.grabbed)
	require.Contains(t, plainView(page), "no rank bound")
}

// TestGrabAndDropWritesARankBetweenTheNeighbours is the drag: one key, one
// operation, on one issue (441dcbb).
func TestGrabAndDropWritesARankBetweenTheNeighbours(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "first", "rank": "a"})
	second := newIssue(t, repo, map[string]any{"title": "second", "rank": "b"})
	third := newIssue(t, repo, map[string]any{"title": "third", "rank": "c"})

	page := list(t, repo, `{"rank":"rank","fields":["title","rank"]}`)
	// a bound rank orders the rows, whatever the query's own order was
	require.Equal(t, first, page.currentId())

	page = send(page, "space").(*listPage)
	require.GreaterOrEqual(t, page.grabbed, 0)
	require.Contains(t, plainView(page), "first")

	page = send(page, "j", "space").(*listPage)
	require.Equal(t, -1, page.grabbed)

	// it landed between its new neighbours, and only it moved
	moved := fieldOf(t, repo, first, "rank")
	expected, err := rank.Between("b", "c")
	require.NoError(t, err)
	require.Equal(t, expected, moved)
	require.Greater(t, moved, "b")
	require.Less(t, moved, "c")
	require.Equal(t, "b", fieldOf(t, repo, second, "rank"))
	require.Equal(t, "c", fieldOf(t, repo, third, "rank"))

	// and the order it is drawn in is the order it was dropped in
	require.Equal(t, []string{second, first, third}, drawnIds(page))
}

// TestGrabCanBePutBack: esc is the way out of a drag nobody meant to start.
func TestGrabCanBePutBack(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "first", "rank": "a"})
	newIssue(t, repo, map[string]any{"title": "second", "rank": "b"})

	page := list(t, repo, `{"rank":"rank"}`)
	page = send(page, "space", "j", "esc").(*listPage)

	require.Equal(t, -1, page.grabbed)
	require.Equal(t, "a", fieldOf(t, repo, first, "rank"))
}

func drawnIds(p *listPage) []string {
	ids := make([]string, 0, len(p.order))
	for _, at := range p.order {
		ids = append(ids, p.rows[at].id)
	}
	return ids
}
