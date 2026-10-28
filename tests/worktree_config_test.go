package tests

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/repository"
)

// TestWorktreeConfig opens a repository that enables extensions.worktreeConfig
// and holds a config.worktree,
// which go-git v5 refused as an unsupported extension,
// then writes its config as git would:
// reads see config.worktree, writes land in .git/config.
func TestWorktreeConfig(t *testing.T) {
	dir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	git("init")
	git("config", "extensions.worktreeConfig", "true")
	git("config", "--worktree", "some.key", "val")

	raw, err := repository.OpenGoGitRepo(dir, "git-bug", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	repo := gitcli.WrapRepo(raw, dir)

	val, err := repo.LocalConfig().ReadString("some.key")
	require.NoError(t, err)
	require.Equal(t, "val", val)

	require.NoError(t, repo.LocalConfig().StoreString("other.key", "other"))
	val, err = repo.LocalConfig().ReadString("other.key")
	require.NoError(t, err)
	require.Equal(t, "other", val)
	require.Equal(t, "other", git("config", "--file", ".git/config", "other.key"))

	// `git work user new` stores the new identity's id in the config.
	c, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	me, err := c.Identities().New("Alice", "alice@example.com")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(me))
	require.Equal(t, me.Id().String(), git("config", "--file", ".git/config", "git-bug.identity"))

	// go-git alone refuses this config rather than lose its sections.
	_, err = raw.LocalConfig().ReadString("some.key")
	require.Error(t, err)
}
