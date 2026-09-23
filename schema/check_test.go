package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
)

// testResolver answers for a repository with one identity and two issues.
type testResolver struct{}

func (testResolver) IdentityExists(id string) error {
	if id == "alice" {
		return nil
	}
	return fmt.Errorf("no identity matching %s", id)
}

func (testResolver) IssueType(id string) (string, error) {
	switch id {
	case "e1":
		return "epic", nil
	case "t1":
		return "task", nil
	}
	return "", fmt.Errorf("no issue matching %s", id)
}

func testChecker(t *testing.T) *Checker {
	t.Helper()

	s, err := Compile([]Entry{
		entry("1", config.ShapeType, "epic", map[string]interface{}{AttrOrdinal: 10}),
		entry("2", config.ShapeType, "task", map[string]interface{}{AttrOrdinal: 20}),
		entry("3", config.ShapeField, "task/status", map[string]interface{}{
			AttrKind:       "enum",
			"values/to-do": ValueAttr{Ordinal: 10, Category: CategoryUnstarted},
			"values/done":  ValueAttr{Ordinal: 20, Category: CategoryCompleted},
		}),
		entry("4", config.ShapeField, "task/estimate", map[string]interface{}{AttrKind: "number"}),
		entry("5", config.ShapeField, "task/due", map[string]interface{}{AttrKind: "date"}),
		entry("6", config.ShapeField, "task/assignee", map[string]interface{}{AttrKind: "identity"}),
		entry("7", config.ShapeField, "task/rank", map[string]interface{}{AttrKind: "rank"}),
		entry("8", config.ShapeField, "task/labels", map[string]interface{}{
			AttrKind: "multi-enum", "values/core": ValueAttr{Ordinal: 10},
		}),
		entry("9", config.ShapeField, "task/tags", map[string]interface{}{
			AttrKind: "multi-enum", AttrFreeform: true,
		}),
		entry("10", config.ShapeField, "task/parent", map[string]interface{}{
			AttrKind: "relation", "target_types/epic": struct{}{},
		}),
		entry("11", config.ShapeField, "task/blocks", map[string]interface{}{
			AttrKind: "multi-relation",
		}),
	})
	require.NoError(t, err)
	require.Empty(t, s.Problems)

	return &Checker{Schema: s, Resolver: testResolver{}}
}

func fields(pairs ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = json.RawMessage(pairs[i+1])
	}
	return out
}

func TestCheckerEmptySchema(t *testing.T) {
	s, err := Compile(nil)
	require.NoError(t, err)
	c := &Checker{Schema: s}

	require.False(t, c.Validating())
	require.NoError(t, c.CheckNew(fields("title", `"t"`, "anything", `3`)))
	require.NoError(t, c.CheckFields("", fields("whatever", `"x"`)))
}

func TestCheckerNewNeedsAType(t *testing.T) {
	c := testChecker(t)

	err := c.CheckNew(fields("title", `"t"`))
	require.ErrorContains(t, err, "type is required")
	require.ErrorContains(t, err, "epic, task")

	err = c.CheckNew(fields("title", `"t"`, "type", `"epik"`))
	require.ErrorContains(t, err, `"epik" is not in the schema`)

	require.NoError(t, c.CheckNew(fields("title", `"t"`, "type", `"task"`, "estimate", `3`)))
}

func TestCheckerUnknownField(t *testing.T) {
	c := testChecker(t)

	err := c.CheckFields("task", fields("statuss", `"done"`))
	require.ErrorContains(t, err, `"statuss" is not a field of type task`)
	require.ErrorContains(t, err, "status")
}

func TestCheckerKinds(t *testing.T) {
	c := testChecker(t)

	good := []map[string]json.RawMessage{
		fields("status", `"done"`),
		fields("estimate", `3.5`),
		fields("due", `"2026-09-23"`),
		fields("due", `"2026-09-23T10:00:00Z"`),
		fields("assignee", `"alice"`),
		fields("rank", `"aaz"`),
		fields("labels", `["core"]`),
		fields("tags", `["anything"]`),
		fields("parent", `"e1"`),
		fields("blocks", `["e1","t1"]`),
		fields("title", `"a title"`),
		fields("archived", `true`),
		// a null clears any field
		fields("status", `null`),
	}
	for _, f := range good {
		require.NoError(t, c.CheckFields("task", f), "%v", f)
	}

	bad := map[string]map[string]json.RawMessage{
		"a value not in the schema":    fields("status", `"shipped"`),
		"an enum that is not a string": fields("status", `3`),
		"a number that is a string":    fields("estimate", `"3"`),
		"a date that is not one":       fields("due", `"tuesday"`),
		"an identity that is not one":  fields("assignee", `"bob"`),
		"a list given as a scalar":     fields("labels", `"core"`),
		"an item not in the schema":    fields("labels", `["nope"]`),
		"a relation to nothing":        fields("parent", `"zz"`),
		"a bool that is not one":       fields("archived", `"yes"`),
	}
	for name, f := range bad {
		t.Run(name, func(t *testing.T) {
			require.Error(t, c.CheckFields("task", f))
		})
	}

	// the valid ids are named, because that is what the caller needs next
	err := c.CheckFields("task", fields("status", `"shipped"`))
	require.ErrorContains(t, err, "valid values: to-do, done")
}

func TestCheckerRelationTargetType(t *testing.T) {
	c := testChecker(t)

	require.NoError(t, c.CheckFields("task", fields("parent", `"e1"`)))

	err := c.CheckFields("task", fields("parent", `"t1"`))
	require.ErrorContains(t, err, "is a task")
	require.ErrorContains(t, err, "takes epic")

	// no target types means any issue
	require.NoError(t, c.CheckItems("task", map[string][]json.RawMessage{
		"blocks": {json.RawMessage(`"t1"`)},
	}))
}

func TestCheckerItems(t *testing.T) {
	c := testChecker(t)

	require.NoError(t, c.CheckItems("task", map[string][]json.RawMessage{
		"labels": {json.RawMessage(`"core"`)},
	}))

	err := c.CheckItems("task", map[string][]json.RawMessage{
		"status": {json.RawMessage(`"done"`)},
	})
	require.ErrorContains(t, err, "not a list")

	err = c.CheckItems("task", map[string][]json.RawMessage{
		"labels": {json.RawMessage(`"nope"`)},
	})
	require.ErrorContains(t, err, "not in the schema")
}

func TestCheckerReportsEveryProblem(t *testing.T) {
	c := testChecker(t)

	err := c.CheckFields("task", fields(
		"status", `"shipped"`,
		"estimate", `"three"`,
		"nope", `1`,
	))
	require.Error(t, err)

	var problems *Problems
	require.ErrorAs(t, err, &problems)
	require.Len(t, problems.List, 3, "one error lists every problem, not the first")
}

func TestCheckerTypeChange(t *testing.T) {
	c := testChecker(t)

	// a set that changes the type is checked against the new type
	err := c.CheckFields("task", fields("type", `"epic"`, "status", `"done"`))
	require.ErrorContains(t, err, "not a field of type epic")
}
