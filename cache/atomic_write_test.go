package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/repository"
)

// TestCacheFileWrittenAtomically covers f39878f: the cache file is replaced by
// a rename, not truncated in place, and a cache that does get corrupted is
// recoverable because the refs are the real store.
func TestCacheFileWrittenAtomically(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	c, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	author, err := c.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(author))

	_, _, err = c.Bugs().New("title", "message")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// the staging file does not outlive the write
	_, err = repo.LocalStorage().Stat(filepath.Join("cache", bug.Namespace+".new"))
	require.True(t, os.IsNotExist(err), "temp cache file left behind")

	// what a kill mid-write used to leave: a file that does not decode
	f, err := repo.LocalStorage().Create(filepath.Join("cache", bug.Namespace))
	require.NoError(t, err)
	_, err = f.Write([]byte("truncated"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// recoverable: the cache rebuilds from the refs rather than failing
	c2, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	require.Len(t, c2.Bugs().AllIds(), 1)
	require.NoError(t, c2.Close())
}
