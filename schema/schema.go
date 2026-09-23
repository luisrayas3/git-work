// Package schema is the compiled projection of the type and field config
// entities, and the rules an issue write is checked against
// (doc/design/config-entity.md E4, E8; configurable-schema.md D2, D4, `bb9e89e`).
//
// It is a pure library:
// it reads config entities as documents handed to it
// and never reaches for a repository itself,
// so the cache can depend on it and validate on the write path
// while entities/issue stays free of any schema import (E8).
//
// The store is the source of truth;
// schema.yaml is an authoring file that reaches the refs
// only through `git work schema import` (E1, `0740bf3`).
package schema

import (
	"sort"

	"github.com/git-bug/git-bug/entity"
)

// Value is one value of an enum field, stored by its stable id.
//
// A rename in Jira then changes one attribute and no history (D2),
// which is why nothing ever stores a display name.
type Value struct {
	Id          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Ordinal     int      `json:"ordinal"`
	Category    Category `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Color       string   `json:"color,omitempty"`
}

// Field is one (type, field) config entity, compiled.
//
// Every field belongs to exactly one type (E2, `e7e58f2`):
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

	// Freeform allows values outside the value list, as Jira's labels do (E3).
	Freeform bool `json:"freeform,omitempty"`
	// Values are the enum values, in (ordinal, id) order.
	Values []Value `json:"values,omitempty"`

	// Inverse is what the derived side of a relation reads as (D4),
	// never a key an issue can write.
	Inverse string `json:"inverse,omitempty"`
	// TargetTypes restricts what a relation may point at; empty is anything.
	TargetTypes []string `json:"target_types,omitempty"`

	// Builtin marks the three fields that exist in code on every type (E4).
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

// Type is one type config entity, compiled, with its fields.
type Type struct {
	Key         string `json:"key"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Ordinal     int    `json:"ordinal"`

	// Fields are the type's fields by key, built-ins included.
	Fields map[string]*Field `json:"fields"`

	// EntityId is the type entity's id.
	EntityId entity.Id `json:"entity_id,omitempty"`
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
// a repository with no config entities runs on the built-ins alone (E4),
// and writes are then unvalidated.
type Schema struct {
	// Types are the issue types by key.
	Types map[string]*Type `json:"types"`
	// Problems are the dangling references and unreadable attributes found
	// while compiling. A read never fails on what was written before (D6),
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
// which is the bootstrap state: nothing to validate a write against (E4).
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
