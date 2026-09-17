package cache

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/repository"
)

// CreateGoGitTestRepo configures user.name=testuser and user.email=testuser@example.com.

func TestEnsureUserIdentityCreates(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)
	cache, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer cache.Close()

	_, err = cache.GetUserIdentity()
	require.ErrorIs(t, err, identity.ErrNoIdentitySet)

	i, source, err := cache.EnsureUserIdentity()
	require.NoError(t, err)
	require.Equal(t, UserIdentityCreated, source)
	require.Equal(t, "testuser", i.Name())
	require.Equal(t, "testuser@example.com", i.Email())

	// Recorded in config, and returned as-is from now on.
	set, err := cache.IsUserIdentitySet()
	require.NoError(t, err)
	require.True(t, set)

	again, source, err := cache.EnsureUserIdentity()
	require.NoError(t, err)
	require.Equal(t, UserIdentityConfigured, source)
	require.Equal(t, i.Id(), again.Id())
	require.Len(t, cache.Identities().AllIds(), 1)
}

func TestEnsureUserIdentityAdopts(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)
	cache, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer cache.Close()

	other, err := cache.Identities().New("Someone Else", "else@example.com")
	require.NoError(t, err)
	// Same email, different case and different name: still the same person.
	existing, err := cache.Identities().New("Test User", "TestUser@Example.com")
	require.NoError(t, err)

	i, source, err := cache.EnsureUserIdentity()
	require.NoError(t, err)
	require.Equal(t, UserIdentityAdopted, source)
	require.Equal(t, existing.Id(), i.Id())
	require.NotEqual(t, other.Id(), i.Id())
	require.Len(t, cache.Identities().AllIds(), 2)

	user, err := cache.GetUserIdentity()
	require.NoError(t, err)
	require.Equal(t, existing.Id(), user.Id())
}

func TestEnsureUserIdentityAmbiguous(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)
	cache, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer cache.Close()

	_, err = cache.Identities().New("Old Name", "testuser@example.com")
	require.NoError(t, err)
	_, err = cache.Identities().New("Older Name", "testuser@example.com")
	require.NoError(t, err)

	// Two candidates, neither named like git's user.name: refuse to guess.
	_, _, err = cache.EnsureUserIdentity()
	require.ErrorContains(t, err, "user adopt")
	set, err := cache.IsUserIdentitySet()
	require.NoError(t, err)
	require.False(t, set)

	// A single candidate with the matching name breaks the tie.
	named, err := cache.Identities().New("testuser", "testuser@example.com")
	require.NoError(t, err)
	i, source, err := cache.EnsureUserIdentity()
	require.NoError(t, err)
	require.Equal(t, UserIdentityAdopted, source)
	require.Equal(t, named.Id(), i.Id())
}

func TestEnsureUserIdentityNoGitUser(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)
	// go-git re-marshals user.* from a typed struct, so unset via git.
	out, err := exec.Command("git", "-C", repo.GetLocalRemote(), "config", "--local", "--unset", "user.email").CombinedOutput()
	require.NoError(t, err, string(out))
	// Keep the developer's own global config out of the picture.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer cache.Close()

	_, _, err = cache.EnsureUserIdentity()
	require.ErrorContains(t, err, "user.email")
	require.Empty(t, cache.Identities().AllIds())
}
