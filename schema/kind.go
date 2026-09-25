package schema

import (
	"fmt"
	"strings"
)

// Kind is a field's data type.
//
// The list is fixed and closed (configurable-schema.md D2):
// a missing value is config, a missing kind is a language change,
// which is the line "just configurable enough" is drawn at.
type Kind string

const (
	// KindText is free text: a title, a description.
	KindText Kind = "text"
	// KindEnum is one value id out of the field's values.
	// A value may carry a category, which is what status is keyed on;
	// the earlier `enum-with-category` is this kind with categories set.
	KindEnum Kind = "enum"
	// KindOrdinalEnum is an enum whose values are ordered, like priority.
	KindOrdinalEnum Kind = "ordinal-enum"
	// KindBool is a flag, like archived.
	KindBool Kind = "bool"
	// KindNumber is a number: an estimate, a capacity.
	KindNumber Kind = "number"
	// KindDate is an RFC 3339 date or date-time string.
	KindDate Kind = "date"
	// KindIdentity is the entity id of an identity, like an assignee.
	KindIdentity Kind = "identity"
	// KindMultiEnum is a set of value ids, like labels.
	KindMultiEnum Kind = "multi-enum"
	// KindMultiIdentity is a set of identity ids, like reviewers.
	KindMultiIdentity Kind = "multi-identity"
	// KindRank is a LexoRank-style string, ordered by (rank, id) (441dcbb).
	KindRank Kind = "rank"
	// KindRelation is the entity id of one other issue: parent, iteration.
	KindRelation Kind = "relation"
	// KindMultiRelation is a set of issue ids: blocks, relates-to.
	KindMultiRelation Kind = "multi-relation"
)

// Kinds is every kind, in the order D2's table lists them.
var Kinds = []Kind{
	KindText,
	KindEnum,
	KindOrdinalEnum,
	KindBool,
	KindNumber,
	KindDate,
	KindIdentity,
	KindMultiEnum,
	KindMultiIdentity,
	KindRank,
	KindRelation,
	KindMultiRelation,
}

// ParseKind checks that a kind is one of the fixed list,
// naming the valid ones when it is not,
// because an agent is the main caller and an agent can act on that (D6).
func ParseKind(s string) (Kind, error) {
	for _, kind := range Kinds {
		if Kind(s) == kind {
			return kind, nil
		}
	}
	if s == "" {
		return "", fmt.Errorf("kind is missing; valid kinds: %s", KindList())
	}
	return "", fmt.Errorf("unknown kind %q; valid kinds: %s", s, KindList())
}

// KindList names every kind, for an error message.
func KindList() string {
	names := make([]string, len(Kinds))
	for i, kind := range Kinds {
		names[i] = string(kind)
	}
	return strings.Join(names, ", ")
}

// IsEnum reports whether the field's values are the ids it accepts.
func (k Kind) IsEnum() bool {
	switch k {
	case KindEnum, KindOrdinalEnum, KindMultiEnum:
		return true
	}
	return false
}

// IsRelation reports whether the field's value is another issue's id.
func (k Kind) IsRelation() bool {
	return k == KindRelation || k == KindMultiRelation
}

// IsMulti reports whether the field holds a set of items,
// which is what AddValue and RemoveValue apply to (D2).
func (k Kind) IsMulti() bool {
	switch k {
	case KindMultiEnum, KindMultiIdentity, KindMultiRelation:
		return true
	}
	return false
}

// ItemKind is the kind of one item of a multi-valued field.
func (k Kind) ItemKind() Kind {
	switch k {
	case KindMultiEnum:
		return KindEnum
	case KindMultiIdentity:
		return KindIdentity
	case KindMultiRelation:
		return KindRelation
	}
	return k
}

// Category is what every tool keys behaviour off,
// so that "done" is a property of the value and not of its name.
//
// The set is fixed and closed (D2).
type Category string

const (
	CategoryBacklog   Category = "backlog"
	CategoryUnstarted Category = "unstarted"
	CategoryStarted   Category = "started"
	CategoryCompleted Category = "completed"
	CategoryCanceled  Category = "canceled"
)

// Categories is every category, in workflow order.
var Categories = []Category{
	CategoryBacklog,
	CategoryUnstarted,
	CategoryStarted,
	CategoryCompleted,
	CategoryCanceled,
}

// ParseCategory checks that a category is one of the fixed set.
// The empty string is a category-less value, which is what priority's are.
func ParseCategory(s string) (Category, error) {
	if s == "" {
		return "", nil
	}
	for _, category := range Categories {
		if Category(s) == category {
			return category, nil
		}
	}
	return "", fmt.Errorf("unknown category %q; valid categories: %s", s, CategoryList())
}

// CategoryList names every category, for an error message.
func CategoryList() string {
	names := make([]string, len(Categories))
	for i, category := range Categories {
		names[i] = string(category)
	}
	return strings.Join(names, ", ")
}

// Done reports whether work in this category is over, either way.
func (c Category) Done() bool {
	return c == CategoryCompleted || c == CategoryCanceled
}
