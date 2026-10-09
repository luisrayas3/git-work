package tui

import (
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// `empty_groups` on the board adds the swimlanes no card falls in, so that a
// lane is somewhere to put work and not only a heading over work that exists
// (doc/design/empty-groups.md). Which values, by the field's kind (E2):
//
//   - an enum: every value the schema lists, over the types on the board as
//     the columns read them;
//   - a bool: false and true;
//   - a relation: the siblings of the lanes the cards are in (E3);
//   - anything else, and `type`: none, there being no set of values to draw
//     or, for the type, no value a drop writes.
//
// (none) is not one of them: it stays a lane only while a card has no value.
// Each lane carries its stored value, so a drop into it and its ghosts write
// that (E5); where it goes among the others is the order every group takes
// (group.go, E4).

// emptyLanes is every lane the field could hold here, as values; arrange
// draws the ones no card is in. Nothing when empty_groups is off.
func (p *boardPage) emptyLanes(known *kinds) []lane {
	if !p.emptyGroups || p.groupBy == "" || p.groupBy == schema.TypeKey {
		return nil
	}

	onBoard := map[string]bool{}
	for _, c := range p.cards[:p.real] {
		onBoard[c.typeKey] = true
	}
	kind, order, _ := fieldValues(p.repo, p.groupBy, onBoard)
	typeKey, targets := p.laneField(onBoard)
	if typeKey == "" {
		return nil
	}

	var raws []any
	switch {
	case isEnum(kind):
		for _, value := range order {
			if kind == schema.KindMultiEnum {
				raws = append(raws, []any{value})
			} else {
				raws = append(raws, value)
			}
		}
	case kind == schema.KindBool:
		raws = []any{false, true}
	case isRelation(kind):
		for _, id := range p.siblings(targets) {
			if kind == schema.KindMultiRelation {
				raws = append(raws, []any{id})
			} else {
				raws = append(raws, id)
			}
		}
	}

	lanes := make([]lane, 0, len(raws))
	for _, raw := range raws {
		label := known.cellText(typeKey, p.groupBy, raw)
		if label == "" {
			continue
		}
		lanes = append(lanes, lane{group: label, raw: raw, typeKey: typeKey})
	}
	return lanes
}

// laneField is a type on the board that holds the `group_by` field, which a
// lane with no card reads its kind and its prefill through, and the types
// the field may point at for the types on the board: nil when any of them
// takes any type at all.
func (p *boardPage) laneField(onBoard map[string]bool) (typeKey string, targets map[string]bool) {
	s, err := p.repo.LoadSchema()
	if err != nil {
		return "", nil
	}
	targets = map[string]bool{}
	for _, key := range s.TypeKeys() {
		if len(onBoard) > 0 && !onBoard[key] {
			continue
		}
		field, ok := s.Field(key, p.groupBy)
		if !ok {
			continue
		}
		if typeKey == "" {
			typeKey = key
		}
		if targets == nil {
			continue
		}
		if len(field.TargetTypes) == 0 {
			targets = nil
			continue
		}
		for _, target := range field.TargetTypes {
			targets[target] = true
		}
	}
	return typeKey, targets
}

// siblings is a relation's empty lanes (E3): the query cannot be read for its
// scope, so the scope is read from the lanes it produced. The lanes the cards
// are in are issues; their own `group_by` values, read from the store, are
// the anchors; and the empty lanes are the other issues whose `group_by`
// value is an anchor, of a type the field may point at, unarchived unless
// the board's include_archive says otherwise. A lane issue with no value
// anchors on "no value", whose siblings are the target issues with none
// either. It reaches one level and no further, and names no field but the
// one the board was given.
func (p *boardPage) siblings(targets map[string]bool) []string {
	values, err := host.IssueList(p.repo, "", true)
	if err != nil {
		return nil
	}
	items := host.IssueRows(values)
	fieldsOf := make(map[string]map[string]any, len(items))
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		fieldsOf[host.StringOr(item["id"], "")] = fields
	}

	present := map[string]bool{}
	for _, c := range p.cards[:p.real] {
		for _, id := range linkIds(c.fields[p.groupBy]) {
			present[id] = true
		}
	}
	anchors := map[string]bool{}
	noValue := false
	for id := range present {
		fields, pulled := fieldsOf[id]
		if !pulled {
			continue
		}
		ids := linkIds(fields[p.groupBy])
		if len(ids) == 0 {
			noValue = true
		}
		for _, anchor := range ids {
			anchors[anchor] = true
		}
	}

	var out []string
	for _, item := range items {
		id := host.StringOr(item["id"], "")
		fields, _ := item["fields"].(map[string]any)
		if id == "" || present[id] || fields == nil {
			continue
		}
		if targets != nil && !targets[host.StringOr(fields[schema.TypeKey], "")] {
			continue
		}
		if archived, _ := fields[schema.ArchivedKey].(bool); archived && !p.includeArchive {
			continue
		}
		ids := linkIds(fields[p.groupBy])
		match := len(ids) == 0 && noValue
		for _, value := range ids {
			match = match || anchors[value]
		}
		if match {
			out = append(out, id)
		}
	}
	return out
}
