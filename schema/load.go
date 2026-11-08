package schema

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/util/sorted"
)

// Entry is one config entity as the schema layer reads it:
// the excerpt's whole document, with no entity loaded
// (doc/design/schema.md, "Compiled on demand").
type Entry struct {
	Id         entity.Id
	Shape      config.Shape
	Key        string
	Attributes map[string]config.Value
	// Archived marks an archived entity, which the schema holds only where
	// no unarchived one holds its key
	// (doc/design/schema.md, "Archiving refuses only what is new").
	Archived bool
}

// Source hands Load the winners of refs/work-schema.
//
// It is an interface rather than *cache.RepoCache
// because the cache validates issue writes against a schema
// and so imports this package:
// naming the cache here would close the cycle.
// *cache.RepoCacheConfig implements it, so `schema.Load(backend.Schema())`
// is what every caller writes.
type Source interface {
	// SchemaEntries returns one entry per type and field key, duplicate
	// keys already resolved to their winner
	// (doc/design/schema.md, "Duplicate keys"): the unarchived winner, else
	// the archived one, marked.
	SchemaEntries() ([]Entry, error)
}

// Load compiles the schema from a source of config entities.
func Load(src Source) (*Schema, error) {
	entries, err := src.SchemaEntries()
	if err != nil {
		return nil, err
	}
	return Compile(entries)
}

// Compile builds the schema from config entity documents.
//
// It never fails on what was written before
// (doc/design/schema.md, "Structural rules are frozen"):
// an attribute it can not read, a field naming no type,
// a relation targeting a type that is gone,
// are Problems on the result, not errors.
func Compile(entries []Entry) (*Schema, error) {
	s := &Schema{Types: map[string]*Type{}}

	// deterministic: the problems of one store read the same twice
	sorted := append([]Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Shape != sorted[j].Shape {
			return sorted[i].Shape < sorted[j].Shape
		}
		if sorted[i].Key != sorted[j].Key {
			return sorted[i].Key < sorted[j].Key
		}
		return sorted[i].Id < sorted[j].Id
	})

	for _, e := range sorted {
		if e.Shape != config.ShapeType {
			continue
		}
		t, err := compileType(e)
		if err != nil {
			s.Problems = append(s.Problems, fmt.Sprintf("type %s: %v", e.Key, err))
			continue
		}
		t.Archived = e.Archived
		s.Types[t.Key] = t
	}

	// the built-ins exist in code, before any override
	// (doc/design/schema.md, "Built-ins are code"): on every type, but
	// `color` on a type that switches it on
	// (doc/design/schema.md, "Color is a built-in a type switches on")
	for _, t := range s.Types {
		for _, b := range Builtins {
			if b.Switched && !t.Color {
				continue
			}
			t.Fields[b.Key] = newBuiltinField(t.Key, b)
		}
	}

	for _, e := range sorted {
		if e.Shape != config.ShapeField {
			continue
		}
		typeKey, fieldKey, ok := config.SplitFieldKey(e.Key)
		if !ok {
			s.Problems = append(s.Problems, fmt.Sprintf("field %s: key is not <type>/<field>", e.Key))
			continue
		}
		t, known := s.Types[typeKey]
		if !known {
			s.Problems = append(s.Problems, fmt.Sprintf("field %s: no type %s", e.Key, typeKey))
			continue
		}
		if fieldKey == ColorKey && !t.Color {
			// an override of a color the type has switched off is kept in
			// the store and means nothing until the type switches it on again
			continue
		}
		if e.Archived && IsBuiltin(fieldKey) {
			// a built-in is code: an archived override leaves it as built
			continue
		}
		field, err := compileField(typeKey, fieldKey, e)
		if err != nil {
			s.Problems = append(s.Problems, fmt.Sprintf("field %s: %v", e.Key, err))
			continue
		}
		field.Archived = e.Archived
		t.Fields[fieldKey] = field
	}

	// the type field's values are the types themselves,
	// which is the one field the engine resolves against the type entities
	// (doc/design/schema.md, "type is checked against the types")
	// an archived type is an archived value of it, so a create or a retype
	// into it is refused and a set that keeps it is not
	// (doc/design/schema.md, "Archiving refuses only what is new")
	typeValues := make([]Value, 0, len(s.Types))
	for ordinal, key := range s.AllTypeKeys() {
		t := s.Types[key]
		typeValues = append(typeValues, Value{
			Id:       t.Key,
			Name:     t.Name,
			Ordinal:  ordinal * 10,
			Archived: t.Archived,
		})
	}
	for _, t := range s.Types {
		if field, ok := t.Fields[TypeKey]; ok {
			field.Values = typeValues
		}
	}

	// dangling relation and retype targets are reported, never an error
	// (doc/design/schema.md, "Structural rules are frozen")
	for _, key := range s.AllTypeKeys() {
		t := s.Types[key]
		if t.RetypeTargets != nil {
			for _, target := range *t.RetypeTargets {
				if _, ok := s.Types[target]; !ok {
					s.Problems = append(s.Problems,
						fmt.Sprintf("type %s: retype target %s does not exist", t.Key, target))
				}
			}
		}
		for _, fieldKey := range t.AllFieldKeys() {
			field := t.Fields[fieldKey]
			for _, target := range field.TargetTypes {
				if _, ok := s.Types[target]; !ok {
					s.Problems = append(s.Problems,
						fmt.Sprintf("field %s/%s: target type %s does not exist", t.Key, field.Key, target))
				}
			}
		}
	}

	return s, nil
}

