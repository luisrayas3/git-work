package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/util/sorted"
)

// The document a human edits is a view over the entities
// (doc/design/schema.md, "The document is a view over the entities").
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
//	    aliases: {jira: "10000"}  # what it is in another system
//	    retype_targets: []        # the types it may become; absent is any
//	    color: true               # its issues carry the built-in color
//	    fields:
//	      status: *status
//	      parent: {kind: relation, inverse: children, target_types: [initiative]}
//
// List and mapping position *is* the order:
// `ordinal` is never written by hand and never exported,
// so `export | import` emits zero operations, which is the round-trip test.
// `shared` is the human's anchors and nothing else reads it;
// the store holds one status entity per type
// (doc/design/schema.md, "A field belongs to one type").
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
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
	// Aliases are what the entity is in other systems, by system name,
	// here and on a field and a value alike
	// (doc/design/jira.md, "The mapping is aliases on the schema"). A stated alias is
	// written and an unstated one left alone, so a file without them unmaps
	// nothing; the empty string is stated, and means "never sync this".
	Aliases map[string]string `yaml:"aliases,omitempty"`
	// Show is how show draws an issue of the type where a view's `shows`
	// does not say (doc/design/views.md, "A spec is whole, never merged"):
	// what show takes less the id.
	Show *ShowDoc `yaml:"show,omitempty"`
	// RetypeTargets are the types an issue of this type may become by a
	// `set` of its type (doc/design/schema.md, "A type says what it may
	// become"): nil is any type, which is
	// what a schema written before it means; empty is none, the type is
	// fixed once the issue is created.
	RetypeTargets *[]string `yaml:"retype_targets,omitempty"`
	// Color switches the built-in `color` on for the type's issues
	// (doc/design/schema.md, "Color is a built-in a type switches on").
	Color bool `yaml:"color,omitempty"`
	// Archived is stated as a value's is
	// (doc/design/schema.md, "An archive is stated, never implied").
	Archived *bool     `yaml:"archived,omitempty"`
	Fields   *fieldMap `yaml:"fields,omitempty"`
}

// ShowDoc is a type's show spec in the document: `fields`, the rows of
// show's fields table, and `expand`, its side tables, a relation name, a
// layer, or a list of them. Its meaning is the views' to check, which the
// import does against the schema it would write (host.SchemaImport).
type ShowDoc struct {
	Fields []string `yaml:"fields,omitempty" json:"fields,omitempty"`
	Expand any      `yaml:"expand,omitempty" json:"expand,omitempty"`
}

