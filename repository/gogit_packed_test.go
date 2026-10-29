package repository

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// packedRefsRepo creates a repository holding n refs under refs/entities/,
// each pointing to its own commit, packed into packed-refs as git gc leaves them.
func packedRefsRepo(tb testing.TB, n int) (TestedRepo, map[string]Hash) {
	tb.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git is not on the PATH")
	}

	repo := CreateGoGitTestRepo(tb, false)

	tree, err := repo.StoreTree(nil)
	require.NoError(tb, err)

	want := make(map[string]Hash, n)
	var parent []Hash
	for i := 0; i < n; i++ {
		commit, err := repo.StoreCommit(tree, parent...)
		require.NoError(tb, err)
		parent = []Hash{commit}

		ref := fmt.Sprintf("refs/entities/%08d", i)
		require.NoError(tb, repo.UpdateRef(ref, commit))
		want[ref] = commit
	}

	cmd := exec.Command("git", "pack-refs", "--all")
	cmd.Dir = goGitRepoDir(tb, repo)
	out, err := cmd.CombinedOutput()
	require.NoError(tb, err, string(out))

	return repo, want
}

// TestListRefHashesPacked checks that the one-pass listing sees packed refs,
// and that a loose ref shadows its stale packed copy, as git resolves it.
func TestListRefHashesPacked(t *testing.T) {
	repo, want := packedRefsRepo(t, 50)

	// a loose update over a packed ref: the loose one is current
	moved := want["refs/entities/00000000"]
	require.NoError(t, repo.UpdateRef("refs/entities/00000049", moved))
	want["refs/entities/00000049"] = moved

	got, err := repo.ListRefHashes("refs/entities/")
	require.NoError(t, err)
	require.Equal(t, want, got)

	for ref, hash := range got {
		resolved, err := repo.ResolveRef(ref)
		require.NoError(t, err)
		require.Equal(t, resolved, hash)
	}
}

// BenchmarkResolveNamespace compares resolving every ref of a packed namespace
// one ref at a time, which scans packed-refs per ref (quadratic),
// with ListRefHashes, which reads it once (linear).
//
//	go test ./repository -run '^$' -bench ResolveNamespace
func BenchmarkResolveNamespace(b *testing.B) {
	for _, n := range []int{500, 2000} {
		repo, _ := packedRefsRepo(b, n)

		b.Run(fmt.Sprintf("PerRef/%d", n), func(b *testing.B) {
			for b.Loop() {
				refs, err := repo.ListRefs("refs/entities/")
				require.NoError(b, err)
				for _, ref := range refs {
					_, err := repo.ResolveRef(ref)
					require.NoError(b, err)
				}
			}
		})

		b.Run(fmt.Sprintf("OnePass/%d", n), func(b *testing.B) {
			for b.Loop() {
				refs, err := repo.ListRefHashes("refs/entities/")
				require.NoError(b, err)
				require.Len(b, refs, n)
			}
		})
	}
}
