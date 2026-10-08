package view

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/schema"
)

// Show's `expand`: the list's spec, or a list of it, one flat table per
// element drawn beside the fields (doc/design/show-side-table.md, approved
// 2026-10-08, Luis).
//
// It is the list's argument because it is the list's object: the issues one
// relation reaches from one row, narrowed and drawn as a table. A nested list
// draws that table under a parent row; show draws it beside the one issue it
// is about. So it is read by the same parser and checked by the same check,
// and only what a flat table cannot draw is refused on top.

// parseSideTables reads show's `expand`: a relation name, a layer, or a list
// of either, each a table. A layer here needs its relation, since there is no
// row above to list children, and takes no `details`, `group_by` or `expand`,
// because a side column is too narrow for a second line, a section or a tree.
func parseSideTables(raw json.RawMessage) ([]*Layer, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		elements = []json.RawMessage{raw}
	} else if len(elements) == 0 {
		return nil, fmt.Errorf("is an empty list, it is a relation, a layer, or a list of them")
	}

	out := make([]*Layer, 0, len(elements))
	for at, element := range elements {
		where := ""
		if len(elements) > 1 {
			where = fmt.Sprintf("table %d ", at+1)
		}
		layer, err := parseLayer(element, 1)
		if err != nil {
			return nil, fmt.Errorf("%s%w", where, err)
		}
		var refused []string
		if layer.Details != nil {
			refused = append(refused, "details")
		}
		if layer.GroupBy != "" {
			refused = append(refused, "group_by")
		}
		if layer.Expand != nil || layer.Repeat != nil {
			refused = append(refused, "expand")
		}
		if len(refused) > 0 {
			return nil, fmt.Errorf("%stakes no %s: a side table is flat, it takes relation, query, include_archive, fields and rank",
				where, strings.Join(refused, " or "))
		}
		if layer.Relation == "" {
			return nil, fmt.Errorf("%sneeds relation: the relation, stored or inverse, whose issues the table lists", where)
		}
		out = append(out, layer)
	}
	return out, nil
}

// SideTables returns show's `expand` as parsed, nil when it is absent.
func (c *Call) SideTables() []*Layer {
	raw, ok := c.Args["expand"]
	if !ok || c.Kind != KindShow {
		return nil
	}
	tables, err := parseSideTables(raw)
	if err != nil {
		return nil
	}
	return tables
}

// CheckSchema checks what a call names against the live schema, where the
// table alone cannot: `expand`'s layers, whose relations and field keys have
// to exist, on show a table each. It is called once, in host.View, so that
// every surface refuses the same call with the same words before anything
// is drawn.
func CheckSchema(call *Call, s *schema.Schema) error {
	if call.Kind == KindShow {
		tables := call.SideTables()
		for at, table := range tables {
			if err := checkExpand(s, table); err != nil {
				where := ""
				if len(tables) > 1 {
					where = fmt.Sprintf("table %d ", at+1)
				}
				return fmt.Errorf("view %s: expand %s%w", call.Kind, where, err)
			}
		}
		return nil
	}
	if layer := call.Expand(); layer != nil {
		if err := checkExpand(s, layer); err != nil {
			return fmt.Errorf("view %s: expand %w", call.Kind, err)
		}
	}
	return nil
}

// InverseSources is what a relation name reads on the child's side, for a
// shown issue of a type: per child type, its stored relation fields whose
// inverse is the name and which may point at the shown type. It is empty
// where the name is a stored field of the shown type, which is read on the
// shown issue's own side instead — the side a ghost cannot write in one
// commit (doc/design/show-side-table.md, S6).
func InverseSources(s *schema.Schema, shownType, name string) map[string][]string {
	if s == nil {
		return nil
	}
	if t, ok := s.Type(shownType); ok {
		if field, ok := t.Field(name); ok && field.Kind.IsRelation() {
			return nil
		}
	}
	out := map[string][]string{}
	for _, typeKey := range s.TypeKeys() {
		t, _ := s.Type(typeKey)
		for _, key := range t.FieldKeys() {
			field, _ := t.Field(key)
			if !field.Kind.IsRelation() || field.Inverse != name {
				continue
			}
			if len(field.TargetTypes) > 0 && !contains(field.TargetTypes, shownType) {
				continue
			}
			out[typeKey] = append(out[typeKey], key)
		}
	}
	return out
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
