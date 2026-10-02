package tui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

// showCall builds the show page a set of keyword arguments describes, the
// way the command and a flow reach it.
func showCall(t *testing.T, repo *cache.RepoCache, kwargs string) (*showPage, error) {
	t.Helper()

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))
	call, err := view.Parse(view.KindShow, values)
	require.NoError(t, err)

	page, err := newShowView(repo, call)
	if err != nil {
		return nil, err
	}
	page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return page, nil
}

// TestShowChildrenByTypeAndRelation: an entry names the child's type and
// the relation on it that holds the shown issue's id, and each child is a
// row of the table after the fields, a link, with the fields it names
// after its title.
func TestShowChildrenByTypeAndRelation(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "task one", "parent": story, "status": "in-progress"})
	two := newTyped(t, repo, "task", map[string]any{"title": "task two", "parent": story})
	bug := newTyped(t, repo, "subtask", map[string]any{"title": "a subtask", "parent": story})
	elsewhere := newTyped(t, repo, "task", map[string]any{"title": "not ours"})

	page, err := showCall(t, repo, `{"id":"`+story+`","children":[{"type":"task","relation":"parent","fields":["status"]}]}`)
	require.NoError(t, err)
	drawn := plainView(page)

	require.Contains(t, drawn, "children · task", "the heading is the inverse, and the type named")
	require.Contains(t, rowOf(page, one), "task one · in-progress")
	require.Contains(t, rowOf(page, two), "task two")
	require.Equal(t, "", rowOf(page, bug), "a subtask is not a task")
	require.Equal(t, "", rowOf(page, elsewhere))
	require.Less(t, indexOf(drawn, "rank"), indexOf(drawn, "children · task"), "the sections follow the fields")
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, two[:idWidth]), "the store's order")
	require.Contains(t, page.call.Args, "children", "the call line is the call")

	// the cursor walks onto a child and enter follows it
	var at int
	for at = range page.rows {
		if page.rows[at].link == one {
			break
		}
	}
	page.row = at
	page.focusStop(stopFields)
	require.Equal(t, "", page.field(), "a child row is not a field")
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	pushed, ok := cmd().(pushMsg)
	require.True(t, ok)
	require.Equal(t, one, pushed.page.(*showPage).id)

	// and space rings: there is nothing on it to edit
	_, cmd = page.Update(press("space"))
	require.NotNil(t, cmd, "the bell")
	require.Nil(t, page.editor)

	// live: a new child shows on the next load, as the watcher's refresh
	three := newTyped(t, repo, "task", map[string]any{"title": "task three", "parent": story})
	page.Update(refreshMsg{})
	require.Contains(t, rowOf(page, three), "task three")
}

// TestShowChildrenByInverse: the relation may be the name the schema gives
// the other side, as `expand` reads it, and a type left out means every
// type whose relation matches.
func TestShowChildrenByInverse(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "a task", "parent": story})
	bug := newTyped(t, repo, "subtask", map[string]any{"title": "a subtask", "parent": story})
	gone := newTyped(t, repo, "task", map[string]any{"title": "archived", "parent": story})
	_, err := host.IssueSet(repo, gone, map[string]issue.Value{"archived": issue.MustValue(true)}, false)
	require.NoError(t, err)

	page, err := showCall(t, repo, `{"id":"`+story+`","children":[{"relation":"children"},{"relation":"blocked_by"}]}`)
	require.NoError(t, err)
	drawn := plainView(page)

	require.Contains(t, rowOf(page, task), "children")
	require.Contains(t, drawn, bug[:idWidth])
	require.Equal(t, "", rowOf(page, gone), "the archived are left out, as a list leaves them")
	require.Contains(t, drawn, "blocked_by")
	require.Contains(t, drawn, "(none)", "an empty section says so")

	// enter on the empty section rings, and so does space: it is not a
	// field to edit
	for at := range page.rows {
		if page.rows[at].label == "(none)" {
			page.row = at
		}
	}
	page.focusStop(stopFields)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "the bell")
	_, cmd = page.Update(press("space"))
	require.NotNil(t, cmd, "the bell")
	require.Nil(t, page.editor)
}

// TestShowChildrenRefusesWhatTheSchemaDoesNotHave: an unknown type, a
// relation no type has, and a field that is not a relation are errors that
// name what exists.
func TestShowChildrenRefusesWhatTheSchemaDoesNotHave(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	_, err := showCall(t, repo, `{"id":"`+story+`","children":[{"type":"chore","relation":"parent"}]}`)
	require.ErrorContains(t, err, "chore")
	require.ErrorContains(t, err, "the types are")

	_, err = showCall(t, repo, `{"id":"`+story+`","children":[{"type":"task","relation":"parrent"}]}`)
	require.ErrorContains(t, err, "task/parent (inverse children)")

	_, err = showCall(t, repo, `{"id":"`+story+`","children":[{"type":"task","relation":"status"}]}`)
	require.ErrorContains(t, err, "not a relation")

	_, err = showCall(t, repo, `{"id":"`+story+`","children":[{"type":"task","relation":"parent","fields":["colour"]}]}`)
	require.ErrorContains(t, err, "colour")

	// and host.View refuses it the same way, before anything could draw
	_, err = host.View(t.Context(), repo, nil, view.KindShow, map[string]json.RawMessage{
		"id":       mustJSON(story),
		"children": json.RawMessage(`[{"type":"task","relation":"status"}]`),
	})
	require.ErrorContains(t, err, "view show: children")
	require.ErrorContains(t, err, "not a relation")
}
