package view

import (
	"fmt"
	"sort"
	"strings"
)

// The view kinds, which are the `work.view.*` functions
// and the `git work view` subcommands.
const (
	KindList   = "list"
	KindBoard  = "board"
	KindGantt  = "gantt"
	KindMatrix = "matrix"
	KindShow   = "show"
	KindNew    = "new"
)

// Tier says how much of a view's behaviour an argument is responsible for.
//
// It is in the table rather than in prose
// because the help, the validation and a renderer's own checks
// all need the same answer.
//
// The third tier was called `feature` until 2026-10-02,
// from before any kind was drawn,
// and the help footnoted it as "in the table and not drawn yet",
// which read as *not implemented* once every kind was drawn:
// an agent took `group_by` and `expand` for sketches.
// It is `optional`, which is what it had always meant (f9c991e).
type Tier string

const (
	// Required: the view has no meaning without it.
	Required Tier = "required"
	// Defaulted: absent, it takes the value in the table.
	Defaulted Tier = "defaulted"
	// Optional: no default; absent, the view simply does not do that thing.
	Optional Tier = "optional"
)

// ValueKind is what an argument's JSON value has to be.
//
// It is coarser than the schema's field kinds on purpose:
// this is the shape of the call, not of the data.
type ValueKind string

const (
	// FieldKey is one field key, as a string.
	FieldKey ValueKind = "field key"
	// FieldKeys is a list of field keys.
	FieldKeys ValueKind = "field keys"
	// String is any string.
	String ValueKind = "string"
	// StringList is a list of any strings.
	StringList ValueKind = "strings"
	// Int is a whole number.
	Int ValueKind = "int"
	// Bool is true or false.
	Bool ValueKind = "bool"
	// Enum is one of the argument's Allowed values.
	Enum ValueKind = "enum"
	// Id is an issue id: a prefix or an alias, as everywhere else.
	Id ValueKind = "id"
	// Query is a jq program over the array `git work issue` prints.
	Query ValueKind = "query"
	// ExpandSpec is `expand`: a relation name, or a layer of one carrying
	// the list's own arguments and an `expand` of its own (expand.go).
	ExpandSpec ValueKind = "relation or layer"
	// SideTables is show's `expand`: what the list's takes, or a list of
	// it, one flat table per element beside the fields (side.go).
	SideTables ValueKind = "relations or layers"
	// OpenLevels is `open`: true for every level, false for none, or a whole
	// number of levels from the roots (doc/design/query-rows.md, R4).
	OpenLevels ValueKind = "bool or levels"
	// Document is `new`'s `doc`: the document `git work issue new` takes,
	// {"fields":{…},"body":"…","aliases":{…}}, with what is already decided
	// filled in (doc/design/create.md, C1).
	Document ValueKind = "issue document"
)

// Arg is one keyword argument of one view kind.
//
// Four consumers read the same row:
// Parse validates against it,
// the command's help is generated from it,
// a renderer knows what it may be handed,
// and the Starlark module takes the same keywords the shell does.
type Arg struct {
	// Name is the keyword, in the JSON object and in Starlark alike.
	Name string
	// Tier says whether it is required, defaulted or optional.
	Tier Tier
	// Kind is the shape of its value.
	Kind ValueKind
	// Allowed are the values an Enum accepts.
	Allowed []string
	// Min is the smallest value an Int takes; zero is no floor at all.
	Min int
	// Default is the JSON a Defaulted argument takes when it is absent.
	// The empty string and "null" both mean there is no value to apply,
	// which is what a default the renderer computes looks like here.
	Default string
	// Doc says what the argument does, one clause, for the help and an error.
	Doc string
}

