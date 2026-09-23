package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/repository"
)

func newConfigTestCache(t *testing.T) (*RepoCache, repository.ClockedRepo) {
	t.Helper()

	repo := repository.CreateGoGitTestRepo(t, false)

	c, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	author, err := c.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(author))

	return c, repo
}

func TestConfigCache(t *testing.T) {
	c, repo := newConfigTestCache(t)

	var obsSchema, obsFlows observer
	require.NoError(t, c.registerObserver("repotest", config.SchemaTypename, &obsSchema))
	require.NoError(t, c.registerObserver("repotest", config.FlowTypename, &obsFlows))

	field, _, err := c.Schema().New(config.ShapeField, "task/status", map[string]config.Value{
		"kind": config.StringValue("enum-with-category"),
		"name": config.StringValue("Status"),
	})
	require.NoError(t, err)
	require.Len(t, obsSchema.created, 1)
	require.Empty(t, obsFlows.created)

	// the two namespaces are separate subcaches over one entity
	flow, _, err := c.Flows().New(config.ShapeFlow, "board", map[string]config.Value{
		"script": config.StringValue("def board():\n    pass\n"),
	})
	require.NoError(t, err)
	require.Len(t, obsFlows.created, 1)
	require.Len(t, c.Schema().AllIds(), 1)
	require.Len(t, c.Flows().AllIds(), 1)

	// and neither takes the other's shapes
	_, _, err = c.Schema().New(config.ShapeFlow, "other", nil)
	require.Error(t, err)
	_, _, err = c.Flows().New(config.ShapeType, "epic", nil)
	require.Error(t, err)

	// writing an attribute updates the excerpt
	_, err = field.Set("ordinal", config.MustValue(10))
	require.NoError(t, err)
	require.NoError(t, field.Commit())

	excerpt, err := c.Schema().ResolveExcerpt(field.Id())
	require.NoError(t, err)
	ordinal, ok := excerpt.Attribute("ordinal")
	require.True(t, ok)
	require.JSONEq(t, "10", string(ordinal))

	// the excerpt survives a close and a reopen, read from the cache file
	require.NoError(t, c.Close())

	reopened, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	defer reopened.Close()

	excerpt, err = reopened.Schema().ResolveExcerpt(field.Id())
	require.NoError(t, err)
	require.Equal(t, config.ShapeField, excerpt.Shape)
	require.Equal(t, "task/status", excerpt.Key)
	require.False(t, excerpt.Archived)
	name, ok := excerpt.AttributeString("name")
	require.True(t, ok)
	require.Equal(t, "Status", name)

	flowExcerpt, err := reopened.Flows().ResolveExcerpt(flow.Id())
	require.NoError(t, err)
	require.Equal(t, config.ShapeFlow, flowExcerpt.Shape)
	require.Equal(t, "board", flowExcerpt.Key)

	// each namespace has its own cache file, so neither sees the other's ids
	_, err = reopened.Schema().ResolveExcerpt(flow.Id())
	require.Error(t, err)

	// the resolver covers both namespaces, because one Go type serves both
	resolved, err := reopened.resolveConfig(flow.Id())
	require.NoError(t, err)
	require.Equal(t, flow.Id(), resolved.Id())
}

