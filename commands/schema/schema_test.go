package schemacmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

func newTestEnv(t *testing.T) *execenv.Env {
	t.Helper()
	color.NoColor = true

	env := execenv.NewTestEnv(t)

	i, err := env.Backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(i))

	return env
}

// initJira is the starting point of most of these: the preset, applied.
func initJira(t *testing.T, env *execenv.Env) []string {
	t.Helper()

	env.Out.Reset()
	require.NoError(t, runSchemaInit(env, initOptions{}, nil))

	ids := strings.Fields(env.Out.String())
	env.Out.Reset()
	env.Err.Reset()

	return ids
}

func TestSchemaInit(t *testing.T) {
	env := newTestEnv(t)

	ids := initJira(t, env)

	// seven types, and one entity per (type, field): ids and nothing else
	require.Len(t, env.Backend.Schema().Keys(config.ShapeType), 7)
	require.Equal(t, len(ids), len(env.Backend.Schema().AllIds()))
	for _, id := range ids {
		require.NoError(t, entity.Id(id).Validate(), "a writer prints ids and nothing else")
	}

	s, err := env.Backend.LoadSchema()
	require.NoError(t, err)
	require.Empty(t, s.Problems)

	story, ok := s.Type("story")
	require.True(t, ok)
	status, ok := story.Field("status")
	require.True(t, ok)
	require.Equal(t, schema.KindEnum, status.Kind)
	require.Contains(t, status.ValueIds(), "in-progress")

	// a field belongs to exactly one type: two entities, one key
	_, err = env.Backend.Schema().ResolveKey(config.ShapeField, "story/status")
	require.NoError(t, err)
	_, err = env.Backend.Schema().ResolveKey(config.ShapeField, "task/status")
	require.NoError(t, err)
}

func TestSchemaInitRefusesTwice(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	err := runSchemaInit(env, initOptions{}, nil)
	require.ErrorContains(t, err, "already has")

	err = runSchemaInit(env, initOptions{}, []string{"nope"})
	require.ErrorContains(t, err, "already has")
}

func TestSchemaInitUnknownPreset(t *testing.T) {
	env := newTestEnv(t)

	err := runSchemaInit(env, initOptions{}, []string{"nope"})
	require.ErrorContains(t, err, "shipped presets: jira")
	require.Empty(t, env.Backend.Schema().AllIds())
}

func TestSchemaExportRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	require.NoError(t, runSchemaExport(env, formatOptions{format: "yaml"}))
	exported := env.Out.String()
	env.Out.Reset()

	// what came out is the preset, normalised: same types, same fields
	preset, err := schema.Preset("jira")
	require.NoError(t, err)
	doc, err := schema.ParseDocument([]byte(exported))
	require.NoError(t, err)
	require.Equal(t, preset.Types.Keys(), doc.Types.Keys())
	for _, typeKey := range preset.Types.Keys() {
		want, _ := preset.Types.Get(typeKey)
		got, _ := doc.Types.Get(typeKey)
		require.Equal(t, want.Name, got.Name)
		require.Equal(t, want.Fields.Keys(), got.Fields.Keys(), typeKey)
	}
	require.NotContains(t, exported, "ordinal:", "order is position, never a number in the file")

	// importing it back writes nothing
	before := commitCount(t, env)
	_, err = env.In.(*execenv.TestIn).WriteString(exported)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{}, []string{"-"}))
	require.Equal(t, "", env.Out.String())
	require.Equal(t, before, commitCount(t, env), "export | import emits zero operations")

	// and so does JSON, which is the same document
	require.NoError(t, runSchemaExport(env, formatOptions{format: "json"}))
	asJSON := env.Out.String()
	env.Out.Reset()
	var any map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(asJSON), &any))
	_, err = env.In.(*execenv.TestIn).WriteString(asJSON)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{}, []string{"-"}))
	require.Equal(t, before, commitCount(t, env))
}

