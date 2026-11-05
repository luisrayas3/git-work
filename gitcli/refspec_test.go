package gitcli

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/repository"
)

var trackerNamespaces = []string{"work-users", "work-schema", "work-flows", "work-issues"}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestAddFetchRefspecs(t *testing.T) {
	rawA, _, _, a, _ := setupRemote(t)
	dir := rawA.GetLocalRemote()
	specs := a.(FetchRefspecs)

	custom := "+refs/notes/*:refs/remotes/origin/notes/*"
	gitConfig(t, dir, "--add", "remote.origin.fetch", custom)
	before := configValues(t, dir, "remote.origin.fetch")

	missing, err := specs.MissingFetchRefspecs("origin", trackerNamespaces...)
	require.NoError(t, err)
	require.Equal(t, []string{
		"+refs/work-users/*:refs/remotes/origin/work-users/*",
		"+refs/work-schema/*:refs/remotes/origin/work-schema/*",
		"+refs/work-flows/*:refs/remotes/origin/work-flows/*",
		"+refs/work-issues/*:refs/remotes/origin/work-issues/*",
	}, missing)

	require.NoError(t, specs.AddFetchRefspecs("origin", trackerNamespaces...))

	// the four lines come after the ones already there, which are untouched
	after := configValues(t, dir, "remote.origin.fetch")
	require.Equal(t, append(before, missing...), after)
	require.Contains(t, after, custom)
	require.Contains(t, after, "+refs/heads/*:refs/remotes/origin/*")

	missing, err = specs.MissingFetchRefspecs("origin", trackerNamespaces...)
	require.NoError(t, err)
	require.Empty(t, missing)

	// a second run adds nothing
	require.NoError(t, specs.AddFetchRefspecs("origin", trackerNamespaces...))
	require.Equal(t, after, configValues(t, dir, "remote.origin.fetch"))
}

func TestAddFetchRefspecsSomeMissing(t *testing.T) {
	rawA, _, _, a, _ := setupRemote(t)
	dir := rawA.GetLocalRemote()
	specs := a.(FetchRefspecs)

	gitConfig(t, dir, "--add", "remote.origin.fetch", TrackerRefspec("origin", "work-issues"))

	missing, err := specs.MissingFetchRefspecs("origin", trackerNamespaces...)
	require.NoError(t, err)
	require.Len(t, missing, 3)
	require.NotContains(t, missing, TrackerRefspec("origin", "work-issues"))

	require.NoError(t, specs.AddFetchRefspecs("origin", trackerNamespaces...))
	after := configValues(t, dir, "remote.origin.fetch")
	require.Len(t, after, 5)
}

func TestFetchRefspecsUnconfiguredRemote(t *testing.T) {
	rawA, _, remote, a, _ := setupRemote(t)
	specs := a.(FetchRefspecs)

	require.NoError(t, a.(Remotes).CheckRemote("origin"))
	for _, name := range []string{"nosuchremote", remote.GetLocalRemote()} {
		err := a.(Remotes).CheckRemote(name)
		require.ErrorAs(t, err, &ErrRemoteNotConfigured{})
		require.ErrorContains(t, err, "git remote add NAME URL")

		_, err = specs.MissingFetchRefspecs(name, trackerNamespaces...)
		require.ErrorAs(t, err, &ErrRemoteNotConfigured{})

		err = specs.AddFetchRefspecs(name, trackerNamespaces...)
		require.ErrorAs(t, err, &ErrRemoteNotConfigured{})
	}

	// nothing was written
	out, err := exec.Command("git", "-C", rawA.GetLocalRemote(), "config", "--get-regexp", `^remote\.`).Output()
	require.NoError(t, err)
	require.NotContains(t, string(out), "nosuchremote")
	require.NotContains(t, string(out), "work-")
}

// TestPruneKeepsTracker: after the refspecs are added, git's own prune keeps
// a live tracker ref and drops only one the remote deleted. Without them it
// drops both, which is why install exists.
func TestPruneKeepsTracker(t *testing.T) {
	rawA, rawB, remote, a, b := setupRemote(t)
	dirA := rawA.GetLocalRemote()

	commitRef(t, rawB, "refs/work-issues/live", "live")
	commitRef(t, rawB, "refs/work-issues/gone", "gone")
	_, err := b.PushRefs("origin", "work-issues")
	require.NoError(t, err)

	fetch := func() {
		_, err := a.FetchRefs("origin", "work-issues")
		require.NoError(t, err)
	}
	resolves := func(ref string) bool {
		_, err := rawA.ResolveRef(ref)
		if err == repository.ErrNotFound {
			return false
		}
		require.NoError(t, err)
		return true
	}
	const live = "refs/remotes/origin/work-issues/live"
	const gone = "refs/remotes/origin/work-issues/gone"

	// Without the refspecs, a prune reads the tracker as stale branches.
	fetch()
	require.True(t, resolves(live))
	gitRun(t, dirA, "remote", "prune", "origin")
	require.False(t, resolves(live))
	require.False(t, resolves(gone))

	require.NoError(t, a.(FetchRefspecs).AddFetchRefspecs("origin", trackerNamespaces...))
	fetch()
	require.True(t, resolves(live))
	require.True(t, resolves(gone))

	gitRun(t, remote.GetLocalRemote(), "update-ref", "-d", "refs/work-issues/gone")

	gitRun(t, dirA, "remote", "prune", "origin")
	require.True(t, resolves(live))
	require.False(t, resolves(gone))

	// git fetch --prune behaves the same
	gitRun(t, dirA, "fetch", "--prune", "origin")
	require.True(t, resolves(live))
}
