package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/repository"
)

// newSyncEnv is a clone bound to a throwaway bare remote, plus a second clone
// of that remote to play the other side with.
func newSyncEnv(t *testing.T) (*execenv.Env, *cache.RepoCache) {
	t.Helper()

	env := execenv.NewTestEnv(t)
	remote := repository.CreateGoGitTestRepo(t, true)
	require.NoError(t, env.Repo.(repository.TestedRepo).AddRemote("origin", remote.GetLocalRemote()))

	me, err := env.Backend.Identities().New("Alice", "alice@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(me))

	other := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, other.AddRemote("origin", remote.GetLocalRemote()))
	otherCache, err := cache.NewRepoCacheNoEvents(other)
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherCache.Close() })

	you, err := otherCache.Identities().New("Bob", "bob@example.com")
	require.NoError(t, err)
	require.NoError(t, otherCache.SetUserIdentity(you))

	// a remote with no ref at all cannot be fetched from, so the other side
	// publishes its identity before any of this starts
	_, err = otherCache.Push("origin")
	require.NoError(t, err)

	return env, otherCache
}

func TestSyncPullsThenPushes(t *testing.T) {
	env, other := newSyncEnv(t)

	// the other side publishes an issue
	_, _, err := other.Issues().New("from the other side", "message", nil)
	require.NoError(t, err)
	_, err = other.Push("origin")
	require.NoError(t, err)

	// this side has one of its own, unpublished
	mine, _, err := env.Backend.Issues().New("mine", "message", nil)
	require.NoError(t, err)

	require.NoError(t, runSync(env, syncOptions{format: "json"}, nil))
	require.Contains(t, env.Out.String(), "Fetching remote")

	// the pull brought the other side's issue in
	require.Len(t, env.Backend.Issues().AllIds(), 2)

	// and the push took ours out
	require.NoError(t, other.Pull("origin"))
	require.Contains(t, other.Issues().AllIds(), mine.Id())
}

func TestSyncDryRunNeedsJira(t *testing.T) {
	env, _ := newSyncEnv(t)

	err := runSync(env, syncOptions{dryRun: true, format: "json"}, nil)
	require.ErrorContains(t, err, "--dry-run")
	require.ErrorContains(t, err, "--jira")

	// nothing ran: not even the pull
	require.Empty(t, env.Out.String())
}

// TestSyncJiraFailureStillPushes: the tracker is published whatever Jira did,
// and the failure comes back as the command's error.
func TestSyncJiraFailureStillPushes(t *testing.T) {
	env, other := newSyncEnv(t)

	mine, _, err := env.Backend.Issues().New("mine", "message", nil)
	require.NoError(t, err)

	// no git-work.jira.url: the jira step fails at once, and that is the
	// error the command ends on
	err = runSync(env, syncOptions{jira: true, format: "json"}, nil)
	require.ErrorContains(t, err, host.JiraURLKey)

	require.NoError(t, other.Pull("origin"))
	require.Contains(t, other.Issues().AllIds(), mine.Id())
}

func TestSyncOneRemoteOnly(t *testing.T) {
	env, _ := newSyncEnv(t)

	err := runSync(env, syncOptions{format: "json"}, []string{"origin", "other"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "one remote"))
}
