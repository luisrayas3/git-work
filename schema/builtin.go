package schema

import (
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/entities/issue"
)

// The five built-in field keys (doc/design/schema.md, "Built-ins are code";
// doc/design/schema.md, "Rank is built in";
// doc/design/schema.md, "Color is a built-in a type switches on").
//
// They are taken from entities/issue where the entity names them rather
// than spelled again here,
// because the entity guarantees them
// and two spellings of one key would drift.
const (
	// TitleKey is the field every surface needs to show anything.
	TitleKey = issue.TitleKey
	// TypeKey is the field the rest of the schema is found by.
	TypeKey = "type"
	// ArchivedKey is the field the default listing hides by.
	ArchivedKey = issue.ArchivedKey
	// RankKey is the field a view's manual order is written to.
	RankKey = "rank"
	// ColorKey is the field an issue's color is held in, on a type that
	// switches it on.
	ColorKey = "color"
)

// Builtin describes one of the five fields that exist in code.
//
// Status is deliberately not here: it is a preset field of kind enum on
// every work type, and behaviour keyed on categories applies where a type
// has one (doc/design/schema.md, "No field roles").
type Builtin struct {
	Key         string
	Kind        Kind
	Name        string
	Description string
	Ordinal     int
	// Switched says the field exists only on a type whose `color` attribute
	// is true, which is `color` alone; the other four are on every type
	// (doc/design/schema.md, "Color is a built-in a type switches on").
	Switched bool
}

// Builtins are the built-in fields, in the order they are shown.
//
// Their ordinals are negative so that they sort before every configured field
// whatever ordinals an import assigns, and so that an override that carries no
// ordinal of its own keeps its built-in place.
//
// `rank` is built in (doc/design/schema.md, "Rank is built in"):
// Jira's Rank and Linear's sortOrder put an order on every issue,
// so it is part of the native models the schema has to represent,
// and every type in both presets carried it anyway.
// It is nullable and starts null — `new` writes none, the first drag does —
// and a null rank sorts after every set one, ties broken by id.
var Builtins = []Builtin{
	{
		Key:         TitleKey,
		Kind:        KindText,
		Name:        "Title",
		Description: "What the issue is, in one line.",
		Ordinal:     -30,
	},
	{
		Key:         TypeKey,
		Kind:        KindEnum,
		Name:        "Type",
		Description: "Which type's fields and workflow the issue follows.",
		Ordinal:     -20,
	},
	{
		Key:         ArchivedKey,
		Kind:        KindBool,
		Name:        "Archived",
		Description: "Whether the issue is still worth looking at.",
		Ordinal:     -10,
	},
	{
		Key:         RankKey,
		Kind:        KindRank,
		Name:        "Rank",
		Description: "Manual board and backlog order, sorted by (rank, id).",
		Ordinal:     -5,
	},
	{
		Key:         ColorKey,
		Kind:        KindColor,
		Name:        "Color",
		Description: "The color the issue is drawn in, out of the terminal's palette.",
		Ordinal:     -3,
		Switched:    true,
	},
}

// Colors are the values the built-in `color` takes: the twelve ANSI colors
// that read on any theme, drawn with the terminal's own palette
// (doc/design/schema.md, "Color is a built-in a type switches on").
var Colors = []string{
	"red", "green", "yellow", "blue", "magenta", "cyan",
	"bright-red", "bright-green", "bright-yellow", "bright-blue", "bright-magenta", "bright-cyan",
}

// IsColor reports whether a value is one of Colors.
func IsColor(value string) bool {
	for _, c := range Colors {
		if c == value {
			return true
		}
	}
	return false
}

// checkColor is the refusal of a value that is not one of Colors, naming them
// (doc/design/schema.md, "Errors name the alternatives").
func checkColor(value string) error {
	if IsColor(value) {
		return nil
	}
	return fmt.Errorf("%q is not a color; valid colors: %s", value, strings.Join(Colors, ", "))
}

// IsBuiltin reports whether a field key is one of the five.
func IsBuiltin(key string) bool {
	return builtin(key) != nil
}

// OnEveryType reports whether a field key is a built-in every type has:
// every one but `color`, which only a type that switches it on has.
func OnEveryType(key string) bool {
	b := builtin(key)
	return b != nil && !b.Switched
}

// BuiltinKind returns the kind a built-in key is fixed to.
func BuiltinKind(key string) (Kind, bool) {
	if b := builtin(key); b != nil {
		return b.Kind, true
	}
	return "", false
}

func builtin(key string) *Builtin {
	for i := range Builtins {
		if Builtins[i].Key == key {
			return &Builtins[i]
		}
	}
	return nil
}

// newBuiltinField projects a built-in onto one type.
func newBuiltinField(typeKey string, b Builtin) *Field {
	return &Field{
		Type:        typeKey,
		Key:         b.Key,
		Kind:        b.Kind,
		Name:        b.Name,
		Description: b.Description,
		Ordinal:     b.Ordinal,
		Builtin:     true,
	}
}
