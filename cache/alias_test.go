package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
)

// newAliased creates a task carrying alias:jira = key.
func newAliased(t *testing.T, c *RepoCache, key string) *IssueCache {
	t.Helper()
	i, _, err := c.Issues().NewWithMetadata("copy of "+key, "", map[string]issue.Value{
		"type": issue.StringValue("task"),
	}, map[string]string{AliasMetadataPrefix + "jira": key})
	require.NoError(t, err)
	return i
}

// consolidate stamps loser's create operation as the Jira sync does (JS25).
func consolidate(t *testing.T, c *RepoCache, loser, winner *IssueCache) {
	t.Helper()
	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	require.NoError(t, loser.UpdateShape(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{
			issue.NewSetMetadataOp(me, time.Now().Unix(), snap.Operations[0].Id(),
				map[string]string{ConsolidatedIntoMetadata: winner.Id().String()}),
		}, nil
	}))
}

// TestAliasNamesTheSurvivor: once a sync has consolidated two copies of one
// Jira issue, the key they both carry names the survivor, on every resolve
// path; before that it is ambiguous (doc/design/alias-ids.md A6).
func TestAliasNamesTheSurvivor(t *testing.T) {
	c := newSchemaTestCache(t)
	winner := newAliased(t, c, "PROJ-7")
	loser := newAliased(t, c, "PROJ-7")

	_, err := c.Issues().ResolveAlias("PROJ-7")
	var multiple *entity.ErrMultipleMatch
	require.ErrorAs(t, err, &multiple, "two unconsolidated copies are ambiguous")

	consolidate(t, c, loser, winner)

	i, err := c.Issues().ResolveAlias("PROJ-7")
	require.NoError(t, err)
	require.Equal(t, winner.Id(), i.Id())

	i, err = c.Issues().ResolvePrefixOrAlias("PROJ-7")
	require.NoError(t, err)
	require.Equal(t, winner.Id(), i.Id())

	excerpt, err := c.Issues().ResolveExcerptPrefixOrAlias("PROJ-7")
	require.NoError(t, err)
	require.Equal(t, winner.Id(), excerpt.Id())

	// the loser is still reached by its own id
	i, err = c.Issues().ResolvePrefixOrAlias(loser.Id().Human())
	require.NoError(t, err)
	require.Equal(t, loser.Id(), i.Id())
}

// TestAliasOfOnlyStampedCopies: a stamped copy whose survivor is gone from
// this clone (a local rm) is still what its key names.
func TestAliasOfOnlyStampedCopies(t *testing.T) {
	c := newSchemaTestCache(t)
	winner := newAliased(t, c, "PROJ-8")
	loser := newAliased(t, c, "PROJ-8")
	consolidate(t, c, loser, winner)
	require.NoError(t, c.Issues().Remove(winner.Id().String()))

	i, err := c.Issues().ResolveAlias("PROJ-8")
	require.NoError(t, err)
	require.Equal(t, loser.Id(), i.Id())
}
