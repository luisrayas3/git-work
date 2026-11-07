package host

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIssueCommentDelete: a deleted comment leaves the issue and its excerpt,
// and its text stays in the log and in a replay from before the delete
// (doc/design/issue.md, "Delete hides, the history keeps";
// doc/design/issue.md, "Delete is final").
func TestIssueCommentDelete(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)

	i := newIssueAt(t, repo, author, t0, "a title", nil)
	kept, _, err := i.AddCommentRaw(author, t1.Unix(), "kept", nil, nil)
	require.NoError(t, err)
	wrong, _, err := i.AddCommentRaw(author, t1.Unix(), "on the wrong issue", nil, nil)
	require.NoError(t, err)
	require.NoError(t, i.Commit())

	excerpt, err := repo.Issues().ResolveExcerptPrefixOrAlias(i.Id().String())
	require.NoError(t, err)
	require.Equal(t, 3, excerpt.LenComments)

	_, err = i.DeleteCommentRaw(author, t2.Unix(), wrong, nil)
	require.NoError(t, err)
	require.NoError(t, i.Commit())

	snap := i.Snapshot()
	require.Len(t, snap.Comments, 2)
	require.Equal(t, kept, snap.Comments[1].CombinedId())

	excerpt, err = repo.Issues().ResolveExcerptPrefixOrAlias(i.Id().String())
	require.NoError(t, err)
	require.Equal(t, 2, excerpt.LenComments)

	// the text is in the log, the delete after it
	log, err := IssueLog(repo, i.Id().String())
	require.NoError(t, err)
	var types []string
	for _, op := range log {
		types = append(types, op.Type)
	}
	require.Equal(t, []string{"create", "add-comment", "add-comment", "delete-comment"}, types)
	require.Contains(t, string(log[2].Op), "on the wrong issue")

	// and a replay from before the delete shows the comment
	before, err := IssueSnapshotAt(repo, i.Id().String(), t1)
	require.NoError(t, err)
	require.Len(t, before.Comments, 3)
	require.Equal(t, "on the wrong issue", before.Comments[2].Message)

	// a deleted comment no longer resolves: a second delete and an edit are refused
	require.ErrorContains(t, IssueCommentDelete(repo, wrong.String()), "doesn't exist")
	require.ErrorContains(t, IssueCommentEdit(repo, wrong.String(), "too late"), "doesn't exist")
}

func TestIssueCommentDeleteRefusesTheDescription(t *testing.T) {
	repo, author := atTestRepo(t)
	i := newIssueAt(t, repo, author, time.Now(), "a title", nil)

	description := i.Snapshot().Comments[0].CombinedId()
	err := IssueCommentDelete(repo, description.String())
	require.ErrorContains(t, err, "issue comment edit")
	require.Len(t, i.Snapshot().Comments, 1)
}

func TestIssueCommentDeleteThroughHost(t *testing.T) {
	repo, author := atTestRepo(t)
	i := newIssueAt(t, repo, author, time.Now(), "a title", nil)

	id, err := IssueCommentNew(repo, i.Id().String(), "a comment")
	require.NoError(t, err)
	require.NoError(t, IssueCommentDelete(repo, id.String()))
	require.Len(t, i.Snapshot().Comments, 1)
}
