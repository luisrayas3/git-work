package jq

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/schema"
)

// testSchema is `$schema` as the export prints it: a task with a workflow
// enum, an ordered priority, a multi-enum and a text field; a bug with only
// a status of its own.
const testSchema = `{"types": {
  "task": {"fields": {
    "status": {"kind": "enum", "values": [
      {"id": "backlog", "category": "backlog"},
      {"id": "to-do", "category": "unstarted"},
      {"id": "doing", "category": "started"},
      {"id": "done", "category": "completed"},
      {"id": "dropped", "category": "canceled"}]},
    "priority": {"kind": "ordinal-enum", "values": [{"id": "high"}, {"id": "medium"}, {"id": "low"}]},
    "area": {"kind": "multi-enum", "values": [{"id": "core"}, {"id": "tui"}]},
    "note": {"kind": "text"}}},
  "bug": {"fields": {
    "status": {"kind": "enum", "values": [{"id": "open", "category": "unstarted"}, {"id": "fixed", "category": "completed"}]}}}}}`

func runWithSchema(t *testing.T, program string, input any) any {
	t.Helper()
	var s any
	require.NoError(t, json.Unmarshal([]byte(testSchema), &s))
	in, err := Input(input)
	require.NoError(t, err)
	values, err := Run(program, in, s)
	require.NoError(t, err)
	require.Len(t, values, 1)
	return values[0]
}

func task(fields map[string]any) map[string]any {
	fields["type"] = "task"
	return map[string]any{"fields": fields}
}

// TestOrdinal: a value's position in its field's values, and null wherever
// the schema says nothing about it (J2, J3).
func TestOrdinal(t *testing.T) {
	for _, c := range []struct {
		issue any
		field string
		want  any
	}{
		{task(map[string]any{"priority": "high"}), "priority", 0},
		{task(map[string]any{"priority": "low"}), "priority", 2},
		{task(map[string]any{"status": "doing"}), "status", 2}, // a plain enum too
		{task(map[string]any{}), "priority", nil},              // unset
		{task(map[string]any{"priority": "urgent"}), "priority", nil},
		{task(map[string]any{"area": []any{"core"}}), "area", nil}, // a multi-enum has no one place
		{task(map[string]any{"note": "x"}), "note", nil},           // no values
		{task(map[string]any{"priority": "high"}), "colour", nil},  // no such field
		{map[string]any{"fields": map[string]any{"type": "bug", "priority": "high"}}, "priority", nil},
		{map[string]any{"fields": map[string]any{"type": "epic", "priority": "high"}}, "priority", nil},
		{map[string]any{"fields": map[string]any{"priority": "high"}}, "priority", nil}, // no type
	} {
		require.Equal(t, c.want, runWithSchema(t, `ordinal("`+c.field+`")`, c.issue), "%v %s", c.issue, c.field)
	}
}

// TestCategory: a value's category, null where there is none to read.
func TestCategory(t *testing.T) {
	for _, c := range []struct {
		issue any
		field string
		want  any
	}{
		{task(map[string]any{"status": "doing"}), "status", "started"},
		{task(map[string]any{"status": "dropped"}), "status", "canceled"},
		{map[string]any{"fields": map[string]any{"type": "bug", "status": "fixed"}}, "status", "completed"},
		{task(map[string]any{"priority": "high"}), "priority", nil}, // a value with no category
		{task(map[string]any{"status": "gone"}), "status", nil},
		{task(map[string]any{}), "status", nil},
		{task(map[string]any{"area": []any{"core"}}), "area", nil},
	} {
		require.Equal(t, c.want, runWithSchema(t, `category("`+c.field+`")`, c.issue), "%v %s", c.issue, c.field)
	}
	require.Equal(t, false, runWithSchema(t, `category("status") | done`, task(map[string]any{})), "null | done is false")
}

// TestDoneIsCategoryDone: `done` is schema.Category.Done said in jq.
func TestDoneIsCategoryDone(t *testing.T) {
	for _, category := range schema.Categories {
		require.Equal(t, category.Done(), runWithSchema(t, `done`, string(category)), category)
	}
}

// TestSchemaSort: the J2 example, open work first by priority, unset last.
func TestSchemaSort(t *testing.T) {
	issues := []any{
		task(map[string]any{"title": "a", "status": "done", "priority": "high"}),
		task(map[string]any{"title": "b", "status": "to-do"}),
		task(map[string]any{"title": "c", "status": "doing", "priority": "low"}),
		task(map[string]any{"title": "d", "status": "dropped", "priority": "high"}),
		task(map[string]any{"title": "e", "status": "to-do", "priority": "high"}),
	}
	got := runWithSchema(t, `sort_by((category("status") | done), ordinal("priority") == null, ordinal("priority"), .fields.title) | map(.fields.title)`, issues)
	require.Equal(t, []any{"e", "c", "b", "a", "d"}, got)
}

// TestOwnDefinitionsWin: a program's own function of a helper's name is the
// one it calls, so a program written before the helpers keeps its meaning.
func TestOwnDefinitionsWin(t *testing.T) {
	require.Equal(t, "mine", runWithSchema(t, `def done: "mine"; "completed" | done`, nil))
}

// TestNoSchema: with no schema bound, the helpers read null and nothing fails.
func TestNoSchema(t *testing.T) {
	values, err := Run(`ordinal("priority"), category("status"), $schema`, map[string]any{"fields": map[string]any{"type": "task", "priority": "high"}}, nil)
	require.NoError(t, err)
	require.Equal(t, []any{nil, nil, nil}, values)
}

// TestErrorPositionsAreTheProgramsOwn: the helpers are not text put before
// the program, so a parse error points into the program as written.
func TestErrorPositionsAreTheProgramsOwn(t *testing.T) {
	_, plain := Run(`map(`, nil, nil)
	require.Error(t, plain)
	require.NotContains(t, plain.Error(), "ordinal")
}