func TestSchemaImportChangesOneValue(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	require.NoError(t, runSchemaExport(env, formatOptions{format: "yaml"}))
	exported := env.Out.String()
	env.Out.Reset()

	// rename one value of one field, and nothing else
	edited := strings.Replace(exported, "name: Active", "name: In flight", 1)
	require.NotEqual(t, exported, edited)

	// --dry-run prints the changes and writes nothing
	before := commitCount(t, env)
	_, err := env.In.(*execenv.TestIn).WriteString(edited)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{dryRun: true}, []string{"-"}))

	var changes []schema.Change
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &changes))
	env.Out.Reset()
	require.Len(t, changes, 1, "reconcile emits only what differs")
	require.Equal(t, schema.ActionUpdate, changes[0].Action)
	require.Equal(t, "iteration/status", changes[0].Key)
	require.Len(t, changes[0].Set, 1)
	require.Empty(t, changes[0].Remove)
	require.Equal(t, before, commitCount(t, env))

	// then for real: one entity, one commit, nothing on stdout
	_, err = env.In.(*execenv.TestIn).WriteString(edited)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{}, []string{"-"}))
	require.Equal(t, "", env.Out.String())
	require.Equal(t, before+1, commitCount(t, env))

	s, err := env.Backend.LoadSchema()
	require.NoError(t, err)
	status, ok := s.Field("iteration", "status")
	require.True(t, ok)
	value, ok := status.Value("active")
	require.True(t, ok)
	require.Equal(t, "In flight", value.Name)
	require.Equal(t, []string{"future", "active", "closed"}, status.ValueIds(),
		"an unchanged order keeps its numbers")
}

func TestSchemaImportIsAnUpsert(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	// a partial file: one type, one field, two of its six values
	_, err := env.In.(*execenv.TestIn).WriteString(`
types:
  task:
    name: Task
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, name: To Do, category: unstarted}
          - {id: done,  name: Done,  category: completed}
`)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{}, []string{"-"}))

	s, err := env.Backend.LoadSchema()
	require.NoError(t, err)

	// the entities the file named are the file's
	status, _ := s.Field("task", "status")
	require.Equal(t, []string{"to-do", "done"}, status.ValueIds())
	task, _ := s.Type("task")
	require.Empty(t, task.Description, "an attribute the file drops is removed")

	// everything it did not name is untouched
	require.Equal(t, 7, len(s.TypeKeys()))
	_, ok := s.Field("task", "priority")
	require.True(t, ok, "a partial file never archives what it does not name")
	storyStatus, _ := s.Field("story", "status")
	require.Len(t, storyStatus.Values, 6, "task/status and story/status are two entities")
}

func TestSchemaImportPrune(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	_, err := env.In.(*execenv.TestIn).WriteString(`
types:
  task:
    name: Task
    fields:
      status: {kind: enum, name: Status, values: [{id: to-do, category: unstarted}]}
`)
	require.NoError(t, err)
	require.NoError(t, runSchemaImport(env, importOptions{prune: true}, []string{"-"}))

	s, err := env.Backend.LoadSchema()
	require.NoError(t, err)
	require.Equal(t, []string{"task"}, s.TypeKeys())
	task, _ := s.Type("task")
	require.Equal(t, []string{"title", "type", "archived", "status"}, task.FieldKeys())

	// archived, not deleted: the entities are still there
	require.NotEmpty(t, env.Backend.Schema().Query(cache.ConfigQuery{IncludeArchived: true}))
}

func TestSchemaImportRefusals(t *testing.T) {
	env := newTestEnv(t)

	cases := map[string]string{
		"an unknown kind":     `types: {task: {fields: {status: {kind: enum-with-category}}}}`,
		"an unknown category": `types: {task: {fields: {s: {kind: enum, values: [{id: a, category: shipped}]}}}}`,
		"a dangling target":   `types: {task: {fields: {parent: {kind: relation, target_types: [epic]}}}}`,
		"an unknown key":      `types: {task: {naem: Task}}`,
		"a built-in's kind":   `types: {task: {fields: {title: {kind: number}}}}`,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := env.In.(*execenv.TestIn).WriteString(src)
			require.NoError(t, err)
			require.Error(t, runSchemaImport(env, importOptions{}, []string{"-"}))
			require.Empty(t, env.Backend.Schema().AllIds(), "nothing is written before the whole document is valid")
		})
	}
}

