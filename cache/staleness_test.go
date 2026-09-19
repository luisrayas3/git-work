package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/repository"
)

// TestCacheDetectsStaleExcerpts covers d591cb3. A cache file written before
// someone else moved a ref — another process, or a fetch — used to be
// indistinguishable from a current one, because nothing recorded what each
// excerpt was built from. Now the ref hashes travel with the excerpts and the
// difference decides what to read again.
func TestCacheDetectsStaleExcerpts(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	c, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	author, err := c.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(author))

	changed, _, err := c.Bugs().New("changed elsewhere", "message")
	require.NoError(t, err)
	untouched, _, err := c.Bugs().New("left alone", "message")
	require.NoError(t, err)

	// the cache file on disk now describes both issues
	require.NoError(t, c.Close())

	// somebody else moves one ref, without going through this cache
	elsewhere, err := bug.Read(repo, changed.Id())
	require.NoError(t, err)
	_, err = bug.SetTitle(elsewhere, author.Identity, time.Now().Unix(), "retitled elsewhere", nil)
	require.NoError(t, err)
	require.NoError(t, elsewhere.Commit(repo))

	// reopening reads the changed entity again, and only believes the cache
	// for the one whose ref did not move
	reopened, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer reopened.Close()

	changedExcerpt, err := reopened.Bugs().ResolveExcerpt(changed.Id())
	require.NoError(t, err)
	require.Equal(t, "retitled elsewhere", changedExcerpt.Title)

	untouchedExcerpt, err := reopened.Bugs().ResolveExcerpt(untouched.Id())
	require.NoError(t, err)
	require.Equal(t, "left alone", untouchedExcerpt.Title)
}

// TestCacheDropsDeletedEntities is the other half of the same diff: a ref that
// disappeared has to take its excerpt with it.
func TestCacheDropsDeletedEntities(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	c, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	author, err := c.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(author))

	doomed, _, err := c.Bugs().New("removed elsewhere", "message")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.NoError(t, bug.Remove(repo, doomed.Id()))

	reopened, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer reopened.Close()

	require.Empty(t, reopened.Bugs().AllIds())
}
