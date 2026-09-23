package schema

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/git-bug/git-bug/entities/config"
)

// The document a human edits is a view over the entities (E9).
//
//	preset: jira            # informational, ignored on import
//	shared:                 # YAML anchors, authoring only
//	  status: &status
//	    kind: enum
//	    values:
//	      - {id: to-do, name: To Do, category: unstarted}
//	types:
//	  epic:
//	    name: Epic
//	    fields:
//	      status: *status
//	      parent: {kind: relation, inverse: children, target_types: [initiative]}
//
// List and mapping position *is* the order:
// `ordinal` is never written by hand and never exported,
// so `export | import` emits zero operations, which is the round-trip test.
// `shared` is the human's anchors and nothing else reads it;
// the store holds one status entity per type (E2, `e7e58f2`).
type Document struct {
	// Preset names the preset a schema came from, for a reader.
	// Nothing in the store records it, so export never writes it.
	Preset string `yaml:"preset,omitempty"`
	// Shared is where anchors are declared. It is parsed and dropped:
	// the YAML parser has already expanded every alias by then.
	Shared yaml.MapSlice `yaml:"shared,omitempty"`
	// Types are the issue types, in the order the file lists them.
	Types *typeMap `yaml:"types,omitempty"`
}

// TypeDoc is one type in the document.
type TypeDoc struct {
	Name        string    `yaml:"name,omitempty"`
	Description string    `yaml:"description,omitempty"`
	Fields      *fieldMap `yaml:"fields,omitempty"`
}

