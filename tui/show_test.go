package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
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

	page := show(t, repo, id, nil)
	drawn := plainView(page)

	require.Contains(t, drawn, id[:7])
	require.Contains(t, drawn, "write the renderer  task")
	require.Contains(t, drawn, "━━━━", "the title is a heading, over a rule")
	require.Contains(t, drawn, "status")
	require.Contains(t, drawn, "in-progress")
	require.Contains(t, drawn, "estimate")

	// the body is the first comment, which an issue always has, and the
	// description tab is where it is read
	page.tab = tabDescription
	drawn = plainView(page)
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

// TestShowStartsWithTheCall: `show <id>`, the call the page is, whether the
// command line made it or the list's enter did.
func TestShowStartsWithTheCall(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	first := strings.SplitN(plainView(show(t, repo, id, nil)), "\n", 2)[0]
	require.Equal(t, "show  "+id[:7], strings.TrimSpace(first))
}

// TestShowHasThreeTabs: comments, description and log, one stop, switched
// with left and right.
func TestShowHasThreeTabs(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	_, err := host.IssueCommentNew(repo, id, "a later word")
	require.NoError(t, err)
	_, err = host.IssueSet(repo, id, map[string]issue.Value{"status": issue.StringValue("in-progress")}, false)
	require.NoError(t, err)

	page := show(t, repo, id, []string{"status"})
	require.Equal(t, tabComments, page.tab)
	drawn := plainView(page)
	for _, name := range []string{"comments", "description", "log"} {
		require.Contains(t, drawn, name)
	}
	require.Contains(t, drawn, "a later word")
	require.NotContains(t, drawn, "the body")

	// box → button → fields → tabs
	page = send(page, "tab", "tab", "tab").(*showPage)
	require.Equal(t, stopTabs, page.current().stop)

	page = send(page, "right").(*showPage)
	require.Equal(t, tabDescription, page.tab)
	require.Contains(t, plainView(page), "the body")

	page = send(page, "right").(*showPage)
	require.Equal(t, tabLog, page.tab)
	drawn = plainView(page)
	require.Contains(t, drawn, "set status to in-progress")
	require.Contains(t, drawn, "commented: a later word")

	page = send(page, "right").(*showPage)
	require.Equal(t, tabComments, page.tab, "it wraps")
	page = send(page, "h").(*showPage)
	require.Equal(t, tabLog, page.tab)

	// ctrl+pgdown and vim's gt switch too
	page = send(page, "ctrl+pgdown").(*showPage)
	require.Equal(t, tabComments, page.tab)
	page = send(page, "g", "t").(*showPage)
	require.Equal(t, tabDescription, page.tab)

	// and in the box, where letters and arrows are text, ctrl+pgdown still
	page = show(t, repo, id, nil)
	page = send(page, "t", "ctrl+pgdown").(*showPage)
	require.Equal(t, "t", page.box.draft())
	require.Equal(t, tabDescription, page.tab)
}

// TestShowFieldsAreATable: once the cursor is in the table, up and down walk
// its rows, and at its edges go on to the stops around it.
func TestShowFieldsAreATable(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do", "priority": "high"})

	page := show(t, repo, id, []string{"status", "priority"})
	require.NotContains(t, plainView(page), "value", "no column headers")

	page = send(page, "tab", "tab").(*showPage)
	require.Equal(t, stopFields, page.current().stop)
	require.Equal(t, "status", page.field())

	page = send(page, "j").(*showPage)
	require.Equal(t, "priority", page.field())
	page = send(page, "j").(*showPage)
	require.Equal(t, stopTabs, page.current().stop)
	page = send(page, "k").(*showPage)
	require.Equal(t, "priority", page.field())
	page = send(page, "k", "k").(*showPage)
	require.Equal(t, stopBox, page.current().stop, "up from the first row is the box's button")

	// alt+c copies the id from anywhere outside the text
	updated, cmd := page.Update(press("alt+c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(updated), "copied "+id[:7])
}

// TestShowFollowsALink: a relation row is the issue it names, and enter on
// it opens that issue.
func TestShowFollowsALink(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "the story"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := show(t, repo, id, []string{"parent"})
	drawn := plainView(page)
	require.Contains(t, drawn, story[:7]+" the story")
	require.NotContains(t, drawn, story)

	page = send(page, "tab", "tab").(*showPage)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
}

// TestShowEditsTheFieldUnderTheCursor: ctrl+enter edits the row the cursor
// is on, and the title, above the box, like any field.
func TestShowEditsTheFieldUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := show(t, repo, id, []string{"status"})
	page = send(page, "tab", "tab").(*showPage)
	require.Equal(t, "status", page.field())

	page = send(page, "ctrl+enter").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "status", page.editor.key)

	page = send(page, "j", "enter").(*showPage)
	require.Equal(t, "in-progress", fieldOf(t, repo, id, "status"))

	page = send(page, "shift+tab", "shift+tab", "shift+tab").(*showPage)
	require.Equal(t, stopTitle, page.current().stop)
	page = send(page, "f2").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "title", page.editor.key)
}

// TestShowOpensInTheCommentBox: typing on arrival is writing a comment, and
// ctrl+enter sends it.
func TestShowOpensInTheCommentBox(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	require.True(t, page.inText())

	page = send(page, "h", "i", "q").(*showPage)
	require.Equal(t, "hiq", page.box.draft(), "letters are text in the box, q included")

	page = send(page, "ctrl+enter").(*showPage)
	require.Empty(t, page.box.draft())
	require.Contains(t, plainView(page), "hiq", "the comments tab shows it")

	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, document.Comments, 2)
	require.Equal(t, "hiq", document.Comments[1].Message)
}

// TestShowCommentsWithTheButton is the path every terminal has: tab to the
// button, enter.
func TestShowCommentsWithTheButton(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page.box.area.SetValue("said in the issue")
	require.Contains(t, plainView(page), "Submit comment")

	page = send(page, "tab", "enter").(*showPage)

	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, document.Comments, 2)
	require.Equal(t, "said in the issue", document.Comments[1].Message)
}

// TestShowKeepsADraft: esc leaves a box with a draft in it rather than the
// page, and going back asks twice.
func TestShowKeepsADraft(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page.box.area.SetValue("half a thought")

	updated, cmd := page.Update(press("esc"))
	require.Nil(t, cmd)
	page = updated.(*showPage)
	require.Equal(t, stopBox, page.current().stop)
	require.False(t, page.inText(), "on the button")
	require.Equal(t, "half a thought", page.box.draft())

	_, cmd = page.Update(press("esc"))
	require.Nil(t, cmd, "the first esc outside the box warns")
	require.Contains(t, plainView(page), "esc again")

	_, cmd = page.Update(press("esc"))
	require.NotNil(t, cmd)
	require.IsType(t, popMsg{}, cmd())
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

// TestTheCommentBoxGrows: two lines to start, then as tall as what is typed,
// up to a paragraph.
func TestTheCommentBoxGrows(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	require.Len(t, page.box.View(page.width), commentMinHeight)

	page = send(page, "a", "enter", "b", "enter", "c", "enter", "d").(*showPage)
	require.Len(t, page.box.View(page.width), 4)
}
