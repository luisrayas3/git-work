package schema

import (
	"encoding/json"
	"fmt"

	"github.com/git-bug/git-bug/entities/config"
)

// The attribute names a type or a field entity carries (E3).
//
// Attribute names are forever:
// old operations keep theirs, so a rename is a new name
// plus a projection that reads both (config-entity.md, Risks).
const (
	AttrKind        = "kind"
	AttrName        = "name"
	AttrDescription = "description"
	AttrOrdinal     = "ordinal"
	AttrFreeform    = "freeform"
	AttrInverse     = "inverse"

	// ValuesPrefix folds an enum's values one attribute per member,
	// `values/<id>`, so two people adding two statuses both keep theirs (E3).
	ValuesPrefix = "values"
	// TargetTypesPrefix folds a relation's allowed targets the same way.
	TargetTypesPrefix = "target_types"
)

// ValueAttr is the JSON one `values/<id>` attribute holds.
//
// The id is the attribute's member name, not a member of the object,
// so that renaming a value is impossible by construction.
type ValueAttr struct {
	Name        string   `json:"name,omitempty"`
	Ordinal     int      `json:"ordinal"`
	Category    Category `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Color       string   `json:"color,omitempty"`
}

// valueName is the attribute name one enum value is stored under.
func valueName(id string) string {
	return ValuesPrefix + "/" + id
}

// targetTypeName is the attribute name one allowed target type is stored under.
func targetTypeName(typeKey string) string {
	return TargetTypesPrefix + "/" + typeKey
}

// attrString reads a string attribute, defaulting to empty.
func attrString(attrs map[string]config.Value, name string) (string, error) {
	raw, ok := attrs[name]
	if !ok {
		return "", nil
	}
	s, ok := config.String(raw)
	if !ok {
		return "", fmt.Errorf("attribute %s is not a string", name)
	}
	return s, nil
}

// attrInt reads an integer attribute, defaulting to zero.
func attrInt(attrs map[string]config.Value, name string) (int, error) {
	raw, ok := attrs[name]
	if !ok {
		return 0, nil
	}
	f, ok := config.Number(raw)
	if !ok {
		return 0, fmt.Errorf("attribute %s is not a number", name)
	}
	return int(f), nil
}

// attrBool reads a bool attribute, defaulting to false.
func attrBool(attrs map[string]config.Value, name string) (bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return false, nil
	}
	b, ok := config.Bool(raw)
	if !ok {
		return false, fmt.Errorf("attribute %s is not a bool", name)
	}
	return b, nil
}

// unmarshalAttr decodes an attribute into a typed struct.
//
// Unknown members are tolerated, not refused:
// a newer binary's extra member must not make its entities unreadable here (D6).
func unmarshalAttr(value config.Value, into interface{}) error {
	return json.Unmarshal(value, into)
}

// canonical returns an attribute value as canonical JSON:
// sorted keys, no whitespace, normalised numbers,
// which is what reconcile compares by (E9).
func canonical(value config.Value) (config.Value, error) {
	var any interface{}
	if err := json.Unmarshal(value, &any); err != nil {
		return nil, err
	}
	// encoding/json sorts a map's keys and normalises numbers,
	// so marshalling what was just decoded is the normal form.
	return json.Marshal(any)
}

// sameValue reports whether two attribute values mean the same thing.
func sameValue(a, b config.Value) bool {
	ca, err := canonical(a)
	if err != nil {
		return false
	}
	cb, err := canonical(b)
	if err != nil {
		return false
	}
	return string(ca) == string(cb)
}

// mustValue encodes a Go value as an attribute value.
func mustValue(v interface{}) config.Value {
	return config.MustValue(v)
}
