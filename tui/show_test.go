package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
)

func show(t *testing.T, repo *cache.RepoCache, id string, fields []string) *showPage {
	t.Helper()

	page, err := newShowPage(repo, id, fields)
	require.NoError(t, err)

	page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return page
}

func TestShowDrawsTheIssue(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "write the renderer", "status": "in-progress", "estimate": 3})

	drawn := plainView(show(t, repo, id, nil))

	require.Contains(t, drawn, id[:7])
	require.Contains(t, drawn, "write the renderer")
	require.Contains(t, drawn, "(task)")
	require.Contains(t, drawn, "status")
	require.Contains(t, drawn, "in-progress")
	require.Contains(t, drawn, "estimate")
	// the body is the first comment, which an issue always has
	require.Contains(t, drawn, "the body")
	require.Contains(t, drawn, "John Doe")
}

// TestShowTakesAnIdPrefix: every id position takes a prefix or an alias, and
// a view is an id position like any other.
func TestShowTakesAnIdPrefix(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id[:7], nil)
	require.Equal(t, id, page.id)
}

// TestShowFieldOrderIsTheSchemaOrder is what makes two issues of one type
// read the same: the type's fields, in the order they were authored.
func TestShowFieldOrderIsTheSchemaOrder(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	order := page.fieldOrder()
	require.Equal(t, "title", order[0], "the built-ins sort before every configured field")
	require.Less(t, indexOf(join(order), "status"), indexOf(join(order), "rank"))

	// and a call that names the fields gets exactly those
	page = show(t, repo, id, []string{"status", "title"})
	require.Equal(t, []string{"status", "title"}, page.fieldOrder())
}

func TestShowSwitchesToTheOpLog(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	require.Contains(t, plainView(page), "comments")

	page = send(page, "t").(*showPage)
	drawn := plainView(page)
	require.Contains(t, drawn, "history")
	require.Contains(t, drawn, "create")
	require.NotEmpty(t, page.log)

	// and back
	page = send(page, "t").(*showPage)
	require.Contains(t, plainView(page), "comments")
}

// TestShowEditsAskWhichField: a page shows many fields and the cursor is a
// scroll position, so the field has to be chosen before it can be edited.
func TestShowEditsAskWhichField(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := show(t, repo, id, []string{"status"})
	page = send(page, "e").(*showPage)

	require.NotNil(t, page.choosing)
	require.Contains(t, plainView(page), "which field?")

	page = send(page, "enter").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "status", page.editor.key)

	page = send(page, "j", "enter").(*showPage)
	require.Equal(t, "in-progress", fieldOf(t, repo, id, "status"))
}

func TestShowComments(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page = send(page, "c").(*showPage)
	require.NotNil(t, page.comment)

	page.comment.area.SetValue("said in the issue")
	page = send(page, "ctrl+d").(*showPage)

	require.Contains(t, plainView(page), "said in the issue")

	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, document.Comments, 2)
}

// TestEnterOpensAnIssueAndEscComesBack is the stack: the list is still there,
// where it was, under the issue.
func TestEnterOpensAnIssueAndEscComesBack(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "first"})
	second := newIssue(t, repo, map[string]any{"title": "second"})

	stack := &root{pages: []page{list(t, repo, "")}, width: 100, height: 40}
	stack.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	// enter answers with a command, which the program would run for us
	_, cmd := stack.Update(press("enter"))
	require.NotNil(t, cmd)
	stack.Update(cmd())

	require.Len(t, stack.pages, 2)
	require.Contains(t, ansiPattern.ReplaceAllString(stack.View().Content, ""), second[:7])

	_, cmd = stack.Update(press("esc"))
	require.NotNil(t, cmd)
	stack.Update(cmd())

	require.Len(t, stack.pages, 1)
	require.Contains(t, ansiPattern.ReplaceAllString(stack.View().Content, ""), "first")
}

func join(list []string) string {
	out := ""
	for _, item := range list {
		out += item + " "
	}
	return out
}
