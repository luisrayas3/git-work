package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pickerValues are what a picker's choices write, in order, a "go to" entry
// spelled with a leading →.
func pickerValues(p *picker) []string {
	out := make([]string, 0, len(p.items))
	for _, item := range p.items {
		if item.goTo {
			out = append(out, "→"+item.value)
			continue
		}
		out = append(out, item.value)
	}
	return out
}

// TestEnterOnARelationOpensItsPicker: the first enter on a relation cell is
// its picker, headed by "go to" the issue it names, set apart and where the
// cursor opens; the current value is marked among the rest; esc closes it
// having done nothing.
func TestEnterOnARelationOpensItsPicker(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page = send(page, "l", "l").(*listPage)
	require.Contains(t, plainView(page), relationHint(true), "the status line says what enter does")

	page = send(page, "enter").(*listPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	require.Equal(t, []string{"→" + story, story, ""}, pickerValues(picker))
	require.Equal(t, 0, picker.cursor, "opens on go to")
	require.True(t, picker.items[1].current, "the current value is marked")
	drawn := plainView(page)
	require.Contains(t, drawn, "→ go to "+story[:7]+" north")
	require.Contains(t, drawn, "● current")
	require.Contains(t, drawn, "─────")

	page = send(page, "esc").(*listPage)
	require.Nil(t, page.editor)
	require.Equal(t, story, fieldOf(t, repo, id, "parent"), "nothing written")
}

// TestChangeARelationPicksFromItsTargetTypes: under "go to" are the issues
// the field may name — its target_types, not the issue itself — as links are
// drawn, ending with (none); `/` narrows them, leaving "go to" out, and
// enter writes the id.
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

	page = send(page, "enter").(*showPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	values := pickerValues(picker)
	require.Equal(t, "→"+north, values[0])
	require.ElementsMatch(t, []string{"→" + north, north, south, ""}, values)
	require.Equal(t, "", values[len(values)-1], "(none) is last")
	require.NotContains(t, values, initiative)
	require.NotContains(t, values, sibling)
	require.NotContains(t, values, id)
	require.Contains(t, plainView(page), south[:7]+" south")

	page = send(page, "/", "s", "o", "u").(*showPage)
	require.Equal(t, south, picker.items[picker.cursor].value, "the narrowing keeps south alone")
	require.False(t, picker.items[picker.cursor].goTo)
	require.NotContains(t, join(page.editor.View(100)), north[:7]+" north")
	page = send(page, "enter", "enter").(*showPage)
	require.Nil(t, page.editor)
	require.Equal(t, south, fieldOf(t, repo, id, "parent"))
	require.Contains(t, plainView(page), south[:7]+" south")
}

// TestEnterEnterFollowsARelation, from show: go to is where the picker
// opens, and enter on it opens the issue without writing.
func TestEnterEnterFollowsARelation(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := show(t, repo, id, []string{"parent"})
	page = send(page, "shift+tab", "enter").(*showPage)
	updated, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
	require.Nil(t, updated.(*showPage).editor)
	require.Equal(t, story, fieldOf(t, repo, id, "parent"))
}

// TestChangeARelationToNoneClearsIt, from a list, and an empty relation's
// picker has no go to, because there is nothing to go to.
func TestChangeARelationToNoneClearsIt(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "north"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page = send(page, "l", "l", "enter").(*listPage)
	require.NotNil(t, page.editor)
	for range page.editor.picker.items {
		page = send(page, "down").(*listPage)
	}
	page = send(page, "enter").(*listPage)
	require.Equal(t, "", fieldOf(t, repo, id, "parent"))

	page.status = ""
	require.Contains(t, plainView(page), relationHint(false))
	page = send(page, "enter").(*listPage)
	require.NotNil(t, page.editor)
	require.Equal(t, []string{story, ""}, pickerValues(page.editor.picker), "no go to")
	require.Equal(t, "", page.editor.picker.items[page.editor.picker.cursor].value, "opens on (none)")
	require.NotContains(t, plainView(page), "go to")
}

// TestAMultiRelationGoesToEachAndSaysWhereToChangeIt: a go to per issue it
// names, the cursor on the line's own, and changing the set rings the bell
// for now, naming the commands that do it.
func TestAMultiRelationGoesToEachAndSaysWhereToChangeIt(t *testing.T) {
	repo := testRepo(t)
	one := newIssue(t, repo, map[string]any{"title": "one"})
	two := newIssue(t, repo, map[string]any{"title": "two"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "blocks": []any{one, two}})

	page := show(t, repo, id, []string{"blocks"})
	page = send(page, "shift+tab", "down", "enter").(*showPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	require.Equal(t, []string{"→" + one, "→" + two, ""}, pickerValues(picker))
	require.Equal(t, two, picker.items[picker.cursor].value, "on the line's own issue")

	page = send(page, "down").(*showPage)
	updated, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "the bell")
	shown := updated.(*showPage)
	require.Nil(t, shown.editor)
	require.Contains(t, plainView(shown), "git work issue add/remove")
	require.Equal(t, one+", "+two, fieldOf(t, repo, id, "blocks"), "nothing written")
}