func TestConfigCacheUpdate(t *testing.T) {
	c, _ := newConfigTestCache(t)
	defer c.Close()

	field, _, err := c.Schema().New(config.ShapeField, "task/status", map[string]config.Value{
		"kind":         config.StringValue("enum-with-category"),
		"name":         config.StringValue("Status"),
		"values/to-do": config.MustValue(map[string]interface{}{"name": "To Do", "ordinal": 10}),
		"values/qa":    config.MustValue(map[string]interface{}{"name": "QA", "ordinal": 20}),
	})
	require.NoError(t, err)

	// many changes, one pack and one commit: a renumbered list is atomic
	// inside one entity, which is why ordinals need no fractional scheme (E5)
	err = field.Update(map[string]config.Value{
		"values/to-do":       config.MustValue(map[string]interface{}{"name": "To Do", "ordinal": 10}),
		"values/in-progress": config.MustValue(map[string]interface{}{"name": "In Progress", "ordinal": 20}),
		"name":               config.StringValue("State"),
	}, []string{"values/qa"})
	require.NoError(t, err)
	require.False(t, field.NeedCommit())

	snap := field.Snapshot()
	updatedName, ok := snap.AttributeString("name")
	require.True(t, ok)
	require.Equal(t, "State", updatedName)
	members := snap.Members("values")
	require.Len(t, members, 2)
	require.Contains(t, members, "to-do")
	require.Contains(t, members, "in-progress")
	require.NotContains(t, members, "qa")

	// an empty update writes nothing
	require.NoError(t, field.Update(nil, nil))

	// archiving is the replicated removal, and it hides the entity from Query
	_, err = field.SetArchived(true)
	require.NoError(t, err)
	require.NoError(t, field.Commit())

	require.Empty(t, c.Schema().Query(ConfigQuery{Shape: config.ShapeField}))
	require.Len(t, c.Schema().Query(ConfigQuery{Shape: config.ShapeField, IncludeArchived: true}), 1)

	_, err = c.Schema().Current(config.ShapeField, "task/status")
	require.Error(t, err, "an archived entity is not the current one")
}

func TestConfigCacheDuplicateKeys(t *testing.T) {
	// Two clones defining the same key before either pushes is the one failure
	// the entity boundary does not prevent (E7): the oldest creation wins,
	// deterministically, and the loser is reported rather than merged away.
	c, _ := newConfigTestCache(t)
	defer c.Close()

	first, _, err := c.Schema().New(config.ShapeField, "task/phase", map[string]config.Value{
		"name": config.StringValue("Phase"),
	})
	require.NoError(t, err)

	second, _, err := c.Schema().New(config.ShapeField, "task/phase", map[string]config.Value{
		"name": config.StringValue("Phase, again"),
	})
	require.NoError(t, err)
	require.NotEqual(t, first.Id(), second.Id())

	firstExcerpt, err := c.Schema().ResolveExcerpt(first.Id())
	require.NoError(t, err)
	secondExcerpt, err := c.Schema().ResolveExcerpt(second.Id())
	require.NoError(t, err)
	require.Less(t, firstExcerpt.CreateLamportTime, secondExcerpt.CreateLamportTime)

	current, err := c.Schema().Current(config.ShapeField, "task/phase")
	require.NoError(t, err)
	require.Equal(t, first.Id(), current.Id())

	losers := c.Schema().Duplicates(config.ShapeField, "task/phase")
	require.Len(t, losers, 1)
	require.Equal(t, second.Id(), losers[0].Id())

	// a caller that means to edit one entity is told instead of picking one
	_, err = c.Schema().ResolveKey(config.ShapeField, "task/phase")
	require.Error(t, err)

	// archiving the loser repairs it, and that reaches every clone
	_, err = second.SetArchived(true)
	require.NoError(t, err)
	require.NoError(t, second.Commit())

	require.Empty(t, c.Schema().Duplicates(config.ShapeField, "task/phase"))
	resolved, err := c.Schema().ResolveKey(config.ShapeField, "task/phase")
	require.NoError(t, err)
	require.Equal(t, first.Id(), resolved.Id())

	// no duplicate of a key nobody defined
	require.Empty(t, c.Schema().Duplicates(config.ShapeField, "task/nothing"))
	_, err = c.Schema().Current(config.ShapeField, "task/nothing")
	require.Error(t, err)
}