func TestSchemaLog(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	require.NoError(t, runSchemaLog(env, logOptions{format: "json"}, []string{"task/status"}))

	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.NotEmpty(t, lines)

	var first map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.Equal(t, "create", first["type"])
	require.Equal(t, "field", first["shape"])
	require.Equal(t, "task/status", first["key"])
	require.Equal(t, "John Doe", first["author"].(map[string]interface{})["name"])

	// a key that is neither a type nor a field
	require.Error(t, runSchemaLog(env, logOptions{format: "json"}, []string{"nope"}))
}

func TestSchemaArchiveAndRm(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	// archive is the replicated removal
	require.NoError(t, runSchemaArchive(env, []string{"task/estimate"}))
	require.Equal(t, "", env.Out.String())

	s, err := env.Backend.LoadSchema()
	require.NoError(t, err)
	_, ok := s.Field("task", "estimate")
	require.False(t, ok)

	// archiving a type says what it leaves behind
	env.Err.Reset()
	require.NoError(t, runSchemaArchive(env, []string{"bug"}))
	require.Contains(t, env.Err.String(), "still live on the archived type bug")

	// rm is local, and prints nothing
	before := len(env.Backend.Schema().AllIds())
	require.NoError(t, runSchemaRm(env, []string{"task/due"}))
	require.Equal(t, "", env.Out.String())
	require.Equal(t, before-1, len(env.Backend.Schema().AllIds()))

	require.Error(t, runSchemaRm(env, []string{"task/due"}))
}

func TestSchemaValidatesIssueWrites(t *testing.T) {
	env := newTestEnv(t)
	initJira(t, env)

	// a type is required, and it has to be one of the preset's
	_, _, err := env.Backend.Issues().New("no type", "", nil)
	require.ErrorContains(t, err, "type is required")

	_, _, err = env.Backend.Issues().New("bad type", "", map[string]issue.Value{
		"type": issue.StringValue("epik"),
	})
	require.ErrorContains(t, err, "not in the schema")

	i, _, err := env.Backend.Issues().New("a task", "", map[string]issue.Value{
		"type":   issue.StringValue("task"),
		"status": issue.StringValue("to-do"),
	})
	require.NoError(t, err)

	// an unknown status names the valid ids
	_, err = i.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("shipped")})
	require.ErrorContains(t, err, "valid values: backlog, to-do, in-progress, in-review, done, canceled")

	// a valid one goes through
	ops, err := i.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("done")})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))

	// a relation's target type is enforced
	epic, _, err := env.Backend.Issues().New("an epic", "", map[string]issue.Value{
		"type": issue.StringValue("epic"),
	})
	require.NoError(t, err)
	sprint, _, err := env.Backend.Issues().New("a sprint", "", map[string]issue.Value{
		"type": issue.StringValue("iteration"),
	})
	require.NoError(t, err)

	ops, err = i.PlanSetFields(map[string]issue.Value{"parent": issue.StringValue(epic.Id().String())})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))

	_, err = i.PlanSetFields(map[string]issue.Value{"parent": issue.StringValue(sprint.Id().String())})
	require.ErrorContains(t, err, "takes epic, story")

	ops, err = i.PlanSetFields(map[string]issue.Value{"iteration": issue.StringValue(sprint.Id().String())})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))

	// an iteration has dates and a capacity, and no estimate
	it, err := env.Backend.Issues().Resolve(sprint.Id())
	require.NoError(t, err)
	ops, err = it.PlanSetFields(map[string]issue.Value{
		"start":    issue.StringValue("2026-09-21"),
		"end":      issue.StringValue("2026-10-05"),
		"capacity": issue.MustValue(24),
	})
	require.NoError(t, err)
	require.NoError(t, it.CommitOperations(ops))

	_, err = it.PlanSetFields(map[string]issue.Value{"estimate": issue.MustValue(3)})
	require.ErrorContains(t, err, "is not a field of type iteration")
}

// commitCount counts the commits of every schema ref: an import that changes
// nothing adds none.
func commitCount(t *testing.T, env *execenv.Env) int {
	t.Helper()

	total := 0
	for _, id := range env.Backend.Schema().AllIds() {
		commits, err := env.Repo.ListCommits("refs/" + config.SchemaNamespace + "/" + id.String())
		require.NoError(t, err)
		total += len(commits)
	}
	return total
}
