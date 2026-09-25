package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/git-bug/git-bug/util/sorted"
)

// Resolver answers the two questions a value check can not answer alone:
// whether an identity exists, and what type an issue is.
//
// The cache implements it; a test implements it with two maps.
type Resolver interface {
	// IdentityExists reports an error naming what is wrong
	// when the string is not an identity this repository knows.
	IdentityExists(id string) error
	// IssueType returns the type key of an issue, by id or id prefix.
	IssueType(id string) (string, error)
}

// Checker validates an issue write against the schema.
//
// It lives here rather than on the operation
// because an operation's Validate is frozen and must accept
// everything ever written (config-entity.md, fact 2):
// a schema is a statement about what may be written *now* (D6).
type Checker struct {
	Schema   *Schema
	Resolver Resolver
}

// Problems is every reason a write was refused, as one error.
//
// One error listing every problem, rather than the first one:
// an agent that has to make three round trips to learn three mistakes
// is an agent making three commits (D6).
type Problems struct {
	List []string
}

func (p *Problems) Error() string {
	if len(p.List) == 1 {
		return p.List[0]
	}
	return fmt.Sprintf("%d problems:\n  - %s", len(p.List), strings.Join(p.List, "\n  - "))
}

func (p *Problems) add(format string, args ...interface{}) {
	p.List = append(p.List, fmt.Sprintf(format, args...))
}

func (p *Problems) err() error {
	if len(p.List) == 0 {
		return nil
	}
	return p
}

// Validating reports whether the schema says anything about writes.
//
// An empty schema is the bootstrap state:
// nothing is validated, which is how a repository with no config entities
// still takes every write (E4).
func (c *Checker) Validating() bool {
	return c != nil && c.Schema != nil && !c.Schema.Empty()
}

// CheckNew validates the fields of a new issue.
//
// The type is required, because without it
// nothing can say which fields the issue has (D2).
func (c *Checker) CheckNew(fields map[string]json.RawMessage) error {
	if !c.Validating() {
		return nil
	}

	problems := &Problems{}

	typeKey, ok := stringValue(fields[TypeKey])
	if !ok {
		problems.add("field %s is required: one of %s", TypeKey, strings.Join(c.Schema.TypeKeys(), ", "))
		return problems.err()
	}
	if _, known := c.Schema.Type(typeKey); !known {
		problems.add("%s %q is not in the schema; valid types: %s",
			TypeKey, typeKey, strings.Join(c.Schema.TypeKeys(), ", "))
		return problems.err()
	}

	c.checkFields(problems, typeKey, fields)
	return problems.err()
}

// CheckFields validates a `set`: one value per key, replacing the field whole.
//
// currentType is the issue's type as it stands;
// a set that changes the type is checked against the new one,
// because that is what the issue will be.
func (c *Checker) CheckFields(currentType string, fields map[string]json.RawMessage) error {
	if !c.Validating() {
		return nil
	}

	problems := &Problems{}

	typeKey := currentType
	if raw, ok := fields[TypeKey]; ok {
		if next, ok := stringValue(raw); ok {
			typeKey = next
		}
	}

	if _, known := c.Schema.Type(typeKey); !known {
		if typeKey == "" {
			problems.add("the issue has no %s, so its fields are unknown; set %s first", TypeKey, TypeKey)
		} else {
			problems.add("%s %q is not in the schema; valid types: %s",
				TypeKey, typeKey, strings.Join(c.Schema.TypeKeys(), ", "))
		}
		return problems.err()
	}

	c.checkFields(problems, typeKey, fields)
	return problems.err()
}

// CheckItems validates an `add` or a `remove`: items of list-valued fields.
func (c *Checker) CheckItems(currentType string, items map[string][]json.RawMessage) error {
	if !c.Validating() {
		return nil
	}

	problems := &Problems{}

	t, known := c.Schema.Type(currentType)
	if !known {
		if currentType == "" {
			problems.add("the issue has no %s, so its fields are unknown; set %s first", TypeKey, TypeKey)
		} else {
			problems.add("%s %q is not in the schema; valid types: %s",
				TypeKey, currentType, strings.Join(c.Schema.TypeKeys(), ", "))
		}
		return problems.err()
	}

	for _, key := range sorted.Keys(items) {
		field, ok := t.Field(key)
		if !ok {
			problems.add("%s", unknownField(t, key))
			continue
		}
		if !field.Kind.IsMulti() {
			problems.add("field %s of type %s is %s, not a list; use set", key, t.Key, field.Kind)
			continue
		}
		for _, item := range items[key] {
			if err := c.checkOne(field, field.Kind.ItemKind(), item); err != nil {
				problems.add("field %s: %v", key, err)
			}
		}
	}

	return problems.err()
}