func compileType(e Entry) (*Type, error) {
	name, err := attrString(e.Attributes, AttrName)
	if err != nil {
		return nil, err
	}
	description, err := attrString(e.Attributes, AttrDescription)
	if err != nil {
		return nil, err
	}
	ordinal, err := attrInt(e.Attributes, AttrOrdinal)
	if err != nil {
		return nil, err
	}
	aliases, err := compileAliases(e.Attributes)
	if err != nil {
		return nil, err
	}

	var show json.RawMessage
	if raw, ok := e.Attributes[AttrShow]; ok {
		show = append(json.RawMessage(nil), raw...)
	}

	var retype *[]string
	if raw, ok := e.Attributes[AttrRetypeTargets]; ok {
		var targets []string
		if err := unmarshalAttr(raw, &targets); err != nil {
			return nil, fmt.Errorf("attribute %s is not a list of type keys", AttrRetypeTargets)
		}
		if targets == nil {
			targets = []string{}
		}
		retype = &targets
	}

	color, err := attrBool(e.Attributes, AttrColor)
	if err != nil {
		return nil, err
	}

	return &Type{
		Key:           e.Key,
		Name:          name,
		Description:   description,
		Ordinal:       ordinal,
		Aliases:       aliases,
		Show:          show,
		RetypeTargets: retype,
		Color:         color,
		Fields:        map[string]*Field{},
		EntityId:      e.Id,
	}, nil
}

func compileField(typeKey, fieldKey string, e Entry) (*Field, error) {
	rawKind, err := attrString(e.Attributes, AttrKind)
	if err != nil {
		return nil, err
	}

	// A built-in's kind is code, and an entity can not change it
	// (doc/design/schema.md, "Built-ins are code"):
	// what an entity overrides is the name, the description and the order.
	var kind Kind
	if builtinKind, isBuiltin := BuiltinKind(fieldKey); isBuiltin {
		kind = builtinKind
	} else {
		kind, err = ParseKind(rawKind)
		if err != nil {
			return nil, err
		}
	}

	name, err := attrString(e.Attributes, AttrName)
	if err != nil {
		return nil, err
	}
	description, err := attrString(e.Attributes, AttrDescription)
	if err != nil {
		return nil, err
	}
	freeform, err := attrBool(e.Attributes, AttrFreeform)
	if err != nil {
		return nil, err
	}
	inverse, err := attrString(e.Attributes, AttrInverse)
	if err != nil {
		return nil, err
	}
	link, err := attrBool(e.Attributes, AttrLink)
	if err != nil {
		return nil, err
	}

	field := &Field{
		Type:        typeKey,
		Key:         fieldKey,
		Kind:        kind,
		Name:        name,
		Description: description,
		Freeform:    freeform,
		Inverse:     inverse,
		Link:        link,
		Builtin:     IsBuiltin(fieldKey),
		Configured:  true,
		EntityId:    e.Id,
	}

	if ordinal, ok := e.Attributes[AttrOrdinal]; ok {
		n, ok := config.Number(ordinal)
		if !ok {
			return nil, fmt.Errorf("attribute %s is not a number", AttrOrdinal)
		}
		field.Ordinal = int(n)
	} else if b := builtin(fieldKey); b != nil {
		field.Ordinal = b.Ordinal
	}

	if field.Builtin {
		// values, targets and aliases are meaningless on a built-in,
		// and the type field's values are the types (see Compile)
		return field, nil
	}

	field.Aliases, err = compileAliases(e.Attributes)
	if err != nil {
		return nil, err
	}
	values, err := compileValues(e.Attributes)
	if err != nil {
		return nil, err
	}
	field.Values = values
	field.TargetTypes = compileTargetTypes(e.Attributes)

	return field, nil
}

func compileValues(attrs map[string]config.Value) ([]Value, error) {
	var values []Value
	for _, name := range sorted.Keys(attrs) {
		prefix, id, folded := config.SplitName(name)
		if !folded || prefix != ValuesPrefix {
			continue
		}
		var attr ValueAttr
		if err := unmarshalAttr(attrs[name], &attr); err != nil {
			return nil, fmt.Errorf("value %s: %w", id, err)
		}
		category, err := ParseCategory(string(attr.Category))
		if err != nil {
			return nil, fmt.Errorf("value %s: %w", id, err)
		}
		values = append(values, Value{
			Id:          id,
			Name:        attr.Name,
			Ordinal:     attr.Ordinal,
			Category:    category,
			Description: attr.Description,
			Color:       attr.Color,
			Archived:    attr.Archived,
		})
	}

	// a value's aliases sit beside it; one naming no value is kept in the
	// store, since nothing removes an alias, and means nothing here
	at := make(map[string]int, len(values))
	for i, value := range values {
		at[value.Id] = i
	}
	for _, name := range sorted.Keys(attrs) {
		system, id, ok := valueAliasSystem(name)
		if !ok {
			continue
		}
		i, known := at[id]
		if !known {
			continue
		}
		alias, err := attrString(attrs, name)
		if err != nil {
			return nil, fmt.Errorf("value %s: %w", id, err)
		}
		if values[i].Aliases == nil {
			values[i].Aliases = map[string]string{}
		}
		values[i].Aliases[system] = alias
	}

	sort.Slice(values, func(i, j int) bool {
		if values[i].Ordinal != values[j].Ordinal {
			return values[i].Ordinal < values[j].Ordinal
		}
		return values[i].Id < values[j].Id
	})

	return values, nil
}

func compileTargetTypes(attrs map[string]config.Value) []string {
	var targets []string
	for _, name := range sorted.Keys(attrs) {
		prefix, typeKey, folded := config.SplitName(name)
		if !folded || prefix != TargetTypesPrefix {
			continue
		}
		targets = append(targets, typeKey)
	}
	sort.Strings(targets)
	return targets
}