// Kinds is the whole contract between a view call and a renderer:
// which arguments each kind takes, and what each of them means.
//
// A renderer that is handed a kind it does not draw fails naming itself,
// so a view is never a command on one surface and not the other.
var Kinds = map[string][]Arg{
	KindList: {
		rowsQueryArg,
		includeArchiveArg,
		{Name: "fields", Tier: Defaulted, Kind: FieldKeys, Default: `["type","title"]`,
			Doc: "the fields shown as columns, in order"},
		{Name: "details", Tier: Optional, Kind: FieldKeys,
			Doc: "the fields shown on a dim second line under each row"},
		{Name: "group_by", Tier: Optional, Kind: FieldKey,
			Doc: "the field whose value starts a new section; the rows with no value at all are the last section, (none)"},
		{Name: "expand", Tier: Optional, Kind: ExpandSpec, Doc: expandDoc},
		openArg,
	},
	KindBoard: {
		queryArg,
		includeArchiveArg,
		{Name: "columns", Tier: Required, Kind: FieldKey,
			Doc: "the field whose values are the columns"},
		{Name: "values", Tier: Defaulted, Kind: StringList,
			Doc: "the column values, in order; the field's schema order by default, which is resolved at render time"},
		{Name: "card", Tier: Defaulted, Kind: FieldKeys, Default: `["title"]`,
			Doc: "the fields shown on a card"},
		{Name: "column_width", Tier: Defaulted, Kind: Int, Default: `32`, Min: minColumnWidth,
			Doc: "the narrowest a column goes before the board scrolls sideways; when every column fits they share the width"},
		{Name: "group_by", Tier: Optional, Kind: FieldKey,
			Doc: "the field whose value starts a new swimlane; the cards with no value at all are the last swimlane, (none)"},
	},
	KindGantt: {
		rowsQueryArg,
		includeArchiveArg,
		{Name: "start", Tier: Required, Kind: FieldKey,
			Doc: "the date field a bar starts at"},
		{Name: "stop", Tier: Required, Kind: FieldKey,
			Doc: "the date field a bar ends at"},
		{Name: "label", Tier: Defaulted, Kind: FieldKey, Default: `"title"`,
			Doc: "the field shown on a bar"},
		{Name: "scale", Tier: Defaulted, Kind: Enum,
			Allowed: []string{"day", "week", "month", "quarter"}, Default: `"week"`,
			Doc: "how wide one column of the chart is"},
		{Name: "from", Tier: Defaulted, Kind: String,
			Doc: "the first date shown; the data's own extent by default"},
		{Name: "to", Tier: Defaulted, Kind: String,
			Doc: "the last date shown; the data's own extent by default"},
		{Name: "progress", Tier: Optional, Kind: FieldKey,
			Doc: "the number field, 0 to 1, a bar is filled to"},
		{Name: "group_by", Tier: Optional, Kind: FieldKey,
			Doc: "the field whose value starts a new row group; the rows with no value at all are the last group, (none)"},
		{Name: "expand", Tier: Optional, Kind: ExpandSpec, Doc: expandDoc},
		openArg,
	},
	// matrix is the two-axis summary: rows of one field by columns of
	// another, a sum in each cell (doc/design/allocations.md). It reads any
	// issue set, so allocations and story points are the same call.
	KindMatrix: {
		queryArg,
		includeArchiveArg,
		{Name: "rows", Tier: Required, Kind: FieldKey,
			Doc: "the field whose values are the rows"},
		{Name: "columns", Tier: Required, Kind: FieldKey,
			Doc: "the field whose values are the columns"},
		{Name: "value", Tier: Optional, Kind: FieldKey,
			Doc: "the number field summed in a cell; with none, a cell counts its issues"},
		{Name: "row_values", Tier: Defaulted, Kind: StringList,
			Doc: "the row values, in order; the axis's own order by default, which is resolved at render time"},
		{Name: "column_values", Tier: Defaulted, Kind: StringList,
			Doc: "the column values, in order; the axis's own order by default"},
		{Name: "group_by", Tier: Optional, Kind: FieldKey,
			Doc: "the field whose value starts a new block of rows; the rows with no value at all are the last block, (none)"},
	},
	// show is the one kind that is about a single issue,
	// so it takes an id where every other kind takes a query.
	KindShow: {
		{Name: "id", Tier: Required, Kind: Id,
			Doc: "the issue to show, by id prefix or alias"},
		{Name: "fields", Tier: Defaulted, Kind: FieldKeys,
			Doc: "the fields shown, in order; the type's fields in schema order by default"},
		// expand is the list's, drawn flat beside the fields: one name for
		// one object on every kind (doc/design/show-side-table.md, S1)
		{Name: "expand", Tier: Optional, Kind: SideTables, Doc: sideDoc},
	},
	// new is the interactive `issue new`: show's page over an issue that
	// does not exist yet, nothing written until Create, which commits the
	// draft as the one operation `issue new` commits (doc/design/create.md).
	// It takes the document `issue new` takes, with what is already decided
	// filled in; `fields` keeps show's meaning, so `doc` is the name.
	KindNew: {
		{Name: "doc", Tier: Optional, Kind: Document,
			Doc: `the draft to open on, as issue new takes it: {"fields":{"type":"task","parent":"abc1234"},"body":"…"}; every value stays editable`},
		{Name: "fields", Tier: Defaulted, Kind: FieldKeys,
			Doc: "the fields shown, in order; the type's fields in schema order by default"},
	},
}

// minColumnWidth is the floor under the board's `column_width`:
// a column narrower than this cannot draw a card's id line,
// so the board would scroll sideways past something unreadable
// (doc/design/terminal-renderer.md, Board).
const minColumnWidth = 10

