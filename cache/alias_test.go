package cache

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
)

// The test system's hook reads two create-op keys: the external issue a copy
// is linked to, and the second it first reached the system. A copy with
// neither never reached it.
const (
	testLinked  = "test-linked"
	testReached = "test-reached"
)

func testReach(snap *issue.Snapshot) (Reach, bool) {
	at, ok := snap.GetCreateMetadata(testReached)
	if !ok {
		return Reach{}, false
	}
	sec, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return Reach{}, false
	}
	linked, _ := snap.GetCreateMetadata(testLinked)
	return Reach{Issue: linked, At: time.Unix(sec, 0)}, true
}

// The test system is the jira namespace's; no other namespace has a hook.
func init() {
	RegisterReach("jira", 1, testReach)
}

// newAliased creates a task carrying alias:jira = key, with no contact.
func newAliased(t *testing.T, c *RepoCache, key string) *IssueCache {
	t.Helper()
	return newCopy(t, c, key, "", 0)
}

// newCopy creates a task carrying alias:jira = key, linked to the external
// issue linked and first reaching it at the second reached, when not zero.
func newCopy(t testing.TB, c *RepoCache, key, linked string, reached int64) *IssueCache {
	t.Helper()
	return newCopyIn(t, c, "jira", key, linked, reached)
}

// newCopyIn is newCopy in an alias namespace of its own.
func newCopyIn(t testing.TB, c *RepoCache, namespace, key, linked string, reached int64) *IssueCache {
	t.Helper()
	md := map[string]string{AliasMetadataPrefix + namespace: key}
	if reached != 0 {
		md[testLinked] = linked
		md[testReached] = strconv.FormatInt(reached, 10)
	}
	i, _, err := c.Issues().NewWithMetadata("copy of "+key, "", map[string]issue.Value{
		"type": issue.StringValue("task"),
	}, md)
	require.NoError(t, err)
	return i
}

// archive archives a copy, as consolidation does to the one it does not keep:
// alias resolution reads the archive only when no copy reached the system,
// and the consolidation stamp never.
func archive(t *testing.T, c *RepoCache, loser *IssueCache) {
	t.Helper()
	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	require.NoError(t, loser.UpdateShape(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{
			issue.NewSetFieldOp(me, time.Now().Unix(), "archived", issue.MustValue(true)),
			issue.NewSetMetadataOp(me, time.Now().Unix(), snap.Operations[0].Id(),
				map[string]string{"test-consolidated-into": "x"}),
		}, nil
	}))
}

func requireAmbiguous(t *testing.T, err error, ids ...entity.Id) {
	t.Helper()
	var multiple *entity.ErrMultipleMatch
	require.ErrorAs(t, err, &multiple)
	require.ElementsMatch(t, ids, multiple.Matching)
	for _, id := range ids {
		require.Contains(t, err.Error(), id.String())
	}
}

// TestAliasNamesTheFirstToReach: of two copies of one external issue, the
// key names the one that reached the system first, before any consolidation
// and after it, on every resolve path
// (doc/design/cli.md, "The copy that reached the system first is canonical").
func TestAliasNamesTheFirstToReach(t *testing.T) {
	c := newSchemaTestCache(t)
	later := newCopy(t, c, "PROJ-7", "10007", 200)
	first := newCopy(t, c, "PROJ-7", "10007", 100)

	check := func() {
		i, err := c.Issues().ResolveAlias("PROJ-7")
		require.NoError(t, err)
		require.Equal(t, first.Id(), i.Id())

		i, err = c.Issues().ResolvePrefixOrAlias("PROJ-7")
		require.NoError(t, err)
		require.Equal(t, first.Id(), i.Id())

		excerpt, err := c.Issues().ResolveExcerptPrefixOrAlias("PROJ-7")
		require.NoError(t, err)
		require.Equal(t, first.Id(), excerpt.Id())

		id, err := c.Issues().FirstReached("jira", []entity.Id{later.Id(), first.Id()})
		require.NoError(t, err)
		require.Equal(t, first.Id(), id, "the rule consolidation calls")
	}
	check()
	archive(t, c, later)
	check()

	// the other copy is still reached by its own id
	i, err := c.Issues().ResolvePrefixOrAlias(later.Id().Human())
	require.NoError(t, err)
	require.Equal(t, later.Id(), i.Id())
}