func (c *Checker) checkFields(problems *Problems, typeKey string, fields map[string]json.RawMessage) {
	t, known := c.Schema.Type(typeKey)
	if !known {
		return
	}

	for _, key := range sorted.Keys(fields) {
		field, ok := t.Field(key)
		if !ok {
			problems.add("%s", unknownField(t, key))
			continue
		}
		value := fields[key]
		if isNull(value) {
			// a null clears a field, which is always a legal write;
			// what a missing value means is the reader's business (D6)
			continue
		}
		if field.Kind.IsMulti() {
			items, ok := jsonArray(value)
			if !ok {
				problems.add("field %s: a %s is a list; write a JSON array, not %s",
					key, field.Kind, brief(value))
				continue
			}
			for _, item := range items {
				if err := c.checkOne(field, field.Kind.ItemKind(), item); err != nil {
					problems.add("field %s: %v", key, err)
				}
			}
			continue
		}
		if err := c.checkOne(field, field.Kind, value); err != nil {
			problems.add("field %s: %v", key, err)
		}
	}
}

// checkOne validates one value against one kind.
func (c *Checker) checkOne(field *Field, kind Kind, value json.RawMessage) error {
	switch kind {
	case KindText, KindRank:
		if _, ok := stringValue(value); !ok {
			return fmt.Errorf("a string is expected; %s is not one", brief(value))
		}
		return nil

	case KindBool:
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return fmt.Errorf("true or false is expected; %s is neither", brief(value))
		}
		return nil

	case KindNumber:
		var f float64
		if err := json.Unmarshal(value, &f); err != nil {
			return fmt.Errorf("a number is expected; %s is not one", brief(value))
		}
		return nil

	case KindDate:
		s, ok := stringValue(value)
		if !ok {
			return fmt.Errorf("a date is expected, as a string; %s is not one", brief(value))
		}
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			return nil
		}
		if _, err := time.Parse(time.DateOnly, s); err == nil {
			return nil
		}
		return fmt.Errorf("%q is not a date; write 2026-09-23 or an RFC 3339 time", s)

	case KindEnum, KindOrdinalEnum:
		s, ok := stringValue(value)
		if !ok {
			return fmt.Errorf("a value id is expected, as a string; %s is not one", brief(value))
		}
		if _, ok := field.Value(s); ok {
			return nil
		}
		if field.Freeform {
			return nil
		}
		return fmt.Errorf("%q is not in the schema; valid values: %s",
			s, strings.Join(field.ValueIds(), ", "))

	case KindIdentity:
		s, ok := stringValue(value)
		if !ok {
			return fmt.Errorf("an identity id is expected, as a string; %s is not one", brief(value))
		}
		if c.Resolver == nil {
			return nil
		}
		if err := c.Resolver.IdentityExists(s); err != nil {
			return fmt.Errorf("%q is not an identity: %v", s, err)
		}
		return nil

	case KindRelation:
		s, ok := stringValue(value)
		if !ok {
			return fmt.Errorf("an issue id is expected, as a string; %s is not one", brief(value))
		}
		if c.Resolver == nil {
			return nil
		}
		targetType, err := c.Resolver.IssueType(s)
		if err != nil {
			return fmt.Errorf("%q is not an issue: %v", s, err)
		}
		if len(field.TargetTypes) == 0 {
			return nil
		}
		for _, allowed := range field.TargetTypes {
			if allowed == targetType {
				return nil
			}
		}
		return fmt.Errorf("issue %s is a %s; %s takes %s",
			s, orUnknown(targetType), field.Key, strings.Join(field.TargetTypes, ", "))

	default:
		// A kind this binary does not know can not be written by it,
		// so reaching here means the kind list and this switch disagree.
		return fmt.Errorf("unknown kind %s", kind)
	}
}

// unknownField is the error a key that is not a field of the type gets:
// the type's fields, named, because that is what the caller needs next.
func unknownField(t *Type, key string) string {
	return fmt.Sprintf("field %q is not a field of type %s; its fields are: %s",
		key, t.Key, strings.Join(t.FieldKeys(), ", "))
}

func orUnknown(typeKey string) string {
	if typeKey == "" {
		return "issue of no type"
	}
	return typeKey
}

func stringValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	return items, true
}

// brief renders a value for an error message, short enough to read.
func brief(value json.RawMessage) string {
	const max = 60
	s := strings.TrimSpace(string(value))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
