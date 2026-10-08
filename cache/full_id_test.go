package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// newRelationTestCache is the schema test repository plus a relation, a
// multi-relation and a text field on task, and one task to point at, which
// carries the alias PROJ-1.
func newRelationTestCache(t *testing.T) (*RepoCache, *IssueCache) {
	t.Helper()

	c := newSchemaTestCache(t)
	for key, kind := range map[string]schema.Kind{
		"task/parent": schema.KindRelation,
		"task/blocks": schema.KindMultiRelation,
		"task/note":   schema.KindText,
	} {
		_, _, err := c.Schema().New(config.ShapeField, key, map[string]config.Value{
			schema.AttrKind: config.StringValue(string(kind)),
		})
		require.NoError(t, err)
	}

	target, _, err := c.Issues().NewWithMetadata("target", "", map[string]issue.Value{
		"type": issue.StringValue("task"),
	}, map[string]string{AliasMetadataPrefix + "jira": "PROJ-1"})
	require.NoError(t, err)
	return c, target
}

func newTask(t *testing.T, c *RepoCache, fields map[string]issue.Value) (*IssueCache, error) {
	t.Helper()
	all := map[string]issue.Value{"type": issue.StringValue("task")}
	for k, v := range fields {
		all[k] = v
	}
	i, _, err := c.Issues().New("task", "", all)
	return i, err
}

// ambiguousPrefix creates issues until two share a first hex digit, and
// returns that digit: a prefix naming more than one issue.
func ambiguousPrefix(t *testing.T, c *RepoCache) string {
	t.Helper()
	seen := map[byte]bool{}
	for _, id := range c.Issues().AllIds() {
		seen[id.String()[0]] = true
	}
	for {
		i, err := newTask(t, c, nil)
		require.NoError(t, err)
		first := i.Id().String()[0]
		if seen[first] {
			return string(first)
		}
		seen[first] = true
	}
}

// TestRelationValueIsStoredAsTheFullId: a prefix or an alias of a relation
// is resolved on create, set, add and remove, and on an Update batch, before
// anything is written (2086c12).
func TestRelationValueIsStoredAsTheFullId(t *testing.T) {
	c, target := newRelationTestCache(t)
	full := issue.StringValue(target.Id().String())
	prefix := issue.StringValue(target.Id().Human())
	alias := issue.StringValue("PROJ-1")

	// create
	i, err := newTask(t, c, map[string]issue.Value{
		"parent": prefix,
		"blocks": issue.MustValue([]string{"PROJ-1"}),
		"note":   prefix,
	})
	require.NoError(t, err)
	snap := i.Snapshot()
	require.JSONEq(t, string(full), string(snap.Fields["parent"]))
	require.JSONEq(t, `["`+target.Id().String()+`"]`, string(snap.Fields["blocks"]))
	// a text that reads like a prefix is the text it is
	require.JSONEq(t, string(prefix), string(snap.Fields["note"]))

	// set: the planned operation carries the full id, which is what --dry-run prints
	ops, err := i.PlanSetFields(map[string]issue.Value{"parent": alias})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.JSONEq(t, string(full), string(ops[0].(*issue.SetFieldOperation).Value))

	// add and remove
	other, err := newTask(t, c, nil)
	require.NoError(t, err)
	ops, err = other.PlanAddValues(map[string][]issue.Value{"blocks": {prefix}})
	require.NoError(t, err)
	require.JSONEq(t, string(full), string(ops[0].(*issue.AddValueOperation).Item))
	require.NoError(t, other.CommitOperations(ops))
	ops, err = other.PlanRemoveValues(map[string][]issue.Value{"blocks": {alias}})
	require.NoError(t, err)
	require.JSONEq(t, string(full), string(ops[0].(*issue.RemoveValueOperation).Item))
	require.NoError(t, other.CommitOperations(ops))
	require.JSONEq(t, `[]`, string(other.Snapshot().Fields["blocks"]))

	// an Update batch, the Jira pull's path, with its metadata kept
	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	for _, update := range []func(func(*issue.Snapshot) ([]issue.Operation, error)) error{other.Update, other.UpdateShape} {
		require.NoError(t, update(func(*issue.Snapshot) ([]issue.Operation, error) {
			set := issue.NewSetFieldOp(me, time.Now().Unix(), "parent", prefix)
			set.SetMetadata("origin", "test")
			return []issue.Operation{
				set,
				issue.NewAddValueOp(me, time.Now().Unix(), "blocks", alias),
			}, nil
		}))
		snap = other.Snapshot()
		require.JSONEq(t, string(full), string(snap.Fields["parent"]))
		require.JSONEq(t, `["`+target.Id().String()+`"]`, string(snap.Fields["blocks"]))
	}
	last := snap.Operations[len(snap.Operations)-2]
	origin, ok := last.GetMetadata("origin")
	require.True(t, ok)
	require.Equal(t, "test", origin)
}

