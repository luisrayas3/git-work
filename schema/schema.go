// Package schema is the compiled projection of the type and field config
// entities, and the rules an issue write is checked against
// (doc/design/schema.md).
//
// It is a pure library:
// it reads config entities as documents handed to it
// and never reaches for a repository itself,
// so the cache can depend on it and validate on the write path
// while entities/issue stays free of any schema import
// (doc/design/schema.md, "Validate on write, lenient on read").
//
// The store is the source of truth;
// schema.yaml is an authoring file that reaches the refs
// only through `git work schema import`
// (doc/design/schema.md, "Configuration is entities, not a file").
package schema

import (
	"encoding/json"
	"sort"

	"github.com/git-bug/git-bug/entity"
)

// Value is one value of an enum field, stored by its stable id.
//
// A rename in Jira then changes one attribute and no history
// (doc/design/schema.md, "Values are stored by id"),
// which is why nothing ever stores a display name.
type Value struct {
	Id          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Ordinal     int      `json:"ordinal"`
	Category    Category `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Color       string   `json:"color,omitempty"`

	// Aliases are what the value is in other systems, by system,
	// held on the field entity as `alias_<system>/<id>`
	// (doc/design/jira.md, "The mapping is aliases on the schema").
	Aliases map[string]string `json:"aliases,omitempty"`
}

// Field is one (type, field) config entity, compiled.
//
// Every field belongs to exactly one type
// (doc/design/schema.md, "A field belongs to one type"):
// `task/status` and `epic/status` are two entities,
// and sharing is YAML anchors at authoring time, never in the store.
type Field struct {
	// Type is the key of the type this field belongs to.
	Type string `json:"type"`
	// Key is the field key, what an issue stores the value under.
	Key string `json:"key"`

	Kind        Kind   `json:"kind"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Ordinal     int    `json:"ordinal"`

	// Freeform allows values outside the value list, as Jira's labels do
	// (doc/design/schema.md, "Kinds are a fixed list").
	Freeform bool `json:"freeform,omitempty"`
	// Values are the enum values, in (ordinal, id) order.
	Values []Value `json:"values,omitempty"`

	// Inverse is what the derived side of a relation reads as
	// (doc/design/schema.md, "Relations"),
	// never a key an issue can write.
	Inverse string `json:"inverse,omitempty"`
	// TargetTypes restricts what a relation may point at; empty is anything.
	TargetTypes []string `json:"target_types,omitempty"`
	// Link marks a relation as a link
	// (doc/design/schema.md, "A link is a marked relation"):
	// show draws it folded into one `links` row and its targets as side
	// tables, and its inverse is a link too. Only the views read it; it is
	// not a field role, since Go names no field by it.
	Link bool `json:"link,omitempty"`

	// Aliases are what the field is in other systems, by system
	// (doc/design/jira.md, "The mapping is aliases on the schema"):
	// for Jira a field reference, `customfield_10016` or `link:10000`.
	Aliases map[string]string `json:"aliases,omitempty"`

	// Builtin marks the four fields that exist in code on every type
	// (doc/design/schema.md, "Built-ins are code").
	Builtin bool `json:"builtin,omitempty"`
	// Configured marks a field an entity defines,
	// which for a built-in means an entity overrides its configurable parts.
	Configured bool `json:"configured,omitempty"`
	// EntityId is that entity's id, unset for a bare built-in.
	EntityId entity.Id `json:"entity_id,omitempty"`
}

// Value returns one value of the field by id.
func (f *Field) Value(id string) (Value, bool) {
	for _, value := range f.Values {
		if value.Id == id {
			return value, true
		}
	}
	return Value{}, false
}

// ValueIds lists the field's value ids in order, for an error message.
func (f *Field) ValueIds() []string {
	ids := make([]string, len(f.Values))
	for i, value := range f.Values {
		ids[i] = value.Id
	}
	return ids
}

// ValuesInCategory lists the field's values of one category, in order.
func (f *Field) ValuesInCategory(c Category) []Value {
	var values []Value
	for _, value := range f.Values {
		if value.Category == c {
			values = append(values, value)
		}
	}
	return values
}

