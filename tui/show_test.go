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
	require.Contains(t, drawn, "task  write the renderer", "the type left of the title, as the list has it")
	require.Contains(t, drawn, "━━━━", "the title is a heading, over a rule")
	require.Contains(t, drawn, "status")
	require.Contains(t, drawn, "in-progress")
	require.Contains(t, drawn, "estimate")
	// the built-ins are the header, not rows; archived is a checkbox there
	require.Contains(t, drawn, "task  write the renderer  [ ] archived")
	require.NotContains(t, drawn, " type ")
	require.Less(t, indexOf(drawn, "estimate"), indexOf(drawn, "Submit comment"), "the box is under the fields")
	require.Less(t, indexOf(drawn, "Submit comment"), indexOf(drawn, "comments"), "and the tabs under the box")

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

// TestShowHasThreeTabs: description, comments and log, one stop, switched
// with left and right; the description opens first, as Jira's page does.
func TestShowHasThreeTabs(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	_, err := host.IssueCommentNew(repo, id, "a later word")
	require.NoError(t, err)
	_, err = host.IssueSet(repo, id, map[string]issue.Value{"status": issue.StringValue("in-progress")}, false)
	require.NoError(t, err)

	page := show(t, repo, id, []string{"status"})
	require.Equal(t, tabDescription, page.tab)
	drawn := plainView(page)
	for _, name := range []string{"description", "comments", "log"} {
		require.Contains(t, drawn, name)
	}
	require.Less(t, indexOf(drawn, "description"), indexOf(drawn, "comments"), "description is the first tab")
	require.Contains(t, drawn, "the body")
	require.NotContains(t, drawn, "a later word")

	// box → tabs: the buttons are the box's own, not stops
	page = send(page, "tab").(*showPage)
	require.Equal(t, stopTabs, page.current().stop)

	page = send(page, "right").(*showPage)
	require.Equal(t, tabComments, page.tab)
	drawn = plainView(page)
	require.Contains(t, drawn, "a later word")
	require.NotContains(t, drawn, "the body")

	page = send(page, "right").(*showPage)
	require.Equal(t, tabLog, page.tab)
	drawn = plainView(page)
	require.Contains(t, drawn, "set status to in-progress")
	require.Contains(t, drawn, "commented: a later word")
	require.Less(t, indexOf(drawn, "set status to in-progress"), indexOf(drawn, "commented: a later word"), "latest first")
	require.Less(t, indexOf(drawn, "commented: a later word"), indexOf(drawn, "created"))

	page = send(page, "right").(*showPage)
	require.Equal(t, tabDescription, page.tab, "it wraps")
	page = send(page, "h").(*showPage)
	require.Equal(t, tabLog, page.tab)

	// ctrl+pgdown and vim's gt switch too
	page = send(page, "ctrl+pgdown").(*showPage)
	require.Equal(t, tabDescription, page.tab)
	page = send(page, "g", "t").(*showPage)
	require.Equal(t, tabComments, page.tab)

	// and in the box, where letters and arrows are text, ctrl+pgdown still
	page = show(t, repo, id, nil)
	page = send(page, "t", "ctrl+pgdown").(*showPage)
	require.Equal(t, "t", page.box.draft())
	require.Equal(t, tabComments, page.tab)
}

// TestShowWrapsProse: a description or a comment is read, so a long line
// wraps to the window instead of being cut at it.
func TestShowWrapsProse(t *testing.T) {
	repo := testRepo(t)
	long := strings.Repeat("word ", 30) + "end"
	created, err := host.IssueNew(repo, host.IssueDocument{
		Fields: map[string]issue.Value{"type": issue.StringValue("task"), "title": issue.StringValue("one")},
		Body:   long,
	})
	require.NoError(t, err)
	id := created.String()
	_, err = host.IssueCommentNew(repo, id, "said "+long)
	require.NoError(t, err)

	page := show(t, repo, id, nil)
	page.Update(tea.WindowSizeMsg{Width: 40, Height: 60})
	drawn := plainView(page)
	require.Contains(t, drawn, "end", "the description's last word is drawn")
	require.NotContains(t, drawn, "…", "nothing is cut")

	page = send(page, "tab", "tab", "tab", "right").(*showPage)
	require.Equal(t, tabComments, page.tab)
	drawn = plainView(page)
	require.Contains(t, drawn, "said word")
	require.Contains(t, drawn, "end")
	require.NotContains(t, drawn, "…")
}

// TestCommentsAreNewestFirst: an issue is opened to see what changed, and
// the latest is what that is.
func TestCommentsAreNewestFirst(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})
	for _, body := range []string{"first word", "second word"} {
		_, err := host.IssueCommentNew(repo, id, body)
		require.NoError(t, err)
	}

	page := show(t, repo, id, nil)
	page.tab = tabComments
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, "second word"), indexOf(drawn, "first word"))
}

