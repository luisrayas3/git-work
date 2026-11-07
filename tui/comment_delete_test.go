package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
)

// TestShowDeletesTheCommentUnderTheCursor: on the comments tab down and up
// walk the comments, newest first, and esc d deletes the one under the
// cursor, at once, and reads the page again (doc/design/tui.md, "Deleting a
// comment").
func TestShowDeletesTheCommentUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})
	for _, body := range []string{"first word", "second word", "third word"} {
		_, err := host.IssueCommentNew(repo, id, body)
		require.NoError(t, err)
	}

	stack := stackOf(show(t, repo, id, nil), 100, 40)
	page := send(stack.top(), "tab", "tab", "right").(*showPage)
	require.Equal(t, tabComments, page.tab)
	drawn := plainView(page)
	require.Contains(t, drawn, "›John Doe", "the newest comment is under the cursor")
	require.Contains(t, drawn, "esc d: delete")

	// down to the second newest, and delete it through the palette
	send(page, "down")
	require.Equal(t, 1, page.commentAt())
	run(stack, "esc")
	_, cmd := stack.Update(press("d"))
	require.Nil(t, cmd)
	require.False(t, stack.palette, "a command closes the palette")

	require.Len(t, page.snapshot.Comments, 3, "the description and two comments")
	drawn = plainView(page)
	require.Contains(t, drawn, "deleted comment")
	require.Contains(t, drawn, "third word")
	require.Contains(t, drawn, "first word")
	require.NotContains(t, drawn, "second word")

	// alt+d is the same key; the cursor stays within what is left
	send(page, "down", "down")
	require.Equal(t, 1, page.commentAt())
	send(page, "alt+d")
	require.Len(t, page.snapshot.Comments, 2)
	require.NotContains(t, plainView(page), "first word")
	require.Equal(t, 0, page.commentAt())

	// the log says so
	page.tab = tabLog
	require.Contains(t, plainView(page), "deleted comment")
}

// TestShowDeleteNeedsACommentUnderTheCursor: anywhere but on a comment,
// esc d rings and deletes nothing.
func TestShowDeleteNeedsACommentUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})
	_, err := host.IssueCommentNew(repo, id, "a word")
	require.NoError(t, err)

	page := show(t, repo, id, nil)
	page = rings(t, page, "alt+d", "on the fields").(*showPage)
	require.Len(t, page.snapshot.Comments, 2)

	// the description tab is no comment either
	page = send(page, "tab", "tab").(*showPage)
	require.Equal(t, tabDescription, page.tab)
	page = rings(t, page, "alt+d", "on the description").(*showPage)
	require.Len(t, page.snapshot.Comments, 2)
	require.NotContains(t, pageCommandsOf(page), "delete comment")

	page = send(page, "right").(*showPage)
	require.Contains(t, pageCommandsOf(page), "delete comment")
}

func pageCommandsOf(p page) []string {
	var out []string
	for _, e := range pageCommands(p) {
		out = append(out, e.what)
	}
	return out
}
