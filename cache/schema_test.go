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
func newSchemaTestCache(t testing.TB) *RepoCache {
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

// An archived type, field or value is in the schema, marked, and refuses
// only a write that introduces it: an issue holding it keeps it, is edited,
// clears it and moves off it (doc/design/schema.md, "Archiving refuses only
// what is new").
func TestArchiveRefusesOnlyWhatIsNew(t *testing.T) {
	c := newSchemaTestCache(t)
	_, _, err := c.Schema().New(config.ShapeType, "bug", map[string]config.Value{
		schema.AttrOrdinal: config.MustValue(20),
	})
	require.NoError(t, err)
	_, _, err = c.Schema().New(config.ShapeField, "task/size", map[string]config.Value{
		schema.AttrKind: config.StringValue(string(schema.KindNumber)),
	})
	require.NoError(t, err)

	holder, _, err := c.Issues().New("holds them all", "", map[string]issue.Value{
		schema.TypeKey: issue.StringValue("task"),
		"status":       issue.StringValue("done"),
		"labels":       issue.MustValue([]string{"core"}),
		"size":         issue.MustValue(3),
	})
	require.NoError(t, err)
	bug, _, err := c.Issues().New("a bug", "", map[string]issue.Value{schema.TypeKey: issue.StringValue("bug")})
	require.NoError(t, err)
	fresh, _, err := c.Issues().New("holds none", "", map[string]issue.Value{schema.TypeKey: issue.StringValue("task")})
	require.NoError(t, err)

	archive := func(shape config.Shape, key string) {
		e, err := c.Schema().ResolveKey(shape, key)
		require.NoError(t, err)
		_, err = e.SetArchived(true)
		require.NoError(t, err)
		require.NoError(t, e.Commit())
	}
	status, err := c.Schema().ResolveKey(config.ShapeField, "task/status")
	require.NoError(t, err)
	require.NoError(t, status.Update(map[string]config.Value{
		"values/done": config.MustValue(schema.ValueAttr{Ordinal: 20, Category: schema.CategoryCompleted, Archived: true}),
	}, nil))
	labels, err := c.Schema().ResolveKey(config.ShapeField, "task/labels")
	require.NoError(t, err)
	require.NoError(t, labels.Update(map[string]config.Value{
		"values/core": config.MustValue(schema.ValueAttr{Ordinal: 10, Archived: true}),
		"values/cli":  config.MustValue(schema.ValueAttr{Ordinal: 20}),
	}, nil))
	archive(config.ShapeField, "task/size")
	archive(config.ShapeType, "bug")

	s, err := c.LoadSchema()
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	require.Equal(t, []string{"task"}, s.TypeKeys(), "nothing offers an archived type")
	require.Equal(t, []string{"task", "bug"}, s.AllTypeKeys())
	require.True(t, s.Types["bug"].Archived)
	task, _ := s.Type("task")
	size, ok := task.Field("size")
	require.True(t, ok, "an archived field reads")
	require.True(t, size.Archived)
	require.NotContains(t, task.FieldKeys(), "size")
	done, ok := task.Fields["status"].Value("done")
	require.True(t, ok)
	require.True(t, done.Archived)
	require.Equal(t, 20, done.Ordinal, "an archived value keeps its place")
	require.Equal(t, schema.CategoryCompleted, done.Category)
	require.Equal(t, []string{"to-do"}, task.Fields["status"].ValueIds())

	refused := func(err error, contains string) {
		t.Helper()
		require.ErrorContains(t, err, contains)
	}
	plan := func(i *IssueCache, fields map[string]issue.Value) error {
		_, err := i.PlanSetFields(fields)
		return err
	}

	// introducing: refused, naming what is accepted
	refused(plan(fresh, map[string]issue.Value{"status": issue.StringValue("done")}), `"done" is archived; valid values: to-do`)
	_, err = fresh.PlanAddValues(map[string][]issue.Value{"labels": {issue.StringValue("core")}})
	refused(err, `"core" is archived; valid values: cli`)
	refused(plan(fresh, map[string]issue.Value{"size": issue.MustValue(2)}), "archived on task; its fields: ")
	refused(plan(fresh, map[string]issue.Value{schema.TypeKey: issue.StringValue("bug")}), `"bug" is archived; valid values: task`)
	_, _, err = c.Issues().New("new bug", "", map[string]issue.Value{schema.TypeKey: issue.StringValue("bug")})
	refused(err, `"bug" is archived`)
	_, _, err = c.Issues().New("new done", "", map[string]issue.Value{
		schema.TypeKey: issue.StringValue("task"), "status": issue.StringValue("done")})
	refused(err, `"done" is archived`)
	refused(holder.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{issue.NewSetFieldOp(snap.Author, 1, "size", issue.MustValue(4))}, nil
	}), "archived on task")

	// keeping, editing around, clearing and moving off: accepted
	require.NoError(t, plan(holder, map[string]issue.Value{"status": issue.StringValue("done")}))
	require.NoError(t, plan(holder, map[string]issue.Value{"size": issue.MustValue(3)}))
	require.NoError(t, plan(holder, map[string]issue.Value{"labels": issue.MustValue([]string{"core", "cli"})}))
	require.NoError(t, plan(holder, map[string]issue.Value{"size": issue.MustValue(nil)}))
	_, err = holder.PlanRemoveValues(map[string][]issue.Value{"labels": {issue.StringValue("core")}})
	require.NoError(t, err)
	_, err = fresh.PlanRemoveValues(map[string][]issue.Value{"labels": {issue.StringValue("core")}})
	require.NoError(t, err, "a remove introduces nothing")
	require.NoError(t, plan(bug, map[string]issue.Value{schema.TitleKey: issue.StringValue("renamed")}))
	require.NoError(t, plan(bug, map[string]issue.Value{schema.TypeKey: issue.StringValue("task")}))
	ops, err := holder.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("to-do")})
	require.NoError(t, err)
	require.NoError(t, holder.CommitOperations(ops))
	refused(plan(holder, map[string]issue.Value{"status": issue.StringValue("done")}), "archived")

	// an archived entity is in the schema only where no live one holds the key
	_, _, err = c.Schema().New(config.ShapeType, "bug", map[string]config.Value{schema.AttrOrdinal: config.MustValue(30)})
	require.NoError(t, err)
	s, err = c.LoadSchema()
	require.NoError(t, err)
	require.False(t, s.Types["bug"].Archived)
}
