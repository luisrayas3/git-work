package schema

import (
	"encoding/json"
)

// A relation value is stored as the full id of the issue it names
// (2086c12, 2026-10-08).
// Every id position takes an id prefix or an alias,
// and a relation's value is one of them;
// stored verbatim, a prefix is a value no query matching the full id finds,
// and an alias is a value nothing resolves once the alias is reused.
// So a writer resolves it first, here, on the field's kind,
// and the check that follows refuses what does not resolve, naming it,
// because the resolved value is left as written when the lookup fails.
//
// The kind is the schema's: a field of no known type has none,
// and its value is written as given — a label that reads like an id prefix
// is a label (bb9e89e).
// The one exception is the bootstrap state, a schema with no type at all,
// where an item added or removed that is four hex digits or more and the
// prefix of exactly one issue is taken for one: the guess that keeps a
// repository with no types able to add to a relation (FullIdItem).

// FullIdValue returns the value a `set` of key on an issue of typeKey stores:
// a relation's issue, or each of a multi-relation's, as its full id.
// Anything else, and anything that does not resolve, is returned as given.
func (c *Checker) FullIdValue(typeKey, key string, value json.RawMessage) json.RawMessage {
	field, ok := c.relationField(typeKey, key)
	if !ok {
		return value
	}
	if field.Kind == KindRelation {
		return c.fullId(value)
	}
	items, ok := jsonArray(value)
	if !ok {
		return value
	}
	changed := false
	for at, item := range items {
		full := c.fullId(item)
		if string(full) != string(item) {
			items[at] = full
			changed = true
		}
	}
	if !changed {
		return value
	}
	out, err := json.Marshal(items)
	if err != nil {
		return value
	}
	return out
}

// FullIdItem returns the item an `add` or a `remove` of key stores:
// a multi-relation's issue as its full id.
// With no type in the schema at all, a hex prefix of exactly one issue is
// taken for an issue, as said above.
func (c *Checker) FullIdItem(typeKey, key string, item json.RawMessage) json.RawMessage {
	if c == nil || c.Resolver == nil {
		return item
	}
	if !c.Validating() {
		s, ok := stringValue(item)
		if !ok || len(s) < 4 || !isHex(s) {
			return item
		}
		return c.fullId(item)
	}
	field, ok := c.relationField(typeKey, key)
	if !ok || field.Kind != KindMultiRelation {
		return item
	}
	return c.fullId(item)
}

// relationField is the field key is on typeKey, when it is a relation.
func (c *Checker) relationField(typeKey, key string) (*Field, bool) {
	if c == nil || c.Resolver == nil || c.Schema == nil {
		return nil, false
	}
	field, ok := c.Schema.Field(typeKey, key)
	if !ok || !field.Kind.IsRelation() {
		return nil, false
	}
	return field, true
}

// fullId resolves one string naming an issue; anything else is returned as is.
func (c *Checker) fullId(value json.RawMessage) json.RawMessage {
	s, ok := stringValue(value)
	if !ok {
		return value
	}
	id, err := c.Resolver.IssueId(s)
	if err != nil || id == s {
		return value
	}
	out, err := json.Marshal(id)
	if err != nil {
		return value
	}
	return out
}

func isHex(s string) bool {
	for _, r := range s {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}
