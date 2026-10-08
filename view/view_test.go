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
	require.Equal(t, []string{"type", "title"}, call.Strings("fields"))
	// an optional argument nobody asked for is simply absent
	require.False(t, call.Has("group_by"))
	require.Nil(t, call.Expand())
	// the manual order is the built-in rank, which no call names
	require.False(t, call.Has("rank"))
}

func TestParseKeepsWhatWasGiven(t *testing.T) {
	call, err := Parse(KindList, kwargs(t, `{
		"query": "map(select(.fields.status != \"done\"))",
		"fields": ["title", "status", "assignee"],
		"details": ["labels"],
		"group_by": "status",
		"expand": "children"
	}`))
	require.NoError(t, err)

	require.Equal(t, `map(select(.fields.status != "done"))`, call.String("query"))
	require.Equal(t, []string{"title", "status", "assignee"}, call.Strings("fields"))
	require.Equal(t, []string{"labels"}, call.Strings("details"))
	require.Equal(t, "status", call.String("group_by"))
	require.Equal(t, "children", call.Expand().Relation)
}

// TestRankIsNoArgument: rank is internal, the built-in order every view draws
// and every drag writes, so no kind takes it as an argument and no layer
// takes it as a key; naming it is refused like any unknown name
// (Luis, 2026-10-08).
func TestRankIsNoArgument(t *testing.T) {
	for kind, doc := range map[string]string{
		KindList:   `{"rank":"rank"}`,
		KindBoard:  `{"columns":"status","rank":"rank"}`,
		KindGantt:  `{"start":"a","stop":"b","rank":"rank"}`,
		KindMatrix: `{"rows":"work","columns":"iteration","rank":"rank"}`,
		KindShow:   `{"id":"abc","rank":"rank"}`,
	} {
		_, err := Parse(kind, kwargs(t, doc))
		require.ErrorContains(t, err, "rank", kind)
	}

	for kind, doc := range map[string]string{
		KindList:  `{"expand":{"relation":"children","rank":"rank"}}`,
		KindGantt: `{"start":"a","stop":"b","expand":{"relation":"children","rank":"rank"}}`,
		KindShow:  `{"id":"abc","expand":{"relation":"children","rank":"rank"}}`,
	} {
		_, err := Parse(kind, kwargs(t, doc))
		require.ErrorContains(t, err, "takes no key rank", kind)
	}
}