// FieldDoc is one field in the document.
//
// The type is the mapping it sits under, never a member here:
// a field belongs to exactly one type, and its key is `<type>/<field>`
// (doc/design/schema.md, "A field belongs to one type").
type FieldDoc struct {
	Kind        string            `yaml:"kind,omitempty"`
	Name        string            `yaml:"name,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Aliases     map[string]string `yaml:"aliases,omitempty"`
	Freeform    bool              `yaml:"freeform,omitempty"`
	Inverse     string            `yaml:"inverse,omitempty"`
	TargetTypes []string          `yaml:"target_types,omitempty"`
	// Link marks a relation the views draw as a link
	// (doc/design/schema.md, "A link is a marked relation").
	Link bool `yaml:"link,omitempty"`
	// Archived is stated as a value's is
	// (doc/design/schema.md, "An archive is stated, never implied").
	Archived *bool      `yaml:"archived,omitempty"`
	Values   []ValueDoc `yaml:"values,omitempty"`
}

// ValueDoc is one enum value in the document.
type ValueDoc struct {
	Id          string `yaml:"id"`
	Name        string `yaml:"name,omitempty"`
	Category    string `yaml:"category,omitempty"`
	Description string `yaml:"description,omitempty"`
	Color       string `yaml:"color,omitempty"`
	// Archived is stated, as an alias is: true archives the value, false
	// unarchives it, and absent leaves it as it stands, so a file written
	// before the archive revives nothing
	// (doc/design/schema.md, "An archive is stated, never implied"). Export
	// writes true or nothing.
	Archived *bool `yaml:"archived,omitempty"`
	// Aliases are stored on the field entity, `alias_<system>/<id>`.
	Aliases map[string]string `yaml:"aliases,omitempty"`
}

type typeMap = orderedMap[TypeDoc]
type fieldMap = orderedMap[FieldDoc]

// orderedMap is a YAML mapping that remembers the order it was written in,
// because that order is the schema's order
// (doc/design/schema.md, "Position is the order").
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

// NewDocument returns an empty document, for building one in code:
// the Jira sync derives the schema it wants and imports it
// (doc/design/jira.md, "Derive, then the schema import").
func NewDocument() *Document {
	return &Document{Types: newOrderedMap[TypeDoc]()}
}

// SetType appends or replaces one type, keeping the document's order.
func (d *Document) SetType(key string, t TypeDoc) {
	if d.Types == nil {
		d.Types = newOrderedMap[TypeDoc]()
	}
	d.Types.Set(key, t)
}

// SetField appends or replaces one field of the type, keeping its order.
func (t *TypeDoc) SetField(key string, f FieldDoc) {
	if t.Fields == nil {
		t.Fields = newOrderedMap[FieldDoc]()
	}
	t.Fields.Set(key, f)
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
// which is the rule every document argument follows
// (doc/design/cli.md, "A document is read strictly").
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
// would make the next import create an entity that changes nothing
// (doc/design/schema.md, "Built-ins are code").
func Export(s *Schema) *Document {
	doc := &Document{Types: newOrderedMap[TypeDoc]()}

	for _, typeKey := range s.AllTypeKeys() {
		t := s.Types[typeKey]
		entry := TypeDoc{
			Archived:      statedArchive(t.Archived),
			Name:          t.Name,
			Description:   t.Description,
			Aliases:       copyAliases(t.Aliases),
			Show:          exportShow(t.Show),
			RetypeTargets: copyTargets(t.RetypeTargets),
			Color:         t.Color,
			Fields:        newOrderedMap[FieldDoc](),
		}

		for _, fieldKey := range t.AllFieldKeys() {
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

// Unarchived is the document without its archived types, fields and values:
// what `git work schema` prints unless asked for the archived too
// (doc/design/schema.md, "Export hides what is archived").
// A type or a field left out takes what it holds with it.
// The document is a copy; the receiver is not changed.
func (d *Document) Unarchived() *Document {
	out := &Document{Preset: d.Preset, Shared: d.Shared, Types: newOrderedMap[TypeDoc]()}
	for _, typeKey := range d.Types.Keys() {
		t, _ := d.Types.Get(typeKey)
		if isArchived(t.Archived) {
			continue
		}
		fields := newOrderedMap[FieldDoc]()
		for _, fieldKey := range t.Fields.Keys() {
			field, _ := t.Fields.Get(fieldKey)
			if isArchived(field.Archived) {
				continue
			}
			var values []ValueDoc
			for _, value := range field.Values {
				if !isArchived(value.Archived) {
					values = append(values, value)
				}
			}
			field.Values = values
			fields.Set(fieldKey, field)
		}
		t.Fields = nil
		if fields.Len() > 0 {
			t.Fields = fields
		}
		out.Types.Set(typeKey, t)
	}
	return out
}

func isArchived(archived *bool) bool {
	return archived != nil && *archived
}

func exportField(field *Field) FieldDoc {
	out := FieldDoc{
		Kind:        string(field.Kind),
		Name:        field.Name,
		Description: field.Description,
		Archived:    statedArchive(field.Archived),
	}
	if field.Builtin {
		// a built-in's values and targets are code, not config
		// (doc/design/schema.md, "Built-ins are code")
		return out
	}

	out.Aliases = copyAliases(field.Aliases)
	out.Freeform = field.Freeform
	out.Inverse = field.Inverse
	out.Link = field.Link
	out.TargetTypes = append([]string(nil), field.TargetTypes...)

	for _, value := range field.Values {
		doc := ValueDoc{
			Id:          value.Id,
			Name:        value.Name,
			Category:    string(value.Category),
			Description: value.Description,
			Color:       value.Color,
			Aliases:     copyAliases(value.Aliases),
		}
		doc.Archived = statedArchive(value.Archived)
		out.Values = append(out.Values, doc)
	}

	return out
}

// statedArchive is what export states of an archive: true, or nothing,
// since nothing stated leaves an entity or a value as it stands
// (doc/design/schema.md, "An archive is stated, never implied").
func statedArchive(archived bool) *bool {
	if !archived {
		return nil
	}
	return &archived
}

// exportShow reads a type's stored spec back as the document's, nil where
// there is none or it no longer reads: a read never fails on what was
// written before (doc/design/schema.md, "Validate on write, lenient on read").
func exportShow(raw json.RawMessage) *ShowDoc {
	if len(raw) == 0 {
		return nil
	}
	var show ShowDoc
	if err := json.Unmarshal(raw, &show); err != nil {
		return nil
	}
	return &show
}

// copyTargets keeps a document from sharing a type's retype targets,
// nil staying nil and empty staying empty: the two mean any and none.
func copyTargets(targets *[]string) *[]string {
	if targets == nil {
		return nil
	}
	out := append([]string{}, (*targets)...)
	return &out
}

// copyAliases keeps a document from sharing a map with the schema it came from.
func copyAliases(aliases map[string]string) map[string]string {
	if len(aliases) == 0 {
		return nil
	}
	out := make(map[string]string, len(aliases))
	for system, value := range aliases {
		out[system] = value
	}
	return out
}

// Validate checks the whole document before anything is written
// (doc/design/schema.md, "Import is an upsert").
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

	typeAliases := aliasOwners{}
	for _, typeKey := range d.Types.Keys() {
		if err := config.ValidateKey(config.ShapeType, typeKey); err != nil {
			problems.add("type %s: %v", typeKey, err)
			continue
		}
		t, _ := d.Types.Get(typeKey)
		where := "type " + typeKey
		validateAliases(problems, where, t.Aliases, AliasName)
		typeAliases.claim(problems, where, t.Aliases)
		if t.Show != nil {
			for _, key := range t.Show.Fields {
				if strings.TrimSpace(key) == "" {
					problems.add("%s: show: fields has an empty field key", where)
				}
			}
			if _, err := json.Marshal(t.Show); err != nil {
				problems.add("%s: show: %v", where, err)
			}
		}
		if t.RetypeTargets != nil {
			seen := map[string]bool{}
			for _, target := range *t.RetypeTargets {
				switch _, ok := known[target]; {
				case seen[target]:
					problems.add("%s: retype target %s appears twice", where, target)
				case !ok:
					problems.add("%s: retype target %s is neither in this document nor in the store", where, target)
				}
				seen[target] = true
			}
		}
		d.validateType(problems, typeKey, t, known)
	}

	return problems.err()
}

func (d *Document) validateType(problems *Problems, typeKey string, t TypeDoc, known map[string]struct{}) {
	inverses := map[string]string{}
	fieldAliases := aliasOwners{}

	for _, fieldKey := range t.Fields.Keys() {
		field, _ := t.Fields.Get(fieldKey)
		where := fmt.Sprintf("field %s/%s", typeKey, fieldKey)

		if err := config.ValidateKey(config.ShapeField, typeKey+"/"+fieldKey); err != nil {
			problems.add("%s: %v", where, err)
			continue
		}

		if fieldKey == LinksKey {
			// `links` is a word of the views, the row show folds the links
			// into, so that a view never has to choose between the two
			// (doc/design/views.md, "The links row")
			problems.add("%s: %s is reserved: it is the row show draws the links in", where, LinksKey)
			continue
		}

		kind, err := parseFieldKind(fieldKey, field.Kind)
		if err != nil {
			problems.add("%s: %v", where, err)
			continue
		}

		if fieldKey == ColorKey && !t.Color {
			// the built-in is the type's only where the type switches it on
			// (doc/design/schema.md, "Color is a built-in a type switches on")
			problems.add("%s: %s is built in on the types that switch it on; add color: true to type %s",
				where, fieldKey, typeKey)
			continue
		}

		if builtinKind, isBuiltin := BuiltinKind(fieldKey); isBuiltin {
			// a built-in's kind is code; an entity overrides
			// only its name and description (doc/design/schema.md, "Built-ins are code")
			if kind != builtinKind {
				problems.add("%s: %s is built in with kind %s and can not be changed to %s",
					where, fieldKey, builtinKind, kind)
			}
			if len(field.Values) > 0 || len(field.TargetTypes) > 0 || field.Inverse != "" || field.Freeform ||
				field.Link || len(field.Aliases) > 0 {
				problems.add("%s: %s is built in; only its name and description are configurable",
					where, fieldKey)
			}
			continue
		}

		validateAliases(problems, where, field.Aliases, AliasName)
		fieldAliases.claim(problems, where, field.Aliases)

		if kind.IsEnum() {
			d.validateValues(problems, where, field)
		} else if len(field.Values) > 0 {
			problems.add("%s: kind %s has no values", where, kind)
		}

		if kind.IsRelation() {
			if field.Inverse == LinksKey {
				problems.add("%s: inverse %s is reserved: it is the row show draws the links in", where, LinksKey)
			}
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
			if field.Link {
				problems.add("%s: link is for relation kinds, not %s", where, kind)
			}
		}

		if field.Freeform && kind != KindMultiEnum && kind != KindEnum {
			problems.add("%s: freeform is for enum kinds, not %s", where, kind)
		}
	}
}

func (d *Document) validateValues(problems *Problems, where string, field FieldDoc) {
	seen := map[string]struct{}{}
	valueAliases := aliasOwners{}
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
		at := fmt.Sprintf("%s: value %s", where, value.Id)
		validateAliases(problems, at, value.Aliases, func(system string) string {
			return ValueAliasName(system, value.Id)
		})
		valueAliases.claim(problems, at, value.Aliases)
	}
}

// aliasSystemPattern is what a system name looks like, `jira` or `linear`:
// one segment of an attribute name, after `alias_`.
var aliasSystemPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validateAliases checks that every alias can be stored under its name.
func validateAliases(problems *Problems, where string, aliases map[string]string, name func(system string) string) {
	for _, system := range sorted.Keys(aliases) {
		if !aliasSystemPattern.MatchString(system) {
			problems.add("%s: alias system %q must match %s", where, system, aliasSystemPattern.String())
			continue
		}
		if err := config.ValidateName(name(system)); err != nil {
			problems.add("%s: alias %s: %v", where, system, err)
		}
	}
}

// aliasOwners finds two entities of one document naming one thing of
// another system: two types, two fields of one type, or two values of one
// field. Fields of different types share aliases as they share keys
// (doc/design/schema.md, "A field belongs to one type"),
// and the empty string excludes rather than names, so it is never a duplicate.
type aliasOwners map[string]map[string]string

func (o aliasOwners) claim(problems *Problems, where string, aliases map[string]string) {
	for _, system := range sorted.Keys(aliases) {
		value := aliases[system]
		if value == "" {
			continue
		}
		if o[system] == nil {
			o[system] = map[string]string{}
		}
		if other, twice := o[system][value]; twice {
			problems.add("%s: alias %s %q is already %s's", where, system, value, other)
			continue
		}
		o[system][value] = where
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