// FieldDoc is one field in the document.
//
// The type is the mapping it sits under, never a member here:
// a field belongs to exactly one type, and its key is `<type>/<field>` (E2).
type FieldDoc struct {
	Kind        string     `yaml:"kind,omitempty"`
	Name        string     `yaml:"name,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Freeform    bool       `yaml:"freeform,omitempty"`
	Inverse     string     `yaml:"inverse,omitempty"`
	TargetTypes []string   `yaml:"target_types,omitempty"`
	Values      []ValueDoc `yaml:"values,omitempty"`
}

// ValueDoc is one enum value in the document.
type ValueDoc struct {
	Id          string `yaml:"id"`
	Name        string `yaml:"name,omitempty"`
	Category    string `yaml:"category,omitempty"`
	Description string `yaml:"description,omitempty"`
	Color       string `yaml:"color,omitempty"`
}

type typeMap = orderedMap[TypeDoc]
type fieldMap = orderedMap[FieldDoc]

// orderedMap is a YAML mapping that remembers the order it was written in,
// because that order is the schema's order (E5, E9).
//
// goccy hands a BytesUnmarshaler the node's own bytes with every alias already
// expanded, so an anchored field reads exactly as a spelled-out one does,
// and decoding those bytes twice — once for the keys, once for the values —
// is all an ordered mapping needs.
type orderedMap[T any] struct {
	keys   []string
	values map[string]T
}

func newOrderedMap[T any]() *orderedMap[T] {
	return &orderedMap[T]{values: map[string]T{}}
}

// Keys returns the mapping's keys in file order.
func (m *orderedMap[T]) Keys() []string {
	if m == nil {
		return nil
	}
	return m.keys
}

// Get returns one entry.
func (m *orderedMap[T]) Get(key string) (T, bool) {
	var zero T
	if m == nil {
		return zero, false
	}
	v, ok := m.values[key]
	return v, ok
}

// Set appends or replaces one entry, keeping insertion order.
func (m *orderedMap[T]) Set(key string, value T) {
	if m.values == nil {
		m.values = map[string]T{}
	}
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Len returns the number of entries.
func (m *orderedMap[T]) Len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

func (m *orderedMap[T]) UnmarshalYAML(data []byte) error {
	var order yaml.MapSlice
	if err := yaml.Unmarshal(data, &order); err != nil {
		return err
	}

	values := map[string]T{}
	if err := yaml.UnmarshalWithOptions(data, &values, yaml.DisallowUnknownField()); err != nil {
		return err
	}

	m.keys = nil
	m.values = values
	seen := make(map[string]struct{}, len(order))
	for _, item := range order {
		key := fmt.Sprint(item.Key)
		if _, twice := seen[key]; twice {
			return fmt.Errorf("key %q appears twice", key)
		}
		seen[key] = struct{}{}
		m.keys = append(m.keys, key)
	}

	return nil
}

func (m orderedMap[T]) MarshalYAML() (interface{}, error) {
	out := make(yaml.MapSlice, 0, len(m.keys))
	for _, key := range m.keys {
		out = append(out, yaml.MapItem{Key: key, Value: m.values[key]})
	}
	return out, nil
}

// ParseDocument reads a schema document, YAML or JSON.
//
// JSON is YAML, so one parser reads both,
// and an unknown key is an error rather than a silent no-op,
// which is the rule every document argument follows (cli-convention.md).
func ParseDocument(data []byte) (*Document, error) {
	doc := &Document{}
	if err := yaml.UnmarshalWithOptions(data, doc, yaml.DisallowUnknownField()); err != nil {
		return nil, fmt.Errorf("invalid schema document: %w", err)
	}
	if doc.Types == nil {
		doc.Types = newOrderedMap[TypeDoc]()
	}
	return doc, nil
}

// Marshal renders the document as YAML or as JSON, deterministically.
func (d *Document) Marshal(format string) ([]byte, error) {
	// Shared is the human's anchors; the parser expanded them,
	// so writing it back would duplicate every shared definition.
	out := *d
	out.Shared = nil

	switch format {
	case "yaml":
		return yaml.Marshal(&out)
	case "json":
		return yaml.MarshalWithOptions(&out, yaml.JSON())
	default:
		return nil, fmt.Errorf("unknown format %s", format)
	}
}

// Export renders the live schema as the document a human edits.
//
// A built-in with no entity of its own is left out:
// it exists in code on every type, and writing it to the file
// would make the next import create an entity that changes nothing (E4).
func Export(s *Schema) *Document {
	doc := &Document{Types: newOrderedMap[TypeDoc]()}

	for _, typeKey := range s.TypeKeys() {
		t := s.Types[typeKey]
		entry := TypeDoc{
			Name:        t.Name,
			Description: t.Description,
			Fields:      newOrderedMap[FieldDoc](),
		}

		for _, fieldKey := range t.FieldKeys() {
			field := t.Fields[fieldKey]
			if field.Builtin && !field.Configured {
				continue
			}
			entry.Fields.Set(fieldKey, exportField(field))
		}

		if entry.Fields.Len() == 0 {
			entry.Fields = nil
		}
		doc.Types.Set(typeKey, entry)
	}

	return doc
}

func exportField(field *Field) FieldDoc {
	out := FieldDoc{
		Kind:        string(field.Kind),
		Name:        field.Name,
		Description: field.Description,
	}
	if field.Builtin {
		// a built-in's values and targets are code, not config (E4)
		return out
	}

	out.Freeform = field.Freeform
	out.Inverse = field.Inverse
	out.TargetTypes = append([]string(nil), field.TargetTypes...)

	for _, value := range field.Values {
		out.Values = append(out.Values, ValueDoc{
			Id:          value.Id,
			Name:        value.Name,
			Category:    string(value.Category),
			Description: value.Description,
			Color:       value.Color,
		})
	}

	return out
}

// Validate checks the whole document before anything is written (E9).
//
// knownTypes are the types already in the store,
// so that a partial file adding one field to an existing type is legal.
func (d *Document) Validate(knownTypes []string) error {
	problems := &Problems{}

	known := make(map[string]struct{}, len(knownTypes)+d.Types.Len())
	for _, key := range knownTypes {
		known[key] = struct{}{}
	}
	for _, key := range d.Types.Keys() {
		known[key] = struct{}{}
	}

	for _, typeKey := range d.Types.Keys() {
		if err := config.ValidateKey(config.ShapeType, typeKey); err != nil {
			problems.add("type %s: %v", typeKey, err)
			continue
		}
		t, _ := d.Types.Get(typeKey)
		d.validateType(problems, typeKey, t, known)
	}

	return problems.err()
}

func (d *Document) validateType(problems *Problems, typeKey string, t TypeDoc, known map[string]struct{}) {
	inverses := map[string]string{}

	for _, fieldKey := range t.Fields.Keys() {
		field, _ := t.Fields.Get(fieldKey)
		where := fmt.Sprintf("field %s/%s", typeKey, fieldKey)

		if err := config.ValidateKey(config.ShapeField, typeKey+"/"+fieldKey); err != nil {
			problems.add("%s: %v", where, err)
			continue
		}

		kind, err := ParseKind(field.Kind)
		if err != nil {
			problems.add("%s: %v", where, err)
			continue
		}

		if builtinKind, isBuiltin := BuiltinKind(fieldKey); isBuiltin {
			// a built-in's kind is code; an entity overrides
			// only its name and description (E4)
			if kind != builtinKind {
				problems.add("%s: %s is built in with kind %s and can not be changed to %s",
					where, fieldKey, builtinKind, kind)
			}
			if len(field.Values) > 0 || len(field.TargetTypes) > 0 || field.Inverse != "" || field.Freeform {
				problems.add("%s: %s is built in; only its name and description are configurable",
					where, fieldKey)
			}
			continue
		}

		if kind.IsEnum() {
			d.validateValues(problems, where, field)
		} else if len(field.Values) > 0 {
			problems.add("%s: kind %s has no values", where, kind)
		}

		if kind.IsRelation() {
			if field.Inverse != "" {
				if other, twice := inverses[field.Inverse]; twice {
					problems.add("%s: inverse %s is already %s/%s's",
						where, field.Inverse, typeKey, other)
				}
				inverses[field.Inverse] = fieldKey
				if _, collides := t.Fields.Get(field.Inverse); collides {
					problems.add("%s: inverse %s collides with a field of the same type",
						where, field.Inverse)
				}
			}
			for _, target := range field.TargetTypes {
				if _, ok := known[target]; !ok {
					problems.add("%s: target type %s is neither in this document nor in the store",
						where, target)
				}
			}
		} else {
			if field.Inverse != "" {
				problems.add("%s: kind %s has no inverse", where, kind)
			}
			if len(field.TargetTypes) > 0 {
				problems.add("%s: kind %s has no target types", where, kind)
			}
		}

		if field.Freeform && kind != KindMultiEnum && kind != KindEnum {
			problems.add("%s: freeform is for enum kinds, not %s", where, kind)
		}
	}
}

func (d *Document) validateValues(problems *Problems, where string, field FieldDoc) {
	seen := map[string]struct{}{}
	for _, value := range field.Values {
		if value.Id == "" {
			problems.add("%s: a value has no id", where)
			continue
		}
		if err := config.ValidateName(ValuesPrefix + "/" + value.Id); err != nil {
			problems.add("%s: value %s: %v", where, value.Id, err)
			continue
		}
		if _, twice := seen[value.Id]; twice {
			problems.add("%s: value %s appears twice", where, value.Id)
			continue
		}
		seen[value.Id] = struct{}{}
		if _, err := ParseCategory(value.Category); err != nil {
			problems.add("%s: value %s: %v", where, value.Id, err)
		}
	}
}

// String renders the document as YAML, for an error message or a test.
func (d *Document) String() string {
	raw, err := d.Marshal("yaml")
	if err != nil {
		return fmt.Sprintf("<unprintable schema document: %v>", err)
	}
	return strings.TrimRight(string(raw), "\n")
}
