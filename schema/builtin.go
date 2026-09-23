package schema

import "github.com/git-bug/git-bug/entities/issue"

// The three built-in field keys (E4).
//
// They are taken from entities/issue rather than spelled again here,
// because the entity guarantees them
// and two spellings of one key would drift.
const (
	// TitleKey is the field every surface needs to show anything.
	TitleKey = issue.TitleKey
	// TypeKey is the field the rest of the schema is found by.
	TypeKey = "type"
	// ArchivedKey is the field the default listing hides by.
	ArchivedKey = issue.ArchivedKey
)

// Builtin describes one of the three fields that exist in code on every type.
//
// Status is deliberately not here (`d56e6f1`):
// it is a preset field of kind enum on every work type,
// and behaviour keyed on categories applies where a type has one.
type Builtin struct {
	Key         string
	Kind        Kind
	Name        string
	Description string
	Ordinal     int
}

// Builtins are the fields every type has, in the order they are shown.
//
// Their ordinals are negative so that they sort before every configured field
// whatever ordinals an import assigns, and so that an override that carries no
// ordinal of its own keeps its built-in place.
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
}

// IsBuiltin reports whether a field key is one of the three.
func IsBuiltin(key string) bool {
	return builtin(key) != nil
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
