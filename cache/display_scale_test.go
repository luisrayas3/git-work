package cache

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

// aliasedClone is a store shaped like a Jira-bound clone with many copies per
// key: n issues, most of them one of two or three copies of one alias, every
// copy having reached the system, and a packed-refs file holding other refs
// besides the issues'.
type aliasedClone struct {
	dir     string
	ids     []entity.Id
	aliases map[string][]entity.Id
}

func newAliasedClone(tb testing.TB, n, filler int) aliasedClone {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git is not on PATH")
	}
	c := newSchemaTestCache(tb)
	dir := strings.TrimSuffix(c.repo.(repository.TestedRepo).GetLocalRemote(), "/.git")

	clone := aliasedClone{dir: dir, aliases: map[string][]entity.Id{}}
	for k := 0; len(clone.ids) < n; k++ {
		key := fmt.Sprintf("PROJ-%d", k)
		for cp := 0; cp <= k%3 && len(clone.ids) < n; cp++ {
			i := newCopy(tb, c, key, fmt.Sprint(10000+k), int64(100+cp))
			clone.ids = append(clone.ids, i.Id())
			clone.aliases[key] = append(clone.aliases[key], i.Id())
		}
	}
	require.NoError(tb, c.Close())

	// other refs, so that a lookup in packed-refs scans a file the size of a
	// real clone's
	out, err := exec.Command("git", "-C", dir, "for-each-ref", "--count=1", "--format=%(objectname)", "refs/").Output()
	require.NoError(tb, err)
	head := strings.TrimSpace(string(out))
	var stdin strings.Builder
	for i := 0; i < filler; i++ {
		fmt.Fprintf(&stdin, "create refs/filler/%d %s\n", i, head)
	}
	update := exec.Command("git", "-C", dir, "update-ref", "--stdin")
	update.Stdin = strings.NewReader(stdin.String())
	out, err = update.CombinedOutput()
	require.NoError(tb, err, string(out))
	out, err = exec.Command("git", "-C", dir, "pack-refs", "--all").CombinedOutput()
	require.NoError(tb, err, string(out))

	return clone
}

// open opens the clone as a command does, a fresh repository and the cache
// loaded from its file, drawing ids in the jira namespace, and counts every
// issue read from disk.
func (a aliasedClone) open(tb testing.TB, loads *atomic.Int64) *RepoCache {
	tb.Helper()
	repo, err := repository.OpenGoGitRepo(a.dir, "git-bug", nil)
	require.NoError(tb, err)
	c, err := NewRepoCacheNoEvents(repo)
	require.NoError(tb, err)
	c.display.once.Do(func() { c.display.namespace = "jira" })
	countLoads(c, loads)
	return c
}

// countLoads counts every issue the cache reads from disk from now on.
func countLoads(c *RepoCache, loads *atomic.Int64) {
	read := c.Issues().actions.ReadWithResolver
	c.Issues().actions.ReadWithResolver = func(repo repository.ClockedRepo, resolvers entity.Resolvers, id entity.Id) (*issue.Issue, error) {
		loads.Add(1)
		return read(repo, resolvers, id)
	}
}

// TestIssueHumanIdLoadsNoEntity: drawing every issue's id, resolving every
// alias over the excerpts, and ordering every alias's copies read no entity
// from disk, however many issues share an alias
// (doc/design/store.md, "Readers use the excerpts").
func TestIssueHumanIdLoadsNoEntity(t *testing.T) {
	clone := newAliasedClone(t, 60, 100)
	var loads atomic.Int64
	// a first open reconciles the cache file with the packed refs
	require.NoError(t, clone.open(t, &loads).Close())
	loads.Store(0)

	c := clone.open(t, &loads)
	defer c.Close()
	for key, ids := range clone.aliases {
		// the first copy reached the system first
		require.Equal(t, key, c.IssueHumanId(ids[0]))
		for _, id := range ids[1:] {
			require.Equal(t, id.Human(), c.IssueHumanId(id))
		}
		excerpt, err := c.Issues().ResolveExcerptPrefixOrAlias(key)
		require.NoError(t, err)
		require.Equal(t, ids[0], excerpt.Id())
		first, err := c.Issues().FirstReached("jira", ids)
		require.NoError(t, err)
		require.Equal(t, ids[0], first)
	}
	require.Zero(t, loads.Load())
}