// TestRelationValueThatDoesNotResolveIsRefused: an unknown or an ambiguous
// value is refused at planning time, named, and nothing is written.
func TestRelationValueThatDoesNotResolveIsRefused(t *testing.T) {
	c, _ := newRelationTestCache(t)
	ambiguous := ambiguousPrefix(t, c)

	i, err := newTask(t, c, nil)
	require.NoError(t, err)
	before := issueRef(t, c, i)

	for _, value := range []string{"nosuchissue", ambiguous} {
		_, err = newTask(t, c, map[string]issue.Value{"parent": issue.StringValue(value)})
		require.ErrorContains(t, err, `"`+value+`"`)

		_, err = i.PlanSetFields(map[string]issue.Value{"parent": issue.StringValue(value)})
		require.ErrorContains(t, err, `"`+value+`"`)

		_, err = i.PlanAddValues(map[string][]issue.Value{"blocks": {issue.StringValue(value)}})
		require.ErrorContains(t, err, `"`+value+`"`)

		err = i.UpdateShape(func(*issue.Snapshot) ([]issue.Operation, error) {
			me, err := c.GetUserIdentity()
			require.NoError(t, err)
			return []issue.Operation{issue.NewSetFieldOp(me, time.Now().Unix(), "parent", issue.StringValue(value))}, nil
		})
		require.ErrorContains(t, err, `"`+value+`"`)
	}
	require.Equal(t, before, issueRef(t, c, i))
}

// TestRelationRemoveTakesAHeldPrefix: an item stored before 2086c12 as a
// prefix is removed by naming it as it is, rather than resolved away from it.
func TestRelationRemoveTakesAHeldPrefix(t *testing.T) {
	c, target := newRelationTestCache(t)
	prefix := issue.StringValue(target.Id().Human())

	i, err := newTask(t, c, nil)
	require.NoError(t, err)
	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	// what an old binary wrote: the prefix, verbatim, past any resolution
	_, err = issue.AddValue(i.entity, me, time.Now().Unix(), "blocks", prefix, nil)
	require.NoError(t, err)
	require.NoError(t, i.Commit())
	require.JSONEq(t, `[`+string(prefix)+`]`, string(i.Snapshot().Fields["blocks"]))

	ops, err := i.PlanRemoveValues(map[string][]issue.Value{"blocks": {prefix}})
	require.NoError(t, err)
	require.JSONEq(t, string(prefix), string(ops[0].(*issue.RemoveValueOperation).Item))
	require.NoError(t, i.CommitOperations(ops))
	require.JSONEq(t, `[]`, string(i.Snapshot().Fields["blocks"]))
}

// TestRelationWithoutASchema: with no type, no kind is known, so a set is
// written as given; an added item that is the hex prefix of one issue is
// still taken for one, the bootstrap guess.
func TestRelationWithoutASchema(t *testing.T) {
	c, _ := newConfigTestCache(t)

	target, _, err := c.Issues().New("target", "", nil)
	require.NoError(t, err)
	prefix := issue.StringValue(target.Id().Human())

	i, _, err := c.Issues().New("bootstrap", "", map[string]issue.Value{"parent": prefix})
	require.NoError(t, err)
	require.JSONEq(t, string(prefix), string(i.Snapshot().Fields["parent"]))

	ops, err := i.PlanAddValues(map[string][]issue.Value{"blocks": {prefix}, "labels": {issue.StringValue("core")}})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))
	require.JSONEq(t, `["`+target.Id().String()+`"]`, string(i.Snapshot().Fields["blocks"]))
	require.JSONEq(t, `["core"]`, string(i.Snapshot().Fields["labels"]))
}
