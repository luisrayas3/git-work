package view

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/schema"
)

// `shows`: how each issue type is shown, whichever view meets it
// (doc/design/app.md A7, approved 2026-10-15).
//
// A view's call is two parts. The core is what this view is — its query,
// its fields, its nesting. `shows` maps a type key to a spec, which is
// exactly what show takes less the issue: `fields`, the rows of its fields
// table, and `expand`, the side tables drawn beside them. Show itself takes
// the id and `shows`, draws the issue by its type's spec, and every page it
// opens is show with that issue's id and the same `shows`; so a story is
// shown one way across a flow, whatever it was opened from.
//
// It replaced the map `Enter` opened by (doc/design/show-from-a-view.md, V1),
// called `show`, and show's own `fields` and `expand`. A list's and a
// gantt's `expand`, their nesting, is the core's and is not in a spec.
//
// A type's spec is, most specific first and whole, never merged key by key:
// the call's `shows[type]`, then the type's `show` in the schema, then the
// built-in spec, `{"expand":["links"]}`.

// specKeys are the keys a spec takes, in the order an error names them.
var specKeys = []string{"fields", "expand"}

// BuiltinSpec is the spec of a type neither the call nor the schema names:
// every field in schema order, the link fields folded into the links row,
// and the links themselves as side tables (link-fields.md, L4).
const BuiltinSpec = `{"expand":["links"]}`

// Spec is one type's spec, parsed.
type Spec struct {
	// Fields are the rows of show's fields table, in order; nil is the
	// type's fields in schema order.
	Fields []string
	// Tables are `expand`, a flat side table per element.
	Tables []*Layer
	// Raw is the spec as it was given, compacted.
	Raw json.RawMessage
}

// SpecSource is where a type's spec came from.
type SpecSource int

const (
	// SpecFromCall is the call's own `shows[type]`.
	SpecFromCall SpecSource = iota
	// SpecFromSchema is the type's `show` in the schema.
	SpecFromSchema
	// SpecBuiltin is BuiltinSpec.
	SpecBuiltin
)

// Defaulted says the spec is not the call's, so the first line draws it
// dim, as it draws a defaulted argument (A7).
func (s SpecSource) Defaulted() bool {
	return s != SpecFromCall
}

// ParseSpec reads one spec: an object of `fields`, a list of field keys,
// and `expand`, what show's side tables take (side.go).
func ParseSpec(raw json.RawMessage) (*Spec, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf(`is an object of %s, not %s`, strings.Join(specKeys, " and "), jsonKind(raw))
	}
	for _, key := range sortedKeys(object) {
		if !contains(specKeys, key) {
			return nil, fmt.Errorf("takes no key %s, a spec takes %s", key, strings.Join(specKeys, " and "))
		}
	}

	spec := &Spec{Raw: compact(raw)}
	if fields, ok := object["fields"]; ok && !isNull(fields) {
		var list []string
		if err := json.Unmarshal(fields, &list); err != nil {
			return nil, fmt.Errorf("fields is a list of field keys, not %s", jsonKind(fields))
		}
		for _, item := range list {
			if strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("fields has an empty field key")
			}
		}
		spec.Fields = list
	}
	if expand, ok := object["expand"]; ok && !isNull(expand) {
		tables, err := parseSideTables(expand)
		if err != nil {
			return nil, fmt.Errorf("expand %w", err)
		}
		spec.Tables = tables
	}
	return spec, nil
}

// parseShows reads `shows`: an object of type keys, each a spec. What the
// schema has to say about the keys and the names in each spec is
// CheckSchema's.
func parseShows(raw json.RawMessage) (map[string]*Spec, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf(`is an object of type keys, each a spec of fields and expand: {"epic":{"expand":["children","links"]}}, not %s`, jsonKind(raw))
	}
	out := make(map[string]*Spec, len(entries))
	for _, typeKey := range sortedKeys(entries) {
		if strings.TrimSpace(typeKey) == "" {
			return nil, fmt.Errorf("has an empty type key")
		}
		spec, err := ParseSpec(entries[typeKey])
		if err != nil {
			return nil, fmt.Errorf("%s %w", typeKey, err)
		}
		out[typeKey] = spec
	}
	return out, nil
}

