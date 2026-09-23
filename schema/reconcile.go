package schema

import (
	"sort"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
)

// Change is one entity's share of an import: a create, an update or an archive.
//
// One change is one commit, because one entity is the write unit;
// an import that touches five fields is five commits,
// each valid on its own (E9).
type Change struct {
	Action Action       `json:"action"`
	Shape  config.Shape `json:"shape"`
	Key    string       `json:"key"`
	// Id is the entity the change applies to, unset for a create.
	Id entity.Id `json:"id,omitempty"`
	// Set are the attributes to write; for a create, the whole entity.
	Set map[string]config.Value `json:"set,omitempty"`
	// Remove are the attributes to delete.
	Remove []string `json:"remove,omitempty"`
}

// Action is what a change does to its entity.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionArchive Action = "archive"
)

// ordinalStep is how far apart a fresh numbering spaces its entries,
// so that inserting one entry between two needs one operation, not a renumber (E5).
const ordinalStep = 10

// Reconcile computes what an import has to write.
//
// It emits only differences, which is what keeps every import
// from bloating the log with operations that change nothing (Risks);
// the Jira sync goes through the same function (`69b7be0`).
//
// An unmatched entity is archived only under prune,
// so an import is an upsert by default
// and a partial file from anywhere can never archive anyone's work (E9).
func Reconcile(desired *Document, current []Entry, prune bool) ([]Change, error) {
	index := map[string]Entry{}
	for _, e := range current {
		index[string(e.Shape)+" "+e.Key] = e
	}
	matched := map[string]struct{}{}

	lookup := func(shape config.Shape, key string) (Entry, bool) {
		e, ok := index[string(shape)+" "+key]
		if ok {
			matched[string(shape)+" "+key] = struct{}{}
		}
		return e, ok
	}

	var changes []Change

	// Types first: a field entity's key names its type,
	// and a half-applied import must still read (E9).
	typeKeys := desired.Types.Keys()
	typeOrdinals := assignOrdinals(typeKeys, currentOrdinals(index, config.ShapeType, typeKeys))

	for _, typeKey := range typeKeys {
		t, _ := desired.Types.Get(typeKey)
		attributes := typeAttributes(t, typeOrdinals[typeKey])
		entry, exists := lookup(config.ShapeType, typeKey)
		if change, ok := diff(config.ShapeType, typeKey, entry, exists, attributes, typeAttributeNames); ok {
			changes = append(changes, change)
		}
	}

	for _, typeKey := range typeKeys {
		t, _ := desired.Types.Get(typeKey)
		fieldKeys := t.Fields.Keys()

		qualified := make([]string, len(fieldKeys))
		for i, fieldKey := range fieldKeys {
			qualified[i] = typeKey + "/" + fieldKey
		}
		ordinals := assignOrdinals(qualified, currentOrdinals(index, config.ShapeField, qualified))

		for i, fieldKey := range fieldKeys {
			key := qualified[i]
			field, _ := t.Fields.Get(fieldKey)
			entry, exists := lookup(config.ShapeField, key)

			attributes, err := fieldAttributes(fieldKey, field, ordinals[key], entry, exists)
			if err != nil {
				return nil, err
			}
			if change, ok := diff(config.ShapeField, key, entry, exists, attributes, fieldAttributeNames); ok {
				changes = append(changes, change)
			}
		}
	}

	if prune {
		var orphans []Entry
		for key, e := range index {
			if _, ok := matched[key]; ok {
				continue
			}
			orphans = append(orphans, e)
		}
		sort.Slice(orphans, func(i, j int) bool {
			if orphans[i].Shape != orphans[j].Shape {
				// fields before types, which "field" < "type" already gives:
				// a type's fields go first, so a half-applied prune
				// never leaves a live field on an archived type
				return orphans[i].Shape < orphans[j].Shape
			}
			return orphans[i].Key < orphans[j].Key
		})
		for _, e := range orphans {
			changes = append(changes, Change{
				Action: ActionArchive,
				Shape:  e.Shape,
				Key:    e.Key,
				Id:     e.Id,
			})
		}
	}

	return changes, nil
}

