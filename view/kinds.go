package view

import (
	"fmt"
	"sort"
	"strings"
)

// The view kinds, which are the `work.view.*` functions
// and the `git work view` subcommands.
const (
	KindList  = "list"
	KindBoard = "board"
	KindGantt = "gantt"
	KindShow  = "show"
)

// Tier says how much of a view's behaviour an argument is responsible for.
//
// It is in the table rather than in prose
// because the help, the validation and a renderer's own checks
// all need the same answer,
// and because it is the honest way to publish a table
// whose Feature rows are not drawn yet.
type Tier string

const (
	// Required: the view has no meaning without it.
	Required Tier = "required"
	// Defaulted: absent, it takes the value in the table.
	Defaulted Tier = "defaulted"
	// Feature: absent, the view simply does not do that thing.
	Feature Tier = "feature"
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
	// Enum is one of the argument's Allowed values.
	Enum ValueKind = "enum"
	// Id is an issue id: a prefix or an alias, as everywhere else.
	Id ValueKind = "id"
	// Query is a jq program over the array `git work issue` prints.
	Query ValueKind = "query"
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
	// Tier says whether it is required, defaulted or a feature.
	Tier Tier
	// Kind is the shape of its value.
	Kind ValueKind
	// Allowed are the values an Enum accepts.
	Allowed []string
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
		queryArg,
		{Name: "fields", Tier: Defaulted, Kind: FieldKeys, Default: `["title"]`,
			Doc: "the fields shown as columns, in order"},
		{Name: "details", Tier: Feature, Kind: FieldKeys,
			Doc: "the fields shown on a dim second line under each row"},
		{Name: "group_by", Tier: Feature, Kind: FieldKey,
			Doc: "the field whose value starts a new section"},
		{Name: "expand", Tier: Feature, Kind: FieldKey,
			Doc: "the relation whose targets are nested under a row"},
		{Name: "depth", Tier: Feature, Kind: Int,
			Doc: "how many levels of nesting to expand"},
		{Name: "rank", Tier: Feature, Kind: FieldKey,
			Doc: "the rank field rows are ordered and dragged by"},
	},
	KindBoard: {
		queryArg,
		{Name: "columns", Tier: Required, Kind: FieldKey,
			Doc: "the field whose values are the columns"},
		{Name: "values", Tier: Defaulted, Kind: StringList,
			Doc: "the column values, in order; the field's schema order by default, which is resolved at render time"},
		{Name: "card", Tier: Defaulted, Kind: FieldKeys, Default: `["title"]`,
			Doc: "the fields shown on a card"},
		{Name: "group_by", Tier: Feature, Kind: FieldKey,
			Doc: "the field whose value starts a new swimlane"},
		{Name: "rank", Tier: Feature, Kind: FieldKey,
			Doc: "the rank field cards are ordered and dragged by"},
	},
	KindGantt: {
		queryArg,
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
		{Name: "progress", Tier: Feature, Kind: FieldKey,
			Doc: "the number field, 0 to 1, a bar is filled to"},
		{Name: "group_by", Tier: Feature, Kind: FieldKey,
			Doc: "the field whose value starts a new row group"},
		{Name: "expand", Tier: Feature, Kind: FieldKey,
			Doc: "the relation whose targets are nested under a bar"},
		{Name: "depth", Tier: Feature, Kind: Int,
			Doc: "how many levels of nesting to expand"},
		{Name: "rank", Tier: Feature, Kind: FieldKey,
			Doc: "the rank field rows are ordered and dragged by"},
	},
	// show is the one kind that is about a single issue,
	// so it takes an id where every other kind takes a query.
	KindShow: {
		{Name: "id", Tier: Required, Kind: Id,
			Doc: "the issue to show, by id prefix or alias"},
		{Name: "fields", Tier: Defaulted, Kind: FieldKeys,
			Doc: "the fields shown, in order; the type's fields in schema order by default"},
	},
}

// queryArg is the same row on every kind that draws more than one issue,
// so that `query` means one thing across the whole table.
var queryArg = Arg{
	Name: "query", Tier: Defaulted, Kind: Query, Default: defaultQueryJSON,
	Doc: "the jq program the issues come from; the default is every unarchived issue, last edited first",
}

// Args returns one kind's argument table.
func Args(kind string) ([]Arg, bool) {
	args, ok := Kinds[kind]
	return args, ok
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
func Help(kind string) string {
	var b strings.Builder
	for _, arg := range Kinds[kind] {
		shape := string(arg.Kind)
		if arg.Kind == Enum {
			shape += " " + strings.Join(arg.Allowed, ", ")
		}

		tier := string(arg.Tier)
		if arg.Tier == Defaulted && arg.hasDefault() && len(arg.Default) <= 24 {
			tier += " " + arg.Default
		}

		fmt.Fprintf(&b, "  %-10s %-32s %-18s %s\n", arg.Name, shape, tier, arg.Doc)
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
