package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// newSchemaTestCache gives a repository with one type and two fields,
// which is enough for every rule the write path applies.
func newSchemaTestCache(t *testing.T) *RepoCache {
	t.Helper()

	c, _ := newConfigTestCache(t)

	_, _, err := c.Schema().New(config.ShapeType, "task", map[string]config.Value{
		schema.AttrName:    config.StringValue("Task"),
		schema.AttrOrdinal: config.MustValue(10),
	})
	require.NoError(t, err)

	_, _, err = c.Schema().New(config.ShapeField, "task/status", map[string]config.Value{
		schema.AttrKind: config.StringValue(string(schema.KindEnum)),
		"values/to-do":  config.MustValue(schema.ValueAttr{Ordinal: 10, Category: schema.CategoryUnstarted}),
		"values/done":   config.MustValue(schema.ValueAttr{Ordinal: 20, Category: schema.CategoryCompleted}),
	})
	require.NoError(t, err)

	_, _, err = c.Schema().New(config.ShapeField, "task/labels", map[string]config.Value{
		schema.AttrKind: config.StringValue(string(schema.KindMultiEnum)),
		"values/core":   config.MustValue(schema.ValueAttr{Ordinal: 10}),
	})
	require.NoError(t, err)

	return c
}

func TestLoadSchema(t *testing.T) {
	c := newSchemaTestCache(t)

	s, err := c.LoadSchema()
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	require.False(t, s.Empty())

	task, ok := s.Type("task")
	require.True(t, ok)
	require.Equal(t, "Task", task.Name)
	status, ok := task.Field("status")
	require.True(t, ok)
	require.Equal(t, []string{"to-do", "done"}, status.ValueIds())
}

func TestIssueWritesAreUnvalidatedWithoutASchema(t *testing.T) {
	c, _ := newConfigTestCache(t)

	s, err := c.LoadSchema()
	require.NoError(t, err)
	require.True(t, s.Empty(), "no type entity is the bootstrap state")

	// no type, an unknown field, any value: all of it writes
	i, _, err := c.Issues().New("bootstrap", "", map[string]issue.Value{
		"whatever": issue.MustValue(3),
	})
	require.NoError(t, err)

	ops, err := i.PlanSetFields(map[string]issue.Value{"anything": issue.StringValue("x")})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))
}

func TestIssueWritesAreValidatedAgainstTheSchema(t *testing.T) {
	c := newSchemaTestCache(t)

	// a create needs a known type
	_, _, err := c.Issues().New("no type", "", nil)
	require.ErrorContains(t, err, "type is required")

	_, _, err = c.Issues().New("bad type", "", map[string]issue.Value{
		schema.TypeKey: issue.StringValue("epic"),
	})
	require.ErrorContains(t, err, "not in the schema")

	// a create's fields are checked too, and nothing is written when they fail
	_, _, err = c.Issues().New("bad status", "", map[string]issue.Value{
		schema.TypeKey: issue.StringValue("task"),
		"status":       issue.StringValue("shipped"),
	})
	require.ErrorContains(t, err, "valid values: to-do, done")
	require.Empty(t, c.Issues().AllIds())

	i, _, err := c.Issues().New("a task", "", map[string]issue.Value{
		schema.TypeKey: issue.StringValue("task"),
		"status":       issue.StringValue("to-do"),
	})
	require.NoError(t, err)

	// a set is checked at planning time, so --dry-run reports it too
	_, err = i.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("shipped")})
	require.ErrorContains(t, err, "valid values: to-do, done")

	_, err = i.PlanSetFields(map[string]issue.Value{"nope": issue.StringValue("x")})
	require.ErrorContains(t, err, "is not a field of type task")

	ops, err := i.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("done")})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))

	// items go through the same checks
	_, err = i.PlanAddValues(map[string][]issue.Value{"labels": {issue.StringValue("nope")}})
	require.ErrorContains(t, err, "not in the schema")

	ops, err = i.PlanAddValues(map[string][]issue.Value{"labels": {issue.StringValue("core")}})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))
}

func TestSchemaEntriesSkipArchived(t *testing.T) {
	c := newSchemaTestCache(t)

	labels, err := c.Schema().ResolveKey(config.ShapeField, "task/labels")
	require.NoError(t, err)
	_, err = labels.SetArchived(true)
	require.NoError(t, err)
	require.NoError(t, labels.Commit())

	s, err := c.LoadSchema()
	require.NoError(t, err)
	task, _ := s.Type("task")
	_, ok := task.Field("labels")
	require.False(t, ok, "an archived field stops being settable (D6)")
}