// typeAttributeNames are the attributes an import owns on a type entity.
// Anything else an entity carries — a newer binary's attribute — is left alone.
func typeAttributeNames(name string) bool {
	switch name {
	case AttrName, AttrDescription, AttrOrdinal:
		return true
	}
	return false
}

// fieldAttributeNames are the attributes an import owns on a field entity.
func fieldAttributeNames(name string) bool {
	switch name {
	case AttrKind, AttrName, AttrDescription, AttrOrdinal, AttrFreeform, AttrInverse:
		return true
	}
	prefix, _, folded := config.SplitName(name)
	return folded && (prefix == ValuesPrefix || prefix == TargetTypesPrefix)
}

func typeAttributes(t TypeDoc, ordinal int) map[string]config.Value {
	attributes := map[string]config.Value{
		AttrOrdinal: mustValue(ordinal),
	}
	if t.Name != "" {
		attributes[AttrName] = mustValue(t.Name)
	}
	if t.Description != "" {
		attributes[AttrDescription] = mustValue(t.Description)
	}
	return attributes
}

func fieldAttributes(fieldKey string, field FieldDoc, ordinal int, entry Entry, exists bool) (map[string]config.Value, error) {
	attributes := map[string]config.Value{
		AttrKind:    mustValue(field.Kind),
		AttrOrdinal: mustValue(ordinal),
	}
	if field.Name != "" {
		attributes[AttrName] = mustValue(field.Name)
	}
	if field.Description != "" {
		attributes[AttrDescription] = mustValue(field.Description)
	}
	if field.Freeform {
		attributes[AttrFreeform] = mustValue(true)
	}
	if field.Inverse != "" {
		attributes[AttrInverse] = mustValue(field.Inverse)
	}
	for _, target := range field.TargetTypes {
		attributes[targetTypeName(target)] = mustValue(struct{}{})
	}

	ids := make([]string, len(field.Values))
	for i, value := range field.Values {
		ids[i] = value.Id
	}
	var currentValueOrdinals map[string]int
	if exists {
		currentValueOrdinals = valueOrdinals(entry.Attributes)
	}
	valueOrdinal := assignOrdinals(ids, currentValueOrdinals)

	for _, value := range field.Values {
		attributes[valueName(value.Id)] = mustValue(ValueAttr{
			Name:        value.Name,
			Ordinal:     valueOrdinal[value.Id],
			Category:    Category(value.Category),
			Description: value.Description,
			Color:       value.Color,
		})
	}

	return attributes, nil
}

// diff turns a desired attribute set into the change that gets there,
// comparing by canonical JSON so that "normalise before comparing" holds (E9).
func diff(shape config.Shape, key string, entry Entry, exists bool, desired map[string]config.Value, owned func(string) bool) (Change, bool) {
	if !exists {
		return Change{
			Action: ActionCreate,
			Shape:  shape,
			Key:    key,
			Set:    desired,
		}, true
	}

	change := Change{
		Action: ActionUpdate,
		Shape:  shape,
		Key:    key,
		Id:     entry.Id,
	}

	for _, name := range sortedKeys(desired) {
		have, present := entry.Attributes[name]
		if present && sameValue(have, desired[name]) {
			continue
		}
		if change.Set == nil {
			change.Set = map[string]config.Value{}
		}
		change.Set[name] = desired[name]
	}

	for _, name := range sortedKeys(entry.Attributes) {
		if !owned(name) {
			continue
		}
		if _, wanted := desired[name]; wanted {
			continue
		}
		change.Remove = append(change.Remove, name)
	}

	if len(change.Set) == 0 && len(change.Remove) == 0 {
		return Change{}, false
	}
	return change, true
}

// currentOrdinals reads the ordinal an entity carries today,
// which is what keeps unchanged entries at their numbers.
func currentOrdinals(index map[string]Entry, shape config.Shape, keys []string) map[string]int {
	out := map[string]int{}
	for _, key := range keys {
		entry, ok := index[string(shape)+" "+key]
		if !ok {
			continue
		}
		ordinal, err := attrInt(entry.Attributes, AttrOrdinal)
		if err != nil {
			continue
		}
		out[key] = ordinal
	}
	return out
}

