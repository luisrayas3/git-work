package gitcli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/repository"
)

// setupRemote creates two repos sharing one bare remote,
// each wrapped so its transport runs through the git CLI.
func setupRemote(t *testing.T) (rawA, rawB, remote repository.TestedRepo, a, b repository.ClockedRepo) {
	t.Helper()

	isolateGit(t)

	rawA = repository.CreateGoGitTestRepo(t, false)
	rawB = repository.CreateGoGitTestRepo(t, false)
	remote = repository.CreateGoGitTestRepo(t, true)

	require.NoError(t, rawA.AddRemote("origin", remote.GetLocalRemote()))
	require.NoError(t, rawB.AddRemote("origin", remote.GetLocalRemote()))

	a = WrapRepo(rawA, rawA.GetLocalRemote())
	b = WrapRepo(rawB, rawB.GetLocalRemote())
	require.IsType(t, &repo{}, a, "git must be on PATH for these tests")

	return rawA, rawB, remote, a, b
}

// commitRef stores a one-file commit and points ref at it.
func commitRef(t *testing.T, repo repository.TestedRepo, ref, content string) repository.Hash {
	t.Helper()

	blob, err := repo.StoreData([]byte(content))
	require.NoError(t, err)
	tree, err := repo.StoreTree([]repository.TreeEntry{
		{ObjectType: repository.Blob, Hash: blob, Name: "file"},
	})
	require.NoError(t, err)
	commit, err := repo.StoreCommit(tree)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRef(ref, commit))

	return commit
}

func TestPushFetchRefs(t *testing.T) {
	rawA, rawB, remote, a, b := setupRemote(t)

	first := commitRef(t, rawA, "refs/foo/1", "one")

	out, err := a.PushRefs("origin", "foo")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	// The ref reached the remote ...
	got, err := remote.ResolveRef("refs/foo/1")
	require.NoError(t, err)
	require.Equal(t, first, got)

	// ... and the local remote-tracking ref followed it,
	// which git only does when a fetch refspec maps the pushed ref.
	got, err = rawA.ResolveRef("refs/remotes/origin/foo/1")
	require.NoError(t, err)
	require.Equal(t, first, got)

	// Pushing nothing new is not an error.
	out, err = a.PushRefs("origin", "foo")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	out, err = b.FetchRefs("origin", "foo")
	require.NoError(t, err)
	require.NotEmpty(t, out)

	got, err = rawB.ResolveRef("refs/remotes/origin/foo/1")
	require.NoError(t, err)
	require.Equal(t, first, got)

	// A fetch with nothing to transfer reports it the way go-git did.
	out, err = b.FetchRefs("origin", "foo")
	require.NoError(t, err)
	require.Equal(t, upToDate, out)
}

func TestPushFetchSeveralPrefixes(t *testing.T) {
	rawA, rawB, _, a, b := setupRemote(t)

	foo := commitRef(t, rawA, "refs/foo/1", "foo")
	bar := commitRef(t, rawA, "refs/bar/1", "bar")
	// A namespace that is not pushed must not travel.
	commitRef(t, rawA, "refs/other/1", "other")

	_, err := a.PushRefs("origin", "foo", "bar")
	require.NoError(t, err)

	_, err = b.FetchRefs("origin", "foo", "bar")
	require.NoError(t, err)

	got, err := rawB.ResolveRef("refs/remotes/origin/foo/1")
	require.NoError(t, err)
	require.Equal(t, foo, got)

	got, err = rawB.ResolveRef("refs/remotes/origin/bar/1")
	require.NoError(t, err)
	require.Equal(t, bar, got)

	_, err = rawB.ResolveRef("refs/remotes/origin/other/1")
	require.ErrorIs(t, err, repository.ErrNotFound)
}

// The fetch refspec PushRefs needs is passed with -c,
// so the remote's configured refspecs survive untouched on disk.
func TestPushLeavesConfigAlone(t *testing.T) {
	rawA, _, _, a, _ := setupRemote(t)

	before := configValues(t, rawA.GetLocalRemote(), "remote.origin.fetch")

	commitRef(t, rawA, "refs/foo/1", "one")
	_, err := a.PushRefs("origin", "foo")
	require.NoError(t, err)

	require.Equal(t, before, configValues(t, rawA.GetLocalRemote(), "remote.origin.fetch"))
	require.Contains(t, before, "+refs/heads/*:refs/remotes/origin/*")
}

func TestRemoteErrors(t *testing.T) {
	_, _, _, a, _ := setupRemote(t)

	_, err := a.FetchRefs("nosuchremote", "foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git fetch:")

	_, err = a.PushRefs("nosuchremote", "foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git push:")
}

func configValues(t *testing.T, dir, key string) []string {
	t.Helper()

	out, err := exec.Command("git", "-C", dir, "config", "--get-all", key).Output()
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(out)), "\n")
}