// TestShowFieldsAreATable: once the cursor is in the table, up and down walk
// its rows, and at its edges go on to the stops around it.
func TestShowFieldsAreATable(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do", "priority": "high"})

	page := show(t, repo, id, []string{"status", "priority"})
	require.NotContains(t, plainView(page), "value", "no column headers")

	page = send(page, "shift+tab").(*showPage)
	require.Equal(t, stopFields, page.current().stop)
	require.Equal(t, "status", page.field())

	page = send(page, "j").(*showPage)
	require.Equal(t, "priority", page.field())
	page = send(page, "j").(*showPage)
	require.True(t, page.inText(), "down from the last row is the box")
	page = send(page, "up").(*showPage)
	require.Equal(t, "priority", page.field(), "up from the text's first line is the last row")
	page = send(page, "k", "k").(*showPage)
	require.Equal(t, stopHeader, page.current().stop, "up from the first row is the header")
	require.Equal(t, "title", page.field())

	// alt+c copies the id from anywhere outside the text
	updated, cmd := page.Update(press("alt+c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(updated), "copied "+id[:7])
}

// TestShowFollowsALink: a relation row is the issue it names, and enter,
// enter on it opens that issue.
func TestShowFollowsALink(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "the story"})
	id := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := show(t, repo, id, []string{"parent"})
	drawn := plainView(page)
	require.Contains(t, drawn, story[:7]+" the story")
	require.NotContains(t, drawn, story)

	page = send(page, "shift+tab", "enter").(*showPage)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, story, cmd().(pushMsg).page.(*showPage).id)
}

// TestShowEditsTheFieldUnderTheCursor: enter edits the row the cursor is on,
// and the header's cells like any field.
func TestShowEditsTheFieldUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := show(t, repo, id, []string{"status"})
	page = send(page, "shift+tab").(*showPage)
	require.Equal(t, "status", page.field())

	page = send(page, "enter").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "status", page.editor.key)

	page = send(page, "j", "enter").(*showPage)
	require.Equal(t, "in-progress", fieldOf(t, repo, id, "status"))

	page = send(page, "shift+tab").(*showPage)
	require.Equal(t, stopHeader, page.current().stop)
	require.Equal(t, "title", page.field(), "the header opens on the title")
	page = send(page, "enter").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "title", page.editor.key)
	page = send(page, "esc").(*showPage)

	// left of the title is the type, a list of the schema's types
	page = send(page, "h", "enter").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "type", page.editor.key)
	drawn := plainView(page)
	require.Contains(t, drawn, "story")
	require.NotContains(t, drawn, "(none)", "a type is never cleared")
	page = send(page, "esc").(*showPage)
}

// TestArchivedIsAHeaderCellThatFlips: right of the title, a checkbox, and
// enter toggles it both ways.
func TestArchivedIsAHeaderCellThatFlips(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page = send(page, "shift+tab", "shift+tab", "l", "l").(*showPage)
	require.Equal(t, "archived", page.field())
	require.Contains(t, plainView(page), "one  [ ] archived")

	page = send(page, "enter").(*showPage)
	require.Equal(t, "true", fieldOf(t, repo, id, "archived"))
	drawn := plainView(page)
	require.Contains(t, drawn, "one  [x] archived")
	require.Contains(t, drawn, "\narchived", "the status says so")

	page = send(page, "enter").(*showPage)
	require.Equal(t, "false", fieldOf(t, repo, id, "archived"))
	require.Contains(t, plainView(page), "one  [ ] archived")
	require.Contains(t, plainView(page), "unarchived")
}

// TestShowOpensInTheCommentBox: typing on arrival is writing a comment, and
// down, enter sends it — the footer is reached from the text's last line.
func TestShowOpensInTheCommentBox(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	require.True(t, page.inText())

	page = send(page, "h", "i", "q").(*showPage)
	require.Equal(t, "hiq", page.box.draft(), "letters are text in the box, q included")

	require.Contains(t, plainView(page), "↓ enter sends")
	page = send(page, "down").(*showPage)
	require.False(t, page.inText(), "on the button")
	page = send(page, "enter").(*showPage)
	require.Empty(t, page.box.draft())
	require.Contains(t, plainView(page), "hiq", "the comments tab shows it")

	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, document.Comments, 2)
	require.Equal(t, "hiq", document.Comments[1].Message)
}

// TestTheBoxIsOneStop: tab skips the block whole, down walks into its
// footer only from the last line of the text, and up comes back.
func TestTheBoxIsOneStop(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page = send(page, "tab").(*showPage)
	require.Equal(t, stopTabs, page.current().stop)
	page = send(page, "shift+tab").(*showPage)
	require.True(t, page.inText(), "back into the text, not onto a button")

	page = send(page, "a", "enter", "b", "up").(*showPage)
	require.True(t, page.inText(), "up within two lines of text is the text's")
	page = send(page, "down", "down").(*showPage)
	require.False(t, page.inText())
	require.Equal(t, 0, page.current().button)
	page = send(page, "up").(*showPage)
	require.True(t, page.inText())
	require.Equal(t, "a\nb", page.box.draft())
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
