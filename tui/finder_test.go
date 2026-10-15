package tui

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
)

// TestAPickerIsTypedIntoAtOnce: the input is focused when the picker opens,
// so a letter narrows with no `/` first; the first esc clears the text and
// puts the cursor back on the current value, the second cancels
// (doc/design/app.md, A5).
func TestAPickerIsTypedIntoAtOnce(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := list(t, repo, `{"fields":["title","status"]}`)
	page = send(page, "right", "right", "space", "c", "a", "n").(*listPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	require.Equal(t, []string{"canceled"}, pickerValues(picker))
	require.NotContains(t, plainView(page), "In Progress")

	page = send(page, "esc").(*listPage)
	require.NotNil(t, page.editor, "the first esc clears")
	require.Equal(t, "", picker.text())
	require.Equal(t, "to-do", picker.items[picker.cursor].value, "back on the current value")
	require.Contains(t, plainView(page), "In Progress")

	page = send(page, "esc").(*listPage)
	require.Nil(t, page.editor, "the second cancels")
	require.Equal(t, "to-do", fieldOf(t, repo, id, "status"))

	// typed, then chosen
	page = send(page, "space", "p", "r", "o", "g", "enter").(*listPage)
	require.Nil(t, page.editor)
	require.Equal(t, "in-progress", fieldOf(t, repo, id, "status"))

	// a text that keeps nothing rings on enter
	page = send(page, "space", "z", "z", "z").(*listPage)
	require.Contains(t, plainView(page), "(no match)")
	page = rings(t, page, "enter").(*listPage)
	require.NotNil(t, page.editor)
}

// TestPeopleMatchOnNameLoginAndEmail: a people picker narrows on each of the
// three.
func TestPeopleMatchOnNameLoginAndEmail(t *testing.T) {
	repo := testRepo(t)
	ada, err := repo.Identities().NewRaw("Ada Lovelace", "ada@engines.example", "countess", "", nil, nil)
	require.NoError(t, err)
	newIssue(t, repo, map[string]any{"title": "one"})

	for _, typed := range []string{"lovelace", "countess", "engines"} {
		page := list(t, repo, `{"fields":["title","assignee"]}`)
		page = send(page, "right", "right", "space").(*listPage)
		for _, r := range typed {
			page = send(page, string(r)).(*listPage)
		}
		require.Equal(t, []string{ada.Id().String()}, pickerValues(page.editor.picker), typed)
	}
}

// TestARelationFinderSearchesDescriptionsInItsTypes: the relation's picker
// is the finder, so it matches what `git work issue search` does — here the
// description — and keeps to the field's target types; (none) is matched by
// its own label.
func TestARelationFinderSearchesDescriptionsInItsTypes(t *testing.T) {
	repo := testRepo(t)
	north, err := host.IssueNew(repo, host.IssueDocument{
		Fields: map[string]issue.Value{"type": issue.StringValue("story"), "title": issue.StringValue("north")},
		Body:   "where the compass points",
	})
	require.NoError(t, err)
	_, err = host.IssueNew(repo, host.IssueDocument{
		Fields: map[string]issue.Value{"type": issue.StringValue("initiative"), "title": issue.StringValue("compass work")},
	})
	require.NoError(t, err)
	id := newIssue(t, repo, map[string]any{"title": "the task"})

	page := show(t, repo, id, []string{"parent"})
	page = send(page, "space", "c", "o", "m", "p", "a", "s", "s").(*showPage)
	require.Equal(t, []string{north.String()}, pickerValues(page.editor.picker),
		"found by description, and the initiative is no target")

	page = send(page, "esc", "n", "o", "n", "e").(*showPage)
	require.Equal(t, []string{""}, pickerValues(page.editor.picker))

	// an id prefix finds it too
	page = send(page, "esc").(*showPage)
	for _, r := range north.String()[:6] {
		page = send(page, string(r)).(*showPage)
	}
	page = send(page, "enter").(*showPage)
	require.Nil(t, page.editor)
	require.Equal(t, north.String(), fieldOf(t, repo, id, "parent"))
}

// TestTheFinderDrawsAWindowOfMany: thousands of issues are searched, but
// only finderLimit are handed back to draw, and the rest are counted.
func TestTheFinderDrawsAWindowOfMany(t *testing.T) {
	repo := testRepo(t)
	for at := range finderLimit + 5 {
		newIssue(t, repo, map[string]any{"title": fmt.Sprintf("issue %d", at)})
	}

	finder := newFinder(repo, finderScope{}, nil, "")
	require.Len(t, finder.items, finderLimit)
	require.Equal(t, 5, finder.more)
	require.Contains(t, join(finder.lines(100)), "5 more: type to narrow")

	finder.setText("issue 204")
	require.Len(t, finder.items, 1)
	require.Equal(t, 0, finder.more)
}

// TestThePalettesSearchOpensAShow: `esc s` opens the finder over the page;
// typing narrows it, and enter pushes the chosen issue's show, opened by the
// page's own show map, as enter on a row would.
func TestThePalettesSearchOpensAShow(t *testing.T) {
	repo := testRepo(t)
	_, _, _, task := anEpic(t, repo)

	first := list(t, repo, `{"show":`+epicShows+`}`)
	stack := stackOf(first, 100, 30)
	run(stack, "esc")
	require.Contains(t, drawnStack(stack), "s search", "the palette lists it")
	run(stack, "s")
	require.NotNil(t, stack.search)
	drawn := drawnStack(stack)
	require.Contains(t, drawn, "search")
	require.Contains(t, drawn, "open story")

	for _, r := range "the task" {
		run(stack, string(r))
	}
	require.Equal(t, []string{task}, pickerValues(stack.search.picker))
	require.Contains(t, drawnStack(stack), "/the task")

	// the first esc clears, the second closes, and nothing was pushed
	run(stack, "esc")
	require.NotNil(t, stack.search)
	require.Equal(t, "", stack.search.picker.text())
	run(stack, "esc")
	require.Nil(t, stack.search)
	require.Len(t, stack.pages, 1)

	// alt+s is the same key, and enter opens
	run(stack, "alt+s")
	for _, r := range "the task" {
		run(stack, string(r))
	}
	msg := run(stack, "enter")
	require.IsType(t, pushMsg{}, msg)
	require.Nil(t, stack.search)
	require.Len(t, stack.pages, 2)
	shown := stack.top().(*showPage)
	require.Equal(t, task, shown.id)
	require.JSONEq(t, string(showsOf(first)), string(showsOf(shown)), "opened by the page's map")
}
