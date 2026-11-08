package schema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// colorSchema imports src into an empty store and compiles what it wrote.
func colorSchema(t *testing.T, src string) (*Schema, []Entry) {
	t.Helper()
	doc, err := ParseDocument([]byte(src))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))
	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))
	s, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	return s, entries
}

// TestColorIsSwitchedOnPerType: the built-in color exists only on a type
// whose `color` is true, its kind is its own, and a type without it refuses
// the key as any unknown one (doc/design/schema.md, "Color is a built-in a
// type switches on").
func TestColorIsSwitchedOnPerType(t *testing.T) {
	s, _ := colorSchema(t, `
types:
  story: {color: true}
  task: {}
`)
	story, task := s.Types["story"], s.Types["task"]
	require.True(t, story.Color)
	require.False(t, task.Color)

	field, ok := story.Field(ColorKey)
	require.True(t, ok)
	require.Equal(t, KindColor, field.Kind)
	require.True(t, field.Builtin)
	_, ok = task.Field(ColorKey)
	require.False(t, ok, "a type that does not switch it on has no color")
	_, ok = task.Field(RankKey)
	require.True(t, ok, "the other built-ins are on every type")

	require.True(t, IsBuiltin(ColorKey))
	require.False(t, OnEveryType(ColorKey))
	require.True(t, OnEveryType(RankKey))

	c := &Checker{Schema: s}
	for _, value := range Colors {
		require.NoError(t, c.CheckFields("story", fields("color", `"`+value+`"`)), value)
	}
	require.NoError(t, c.CheckFields("story", fields("color", `null`)), "null clears")

	err := c.CheckFields("story", fields("color", `"orange"`))
	require.ErrorContains(t, err, `"orange" is not a color`)
	require.ErrorContains(t, err, "valid colors: red, green, yellow, blue, magenta, cyan, bright-red,")
	require.ErrorContains(t, c.CheckFields("story", fields("color", `3`)), "a color is expected")
	require.ErrorContains(t, c.Shape().CheckFields("story", fields("color", `"white"`)),
		"is not a color", "the set is shape, not policy")

	err = c.CheckFields("task", fields("color", `"red"`))
	require.ErrorContains(t, err, `"color" is not a field of type task`)
}

// TestColorRoundTrips: `color: true` is written as a type attribute, exported
// only when true, and removed by an import of the type without it.
func TestColorRoundTrips(t *testing.T) {
	s, entries := colorSchema(t, `
types:
  story: {name: Story, color: true}
  task: {name: Task}
`)
	exported := Export(s)
	story, _ := exported.Types.Get("story")
	task, _ := exported.Types.Get("task")
	require.True(t, story.Color)
	require.False(t, task.Color)
	require.Nil(t, story.Fields, "a bare built-in is never exported")
	raw, err := exported.Marshal("yaml")
	require.NoError(t, err)
	require.Contains(t, string(raw), "color: true")
	require.Equal(t, 1, strings.Count(string(raw), "color"), "exported only when true:\n%s", raw)

	require.Empty(t, mustReconcile(t, exported, entries, false), "export | import changes nothing")

	off, err := ParseDocument([]byte("types:\n  story: {name: Story}\n"))
	require.NoError(t, err)
	changes := mustReconcile(t, off, entries, false)
	require.Len(t, changes, 1)
	require.Equal(t, []string{AttrColor}, changes[0].Remove)
	s, err = Compile(applyChanges(t, entries, changes))
	require.NoError(t, err)
	_, ok := s.Types["story"].Field(ColorKey)
	require.False(t, ok)
}

// TestColorsKindIsTheBuiltinsAlone: kind color is refused on any other field,
// an override of the built-in is the type's only where it switches color
// on, and the built-in takes no values or aliases.
func TestColorsKindIsTheBuiltinsAlone(t *testing.T) {
	refused := []struct{ src, want string }{
		{"types:\n  story:\n    fields:\n      tint: {kind: color}\n",
			"kind color is the built-in color's alone"},
		{"types:\n  story:\n    fields:\n      color: {kind: color, name: Tint}\n",
			"add color: true to type story"},
		{"types:\n  story:\n    color: true\n    fields:\n      color: {kind: color, values: [{id: teal}]}\n",
			"only its name and description are configurable"},
		{"types:\n  story:\n    color: true\n    fields:\n      color: {kind: color, aliases: {jira: customfield_10001}}\n",
			"only its name and description are configurable"},
		{"types:\n  story:\n    color: true\n    fields:\n      color: {kind: enum}\n",
			"built in with kind color"},
	}
	for _, c := range refused {
		doc, err := ParseDocument([]byte(c.src))
		require.NoError(t, err)
		require.ErrorContains(t, doc.Validate(nil), c.want, c.src)
	}
	require.NotContains(t, KindList(), "color")

	s, _ := colorSchema(t, "types:\n  story:\n    color: true\n    fields:\n      color: {kind: color, name: Tint}\n")
	field, _ := s.Types["story"].Field(ColorKey)
	require.Equal(t, "Tint", field.Name)
	require.Empty(t, field.Values, "the set is code, not values")
}
