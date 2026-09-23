package schema

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
)

// entry builds one config entity document, as an excerpt hands it over.
func entry(id string, shape config.Shape, key string, attributes map[string]interface{}) Entry {
	values := make(map[string]config.Value, len(attributes))
	for name, value := range attributes {
		values[name] = config.MustValue(value)
	}
	return Entry{Id: idOf(id), Shape: shape, Key: key, Attributes: values}
}

func TestCompileEmpty(t *testing.T) {
	s, err := Compile(nil)
	require.NoError(t, err)
	require.True(t, s.Empty(), "no type entity is the bootstrap state, not an error")
	require.Empty(t, s.Problems)
}

func TestCompileTypesAndFields(t *testing.T) {
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "story", map[string]interface{}{
			AttrName: "Story", AttrOrdinal: 20,
		}),
		entry("2", config.ShapeType, "epic", map[string]interface{}{
			AttrName: "Epic", AttrOrdinal: 10,
		}),
		entry("3", config.ShapeField, "story/status", map[string]interface{}{
			AttrKind: "enum", AttrName: "Status", AttrOrdinal: 10,
			"values/done":  ValueAttr{Name: "Done", Ordinal: 20, Category: CategoryCompleted},
			"values/to-do": ValueAttr{Name: "To Do", Ordinal: 10, Category: CategoryUnstarted},
		}),
		entry("4", config.ShapeField, "story/parent", map[string]interface{}{
			AttrKind: "relation", AttrOrdinal: 20, AttrInverse: "children",
			"target_types/epic": struct{}{},
		}),
	})
	require.NoError(t, err)
	require.Empty(t, s.Problems)

	// types come back in ordinal order, not alphabetical
	require.Equal(t, []string{"epic", "story"}, s.TypeKeys())

	story, ok := s.Type("story")
	require.True(t, ok)
	require.Equal(t, "Story", story.Name)

	// the built-ins are there without an entity, and sort first
	require.Equal(t, []string{TitleKey, TypeKey, ArchivedKey, "status", "parent"}, story.FieldKeys())
	title, _ := story.Field(TitleKey)
	require.True(t, title.Builtin)
	require.False(t, title.Configured)
	require.Equal(t, KindText, title.Kind)

	// the type field's values are the types
	typeField, _ := story.Field(TypeKey)
	require.Equal(t, []string{"epic", "story"}, typeField.ValueIds())

	status, _ := story.Field("status")
	require.Equal(t, KindEnum, status.Kind)
	require.Equal(t, []string{"to-do", "done"}, status.ValueIds(), "values sort by (ordinal, id)")
	done, ok := status.Value("done")
	require.True(t, ok)
	require.Equal(t, CategoryCompleted, done.Category)

	parent, _ := story.Field("parent")
	require.Equal(t, KindRelation, parent.Kind)
	require.Equal(t, "children", parent.Inverse)
	require.Equal(t, []string{"epic"}, parent.TargetTypes)
}

func TestCompileBuiltinOverride(t *testing.T) {
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "task", map[string]interface{}{AttrName: "Task"}),
		entry("2", config.ShapeField, "task/title", map[string]interface{}{
			// an entity that would change the kind can not: the kind is code
			AttrKind: "number", AttrName: "Summary", AttrOrdinal: 5,
		}),
	})
	require.NoError(t, err)

	task, _ := s.Type("task")
	title, _ := task.Field(TitleKey)
	require.Equal(t, KindText, title.Kind, "a built-in's kind is code (E4)")
	require.Equal(t, "Summary", title.Name)
	require.Equal(t, 5, title.Ordinal)
	require.True(t, title.Builtin)
	require.True(t, title.Configured)
}

func TestCompileProblemsAreNotErrors(t *testing.T) {
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "task", map[string]interface{}{AttrName: "Task"}),
		// a field of a type that is gone
		entry("2", config.ShapeField, "epic/status", map[string]interface{}{AttrKind: "enum"}),
		// a kind this binary does not know
		entry("3", config.ShapeField, "task/weight", map[string]interface{}{AttrKind: "duration"}),
		// a relation pointing at nothing
		entry("4", config.ShapeField, "task/parent", map[string]interface{}{
			AttrKind: "relation", "target_types/epic": struct{}{},
		}),
	})
	require.NoError(t, err, "a read never fails on what was written before (D6)")
	require.Len(t, s.Problems, 3)

	task, _ := s.Type("task")
	_, ok := task.Field("weight")
	require.False(t, ok, "an unreadable field is reported and left out")
	_, ok = task.Field("parent")
	require.True(t, ok, "a dangling target renders")
}

// idOf pads a test id to the length an entity id has.
func idOf(s string) entity.Id {
	out := make([]byte, 40)
	for i := range out {
		out[i] = '0'
	}
	copy(out[40-len(s):], s)
	return entity.Id(out)
}