// Type is one type config entity, compiled, with its fields.
type Type struct {
	Key         string `json:"key"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Ordinal     int    `json:"ordinal"`

	// Aliases are what the type is in other systems, by system
	// (doc/design/jira.md, "The mapping is aliases on the schema"):
	// for Jira an issue type id.
	Aliases map[string]string `json:"aliases,omitempty"`

	// Show is the type's show spec as JSON, nil where the type has none:
	// what show takes less the id, `{"expand":["children","links"]}`, which
	// a view's `shows` overrides whole
	// (doc/design/views.md, "A spec is whole, never merged"). The views
	// parse and check it; it changes no storage, write or query.
	Show json.RawMessage `json:"show,omitempty"`

	// RetypeTargets are the types an issue of this type may become by a
	// `set` of its type (doc/design/schema.md, "A type says what it may
	// become"): nil is any type, empty is none. It is policy: a local write
	// is checked against it, a Jira pull is not
	// (doc/design/issue.md, "A type change is a set").
	RetypeTargets *[]string `json:"retype_targets,omitempty"`

	// Fields are the type's fields by key, built-ins included.
	Fields map[string]*Field `json:"fields"`

	// EntityId is the type entity's id.
	EntityId entity.Id `json:"entity_id,omitempty"`
}

// MayBecome reports whether an issue of the type may be retyped to another,
// and else the types it may become, for the refusal
// (doc/design/schema.md, "A type says what it may become"). Absent retype
// targets allow any type.
func (t *Type) MayBecome(typeKey string) (bool, []string) {
	if t.RetypeTargets == nil || typeKey == t.Key {
		return true, nil
	}
	for _, target := range *t.RetypeTargets {
		if target == typeKey {
			return true, nil
		}
	}
	return false, *t.RetypeTargets
}

// CheckRetypeSystems refuses a type that syncs to a system listing a retype
// target that does not (type-change.md, TC6): a type with a non-empty alias
// for a system may become only types with a non-empty alias for that same
// system, or an issue linked there could be retyped out of the sync. A type
// with no aliases is unconstrained, and nothing here knows what a system is.
// An import checks the schema it would leave, so that a partial file is
// checked against the types already in the store.
func (s *Schema) CheckRetypeSystems() error {
	problems := &Problems{}
	for _, key := range s.TypeKeys() {
		t := s.Types[key]
		if t.RetypeTargets == nil {
			continue
		}
		systems := make([]string, 0, len(t.Aliases))
		for system, alias := range t.Aliases {
			if alias != "" {
				systems = append(systems, system)
			}
		}
		sort.Strings(systems)
		for _, target := range *t.RetypeTargets {
			other, ok := s.Types[target]
			if !ok {
				continue // a dangling target is reported on compile (D4)
			}
			for _, system := range systems {
				if other.Aliases[system] == "" {
					problems.add("type %s: retype target %s does not sync to %s, as %s does", key, target, system, key)
				}
			}
		}
	}
	return problems.err()
}

// Field returns one field of the type by key.
func (t *Type) Field(key string) (*Field, bool) {
	f, ok := t.Fields[key]
	return f, ok
}

// FieldKeys lists the type's fields in (ordinal, key) order.
func (t *Type) FieldKeys() []string {
	fields := make([]*Field, 0, len(t.Fields))
	for _, field := range t.Fields {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Ordinal != fields[j].Ordinal {
			return fields[i].Ordinal < fields[j].Ordinal
		}
		return fields[i].Key < fields[j].Key
	})

	keys := make([]string, len(fields))
	for i, field := range fields {
		keys[i] = field.Key
	}
	return keys
}

// Schema is every unarchived type and field entity, compiled, plus the built-ins.
//
// An empty schema is the bootstrap state, not an error:
// a repository with no config entities runs on the built-ins alone
// (doc/design/schema.md, "Built-ins are code"),
// and writes are then unvalidated.
type Schema struct {
	// Types are the issue types by key.
	Types map[string]*Type `json:"types"`
	// Problems are the dangling references and unreadable attributes found
	// while compiling. A read never fails on what was written before
	// (doc/design/schema.md, "Structural rules are frozen"),
	// so they are reported and never returned as an error.
	Problems []string `json:"problems,omitempty"`
}

// Type returns one type by key.
func (s *Schema) Type(key string) (*Type, bool) {
	t, ok := s.Types[key]
	return t, ok
}

// TypeKeys lists the types in (ordinal, key) order.
func (s *Schema) TypeKeys() []string {
	types := make([]*Type, 0, len(s.Types))
	for _, t := range s.Types {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		if types[i].Ordinal != types[j].Ordinal {
			return types[i].Ordinal < types[j].Ordinal
		}
		return types[i].Key < types[j].Key
	})

	keys := make([]string, len(types))
	for i, t := range types {
		keys[i] = t.Key
	}
	return keys
}

// Empty reports whether the schema holds no type at all,
// which is the bootstrap state: nothing to validate a write against
// (doc/design/schema.md, "Built-ins are code").
func (s *Schema) Empty() bool {
	return len(s.Types) == 0
}

// Field returns one field of one type.
func (s *Schema) Field(typeKey, fieldKey string) (*Field, bool) {
	t, ok := s.Types[typeKey]
	if !ok {
		return nil, false
	}
	return t.Field(fieldKey)
}