func TestConfigCacheQuery(t *testing.T) {
	c, _ := newConfigTestCache(t)
	defer c.Close()

	for _, key := range []string{"story", "epic", "task"} {
		_, _, err := c.Schema().New(config.ShapeType, key, map[string]config.Value{
			"name": config.StringValue(key),
		})
		require.NoError(t, err)
	}
	_, _, err := c.Schema().New(config.ShapeField, "task/status", nil)
	require.NoError(t, err)

	types := c.Schema().Query(ConfigQuery{Shape: config.ShapeType})
	require.Len(t, types, 3)
	require.Equal(t, []string{"epic", "story", "task"}, []string{types[0].Key, types[1].Key, types[2].Key})

	require.Equal(t, []string{"epic", "story", "task"}, c.Schema().Keys(config.ShapeType))
	require.Equal(t, []string{"task/status"}, c.Schema().Keys(config.ShapeField))
	require.Len(t, c.Schema().Query(ConfigQuery{}), 4)
	require.Len(t, c.Schema().Query(ConfigQuery{Shape: config.ShapeType, Key: "epic"}), 1)
}

func TestConfigPushPull(t *testing.T) {
	// Config travels with the tracker, and two clones adding two members of
	// the same collection both keep theirs: the entity is the coarse merge
	// unit and one attribute per member is the fine one (E1, E3).
	repoA, repoB, _ := repository.SetupGoGitReposAndRemote(t)

	cacheA := createTestRepoCacheNoEvents(t, repoA)
	cacheB := createTestRepoCacheNoEvents(t, repoB)

	reneA, err := cacheA.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, cacheA.SetUserIdentity(reneA))

	_, err = cacheA.Push("origin")
	require.NoError(t, err)
	require.NoError(t, cacheB.Pull("origin"))

	reneB, err := cacheB.Identities().Resolve(reneA.Id())
	require.NoError(t, err)
	require.NoError(t, cacheB.SetUserIdentity(reneB))

	fieldA, _, err := cacheA.Schema().New(config.ShapeField, "task/status", map[string]config.Value{
		"kind": config.StringValue("enum-with-category"),
	})
	require.NoError(t, err)
	_, _, err = cacheA.Flows().New(config.ShapeFlow, "board", map[string]config.Value{
		"script": config.StringValue("def board():\n    pass\n"),
	})
	require.NoError(t, err)

	_, err = cacheA.Push("origin")
	require.NoError(t, err)
	require.NoError(t, cacheB.Pull("origin"))

	require.Len(t, cacheB.Schema().AllIds(), 1)
	require.Len(t, cacheB.Flows().AllIds(), 1)

	// each clone adds one value to the same field, offline
	_, err = fieldA.Set("values/to-do", config.MustValue(map[string]string{"name": "To Do"}))
	require.NoError(t, err)
	require.NoError(t, fieldA.Commit())

	fieldB, err := cacheB.Schema().Current(config.ShapeField, "task/status")
	require.NoError(t, err)
	_, err = fieldB.Set("values/done", config.MustValue(map[string]string{"name": "Done"}))
	require.NoError(t, err)
	require.NoError(t, fieldB.Commit())

	_, err = cacheA.Push("origin")
	require.NoError(t, err)
	require.NoError(t, cacheB.Pull("origin"))
	_, err = cacheB.Push("origin")
	require.NoError(t, err)
	require.NoError(t, cacheA.Pull("origin"))

	// Both sides now hold both values, and both say so from the cache: B's
	// merge diverged, which is the path where the merge used to leave a stale
	// excerpt behind (see SubCache.MergeAll).
	for _, c := range []*RepoCache{cacheA, cacheB} {
		merged, err := c.Schema().Current(config.ShapeField, "task/status")
		require.NoError(t, err)
		members := merged.Snapshot().Members("values")
		require.Len(t, members, 2)
		require.Contains(t, members, "to-do")
		require.Contains(t, members, "done")
	}
}

func TestConfigNamespacesArePushed(t *testing.T) {
	// Fetch and Push take their ref prefixes from the subcache list, so the
	// two config namespaces travel with no code of their own (E8).
	c, _ := newConfigTestCache(t)
	defer c.Close()

	var namespaces []string
	for _, subcache := range c.subcaches {
		namespaces = append(namespaces, subcache.GetNamespace())
	}
	require.Contains(t, namespaces, config.SchemaNamespace)
	require.Contains(t, namespaces, config.FlowNamespace)
}
