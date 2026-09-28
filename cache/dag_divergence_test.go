package cache

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/repository"
)

// divergence is one unequal fork of one entity: the puller commits `local`
// times, the other clone `remote` times, then the puller pulls.
type divergence struct{ local, remote int }

var divergences = []divergence{
	{1, 1}, {2, 2}, {2, 1},
	// unreadable until dag.read sorted parents first (doc/design/dag-read-order.md)
	{3, 1}, {5, 2}, {1, 2}, {1, 3}, {2, 5},
}

// twoClones gives two caches sharing one identity through a remote.
func twoClones(t *testing.T) (a, b *RepoCache, repoA, repoB repository.TestedRepo) {
	t.Helper()
	ra, rb, _ := repository.SetupGoGitReposAndRemote(t)
	a = createTestRepoCacheNoEvents(t, ra)
	b = createTestRepoCacheNoEvents(t, rb)

	me, err := a.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, a.SetUserIdentity(me))
	_, err = a.Push("origin")
	require.NoError(t, err)
	require.NoError(t, b.Pull("origin"))
	meB, err := b.Identities().Resolve(me.Id())
	require.NoError(t, err)
	require.NoError(t, b.SetUserIdentity(meB))
	return a, b, ra, rb
}

// TestIssueUnequalDivergence: A and B edit one issue concurrently, B more
// often than A; A pulls. The merge must succeed and the merged history must
// read back, from the cache and from git.
func TestIssueUnequalDivergence(t *testing.T) {
	for _, d := range divergences {
		t.Run(fmt.Sprintf("local%d_remote%d", d.local, d.remote), func(t *testing.T) {
			a, b, repoA, _ := twoClones(t)

			i, _, err := a.Issues().New("shared", "", nil)
			require.NoError(t, err)
			_, err = a.Push("origin")
			require.NoError(t, err)
			require.NoError(t, b.Pull("origin"))
			ib, err := b.Issues().Resolve(i.Id())
			require.NoError(t, err)

			set := func(c *IssueCache, key string, n int) {
				ops, err := c.PlanSetFields(map[string]issue.Value{key: issue.StringValue(fmt.Sprint(n))})
				require.NoError(t, err)
				require.NoError(t, c.CommitOperations(ops))
			}
			for n := 1; n <= d.local; n++ {
				set(i, "a", n)
			}
			for n := 1; n <= d.remote; n++ {
				set(ib, "b", n)
			}
			_, err = b.Push("origin")
			require.NoError(t, err)

			require.NoError(t, a.Pull("origin"))

			// the cache's own copy
			ia, err := a.Issues().Resolve(i.Id())
			require.NoError(t, err)
			snap := ia.Snapshot()
			require.Equal(t, issue.StringValue(fmt.Sprint(d.local)), snap.Fields["a"])
			require.Equal(t, issue.StringValue(fmt.Sprint(d.remote)), snap.Fields["b"])

			// straight from git, no cache involved
			fresh, err := issue.Read(repoA, i.Id())
			require.NoError(t, err)
			require.Equal(t, issue.StringValue(fmt.Sprint(d.remote)), fresh.Compile().Fields["b"])
		})
	}
}

// TestBugUnequalDivergence is the same fork on git-bug's own bug entity,
// with git-bug's own cache calls, to show the defect is upstream's.
func TestBugUnequalDivergence(t *testing.T) {
	for _, d := range divergences {
		t.Run(fmt.Sprintf("local%d_remote%d", d.local, d.remote), func(t *testing.T) {
			a, b, _, _ := twoClones(t)

			ba, _, err := a.Bugs().New("shared", "")
			require.NoError(t, err)
			_, err = a.Push("origin")
			require.NoError(t, err)
			require.NoError(t, b.Pull("origin"))
			bb, err := b.Bugs().Resolve(ba.Id())
			require.NoError(t, err)

			for n := 1; n <= d.local; n++ {
				_, _, err = ba.AddComment(fmt.Sprintf("a%d", n))
				require.NoError(t, err)
				require.NoError(t, ba.Commit())
			}
			for n := 1; n <= d.remote; n++ {
				_, _, err = bb.AddComment(fmt.Sprintf("b%d", n))
				require.NoError(t, err)
				require.NoError(t, bb.Commit())
			}
			_, err = b.Push("origin")
			require.NoError(t, err)

			require.NoError(t, a.Pull("origin"))
			got, err := a.Bugs().Resolve(ba.Id())
			require.NoError(t, err)
			require.Len(t, got.Snapshot().Comments, 1+d.local+d.remote)
		})
	}
}