// expandDoc is `expand` on the list and the gantt, which take one spec.
//
// It says the three things a reader cannot guess and has had to go and read
// the renderer for (f9c991e): either side of a relation is a name it takes,
// a layer's query is over that row's own children and not over the store,
// and a layer carries the level below it.
const expandDoc = `the relation nested under a row: "children", or a layer ` +
	`{"relation":…,"query":…,"include_archive":…,"fields":…,"details":…,"group_by":…,"expand":…}; ` +
	`a relation is a stored one (parent) or the inverse name of one (children), a row's own top-level ` +
	`children (ids or rows) replace it for that row, so a layer needs one only where a row above lists none; ` +
	`a layer's query runs over ` +
	`that row's own unarchived children (the archived too with include_archive, which a layer inherits from ` +
	`the call or the layer above unless it names its own), the keys it leaves out ` +
	`are the layer above's, and its expand is ` +
	`the level below: a layer, or a number of further levels this same layer draws, 0 for every one`

// sideDoc is `expand` on show: the list's spec, drawn as a flat table beside
// the fields rather than nested under a row (doc/design/show-side-table.md).
const sideDoc = `the issues a relation reaches from this one, a table each beside the fields ` +
	`(under them in a narrow window): what a list's expand takes, a relation name ("children", "blocks") ` +
	`or a layer {"relation":…,"query":…,"include_archive":…,"fields":…}, or a list of them, ` +
	`one table per element; a relation is a stored one or the inverse name the schema gives one, ` +
	`the query runs over the unarchived issues it reaches, fields are the columns after id and title, ` +
	`and details, group_by and expand are refused, a side table being flat`

// openArg is how unfolded a nested view is drawn (doc/design/query-rows.md,
// R4): a call argument and not a layer key, because it is how the view
// opens, which is the call's, and the folds after that are the person's.
var openArg = Arg{
	Name: "open", Tier: Optional, Kind: OpenLevels,
	Doc: "how unfolded a nested view opens: true every parent, a number that many levels from the roots, false or absent folded",
}

// queryArg is the same row on every kind that draws more than one issue,
// so that `query` means one thing across the whole table.
var queryArg = Arg{
	Name: "query", Tier: Defaulted, Kind: Query, Default: defaultQueryJSON,
	Doc: "the jq program the issues come from, over every unarchived issue; the default is all of them, last edited first",
}

// rowsQueryArg is `query` on the kinds that nest, whose rows may be more
// than issues (doc/design/query-rows.md): a top-level key is a row's
// identity on the screen, its id optional beside one, and a top-level
// children lists what nests under it.
var rowsQueryArg = Arg{
	Name: "query", Tier: Defaulted, Kind: Query, Default: defaultQueryJSON,
	Doc: "the jq program the rows come from, over every unarchived issue; the default is all of them, last edited first; " +
		"a row may add a key, its identity on screen (unique, not starting with +), with or without the id of the issue it acts on, " +
		"and children, the ids or rows nested under it",
}

// includeArchiveArg brings the archived back into the input `query` runs
// over, the archived being left out of it otherwise
// (doc/design/include-archive.md).
//
// Its default, false, is the absence of the argument rather than a value the
// table applies, so that a call that does not name it neither says it on the
// call line nor carries it into the copied command.
var includeArchiveArg = Arg{
	Name: "include_archive", Tier: Defaulted, Kind: Bool,
	Doc: "include the archived issues in the input the query runs over; false by default",
}

// KindNames lists the view kinds, in a stable order.
func KindNames() []string {
	names := make([]string, 0, len(Kinds))
	for name := range Kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Help is a kind's argument table as the command line prints it,
// so that the help and the validation can never drift apart.
//
// A default is shown where it is short enough to read in a column; the one
// that is not — the default query — is a sentence in its own doc instead.
// An `optional` row has no default at all: absent, the view does not do that
// thing, which is what the footnote under the table says.
func Help(kind string) string {
	var b strings.Builder
	for _, arg := range Kinds[kind] {
		shape := string(arg.Kind)
		switch {
		case arg.Kind == Enum:
			shape += " " + strings.Join(arg.Allowed, ", ")
		case arg.Kind == Int && arg.Min != 0:
			shape += fmt.Sprintf(", at least %d", arg.Min)
		}

		tier := string(arg.Tier)
		if arg.Tier == Defaulted && arg.hasDefault() && len(arg.Default) <= 24 {
			tier += " " + arg.Default
		}

		fmt.Fprintf(&b, "  %-16s %-32s %-18s %s\n", arg.Name, shape, tier, arg.Doc)
	}
	return b.String()
}

// argNames names a kind's arguments with their tier, for an error.
func argNames(args []Arg) string {
	names := make([]string, 0, len(args))
	for _, arg := range args {
		names = append(names, fmt.Sprintf("%s (%s)", arg.Name, arg.Tier))
	}
	return strings.Join(names, ", ")
}

func (a Arg) hasDefault() bool {
	return a.Default != "" && a.Default != "null"
}