// TestExcerptReachIsTheEntitys: what the excerpt carries of each copy's reach
// is what the hook reads off the entity, whether the excerpt was built from
// the entity or read back from the cache file, so ordering copies by the
// excerpts picks what ordering them by the entities would
// (doc/design/store.md, "The excerpt carries the reach").
func TestExcerptReachIsTheEntitys(t *testing.T) {
	c := newSchemaTestCache(t)

	var all []*IssueCache
	all = append(all,
		newCopy(t, c, "PROJ-1", "10001", 200),
		newCopy(t, c, "PROJ-1", "10001", 100),
		newCopy(t, c, "PROJ-2", "10002", 100),
		newCopy(t, c, "PROJ-2", "10020", 300),
		newCopy(t, c, "PROJ-3", "", 100),
		newAliased(t, c, "PROJ-4"),
		newAliased(t, c, "PROJ-4"),
		newCopyIn(t, c, "linear", "ENG-1", "e1", 100),
	)
	plain, err := newTask(t, c, nil)
	require.NoError(t, err)
	all = append(all, plain)
	archive(t, c, all[0])
	archive(t, c, all[5])

	check := func(c *RepoCache) {
		for _, i := range all {
			snap := i.Snapshot()
			want, ok := testReach(snap)
			want.At = want.At.UTC()
			var archived bool
			_ = json.Unmarshal(snap.Fields[issue.ArchivedKey], &archived)

			got, err := c.Issues().reachAll("jira", []entity.Id{i.Id()})
			require.NoError(t, err)
			require.Equal(t, reached{id: i.Id(), reach: want, ok: ok, archived: archived}.withoutReachIfNot(), got[0])
		}
	}
	check(c)

	reopened := reopen(t, c)
	defer reopened.Close()
	check(reopened)
}

// withoutReachIfNot is a copy that never reached the system as reachAll
// reports it: with no reach.
func (r reached) withoutReachIfNot() reached {
	if !r.ok {
		r.reach = Reach{}
	}
	return r
}

// reopen closes a cache and opens its repository afresh, the cache loaded
// from its file.
func reopen(t *testing.T, c *RepoCache) *RepoCache {
	t.Helper()
	dir := strings.TrimSuffix(c.repo.(repository.TestedRepo).GetLocalRemote(), "/.git")
	require.NoError(t, c.Close())
	repo, err := repository.OpenGoGitRepo(dir, "git-bug", nil)
	require.NoError(t, err)
	reopened, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	return reopened
}

// TestCacheFileOfOtherReachHooksIsRebuilt: a cache file built under other
// reach hooks is rebuilt on open, since its excerpts carry their answers
// (doc/design/store.md, "The excerpt carries the reach").
func TestCacheFileOfOtherReachHooksIsRebuilt(t *testing.T) {
	c := newSchemaTestCache(t)
	dir := strings.TrimSuffix(c.repo.(repository.TestedRepo).GetLocalRemote(), "/.git")
	i := newCopy(t, c, "PROJ-1", "10001", 100)

	rebuilt := func() bool {
		repo, err := repository.OpenGoGitRepo(dir, "git-bug", nil)
		require.NoError(t, err)
		reopened, events := NewRepoCache(repo)
		built := false
		for event := range events {
			require.NoError(t, event.Err)
			built = built || event.Event == BuildEventCacheIsBuilt
		}
		excerpt, err := reopened.Issues().ResolveExcerpt(i.Id())
		require.NoError(t, err)
		require.Contains(t, excerpt.Reaches, "jira")
		require.NoError(t, reopened.Close())
		return built
	}

	c.Issues().stamp = "jira@0"
	require.NoError(t, c.Issues().write())
	require.NoError(t, c.Close())
	require.True(t, rebuilt(), "a file of other hooks")
	require.False(t, rebuilt(), "the file it rebuilt")
}

// BenchmarkIssueHumanIdShared draws every issue's id in a clone where most
// issues share a key with one or two copies: what `issue list` and every
// view's first draw pay.
func BenchmarkIssueHumanIdShared(b *testing.B) {
	clone := newAliasedClone(b, 1000, 10000)
	var loads atomic.Int64
	// a first open reconciles the cache file with the packed refs
	require.NoError(b, clone.open(b, &loads).Close())
	loads.Store(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := clone.open(b, &loads)
		for _, id := range clone.ids {
			c.IssueHumanId(id)
		}
		b.StopTimer()
		require.NoError(b, c.Close())
		b.StartTimer()
	}
	b.ReportMetric(float64(loads.Load())/float64(b.N), "loads/op")
}