// valueOrdinals reads the ordinals of an enum's values as they stand.
func valueOrdinals(attrs map[string]config.Value) map[string]int {
	out := map[string]int{}
	for name, raw := range attrs {
		prefix, id, folded := config.SplitName(name)
		if !folded || prefix != ValuesPrefix {
			continue
		}
		var attr ValueAttr
		if err := unmarshalAttr(raw, &attr); err != nil {
			continue
		}
		out[id] = attr.Ordinal
	}
	return out
}

// assignOrdinals numbers a list so that unchanged relative order keeps its
// numbers, a moved or new entry takes a number between its neighbours, and a
// list is renumbered only when no number fits (E9, step 3).
func assignOrdinals(keys []string, current map[string]int) map[string]int {
	out := make(map[string]int, len(keys))
	if len(keys) == 0 {
		return out
	}

	anchors := keptAnchors(keys, current)
	if len(anchors) == 0 {
		return renumber(keys)
	}

	for _, anchor := range anchors {
		out[keys[anchor]] = current[keys[anchor]]
	}

	// before the first anchor
	if first := anchors[0]; first > 0 {
		if !fillBefore(keys[:first], current[keys[first]], out) {
			return renumber(keys)
		}
	}

	// between anchors
	for i := 0; i+1 < len(anchors); i++ {
		lo, hi := anchors[i], anchors[i+1]
		if hi == lo+1 {
			continue
		}
		if !fillBetween(keys[lo+1:hi], current[keys[lo]], current[keys[hi]], out) {
			return renumber(keys)
		}
	}

	// after the last anchor
	last := anchors[len(anchors)-1]
	ordinal := current[keys[last]]
	for _, key := range keys[last+1:] {
		ordinal += ordinalStep
		out[key] = ordinal
	}

	return out
}

// keptAnchors is the longest run of entries whose current ordinals already
// increase in the desired order: those are the numbers worth keeping.
func keptAnchors(keys []string, current map[string]int) []int {
	var positions []int
	for at, key := range keys {
		if _, ok := current[key]; ok {
			positions = append(positions, at)
		}
	}
	if len(positions) == 0 {
		return nil
	}

	// longest strictly increasing subsequence, O(n²) over a few dozen entries
	best := make([]int, len(positions))
	from := make([]int, len(positions))
	bestAt, bestLen := 0, 0
	for i := range positions {
		best[i], from[i] = 1, -1
		for j := 0; j < i; j++ {
			if current[keys[positions[j]]] < current[keys[positions[i]]] && best[j]+1 > best[i] {
				best[i], from[i] = best[j]+1, j
			}
		}
		if best[i] > bestLen {
			bestLen, bestAt = best[i], i
		}
	}

	var anchors []int
	for at := bestAt; at >= 0; at = from[at] {
		anchors = append(anchors, positions[at])
	}
	sort.Ints(anchors)
	return anchors
}

// fillBefore numbers the entries that come before the first anchor.
func fillBefore(keys []string, hi int, out map[string]int) bool {
	if len(keys) == 0 {
		return true
	}
	if hi-len(keys) < 1 {
		return false
	}
	for i, key := range keys {
		out[key] = hi - len(keys) + i
	}
	return true
}

// fillBetween numbers the entries between two anchors, evenly.
func fillBetween(keys []string, lo, hi int, out map[string]int) bool {
	if len(keys) == 0 {
		return true
	}
	if hi-lo-1 < len(keys) {
		return false
	}
	step := (hi - lo) / (len(keys) + 1)
	for i, key := range keys {
		out[key] = lo + (i+1)*step
	}
	return true
}

// renumber gives the whole list fresh numbers, spaced by tens.
func renumber(keys []string) map[string]int {
	out := make(map[string]int, len(keys))
	for i, key := range keys {
		out[key] = (i + 1) * ordinalStep
	}
	return out
}
