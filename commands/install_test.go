package commands

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/repository"
)

const refspecHint = "remote.origin.fetch lacks the tracker's refspecs; git work install adds them"

// newInstallEnv is a clone whose config runs through the git CLI, as the
// command line's does, bound to a throwaway bare remote that holds one
// identity, so that it can be pulled from.
func newInstallEnv(t *testing.T) (*execenv.Env, string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	remote := repository.CreateGoGitTestRepo(t, true)

	other := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, other.AddRemote("origin", remote.GetLocalRemote()))
	otherCache, err := cache.NewRepoCacheNoEvents(other)
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherCache.Close() })
	you, err := otherCache.Identities().New("Bob", "bob@example.com")
	require.NoError(t, err)
	require.NoError(t, otherCache.SetUserIdentity(you))
	_, err = otherCache.Push("origin")
	require.NoError(t, err)

	raw := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, raw.AddRemote("origin", remote.GetLocalRemote()))
	dir := raw.GetLocalRemote()
	repo := gitcli.WrapRepo(raw, dir)

	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	return &execenv.Env{
		Ctx:     t.Context(),
		Repo:    repo,
		Backend: backend,
		In:      &execenv.TestIn{Buffer: &bytes.Buffer{}},
		Out:     &execenv.TestOut{Buffer: &bytes.Buffer{}},
		Err:     &execenv.TestOut{Buffer: &bytes.Buffer{}},
	}, dir
}

func fetchLines(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "--get-all", "remote.origin.fetch").Output()
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestInstall(t *testing.T) {
	env, dir := newInstallEnv(t)

	require.NoError(t, runInstall(env, nil))
	lines := fetchLines(t, dir)
	require.Equal(t, []string{
		"+refs/heads/*:refs/remotes/origin/*",
		"+refs/work-users/*:refs/remotes/origin/work-users/*",
		"+refs/work-schema/*:refs/remotes/origin/work-schema/*",
		"+refs/work-flows/*:refs/remotes/origin/work-flows/*",
		"+refs/work-issues/*:refs/remotes/origin/work-issues/*",
	}, lines)
	require.Empty(t, env.Out.String())
	require.Empty(t, env.Err.String())

	require.NoError(t, runInstall(env, nil))
	require.Equal(t, lines, fetchLines(t, dir))
}

func TestInstallUnconfiguredRemote(t *testing.T) {
	env, _ := newInstallEnv(t)

	err := runInstall(env, []string{"nosuchremote"})
	require.ErrorAs(t, err, &gitcli.ErrRemoteNotConfigured{})
}

func TestPullHintsRefspecs(t *testing.T) {
	env, _ := newInstallEnv(t)

	require.NoError(t, runPull(env, nil))
	require.Equal(t, refspecHint+"\n", env.Err.String())

	require.NoError(t, runInstall(env, nil))
	env.Err.(*execenv.TestOut).Reset()

	require.NoError(t, runPull(env, nil))
	require.NotContains(t, env.Err.String(), refspecHint)
}

func TestPullNoHintForURL(t *testing.T) {
	env, _ := newInstallEnv(t)

	url, err := env.Repo.AnyConfig().ReadString("remote.origin.url")
	require.NoError(t, err)

	// The hint alone: a pull from a URL fails later, at the fetch, since the
	// remote-tracking refs it would write are named after the remote.
	hintRefspecs(env, url)
	require.Empty(t, env.Err.String())
}
