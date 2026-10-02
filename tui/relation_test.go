package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pickerValues are what a picker's choices write, in order.
func pickerValues(p *picker) []string {
	out := make([]string, 0, len(p.items))
	for _, item := range p.items {
		out = append(out, item.value)
	}
	return out
}

// TestSpaceOnARelationOpensItsPicker: space on a relation cell is its
// picker, with no "go to" entry — enter on the cell is that — and the cursor
// on the current value, marked; esc closes it having done nothing.
func TestSpaceOnARelationOpensItsPicker(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page = send(page, "l", "l").(*listPage)
	require.Contains(t, plainView(page), relationHint(true), "the status line says what enter and space do")

	page = send(page, "space").(*listPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	require.Equal(t, []string{story, ""}, pickerValues(picker))
	require.Equal(t, story, picker.items[picker.cursor].value, "opens on the current value")
	require.True(t, picker.items[picker.cursor].current, "the current value is marked")
	drawn := plainView(page)
	require.NotContains(t, drawn, "go to")
	require.Contains(t, drawn, "● current")

	page = send(page, "esc").(*listPage)
	require.Nil(t, page.editor)
	require.Equal(t, story, fieldOf(t, repo, id, "parent"), "nothing written")
}

// TestChangeARelationPicksFromItsTargetTypes: the picker is the issues the
// field may name — its target_types, not the issue itself — as links are
// drawn, ending with (none); `/` narrows them, and enter writes the id.
func TestChangeARelationPicksFromItsTargetTypes(t *testing.T) {
	repo := testRepo(t)
	north := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	south := newIssue(t, repo, map[string]any{"type": "story", "title": "south"})
	initiative := newIssue(t, repo, map[string]any{"type": "initiative", "title": "not a target"})
	sibling := newIssue(t, repo, map[string]any{"title": "sibling"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": north})

	page := show(t, repo, id, []string{"parent"})
	page = send(page, "shift+tab").(*showPage)
	require.Contains(t, plainView(page), relationHint(true))

	page = send(page, "space").(*showPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	values := pickerValues(picker)
	require.ElementsMatch(t, []string{north, south, ""}, values)
	require.Equal(t, "", values[len(values)-1], "(none) is last")
	require.Equal(t, north, picker.items[picker.cursor].value, "on the current value")
	require.NotContains(t, values, initiative)
	require.NotContains(t, values, sibling)
	require.NotContains(t, values, id)
	require.Contains(t, plainView(page), south[:7]+" south")

	page = send(page, "/", "s", "o", "u").(*showPage)
	require.Equal(t, south, picker.items[picker.cursor].value, "the narrowing keeps south alone")
	require.NotContains(t, join(page.editor.View(100)), north[:7]+" north")
	page = send(page, "enter", "enter").(*showPage)
	require.Nil(t, page.editor)
	require.Equal(t, south, fieldOf(t, repo, id, "parent"))
	require.Contains(t, plainView(page), south[:7]+" south")
}

// TestEnterOnARelationGoesToIt, from show and from a list: enter opens the
// issue the cell names at once, with no picker and nothing written.
func TestEnterOnARelationGoesToIt(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := show(t, repo, id, []string{"parent"})
	page = send(page, "shift+tab").(*showPage)
	updated, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
	require.Nil(t, updated.(*showPage).editor, "no picker")
	require.Equal(t, story, fieldOf(t, repo, id, "parent"))

	listed := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	listed = send(listed, "l", "l").(*listPage)
	moved, cmd := listed.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
	require.Nil(t, moved.(*listPage).editor, "no picker")

	// and enter on any other cell opens the row's own issue
	listed = send(listed, "h").(*listPage)
	_, cmd = listed.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, id, cmd().(pushMsg).page.(*showPage).id)
}

// TestChangeARelationToNoneClearsIt, from a list; enter on the empty
// relation then has nothing to go to, and rings the bell on show.
func TestChangeARelationToNoneClearsIt(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page = send(page, "l", "l", "space").(*listPage)
	require.NotNil(t, page.editor)
	for range page.editor.picker.items {
		page = send(page, "down").(*listPage)
	}
	page = send(page, "enter").(*listPage)
	require.Equal(t, "", fieldOf(t, repo, id, "parent"))

	page.status = ""
	require.Contains(t, plainView(page), relationHint(false))
	page = send(page, "space").(*listPage)
	require.NotNil(t, page.editor)
	require.Equal(t, []string{story, ""}, pickerValues(page.editor.picker))
	require.Equal(t, "", page.editor.picker.items[page.editor.picker.cursor].value, "opens on (none)")

	shown := show(t, repo, id, []string{"parent"})
	shown = send(shown, "shift+tab").(*showPage)
	_, cmd := shown.Update(press("enter"))
	require.NotNil(t, cmd, "the bell")
	require.Contains(t, plainView(shown), "no link")
}

// TestAMultiRelationGoesToEachAndSaysWhereToChangeIt: enter on a line goes to
// that line's issue, and space, changing the set, rings the bell for now,
// naming the commands that do it.
func TestAMultiRelationGoesToEachAndSaysWhereToChangeIt(t *testing.T) {
	repo := testRepo(t)
	one := newIssue(t, repo, map[string]any{"title": "one"})
	two := newIssue(t, repo, map[string]any{"title": "two"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "blocks": []any{one, two}})

	page := show(t, repo, id, []string{"blocks"})
	page = send(page, "shift+tab", "down").(*showPage)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, two, cmd().(pushMsg).page.(*showPage).id, "the line's own issue")

	updated, cmd := page.Update(press("space"))
	require.NotNil(t, cmd, "the bell")
	shown := updated.(*showPage)
	require.Nil(t, shown.editor)
	require.Contains(t, plainView(shown), "git work issue add/remove")
	require.Equal(t, one+", "+two, fieldOf(t, repo, id, "blocks"), "nothing written")

	// a list's cell holds both, and enter goes to the first
	listed := list(t, repo, `{"fields":["title","blocks"],"query":"map(select(.fields.title == \"the task\"))"}`)
	listed = send(listed, "l", "l").(*listPage)
	_, cmd = listed.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, one, cmd().(pushMsg).page.(*showPage).id)
}

// TestSpaceOnAViewOnlyColumnRings: a list column that is not a field of the
// row's type is drawn and never written, so space there rings the bell; on
// the id, with no rank bound, there is nothing to grab either.
func TestSpaceOnAViewOnlyColumnRings(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, `{"fields":["title","children"]}`)
	page = send(page, "l", "l").(*listPage)
	updated, cmd := page.Update(press("space"))
	require.NotNil(t, cmd, "the bell")
	require.Nil(t, updated.(*listPage).editor)
	require.Contains(t, plainView(updated), "not a field")

	page = send(page, "h", "h").(*listPage)
	updated, cmd = page.Update(press("space"))
	require.NotNil(t, cmd, "the bell")
	require.Equal(t, -1, updated.(*listPage).grabbed)
	require.Contains(t, plainView(updated), "no rank")
}
