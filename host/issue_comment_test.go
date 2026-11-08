package host

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
)

// TestIssueCommentEditOnBase: get prints each comment's message_hash, an edit
// naming the current hash is written, one naming a stale hash or a prefix is
// refused and writes nothing, and one with no base replaces
// (doc/design/issue.md, "An edit can name its base").
func TestIssueCommentEditOnBase(t *testing.T) {
	repo, author := atTestRepo(t)
	i := newIssueAt(t, repo, author, time.Now(), "a title", nil)
	id := i.Id().String()

	got, err := IssueGet(repo, id)
	require.NoError(t, err)
	description := got.Comments[0]
	sum := sha256.Sum256([]byte("body"))
	require.Equal(t, hex.EncodeToString(sum[:]), description.MessageHash)

	// the current hash: written
	require.NoError(t, IssueCommentEdit(repo, description.Id, "first rewrite", description.MessageHash))
	got, err = IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "first rewrite", got.Comments[0].Message)
	read := got.Comments[0].MessageHash

	// another writer edits in between, with no base: it replaces
	require.NoError(t, IssueCommentEdit(repo, description.Id, "someone else's", ""))
	got, err = IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "someone else's", got.Comments[0].Message)
	before, err := IssueLog(repo, id)
	require.NoError(t, err)

	// a stale hash, or a prefix of the current one: refused, nothing written
	for _, base := range []string{description.MessageHash, read, got.Comments[0].MessageHash[:12]} {
		err = IssueCommentEdit(repo, description.Id, "my rewrite", base)
		require.ErrorIs(t, err, cache.ErrCommentChanged)
	}
	after, err := IssueLog(repo, id)
	require.NoError(t, err)
	require.Len(t, after, len(before))
	got, err = IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "someone else's", got.Comments[0].Message)

	// an edit back to the text that was read is no conflict: the hash is the content's
	require.NoError(t, IssueCommentEdit(repo, description.Id, "first rewrite", ""))
	require.NoError(t, IssueCommentEdit(repo, description.Id, "my rewrite", read))
	got, err = IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "my rewrite", got.Comments[0].Message)
}

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
	require.ErrorContains(t, IssueCommentEdit(repo, wrong.String(), "too late", ""), "doesn't exist")
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
