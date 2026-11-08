package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
)

// archivedChecker is a schema with an archived value, an archived field and
// an archived type, holding what an issue of each would.
func archivedChecker(t *testing.T) *Checker {
	t.Helper()
	bug := entry("3", config.ShapeType, "bug", map[string]interface{}{AttrOrdinal: 30})
	bug.Archived = true
	size := entry("6", config.ShapeField, "task/size", map[string]interface{}{AttrKind: "number"})
	size.Archived = true
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "task", map[string]interface{}{AttrOrdinal: 10}),
		bug,
		entry("4", config.ShapeField, "task/status", map[string]interface{}{
			AttrKind:       "enum",
			"values/to-do": ValueAttr{Ordinal: 10, Category: CategoryUnstarted},
			"values/doing": ValueAttr{Ordinal: 20, Category: CategoryStarted, Archived: true},
			"values/done":  ValueAttr{Ordinal: 30, Category: CategoryCompleted},
		}),
		entry("5", config.ShapeField, "task/labels", map[string]interface{}{
			AttrKind:      "multi-enum",
			"values/core": ValueAttr{Ordinal: 10, Archived: true},
			"values/cli":  ValueAttr{Ordinal: 20},
		}),
		size,
	})
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	return &Checker{Schema: s, Resolver: testResolver{}}
}

// holding is an issue holding these fields.
func holding(held map[string]string) func(string, json.RawMessage) bool {
	return func(key string, value json.RawMessage) bool {
		raw, ok := held[key]
		if !ok {
			return false
		}
		if raw == string(value) {
			return true
		}
		var items []json.RawMessage
		_ = json.Unmarshal([]byte(raw), &items)
		for _, item := range items {
			if string(item) == string(value) {
				return true
			}
		}
		return false
	}
}

// (doc/design/schema.md, "Archiving refuses only what is new")
func TestArchivedValueKeepsItsPlace(t *testing.T) {
	c := archivedChecker(t)
	status, ok := c.Schema.Field("task", "status")
	require.True(t, ok)
	require.Equal(t, []string{"to-do", "done"}, status.ValueIds(), "what a write may set")
	require.Len(t, status.Values, 3, "an archived value reads")
	require.Equal(t, "doing", status.Values[1].Id, "and keeps its place")
	require.Equal(t, CategoryStarted, status.Values[1].Category)
	require.Empty(t, status.ValuesInCategory(CategoryStarted), "nothing offers it")
	require.Equal(t, []string{"task"}, c.Schema.TypeKeys())
	require.Equal(t, []string{"task", "bug"}, c.Schema.AllTypeKeys())
	task, _ := c.Schema.Type("task")
	require.NotContains(t, task.FieldKeys(), "size")
	require.Contains(t, task.AllFieldKeys(), "size")
	typ, _ := task.Field(TypeKey)
	require.Equal(t, []string{"task"}, typ.ValueIds())
}

// (doc/design/schema.md, "Archiving refuses only what is new")
func TestArchiveRefusesWhatIsNew(t *testing.T) {
	c := archivedChecker(t)

	err := c.CheckNew(fields("type", `"task"`, "status", `"doing"`, "labels", `["core"]`, "size", `2`))
	require.ErrorContains(t, err, `field status: "doing" is archived; valid values: to-do, done`)
	require.ErrorContains(t, err, `field labels: "core" is archived; valid values: cli`)
	require.ErrorContains(t, err, `field size: the field is archived on task; its fields:`)
	require.ErrorContains(t, c.CheckNew(fields("type", `"bug"`)), `"bug" is archived; valid values: task`)
	require.ErrorContains(t, c.CheckFields("task", fields("type", `"bug"`)), `"bug" is archived`)
	require.ErrorContains(t, c.CheckItems("task", map[string][]json.RawMessage{"labels": {json.RawMessage(`"core"`)}}),
		`"core" is archived`)

	held := c.Holding(holding(map[string]string{
		"type": `"bug"`, "status": `"doing"`, "labels": `["core"]`, "size": `3`,
	}))
	require.NoError(t, held.CheckFields("task", fields("status", `"doing"`)), "kept")
	require.NoError(t, held.CheckFields("task", fields("labels", `["core","cli"]`)), "kept, one added")
	require.NoError(t, held.CheckFields("task", fields("size", `3`, "status", `"done"`)), "kept, moved off")
	require.NoError(t, held.CheckFields("task", fields("size", `null`)), "cleared")
	require.NoError(t, held.CheckFields("bug", fields("title", `"t"`)), "an issue of an archived type is edited")
	require.ErrorContains(t, held.CheckFields("task", fields("size", `4`)), "archived")
	require.ErrorContains(t, held.CheckFields("task", fields("status", `"to-do"`, "labels", `["core","x"]`)), `"x" is not in the schema`)
	require.NoError(t, c.Holding(HoldsAnything).CheckItems("task", map[string][]json.RawMessage{"labels": {json.RawMessage(`"core"`)}}),
		"a remove introduces nothing")

	// a pull is checked for shape, and an archive is policy
	require.NoError(t, c.Shape().CheckFields("task", fields("status", `"doing"`, "size", `5`)))
}

// An archive is stated: true archives, false unarchives, absent leaves it as
// it stands, and export states it (doc/design/schema.md, "An archive is
// stated, never implied").
func TestArchiveIsStated(t *testing.T) {
	doc, err := ParseDocument([]byte(`
types:
  task:
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, category: unstarted}
          - {id: doing, category: started}
      size: {kind: number}
  bug: {}
`))
	require.NoError(t, err)
	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))

	archived, err := ParseDocument([]byte(`
types:
  task:
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, category: unstarted}
          - {id: doing, category: started, archived: true}
      size: {kind: number, archived: true}
  bug: {archived: true}
`))
	require.NoError(t, err)
	changes := mustReconcile(t, archived, entries, false)
	require.Len(t, changes, 3)
	entries = applyChanges(t, entries, changes)
	s, err := Compile(entries)
	require.NoError(t, err)
	require.True(t, s.Types["bug"].Archived)
	require.True(t, s.Types["task"].Fields["size"].Archived)
	doing, _ := s.Types["task"].Fields["status"].Value("doing")
	require.True(t, doing.Archived)

	// export states it, and round-trips
	exported := Export(s)
	require.Empty(t, mustReconcile(t, exported, entries, true), "export | import emits zero operations")
	out, err := exported.Marshal("yaml")
	require.NoError(t, err)
	require.Contains(t, string(out), "archived: true")

	// a file written before the archive revives nothing, and prune skips
	// what is archived already
	require.Empty(t, mustReconcile(t, doc, entries, false))
	require.Empty(t, mustReconcile(t, doc, entries, true))

	// false unarchives
	revived, err := ParseDocument([]byte(`
types:
  task:
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, category: unstarted}
          - {id: doing, category: started, archived: false}
      size: {kind: number, archived: false}
  bug: {archived: false}
`))
	require.NoError(t, err)
	entries = applyChanges(t, entries, mustReconcile(t, revived, entries, false))
	s, err = Compile(entries)
	require.NoError(t, err)
	require.False(t, s.Types["bug"].Archived)
	require.False(t, s.Types["task"].Fields["size"].Archived)
	doing, _ = s.Types["task"].Fields["status"].Value("doing")
	require.False(t, doing.Archived)
	require.Empty(t, mustReconcile(t, Export(s), entries, true))
}
