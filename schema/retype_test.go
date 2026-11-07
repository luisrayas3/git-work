package schema

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
)

// retypeDocument has a fixed type, a type with targets, and one that may
// become any type, which is what a type without retype_targets means (TC1).
const retypeDocument = `
types:
  epic:
    retype_targets: []
  story:
    retype_targets: [task, bug]
  task:
    retype_targets: [story]
  bug: {}
`

// TestRetypeTargetsRoundTrip: retype_targets goes through import, compile
// and export unchanged, empty staying empty and absent absent, and export |
// import then writes nothing.
func TestRetypeTargetsRoundTrip(t *testing.T) {
	doc, err := ParseDocument([]byte(retypeDocument))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))

	changes, err := Reconcile(doc, nil, false)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(changes[0].Set[AttrRetypeTargets]))
	require.JSONEq(t, `["task","bug"]`, string(changes[1].Set[AttrRetypeTargets]))
	require.NotContains(t, changes[3].Set, AttrRetypeTargets)
	entries := applyChanges(t, nil, changes)

	s, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	require.NotNil(t, s.Types["epic"].RetypeTargets)
	require.Empty(t, *s.Types["epic"].RetypeTargets)
	require.Equal(t, []string{"task", "bug"}, *s.Types["story"].RetypeTargets)
	require.Nil(t, s.Types["bug"].RetypeTargets)

	exported := Export(s)
	again, err := Reconcile(exported, entries, true)
	require.NoError(t, err)
	require.Empty(t, again, "export | import emits zero operations")

	for _, format := range []string{"yaml", "json"} {
		raw, err := exported.Marshal(format)
		require.NoError(t, err)
		parsed, err := ParseDocument(raw)
		require.NoError(t, err, format)
		require.Equal(t, exported.String(), parsed.String(), format)
		epic, _ := parsed.Types.Get("epic")
		require.NotNil(t, epic.RetypeTargets, "%s: an empty list is written, not dropped", format)
		bug, _ := parsed.Types.Get("bug")
		require.Nil(t, bug.RetypeTargets, format)
	}

	// a file that leaves retype_targets out makes the type retypable again
	doc, err = ParseDocument([]byte("types:\n  epic: {}\n"))
	require.NoError(t, err)
	changes, err = Reconcile(doc, entries, false)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, []string{AttrRetypeTargets}, changes[0].Remove)
}

func TestRetypeTargetsValidate(t *testing.T) {
	cases := map[string]string{
		"a target that is not a type": "types:\n  epic:\n    retype_targets: [initiative]\n",
		"a target twice":              "types:\n  epic:\n    retype_targets: [epic, epic]\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(src))
			require.NoError(t, err)
			require.ErrorContains(t, doc.Validate(nil), "retype target")
		})
	}

	// a target already in the store is fine: a partial file is legal
	doc, err := ParseDocument([]byte("types:\n  epic:\n    retype_targets: [initiative]\n"))
	require.NoError(t, err)
	require.NoError(t, doc.Validate([]string{"initiative"}))

	// a list of something else is a mistake, not a no-op
	_, err = ParseDocument([]byte("types:\n  epic:\n    retype_targets: {a: b}\n"))
	require.Error(t, err)

	// a target archived since is reported on compile, never an error (D4)
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "epic", map[string]interface{}{AttrRetypeTargets: []string{"gone"}}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"type epic: retype target gone does not exist"}, s.Problems)
}

func retypeChecker(t *testing.T) *Checker {
	t.Helper()
	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "initiative", map[string]interface{}{AttrOrdinal: 10, AttrRetypeTargets: []string{}}),
		entry("2", config.ShapeType, "epic", map[string]interface{}{AttrOrdinal: 20, AttrRetypeTargets: []string{}}),
		entry("3", config.ShapeType, "story", map[string]interface{}{AttrOrdinal: 30, AttrRetypeTargets: []string{"task", "bug"}}),
		entry("4", config.ShapeType, "task", map[string]interface{}{AttrOrdinal: 40}),
		entry("5", config.ShapeType, "bug", map[string]interface{}{AttrOrdinal: 50, AttrRetypeTargets: []string{"story"}}),
	})
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	return &Checker{Schema: s, Resolver: testResolver{}}
}

// TestCheckerRetype: a type change the current type's retype_targets does
// not list is refused naming what it may become; absent targets allow any
// type; the shape check, a Jira pull's, ignores them; a new issue is not a
// type change (TC2).
func TestCheckerRetype(t *testing.T) {
	c := retypeChecker(t)

	err := c.CheckFields("epic", fields("type", `"initiative"`))
	require.EqualError(t, err, "type: type change from epic to initiative is not allowed; allowed: (none)")

	err = c.CheckFields("story", fields("type", `"epic"`, "title", `"t"`))
	require.EqualError(t, err, "type: type change from story to epic is not allowed; allowed: task, bug")

	require.NoError(t, c.CheckFields("story", fields("type", `"bug"`)))
	require.NoError(t, c.CheckFields("epic", fields("type", `"epic"`)), "setting the type it has is no change")
	require.NoError(t, c.CheckFields("task", fields("type", `"epic"`)), "absent retype_targets allows any type")
	require.NoError(t, c.CheckFields("", fields("type", `"epic"`)), "an issue of no type has nothing to check")

	// one error still lists every problem
	err = c.CheckFields("epic", fields("type", `"story"`, "nope", `1`))
	var problems *Problems
	require.ErrorAs(t, err, &problems)
	require.Len(t, problems.List, 2)

	shape := c.Shape()
	require.NoError(t, shape.CheckFields("epic", fields("type", `"initiative"`)))
	require.NoError(t, shape.CheckRetype("epic", "initiative"))

	require.NoError(t, c.CheckNew(fields("type", `"epic"`, "title", `"t"`)))
}