// TestAliasTieGoesToTheLowerId: two copies that reached the system at one
// second, at one rank, are ordered by entity id.
func TestAliasTieGoesToTheLowerId(t *testing.T) {
	c := newSchemaTestCache(t)
	a := newCopy(t, c, "PROJ-9", "10009", 100)
	b := newCopy(t, c, "PROJ-9", "10009", 100)

	i, err := c.Issues().ResolveAlias("PROJ-9")
	require.NoError(t, err)
	require.Equal(t, min(a.Id(), b.Id()), i.Id())
}

// TestAliasUnorderable: copies the rule cannot order are ambiguous, and the
// error names every one: copies that never reached the system, copies of a
// namespace with no hook, and copies linked to two external issues.
func TestAliasUnorderable(t *testing.T) {
	t.Run("never reached", func(t *testing.T) {
		c := newSchemaTestCache(t)
		a := newAliased(t, c, "PROJ-7")
		b := newAliased(t, c, "PROJ-7")
		_, err := c.Issues().ResolvePrefixOrAlias("PROJ-7")
		requireAmbiguous(t, err, a.Id(), b.Id())
		_, err = c.Issues().ResolveExcerptPrefixOrAlias("PROJ-7")
		requireAmbiguous(t, err, a.Id(), b.Id())
	})
	t.Run("no hook", func(t *testing.T) {
		c := newSchemaTestCache(t)
		a := newCopyIn(t, c, "linear", "ENG-7", "10007", 100)
		b := newCopyIn(t, c, "linear", "ENG-7", "10007", 200)
		_, err := c.Issues().ResolveAlias("ENG-7")
		requireAmbiguous(t, err, a.Id(), b.Id())
	})
	t.Run("two external issues", func(t *testing.T) {
		c := newSchemaTestCache(t)
		a := newCopy(t, c, "PROJ-7", "10007", 100)
		b := newCopy(t, c, "PROJ-7", "10070", 200)
		_, err := c.Issues().ResolveAlias("PROJ-7")
		requireAmbiguous(t, err, a.Id(), b.Id())
	})
}

// TestAliasArchiveBreaksATie: of copies none of which reached the system,
// the one copy not archived is first, for alias resolution and the rule
// consolidation calls; with none or two left it is ambiguous
// (doc/design/cli.md, "Archiving breaks a tie nothing else orders").
func TestAliasArchiveBreaksATie(t *testing.T) {
	c := newSchemaTestCache(t)
	a := newAliased(t, c, "PROJ-7")
	b := newAliased(t, c, "PROJ-7")
	d := newAliased(t, c, "PROJ-7")
	ids := []entity.Id{a.Id(), b.Id(), d.Id()}

	archive(t, c, b)
	_, err := c.Issues().ResolveAlias("PROJ-7")
	requireAmbiguous(t, err, ids...)
	_, err = c.Issues().FirstReached("jira", ids)
	requireAmbiguous(t, err, ids...)

	archive(t, c, d)
	i, err := c.Issues().ResolveAlias("PROJ-7")
	require.NoError(t, err)
	require.Equal(t, a.Id(), i.Id())
	first, err := c.Issues().FirstReached("jira", ids)
	require.NoError(t, err)
	require.Equal(t, a.Id(), first)

	archive(t, c, a)
	_, err = c.Issues().ResolveAlias("PROJ-7")
	requireAmbiguous(t, err, ids...)
}

// TestAliasReachedBeatsHandSet:a copy whose alias was set by hand and never
// reached the system loses to the copy that did.
func TestAliasReachedBeatsHandSet(t *testing.T) {
	c := newSchemaTestCache(t)
	newAliased(t, c, "PROJ-7")
	synced := newCopy(t, c, "PROJ-7", "10007", 100)

	i, err := c.Issues().ResolveAlias("PROJ-7")
	require.NoError(t, err)
	require.Equal(t, synced.Id(), i.Id())
}

// TestAliasOfTheOnlyCopy: a copy whose keeper is gone from this clone
// (a local rm) is what its key names.
func TestAliasOfTheOnlyCopy(t *testing.T) {
	c := newSchemaTestCache(t)
	first := newCopy(t, c, "PROJ-8", "10008", 100)
	later := newCopy(t, c, "PROJ-8", "10008", 200)
	archive(t, c, later)
	require.NoError(t, c.Issues().Remove(first.Id().String()))

	i, err := c.Issues().ResolveAlias("PROJ-8")
	require.NoError(t, err)
	require.Equal(t, later.Id(), i.Id())
}
