package view

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/schema"
)

// Child is one entry of show's `children`: which issues point at the shown
// one, named by the relation that holds its id
// (doc/design/terminal-renderer.md, Show, Children; Luis, 2026-09-29).
//
// The relation is stored on the child, never on the shown issue — `parent`
// on a task names its story, and the story stores nothing (schema.yaml, D4)
// — so the entry names the child's side: its type and its relation field.
// Relation may instead be the name the schema's `inverse` gives the other
// side, `children` for `parent`, the way `expand` reads a derived side.
type Child struct {
	// Type is the child's type; empty, every type whose relation matches.
	Type string `json:"type,omitempty"`
	// Relation is the relation field on the child that holds the shown
	// issue's id, or that field's inverse name.
	Relation string `json:"relation"`
	// Fields are drawn after each child's title, in order.
	Fields []string `json:"fields,omitempty"`
}

// Children is one resolved entry: the heading its section is drawn under,
// and, per child type, the stored relation fields that name the shown issue.
type Children struct {
	Child
	Heading string
	// Sources maps a type key to its relation fields that point at the
	// shown issue.
	Sources map[string][]string
}

// parseChildren checks the shape of `children`, which needs no schema: a
// list of objects, each with a relation, and nothing the entry does not have.
func parseChildren(raw json.RawMessage) ([]Child, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf(`is a list of {"type","relation","fields"} objects, not %s`, jsonKind(raw))
	}

	out := make([]Child, 0, len(entries))
	for at, entry := range entries {
		for key := range entry {
			if key != "type" && key != "relation" && key != "fields" {
				return nil, fmt.Errorf("entry %d has no key %s, it takes type, relation and fields", at, key)
			}
		}
		var child Child
		if raw, ok := entry["type"]; ok && !isNull(raw) {
			s, err := asString(raw)
			if err != nil {
				return nil, fmt.Errorf("entry %d: type %w", at, err)
			}
			child.Type = s
		}
		raw, ok := entry["relation"]
		if !ok || isNull(raw) {
			return nil, fmt.Errorf("entry %d needs relation: the relation field on the child that holds the shown issue's id", at)
		}
		s, err := asString(raw)
		if err != nil {
			return nil, fmt.Errorf("entry %d: relation %w", at, err)
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("entry %d: relation is empty", at)
		}
		child.Relation = s
		if raw, ok := entry["fields"]; ok && !isNull(raw) {
			if err := json.Unmarshal(raw, &child.Fields); err != nil {
				return nil, fmt.Errorf("entry %d: fields is a list of strings, not %s", at, jsonKind(raw))
			}
		}
		out = append(out, child)
	}
	return out, nil
}

// ChildList returns `children` as parsed, nil when it is absent.
func (c *Call) ChildList() []Child {
	raw, ok := c.Args["children"]
	if !ok {
		return nil
	}
	children, err := parseChildren(raw)
	if err != nil {
		return nil
	}
	return children
}

// CheckSchema checks what a call names against the live schema, where the
// table alone cannot: today `children` on show, whose types and relations
// have to exist. It is called once, in host.View, so that every surface
// refuses the same call with the same words before anything is drawn.
func CheckSchema(call *Call, s *schema.Schema) error {
	if !call.Has("children") {
		return nil
	}
	_, err := ResolveChildren(s, call.ChildList())
	if err != nil {
		return fmt.Errorf("view %s: children %w", call.Kind, err)
	}
	return nil
}

// ResolveChildren reads each entry against the schema: the child types it
// means, the stored relation fields on each that hold the shown issue's id,
// and the heading its section is drawn under.
//
// A relation is found first as a field of the type, which then has to be a
// relation, and else as the inverse of the type's relation fields, which is
// what makes `{"relation":"children"}` alone mean every issue whose `parent`
// names this one. Every failure names what exists, because that is what the
// caller needs next.
func ResolveChildren(s *schema.Schema, children []Child) ([]Children, error) {
	if s == nil || s.Empty() {
		return nil, fmt.Errorf("needs a schema, and no type is defined")
	}

	out := make([]Children, 0, len(children))
	for _, child := range children {
		types := s.TypeKeys()
		if child.Type != "" {
			if _, ok := s.Type(child.Type); !ok {
				return nil, fmt.Errorf("names type %s, which does not exist; the types are %s",
					child.Type, strings.Join(s.TypeKeys(), ", "))
			}
			types = []string{child.Type}
		}

		sources := map[string][]string{}
		byInverse := false
		var notRelation []string
		for _, typeKey := range types {
			t, _ := s.Type(typeKey)
			if field, ok := t.Field(child.Relation); ok {
				if !field.Kind.IsRelation() {
					notRelation = append(notRelation, fmt.Sprintf("%s/%s is %s", typeKey, field.Key, field.Kind))
					continue
				}
				sources[typeKey] = append(sources[typeKey], field.Key)
				continue
			}
			for _, key := range t.FieldKeys() {
				field, _ := t.Field(key)
				if field.Kind.IsRelation() && field.Inverse == child.Relation {
					sources[typeKey] = append(sources[typeKey], key)
					byInverse = true
				}
			}
		}

		if len(sources) == 0 {
			if len(notRelation) > 0 && child.Type != "" {
				return nil, fmt.Errorf("names %s, not a relation; the relations are %s",
					notRelation[0], relationNames(s, types))
			}
			where := "no type has"
			if child.Type != "" {
				where = "type " + child.Type + " has no"
			}
			return nil, fmt.Errorf("%s relation %s, nor one whose inverse is %s; the relations are %s",
				where, child.Relation, child.Relation, relationNames(s, types))
		}

		for _, key := range child.Fields {
			found := false
			for typeKey := range sources {
				if _, ok := s.Field(typeKey, key); ok {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("names field %s for %s, which is not a field of %s",
					key, child.Relation, strings.Join(sortedKeys(sources), " or "))
			}
		}

		out = append(out, Children{Child: child, Heading: heading(s, child, sources, byInverse), Sources: sources})
	}
	return out, nil
}

// heading is what a section is called: the relation from the shown issue's
// side, which is the inverse's name when the schema gives one — a story's
// tasks are its `children` — and the stored key behind an arrow when it
// does not; a named type follows it, so two sections of one relation and two
// types read apart.
func heading(s *schema.Schema, child Child, sources map[string][]string, byInverse bool) string {
	name := child.Relation
	if !byInverse {
		name = "← " + child.Relation
		for _, typeKey := range sortedKeys(sources) {
			if field, ok := s.Field(typeKey, child.Relation); ok && field.Inverse != "" {
				name = field.Inverse
				break
			}
		}
	}
	if child.Type != "" {
		name += " · " + child.Type
	}
	return name
}

// relationNames is every relation field of the types, as type/key with its
// inverse, for an error.
func relationNames(s *schema.Schema, types []string) string {
	var names []string
	for _, typeKey := range types {
		t, _ := s.Type(typeKey)
		for _, key := range t.FieldKeys() {
			field, _ := t.Field(key)
			if !field.Kind.IsRelation() {
				continue
			}
			name := typeKey + "/" + key
			if field.Inverse != "" {
				name += " (inverse " + field.Inverse + ")"
			}
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
