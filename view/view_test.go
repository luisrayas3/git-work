package view

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// kwargs is the JSON object a call is, from a literal.
func kwargs(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(doc), &out))
	return out
}

func TestParseAppliesTheDefaults(t *testing.T) {
	call, err := Parse(KindList, nil)
	require.NoError(t, err)

	require.Equal(t, KindList, call.Kind)
	// every kind that draws more than one issue queries the same way
	require.Equal(t, DefaultQuery, call.String("query"))
	require.Equal(t, []string{"title"}, call.Strings("fields"))
	// a feature nobody asked for is simply absent
	require.False(t, call.Has("group_by"))
	require.False(t, call.Has("rank"))
	require.Equal(t, 0, call.Int("depth"))
}

func TestParseKeepsWhatWasGiven(t *testing.T) {
	call, err := Parse(KindList, kwargs(t, `{
		"query": "map(select(.fields.status != \"done\"))",
		"fields": ["title", "status", "assignee"],
		"details": ["labels"],
		"group_by": "status",
		"depth": 2,
		"rank": "rank"
	}`))
	require.NoError(t, err)

	require.Equal(t, `map(select(.fields.status != "done"))`, call.String("query"))
	require.Equal(t, []string{"title", "status", "assignee"}, call.Strings("fields"))
	require.Equal(t, []string{"labels"}, call.Strings("details"))
	require.Equal(t, "status", call.String("group_by"))
	require.Equal(t, 2, call.Int("depth"))
	require.True(t, call.Has("rank"))
}

func TestParseUnknownKeyNamesTheArguments(t *testing.T) {
	_, err := Parse(KindBoard, kwargs(t, `{"columns":"status","colums":"status"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "colums")
	// the error names what the view does take, with the tier
	require.Contains(t, err.Error(), "columns (required)")
	require.Contains(t, err.Error(), "card (defaulted)")
	require.Contains(t, err.Error(), "group_by (feature)")
}

func TestParseMissingRequired(t *testing.T) {
	_, err := Parse(KindBoard, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	// gantt needs both ends, and says which one is missing
	_, err = Parse(KindGantt, kwargs(t, `{"start":"start_date"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "stop")

	call, err := Parse(KindGantt, kwargs(t, `{"start":"start_date","stop":"due"}`))
	require.NoError(t, err)
	require.Equal(t, "week", call.String("scale"))
	require.Equal(t, "title", call.String("label"))
	// a default the renderer computes is absent here, not null
	require.False(t, call.Has("from"))
	require.False(t, call.Has("to"))

	// show takes an id, and no query at all
	_, err = Parse(KindShow, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")

	_, err = Parse(KindShow, kwargs(t, `{"id":"abcdef0","query":"."}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "query")
}

func TestParseChecksValueTypes(t *testing.T) {
	// a field key is a string
	_, err := Parse(KindBoard, kwargs(t, `{"columns":3}`))
	require.ErrorContains(t, err, "a string")

	// and not an empty one
	_, err = Parse(KindBoard, kwargs(t, `{"columns":"  "}`))
	require.ErrorContains(t, err, "empty")

	// a list of field keys is a list
	_, err = Parse(KindList, kwargs(t, `{"fields":"title"}`))
	require.ErrorContains(t, err, "a list of strings")

	_, err = Parse(KindList, kwargs(t, `{"fields":["title", 3]}`))
	require.ErrorContains(t, err, "a list of strings")

	// depth is a whole number
	_, err = Parse(KindList, kwargs(t, `{"depth":"two"}`))
	require.ErrorContains(t, err, "whole number")

	// the values of a board are any strings, not necessarily field keys
	call, err := Parse(KindBoard, kwargs(t, `{"columns":"status","values":["to-do","done"]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"to-do", "done"}, call.Strings("values"))
}

func TestParseChecksAnEnum(t *testing.T) {
	_, err := Parse(KindGantt, kwargs(t, `{"start":"a","stop":"b","scale":"fortnight"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "fortnight")
	require.Contains(t, err.Error(), "day, week, month, quarter")

	call, err := Parse(KindGantt, kwargs(t, `{"start":"a","stop":"b","scale":"quarter"}`))
	require.NoError(t, err)
	require.Equal(t, "quarter", call.String("scale"))
}

// TestNullMeansTheDefault is what a Starlark keyword of None has to mean:
// the argument was not given.
func TestNullMeansTheDefault(t *testing.T) {
	call, err := Parse(KindList, kwargs(t, `{"query":null,"group_by":null}`))
	require.NoError(t, err)
	require.Equal(t, DefaultQuery, call.String("query"))
	require.False(t, call.Has("group_by"))

	// but a required argument is still required
	_, err = Parse(KindBoard, kwargs(t, `{"columns":null}`))
	require.ErrorContains(t, err, "columns")
}

func TestParseUnknownKind(t *testing.T) {
	_, err := Parse("burndown", nil)
	require.Error(t, err)
	for _, kind := range []string{"board", "gantt", "list", "show"} {
		require.Contains(t, err.Error(), kind)
	}
}

// TestHelpIsGeneratedFromTheTable pins the one rule the help follows:
// every argument is in it, with its tier, so the help and the check agree.
func TestHelpIsGeneratedFromTheTable(t *testing.T) {
	help := Help(KindBoard)
	for _, arg := range Kinds[KindBoard] {
		require.Contains(t, help, arg.Name)
		require.Contains(t, help, string(arg.Tier))
	}
	require.Contains(t, Help(KindList), `["title"]`)
}
