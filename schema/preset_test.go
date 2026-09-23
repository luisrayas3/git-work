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

func TestPresetRoundTrip(t *testing.T) {
	doc, err := Preset("jira")
	require.NoError(t, err)

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
}

func TestPresetUnknown(t *testing.T) {
	_, err := Preset("nope")
	require.ErrorContains(t, err, "shipped presets: jira")
}
