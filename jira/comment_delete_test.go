package jira_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// the rows of a comment deleted locally
// (doc/design/jira.md, "A comment deleted locally is deleted in Jira").

func deleteLocal(t *testing.T, ic *cache.IssueCache, i int) {
	t.Helper()
	_, err := ic.DeleteComment(ic.Snapshot().Comments[i].CombinedId())
	require.NoError(t, err)
	require.NoError(t, ic.Commit())
}

func deleteRequests(srv *jiratest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.Method == "DELETE" && strings.Contains(r.Path, "/comment/") {
			n++
		}
	}
	return n
}

// Paired and present: deleted in Jira, never re-imported.
func TestCommentDeletedLocally(t *testing.T) {
	w := newWorld(t, jiratest.WithCommentBumps(true, true))
	key, ic := w.imported("Wrong issue")
	w.comment(ic, "on the wrong issue")
	w.comment(ic, "kept")
	w.mustSync(jira.Options{})
	require.Len(t, w.srv.Issue(key).Comments, 2)
	w.quiet()

	deleteLocal(t, ic, 1)
	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{})
	require.Equal(t, 1, deleteRequests(w.srv))
	cs := w.srv.Issue(key).Comments
	require.Len(t, cs, 1)
	require.Equal(t, "kept", cs[0].Text)
	require.Len(t, lines, 1)
	require.NotNil(t, lines[0].Comments)
	require.Equal(t, 1, lines[0].Comments.Deleted)
	require.Empty(t, lines[0].Conflicts)

	snap := ic.Snapshot()
	require.Len(t, snap.Comments, 2, "not re-imported, not tombstoned")
	require.Equal(t, "kept", snap.Comments[1].Message)
	w.still()
	w.quietFull()
}

// Never paired: never exported.
func TestCommentDeletedBeforeExport(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Never sent")
	w.comment(ic, "a slip")
	deleteLocal(t, ic, 1)
	w.srv.ResetRequests()
	w.mustSync(jira.Options{})
	require.Empty(t, w.srv.Issue(key).Comments)
	require.Zero(t, w.srv.Writes())
	w.still()
	w.quietFull()
}

// Edited in Jira since the base: Jira's text goes into git, the delete goes
// ahead, and no conflict is reported.
func TestCommentDeletedLocallyAfterJiraEdit(t *testing.T) {
	w := newWorld(t, jiratest.WithCommentBumps(true, true))
	key, ic := w.imported("Edited there")
	cid := w.srv.AddComment(key, "v0")
	w.mustSync(jira.Options{})
	require.Len(t, ic.Snapshot().Comments, 2)

	w.srv.EditComment(key, cid, "v1, edited in Jira")
	deleteLocal(t, ic, 1)
	_, sum := w.mustSync(jira.Options{})
	require.Zero(t, sum.Conflicts)
	require.Empty(t, w.srv.Issue(key).Comments)
	snap := ic.Snapshot()
	require.Len(t, snap.Comments, 1)
	require.Empty(t, notes(ic, jira.NoteConflict))

	var edits []string
	for _, op := range snap.Operations {
		if e, ok := op.(*issue.EditCommentOperation); ok {
			edits = append(edits, e.Message)
		}
	}
	require.Contains(t, edits, "v1, edited in Jira", "Jira's text is not lost to git")
	w.still()
	w.quietFull()
}

// Deleted on both sides: no request and no tombstone.
func TestCommentDeletedOnBothSides(t *testing.T) {
	w := newWorld(t, jiratest.WithCommentBumps(true, true))
	key, ic := w.imported("Both")
	cid := w.srv.AddComment(key, "doomed")
	w.mustSync(jira.Options{})

	w.srv.DeleteComment(key, cid)
	deleteLocal(t, ic, 1)
	w.srv.ResetRequests()
	w.mustSync(jira.Options{})
	require.Zero(t, deleteRequests(w.srv))
	require.Zero(t, w.srv.Writes())
	snap := ic.Snapshot()
	require.Len(t, snap.Comments, 1)
	for _, op := range snap.Operations {
		if e, ok := op.(*issue.EditCommentOperation); ok {
			require.False(t, jira.IsTombstone(e.Message), "no tombstone")
		}
	}
	w.still()
	w.quietFull()
}

// A refused delete is a failed request, pending and retried.
func TestCommentDeleteRefused(t *testing.T) {
	w := newWorld(t, jiratest.WithCommentBumps(true, true))
	key, ic := w.imported("Refused")
	w.srv.AddComment(key, "stays for now")
	w.mustSync(jira.Options{})

	deleteLocal(t, ic, 1)
	w.srv.Deny("deleteComment")
	lines, _ := w.mustSync(jira.Options{})
	require.Len(t, w.srv.Issue(key).Comments, 1)
	require.Len(t, lines, 1)
	require.NotEmpty(t, lines[0].Pending)
	require.True(t, lines[0].Comments == nil || lines[0].Comments.Deleted == 0)

	w.srv.Allow("deleteComment")
	w.mustSync(jira.Options{})
	require.Empty(t, w.srv.Issue(key).Comments)
	require.Len(t, ic.Snapshot().Comments, 1)
	w.still()
	w.quietFull()
}