func TestParseUnknownKeyNamesTheArguments(t *testing.T) {
	_, err := Parse(KindBoard, kwargs(t, `{"columns":"status","colums":"status"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "colums")
	// the error names what the view does take, with the tier
	require.Contains(t, err.Error(), "columns (required)")
	require.Contains(t, err.Error(), "card (defaulted)")
	require.Contains(t, err.Error(), "group_by (optional)")
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

	// an argument the table does not have is refused naming the ones it does
	_, err = Parse(KindList, kwargs(t, `{"depth":2}`))
	require.ErrorContains(t, err, "takes no argument depth")

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

// TestParseSideTables: show's expand is the list's spec, or a list of it, a
// table per element, checked for shape by the table, which needs no store;
// what a flat table cannot draw is refused by name, and `children` is gone
// (doc/design/show-side-table.md, S1 and S3).
func TestParseSideTables(t *testing.T) {
	call, err := Parse(KindShow, kwargs(t, `{"id":"abc","expand":"children"}`))
	require.NoError(t, err)
	require.Len(t, call.SideTables(), 1)
	require.Equal(t, "children", call.SideTables()[0].Relation)
	require.Nil(t, call.Expand(), "show's expand is no list's spec")

	call, err = Parse(KindShow, kwargs(t, `{"id":"abc","expand":[{"relation":"children","query":"map(.)","fields":["status"],"include_archive":true},"blocks"]}`))
	require.NoError(t, err)
	tables := call.SideTables()
	require.Len(t, tables, 2)
	require.Equal(t, []string{"status"}, tables[0].Fields)
	require.Equal(t, "map(.)", tables[0].Query)
	require.True(t, *tables[0].IncludeArchive)
	require.Equal(t, "blocks", tables[1].Relation)

	for doc, says := range map[string]string{
		`{"id":"abc","expand":[]}`:                                                     "empty list",
		`{"id":"abc","expand":{"query":"."}}`:                                          "needs relation",
		`{"id":"abc","expand":[{"relation":""}]}`:                                      "empty",
		`{"id":"abc","expand":[{"relation":"children","as":"x"}]}`:                     "no key as",
		`{"id":"abc","expand":{"relation":"children","fields":"status"}}`:              "fields is a list",
		`{"id":"abc","expand":{"relation":"children","details":["status"]}}`:           "takes no details",
		`{"id":"abc","expand":["blocks",{"relation":"children","group_by":"status"}]}`: "table 2 takes no group_by",
		`{"id":"abc","expand":{"relation":"children","expand":"children"}}`:            "takes no expand",
		`{"id":"abc","expand":{"relation":"children","expand":0}}`:                     "takes no expand",
		`{"id":"abc","expand":3}`:                                                      "relation name or an object",
		`{"id":"abc","children":[{"relation":"children"}]}`:                            "takes no argument children",
	} {
		_, err := Parse(KindShow, kwargs(t, doc))
		require.ErrorContains(t, err, says, doc)
	}
}

func TestParseUnknownKind(t *testing.T) {
	_, err := Parse("burndown", nil)
	require.Error(t, err)
	for _, kind := range []string{"board", "gantt", "list", "matrix", "show"} {
		require.Contains(t, err.Error(), kind)
	}
}

// TestParseMatrix: the matrix needs both axes, counts when no number is
// named, and takes an order for either axis
// (doc/design/allocations.md A4, A5).
func TestParseMatrix(t *testing.T) {
	_, err := Parse(KindMatrix, kwargs(t, `{"rows":"work"}`))
	require.ErrorContains(t, err, "columns")

	_, err = Parse(KindMatrix, kwargs(t, `{"columns":"iteration"}`))
	require.ErrorContains(t, err, "rows")

	call, err := Parse(KindMatrix, kwargs(t, `{"rows":"work","columns":"iteration"}`))
	require.NoError(t, err)
	require.Equal(t, DefaultQuery, call.String("query"))
	// with no number named a cell counts, so `value` is simply absent
	require.False(t, call.Has("value"))
	require.False(t, call.Has("row_values"))
	require.False(t, call.Has("group_by"))

	call, err = Parse(KindMatrix, kwargs(t, `{
		"rows": "assignee",
		"columns": "iteration",
		"value": "points",
		"row_values": ["a", "b"],
		"column_values": ["s1"],
		"group_by": "work",
		"query": "map(select(.fields.type == \"allocation\"))"
	}`))
	require.NoError(t, err)
	require.Equal(t, "points", call.String("value"))
	require.Equal(t, []string{"a", "b"}, call.Strings("row_values"))
	require.Equal(t, []string{"s1"}, call.Strings("column_values"))
	require.Equal(t, "work", call.String("group_by"))

	// there is no rank on a matrix: the axes own the order
	_, err = Parse(KindMatrix, kwargs(t, `{"rows":"work","columns":"iteration","rank":"rank"}`))
	require.ErrorContains(t, err, "rank")
	require.ErrorContains(t, err, "rows (required)")
}

// TestHelpIsGeneratedFromTheTable pins the one rule the help follows:
// every argument is in it, with its tier, so the help and the check agree.
func TestHelpIsGeneratedFromTheTable(t *testing.T) {
	help := Help(KindBoard)
	for _, arg := range Kinds[KindBoard] {
		require.Contains(t, help, arg.Name)
		require.Contains(t, help, string(arg.Tier))
	}
	require.Contains(t, Help(KindList), `["type","title"]`)
}

// TestParseExpandIsALayerSpec: `expand` is a relation name or a layer of
// one, every layer carrying the list's own arguments and, optionally, the
// layer below it; `self` repeats a layer, and cannot be the first one
// because there is nothing above it to repeat (f4426ff).
func TestParseExpandIsALayerSpec(t *testing.T) {
	call, err := Parse(KindList, kwargs(t, `{"expand":"children"}`))
	require.NoError(t, err)
	layer := call.Expand()
	require.Equal(t, "children", layer.Relation)
	require.Nil(t, layer.Expand)

	call, err = Parse(KindGantt, kwargs(t, `{"start":"a","stop":"b","expand":{
		"relation": "children",
		"query": "map(select(.fields.status != \"done\"))",
		"fields": ["status", "title"],
		"details": ["labels"],
		"group_by": "assignee",
		"expand": 0
	}}`))
	require.NoError(t, err)
	layer = call.Expand()
	require.Equal(t, "children", layer.Relation)
	require.Equal(t, `map(select(.fields.status != "done"))`, layer.Query)
	require.Equal(t, []string{"status", "title"}, layer.Fields)
	require.Equal(t, []string{"labels"}, layer.Details)
	require.Equal(t, "assignee", layer.GroupBy)

	layers, forever := layer.Layers()
	require.Len(t, layers, 1)
	require.True(t, forever, "0: this layer, as far down as the relation goes")

	// a positive count is that many more levels of the same layer
	call, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","fields":["status"],"expand":2}}`))
	require.NoError(t, err)
	layers, forever = call.Expand().Layers()
	require.False(t, forever)
	require.Len(t, layers, 3)
	require.Same(t, layers[0], layers[2], "the layer itself, drawn at every level")

	// three layers, each the level below the one before it
	call, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","expand":{"relation":"blocks","expand":"children"}}}`))
	require.NoError(t, err)
	layers, forever = call.Expand().Layers()
	require.False(t, forever)
	require.Equal(t, []string{"children", "blocks", "children"},
		[]string{layers[0].Relation, layers[1].Relation, layers[2].Relation})

	// a layer may leave its relation out, for rows that list their own
	// children (query-rows.md, R3), but not give an empty one; it takes no
	// key of its own invention, a count is a whole number of levels, and
	// has nothing to repeat at the top
	call, err = Parse(KindList, kwargs(t, `{"expand":{"query":"."}}`))
	require.NoError(t, err)
	require.Equal(t, "", call.Expand().Relation)

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":""}}`))
	require.ErrorContains(t, err, "relation is empty")

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","depth":2}}`))
	require.ErrorContains(t, err, "takes no key depth")

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","expand":{"depth":2}}}`))
	require.ErrorContains(t, err, "layer 2 takes no key depth")

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","expand":-1}}`))
	require.ErrorContains(t, err, "expand is -1")

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","expand":{"relation":"blocks","expand":1.5}}}`))
	require.ErrorContains(t, err, "layer 2 expand is 1.5")

	_, err = Parse(KindList, kwargs(t, `{"expand":0}`))
	require.ErrorContains(t, err, "cannot be the first one")

	_, err = Parse(KindList, kwargs(t, `{"expand":true}`))
	require.ErrorContains(t, err, "a relation name or an object")
}