// SpecFor is the spec an issue of a type is shown by: the call's
// `shows[type]`, else the type's `show` in the schema, else the built-in
// spec, whole (A7). A schema spec that no longer reads falls to the built-in,
// since a read never fails on what was written before.
func SpecFor(shows json.RawMessage, s *schema.Schema, typeKey string) (*Spec, SpecSource) {
	if !isNull(shows) {
		if entries, err := parseShows(shows); err == nil {
			if spec, ok := entries[typeKey]; ok {
				return spec, SpecFromCall
			}
		}
	}
	if s != nil {
		if t, ok := s.Type(typeKey); ok && len(t.Show) > 0 {
			if spec, err := ParseSpec(t.Show); err == nil {
				return spec, SpecFromSchema
			}
		}
	}
	spec, _ := ParseSpec(json.RawMessage(BuiltinSpec))
	return spec, SpecBuiltin
}

// ShowFor is the show call that opens an issue from a view: its id, and the
// view's `shows`, carried on so that every page it opens opens by them too.
func ShowFor(shows json.RawMessage, id string) (*Call, error) {
	kwargs := map[string]json.RawMessage{"id": mustString(id)}
	if !isNull(shows) {
		kwargs["shows"] = shows
	}
	return Parse(KindShow, kwargs)
}

// checkShows checks `shows` against the live schema: every key a type the
// schema has, and every spec what CheckSpec accepts of that type.
func checkShows(call *Call, s *schema.Schema) error {
	raw, ok := call.Args["shows"]
	if !ok {
		return nil
	}
	entries, err := parseShows(raw)
	if err != nil {
		return fmt.Errorf("view %s: shows %w", call.Kind, err)
	}
	for _, typeKey := range sortedKeys(entries) {
		if s == nil || s.Empty() {
			return fmt.Errorf("view %s: shows %s needs a schema, and no type is defined", call.Kind, typeKey)
		}
		if _, ok := s.Type(typeKey); !ok {
			return fmt.Errorf("view %s: shows names type %s, which the schema does not have; the types are %s",
				call.Kind, typeKey, strings.Join(s.TypeKeys(), ", "))
		}
		if err := CheckSpec(entries[typeKey], s, typeKey); err != nil {
			return fmt.Errorf("view %s: shows %s %w", call.Kind, typeKey, err)
		}
	}
	return nil
}

// CheckSpec checks one spec against the type it is keyed under, as show's
// call is checked (show-from-a-view.md, V2): every field one of the type's,
// or links; every table a relation the type's issues have, stored or
// inverse, or links; and every layer's names what its rows can hold. It is
// how a call's `shows` and a type's `show` in the schema are both checked.
func CheckSpec(spec *Spec, s *schema.Schema, typeKey string) error {
	if s == nil || s.Empty() {
		return fmt.Errorf("needs a schema, and no type is defined")
	}
	t, ok := s.Type(typeKey)
	if !ok {
		return fmt.Errorf("is for type %s, which the schema does not have; the types are %s",
			typeKey, strings.Join(s.TypeKeys(), ", "))
	}
	for _, key := range spec.Fields {
		if key == schema.LinksKey {
			continue
		}
		if _, ok := t.Field(key); !ok {
			return fmt.Errorf("fields names %s, which is not a field of %s; its fields are %s",
				key, typeKey, strings.Join(append(t.FieldKeys(), schema.LinksKey), ", "))
		}
	}
	relations := typeRelations(s, typeKey)
	for at, table := range spec.Tables {
		where := ""
		if len(spec.Tables) > 1 {
			where = fmt.Sprintf("table %d ", at+1)
		}
		if table.Relation != schema.LinksKey && !contains(relations, table.Relation) {
			names := strings.Join(append(relations, schema.LinksKey), ", ")
			return fmt.Errorf("expand %snames %s, which is not a relation of %s; its relations are %s",
				where, table.Relation, typeKey, names)
		}
		if err := checkExpand(s, table); err != nil {
			return fmt.Errorf("expand %s%w", where, err)
		}
	}
	return nil
}

