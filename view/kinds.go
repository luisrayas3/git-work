package view

// The view kinds, which are the `view.*` functions and the `view` subcommands.
const (
	KindList  = "list"
	KindBoard = "board"
	KindGantt = "gantt"
)

// Binding is one slot a view kind fills with a field key.
//
// It is data rather than a parameter list
// because three consumers read the same table:
// the view functions validate against it,
// a renderer knows what it may be handed,
// and the command line's help is generated from it.
type Binding struct {
	// Name is the keyword argument, and the key in a spec's bindings map.
	Name string
	// Required says a spec without it has no meaning.
	Required bool
	// Doc says what the field it names is for, one clause, for an error.
	Doc string
}

// Kinds is the whole contract between the views and the renderers:
// which slots each kind takes, and which of them it cannot do without.
//
// Every binding's value is one field key.
// A renderer that is handed a kind it does not have fails naming itself,
// so a view is never a command on one surface and not the other.
var Kinds = map[string][]Binding{
	KindList: {
		{Name: "title", Doc: "the field to show as each line's label"},
		{Name: "group_by", Doc: "the field whose value starts a new section"},
		{Name: "sort_by", Doc: "the field the lines are ordered by"},
	},
	KindBoard: {
		{Name: "columns", Required: true, Doc: "the field whose values are the columns"},
		{Name: "card_title", Doc: "the field to show on a card"},
		{Name: "group_by", Doc: "the field whose value starts a new swimlane"},
		{Name: "sort_by", Doc: "the field the cards within a column are ordered by"},
	},
	KindGantt: {
		{Name: "start", Required: true, Doc: "the date field a bar starts at"},
		{Name: "end", Required: true, Doc: "the date field a bar ends at"},
		{Name: "group_by", Doc: "the field whose value starts a new row group"},
		{Name: "label", Doc: "the field to show on a bar"},
		{Name: "progress", Doc: "the number field, 0 to 1, a bar is filled to"},
	},
}