// TestParseBoardColumnWidth: the board's column width is a defaulted
// argument with a floor, because a column too narrow for a card's id line
// would be scrolled sideways past rather than read (10f676e).
func TestParseBoardColumnWidth(t *testing.T) {
	call, err := Parse(KindBoard, kwargs(t, `{"columns":"status"}`))
	require.NoError(t, err)
	require.Equal(t, 32, call.Int("column_width"))

	call, err = Parse(KindBoard, kwargs(t, `{"columns":"status","column_width":48}`))
	require.NoError(t, err)
	require.Equal(t, 48, call.Int("column_width"))

	// the floor is refused at parse time, so no renderer ever sees it
	_, err = Parse(KindBoard, kwargs(t, `{"columns":"status","column_width":9}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "column_width is 9, and the smallest is 10")

	_, err = Parse(KindBoard, kwargs(t, `{"columns":"status","column_width":"wide"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "whole number")

	// the help says the floor, from the same row of the table
	require.Contains(t, Help(KindBoard), "at least 10")
}

// TestParseOpen: the list and the gantt take `open`, true for every level, a
// number for that many from the roots, false or absent for none
// (doc/design/query-rows.md, R4); the kinds that do not nest do not.
func TestParseOpen(t *testing.T) {
	for _, kind := range []string{KindList, KindGantt} {
		base := `"start":"due","stop":"due",`
		if kind == KindList {
			base = ""
		}
		for text, levels := range map[string]int{`true`: -1, `false`: 0, `2`: 2, `0`: 0} {
			call, err := Parse(kind, kwargs(t, `{`+base+`"open":`+text+`}`))
			require.NoError(t, err, text)
			require.Equal(t, levels, call.OpenLevels(), text)
		}
		call, err := Parse(kind, kwargs(t, `{`+base[:max(len(base)-1, 0)]+`}`))
		require.NoError(t, err)
		require.Equal(t, 0, call.OpenLevels())

		_, err = Parse(kind, kwargs(t, `{`+base+`"open":"all"}`))
		require.ErrorContains(t, err, "open is true, false or a number of levels, not a string")
		_, err = Parse(kind, kwargs(t, `{`+base+`"open":1.5}`))
		require.ErrorContains(t, err, "open is 1.5")
		_, err = Parse(kind, kwargs(t, `{`+base+`"open":-1}`))
		require.ErrorContains(t, err, "open is -1")
	}

	_, err := Parse(KindBoard, kwargs(t, `{"columns":"status","open":true}`))
	require.ErrorContains(t, err, "takes no argument open")
}

// TestParseIncludeArchive: every kind with a query takes include_archive, a
// boolean that is false when absent, and so does a layer of `expand`
// (doc/design/include-archive.md, I2, I5).
func TestParseIncludeArchive(t *testing.T) {
	for _, kind := range []string{KindList, KindBoard, KindGantt, KindMatrix} {
		args := map[string]Arg{}
		for _, arg := range Kinds[kind] {
			args[arg.Name] = arg
		}
		require.Equal(t, Bool, args["include_archive"].Kind, kind)
	}

	call, err := Parse(KindList, kwargs(t, `{}`))
	require.NoError(t, err)
	require.False(t, call.Bool("include_archive"))
	require.False(t, call.Has("include_archive"), "the default is the absence of the argument")

	call, err = Parse(KindList, kwargs(t, `{"include_archive":true}`))
	require.NoError(t, err)
	require.True(t, call.Bool("include_archive"))

	_, err = Parse(KindList, kwargs(t, `{"include_archive":"yes"}`))
	require.ErrorContains(t, err, "include_archive is true or false")

	_, err = Parse(KindShow, kwargs(t, `{"id":"abc","include_archive":true}`))
	require.ErrorContains(t, err, "takes no argument include_archive")

	call, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","include_archive":true,"expand":{"relation":"children"}}}`))
	require.NoError(t, err)
	require.True(t, *call.Expand().IncludeArchive)
	require.Nil(t, call.Expand().Expand.IncludeArchive, "unnamed, it is the layer above's, resolved by the renderer")

	_, err = Parse(KindList, kwargs(t, `{"expand":{"relation":"children","include_archive":1}}`))
	require.ErrorContains(t, err, "include_archive is true or false")
}

// TestParseNewTakesADocument: `new` takes the document `issue new` takes,
// and refuses anything else in it, so that a flow learns at the call.
func TestParseNewTakesADocument(t *testing.T) {
	call, err := Parse(KindNew, nil)
	require.NoError(t, err)
	require.False(t, call.Has("doc"), "an empty form needs nothing")

	call, err = Parse(KindNew, kwargs(t, `{"doc":{"fields":{"type":"task","parent":"abc1234"},"body":"why"},"fields":["status"]}`))
	require.NoError(t, err)
	require.True(t, call.Has("doc"))
	require.Equal(t, []string{"status"}, call.Strings("fields"))

	_, err = Parse(KindNew, kwargs(t, `{"doc":"a title"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "object")

	_, err = Parse(KindNew, kwargs(t, `{"doc":{"title":"a title"}}`))
	require.Error(t, err, "a key issue new does not read is a mistake, not a field")
	require.Contains(t, err.Error(), "title")

	_, err = Parse(KindNew, kwargs(t, `{"doc":{"fields":{"type":"task"},"body":3}}`))
	require.Error(t, err)
}