// CheckSpecJSON is CheckSpec over a spec's JSON, for the schema's own
// `show`, which the import checks before it writes anything.
func CheckSpecJSON(raw json.RawMessage, s *schema.Schema, typeKey string) error {
	spec, err := ParseSpec(raw)
	if err != nil {
		return err
	}
	return CheckSpec(spec, s, typeKey)
}

// typeRelations are the relations an issue of a type has: its own relation
// fields, and the inverse names of the relation fields that may point at it.
func typeRelations(s *schema.Schema, typeKey string) []string {
	var out []string
	if t, ok := s.Type(typeKey); ok {
		for _, key := range t.FieldKeys() {
			if field, _ := t.Field(key); field.Kind.IsRelation() {
				out = append(out, key)
			}
		}
	}
	for _, other := range s.TypeKeys() {
		t, _ := s.Type(other)
		for _, key := range t.FieldKeys() {
			field, _ := t.Field(key)
			if !field.Kind.IsRelation() || field.Inverse == "" || contains(out, field.Inverse) {
				continue
			}
			if len(field.TargetTypes) > 0 && !contains(field.TargetTypes, typeKey) {
				continue
			}
			out = append(out, field.Inverse)
		}
	}
	return out
}

// TypeRelations are the relations an issue of a type has, stored and
// inverse, in the order a spec's check names them: what `e` on a show page
// offers to expand, beside `links` (A7).
func TypeRelations(s *schema.Schema, typeKey string) []string {
	if s == nil {
		return nil
	}
	return typeRelations(s, typeKey)
}

// ExpandNames are the relations a spec's `expand` names, in its order.
func (s *Spec) ExpandNames() []string {
	out := make([]string, 0, len(s.Tables))
	for _, table := range s.Tables {
		out = append(out, table.Relation)
	}
	return out
}

// ToggleExpand is a spec with one relation's table taken out, wherever its
// `expand` names it, or added at the end where it names it nowhere: `e` on
// a show page (A7). Everything else is kept as it was given, a layer's own
// keys included; an `expand` left empty is dropped, since an empty list is
// not one.
func ToggleExpand(spec *Spec, relation string) (json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if !isNull(spec.Raw) {
		if err := json.Unmarshal(spec.Raw, &object); err != nil {
			return nil, err
		}
	}

	var elements []json.RawMessage
	if raw, ok := object["expand"]; ok && !isNull(raw) {
		if err := json.Unmarshal(raw, &elements); err != nil {
			elements = []json.RawMessage{raw}
		}
	}
	kept := make([]json.RawMessage, 0, len(elements)+1)
	for _, element := range elements {
		if elementRelation(element) != relation {
			kept = append(kept, element)
		}
	}
	if len(kept) == len(elements) {
		kept = append(kept, mustString(relation))
	}

	if len(kept) == 0 {
		delete(object, "expand")
	} else {
		list, err := json.Marshal(kept)
		if err != nil {
			return nil, err
		}
		object["expand"] = list
	}
	out, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return compact(out), nil
}

// elementRelation is the relation one element of `expand` names: the
// element itself, or a layer's `relation`.
func elementRelation(element json.RawMessage) string {
	var name string
	if json.Unmarshal(element, &name) == nil {
		return name
	}
	var layer struct {
		Relation string `json:"relation"`
	}
	_ = json.Unmarshal(element, &layer)
	return layer.Relation
}

// WithSpec is a `shows` with one type's spec in place of what it had, every
// other type's as given.
func WithSpec(shows json.RawMessage, typeKey string, spec json.RawMessage) (json.RawMessage, error) {
	entries := map[string]json.RawMessage{}
	if !isNull(shows) {
		if err := json.Unmarshal(shows, &entries); err != nil {
			return nil, err
		}
	}
	entries[typeKey] = spec
	out, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return compact(out), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mustString(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}
