package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPresetJira(t *testing.T) {
	doc, err := Preset("jira")
	require.NoError(t, err)

	require.Equal(t,
		[]string{"initiative", "epic", "story", "task", "bug", "subtask", "iteration"},
		doc.Types.Keys(),
		"types keep the file's order, which is the schema's order")

	story, ok := doc.Types.Get("story")
	require.True(t, ok)
	require.Equal(t, "Story", story.Name)

	// an anchor reads as a spelled-out field
	status, ok := story.Fields.Get("status")
	require.True(t, ok)
	require.Equal(t, "enum", status.Kind)
	require.Equal(t, "backlog", status.Values[0].Id)
	require.Equal(t, "canceled", status.Values[len(status.Values)-1].Category)

	// hierarchy is target_types on each type's parent (D4)
	parent, _ := story.Fields.Get("parent")
	require.Equal(t, []string{"epic"}, parent.TargetTypes)
	subtask, _ := doc.Types.Get("subtask")
	subtaskParent, _ := subtask.Fields.Get("parent")
	require.Equal(t, []string{"story", "task"}, subtaskParent.TargetTypes)

	// an iteration is a type, and its dates and capacity are its fields (D5)
	iteration, ok := doc.Types.Get("iteration")
	require.True(t, ok)
	require.Equal(t, []string{"status", "start", "end", "capacity", "rank"}, iteration.Fields.Keys())
	capacity, _ := iteration.Fields.Get("capacity")
	require.Equal(t, "number", capacity.Kind)

	membership, _ := story.Fields.Get("iteration")
	require.Equal(t, "relation", membership.Kind)
	require.Equal(t, []string{"iteration"}, membership.TargetTypes)
}

func TestPresetLinear(t *testing.T) {
	doc, err := Preset("linear")
	require.NoError(t, err)

	require.Equal(t,
		[]string{"initiative", "project", "issue", "cycle"},
		doc.Types.Keys(),
		"types keep the file's order, which is the schema's order")

	// a project plays the epic's role, under an initiative
	project, ok := doc.Types.Get("project")
	require.True(t, ok)
	parent, _ := project.Fields.Get("parent")
	require.Equal(t, []string{"initiative"}, parent.TargetTypes)

	// Linear has one work type, and a sub-issue is one whose parent is an issue
	issueType, ok := doc.Types.Get("issue")
	require.True(t, ok)
	issueParent, _ := issueType.Fields.Get("parent")
	require.Equal(t, []string{"project", "issue"}, issueParent.TargetTypes)
	require.Equal(t, "children", issueParent.Inverse)

	status, _ := issueType.Fields.Get("status")
	require.Equal(t,
		[]string{"backlog", "todo", "in-progress", "done", "canceled"},
		valueIds(status))
	require.Equal(t, "canceled", status.Values[len(status.Values)-1].Category)

	priority, _ := issueType.Fields.Get("priority")
	require.Equal(t, "ordinal-enum", priority.Kind)
	require.Equal(t,
		[]string{"urgent", "high", "medium", "low", "no-priority"},
		valueIds(priority))

	// the iteration type is the cycle, and membership a target-typed relation
	membership, _ := issueType.Fields.Get("cycle")
	require.Equal(t, "relation", membership.Kind)
	require.Equal(t, []string{"cycle"}, membership.TargetTypes)

	cycle, ok := doc.Types.Get("cycle")
	require.True(t, ok)
	require.Equal(t, []string{"start", "end", "capacity", "rank"}, cycle.Fields.Keys())
	capacity, _ := cycle.Fields.Get("capacity")
	require.Equal(t, "number", capacity.Kind)
}

// TestPresetRoundTrip is the table every shipped preset answers to.
//
// A preset that does not survive init | export | import is a preset that
// rewrites the store every time someone exports it, so the round trip is the
// only thing that says a preset is representable at all (E9).
func TestPresetRoundTrip(t *testing.T) {
	require.Equal(t, []string{"jira", "linear"}, PresetNames())

	for _, name := range PresetNames() {
		t.Run(name, func(t *testing.T) {
			doc, err := Preset(name)
			require.NoError(t, err)
			require.NoError(t, doc.Validate(nil), "a preset validates on its own")

			// every relation points at a type the preset defines
			for _, typeKey := range doc.Types.Keys() {
				docType, _ := doc.Types.Get(typeKey)
				for _, fieldKey := range docType.Fields.Keys() {
					field, _ := docType.Fields.Get(fieldKey)
					for _, target := range field.TargetTypes {
						_, ok := doc.Types.Get(target)
						require.True(t, ok, "%s/%s targets %s", typeKey, fieldKey, target)
					}
				}
			}

			entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))

			s, err := Compile(entries)
			require.NoError(t, err)
			require.Empty(t, s.Problems)

			// every type and field of the preset is in the store
			require.Equal(t, doc.Types.Keys(), s.TypeKeys())
			for _, typeKey := range doc.Types.Keys() {
				docType, _ := doc.Types.Get(typeKey)
				compiled, ok := s.Type(typeKey)
				require.True(t, ok)
				for _, fieldKey := range docType.Fields.Keys() {
					_, ok := compiled.Field(fieldKey)
					require.True(t, ok, "%s/%s", typeKey, fieldKey)
				}
			}

			// exporting and importing what was just written writes nothing
			again, err := Reconcile(Export(s), entries, true)
			require.NoError(t, err)
			require.Empty(t, again)
		})
	}
}

func TestPresetUnknown(t *testing.T) {
	_, err := Preset("nope")
	require.ErrorContains(t, err, "shipped presets: jira, linear")
}

func valueIds(field FieldDoc) []string {
	ids := make([]string, 0, len(field.Values))
	for _, value := range field.Values {
		ids = append(ids, value.Id)
	}
	return ids
}
