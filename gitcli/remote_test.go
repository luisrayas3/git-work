package gitcli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func TestPushSkipsPrePushHook(t *testing.T) {
	rawA, _, remote, a, _ := setupRemote(t)

	hooks := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-push"),
		[]byte("#!/bin/sh\necho refused >&2\nexit 1\n"), 0o700))
	gitConfig(t, rawA.GetLocalRemote(), "core.hooksPath", hooks)

	commit := commitRef(t, rawA, "refs/foo/1", "one")

	_, err := a.PushRefs("origin", "foo")
	require.NoError(t, err)

	got, err := remote.ResolveRef("refs/foo/1")
	require.NoError(t, err)
	require.Equal(t, commit, got)
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

// A remote that caps the refs one push may update, the way GitHub's "limit
// how many branches and tags can be updated in a single push" rule does,
// gets the refs it refused in pushes of that size.
func TestPushUnderRefUpdateCap(t *testing.T) {
	rawA, _, remote, a, _ := setupRemote(t)

	hooks := t.TempDir()
	log := filepath.Join(hooks, "log")
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-receive"), []byte(
		"#!/bin/sh\n"+
			"n=$(wc -l | tr -d ' ')\n"+
			"echo \"$n\" >> "+log+"\n"+
			"if [ \"$n\" -gt 2 ]; then\n"+
			"  echo '- Pushes can not update more than 2 branches or tags.' >&2\n"+
			"  exit 1\n"+
			"fi\n"), 0o700))
	gitConfig(t, remote.GetLocalRemote(), "core.hooksPath", hooks)

	want := make(map[string]repository.Hash)
	for i := range 5 {
		ref := fmt.Sprintf("refs/foo/%d", i)
		want[ref] = commitRef(t, rawA, ref, ref)
	}
	want["refs/bar/1"] = commitRef(t, rawA, "refs/bar/1", "bar")

	// A ref already on the remote is not pushed again.
	_, err := a.PushRefs("origin", "bar")
	require.NoError(t, err)

	_, err = a.PushRefs("origin", "foo", "bar")
	require.NoError(t, err)

	for ref, hash := range want {
		got, err := remote.ResolveRef(ref)
		require.NoError(t, err, ref)
		require.Equal(t, hash, got, ref)

		got, err = rawA.ResolveRef("refs/remotes/origin/" + strings.TrimPrefix(ref, "refs/"))
		require.NoError(t, err, ref)
		require.Equal(t, hash, got, ref)
	}

	// The bar push, the refused push of the five that differed, then those five two at a time.
	counts, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "1\n5\n2\n2\n1\n", string(counts))

	out, err := a.PushRefs("origin", "foo", "bar")
	require.NoError(t, err)
	require.NotEmpty(t, out)
}

// What GitHub answered a three-ref push on 2026-10-02, abridged.
const gitHubCapRejection = `git push: remote: error: GH013: Repository rule violations found for refs/work-issues/d3d5.
remote: - Pushes can not update more than 2 branches or tags.
To github.com:chef-robotics/ChefAutonomy.git
 ! [remote rejected]       refs/work-issues/d3d5 -> refs/work-issues/d3d5 (push declined due to repository rule violations)
 ! [remote rejected]       refs/work-issues/21e1 -> refs/work-issues/21e1 (push declined due to repository rule violations)
 ! [remote rejected]       refs/work-issues/c2e7 -> refs/work-issues/c2e7 (push declined due to repository rule violations)
error: failed to push some refs to 'github.com:chef-robotics/ChefAutonomy.git'`

func TestRefUpdateCap(t *testing.T) {
	limit, refused := refUpdateCap(errors.New(gitHubCapRejection))
	require.Equal(t, 2, limit)
	require.Equal(t, []string{
		"refs/work-issues/d3d5:refs/work-issues/d3d5",
		"refs/work-issues/21e1:refs/work-issues/21e1",
		"refs/work-issues/c2e7:refs/work-issues/c2e7",
	}, refused)

	limit, _ = refUpdateCap(errors.New("git push: ! [rejected] refs/foo/1 -> refs/foo/1 (non-fast-forward)"))
	require.Equal(t, 0, limit)
}
