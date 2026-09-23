package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/git-bug/git-bug/util/text"
)

// Value is an attribute value as stored in an operation: verbatim JSON.
//
// The entity does not know what a shape's attributes mean,
// so it keeps the bytes and lets the schema layer above interpret them.
type Value = json.RawMessage

const (
	// MaxKeySize bounds a config key, in bytes.
	MaxKeySize = 64
	// MaxNameSize bounds an attribute name, in bytes.
	MaxNameSize = 64
	// MaxValueSize bounds one attribute value, in bytes of JSON.
	// A flow's script is the largest thing stored here.
	MaxValueSize = 64 * 1024
)

// slugPattern is what one half of a key looks like:
// the stricter of the two slugs,
// the same the issue entity applies to a field key (E3).
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// configKeyPattern is the frozen, shape-agnostic shape of a key:
// a slug with one optional `/` (E3).
//
// It is what the create operation checks,
// because an operation may never reject what a later binary writes;
// the per-shape rule below is stricter and lives on the write path.
var configKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*(/[a-z0-9][a-z0-9_.-]*)?$`)

// namePattern is what an attribute name looks like,
// with one optional `/` for the one-attribute-per-member folding,
// `values/<id>` and `target_types/<type>` (E3).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(/[a-z0-9][a-z0-9_.-]*)?$`)

// ValidateShape checks that the shape is one this binary knows.
//
// It is a write-path rule only.
// The create operation does not call it,
// so an entity of a shape a later phase adds
// still reads, lists and syncs on this binary (E2).
func ValidateShape(shape Shape) error {
	switch shape {
	case ShapeType, ShapeField, ShapeFlow:
		return nil
	case "":
		return fmt.Errorf("shape is empty")
	default:
		return fmt.Errorf("unknown shape %q, expected one of type, field, flow", shape)
	}
}

// ValidateConfigKey checks the frozen, shape-agnostic shape of a key.
func ValidateConfigKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is empty")
	}
	if len(key) > MaxKeySize {
		return fmt.Errorf("key is longer than %d bytes", MaxKeySize)
	}
	if !configKeyPattern.MatchString(key) {
		return fmt.Errorf("key %q must match %s", key, configKeyPattern.String())
	}
	return nil
}

// ValidateKey checks a key against the rule of its shape (E3):
// a type or a flow is one slug,
// a field is `<type>/<field>` with both halves a slug.
//
// A shape this binary does not know is checked
// against the shape-agnostic rule alone,
// which is the same tolerance ValidateShape's caller grants on read.
func ValidateKey(shape Shape, key string) error {
	if err := ValidateConfigKey(key); err != nil {
		return err
	}

	switch shape {
	case ShapeType, ShapeFlow:
		if !slugPattern.MatchString(key) {
			return fmt.Errorf("%s key %q must match %s", shape, key, slugPattern.String())
		}
	case ShapeField:
		typeKey, fieldKey, found := strings.Cut(key, "/")
		if !found {
			return fmt.Errorf("field key %q must be <type>/<field>", key)
		}
		if !slugPattern.MatchString(typeKey) {
			return fmt.Errorf("field key %q: type %q must match %s", key, typeKey, slugPattern.String())
		}
		if !slugPattern.MatchString(fieldKey) {
			return fmt.Errorf("field key %q: field %q must match %s", key, fieldKey, slugPattern.String())
		}
	}

	return nil
}

// SplitFieldKey splits a field key into its type and field halves.
func SplitFieldKey(key string) (typeKey, fieldKey string, ok bool) {
	typeKey, fieldKey, ok = strings.Cut(key, "/")
	if !ok {
		return "", "", false
	}
	return typeKey, fieldKey, slugPattern.MatchString(typeKey) && slugPattern.MatchString(fieldKey)
}

// ValidateName checks the shape of an attribute name.
//
// Deliberately strict: this check is structural and can only ever be loosened,
// because tightening it later would make existing history unreadable.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("attribute name is empty")
	}
	if len(name) > MaxNameSize {
		return fmt.Errorf("attribute name is longer than %d bytes", MaxNameSize)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("attribute name %q must match %s", name, namePattern.String())
	}
	return nil
}

// SplitName splits an attribute name into its member prefix and member id,
// for the names that fold a collection one attribute per member,
// `values/<id>` and `target_types/<type>` (E3).
func SplitName(name string) (prefix, member string, ok bool) {
	return strings.Cut(name, "/")
}

// ValidateValue checks the shape of an attribute value.
func ValidateValue(value Value) error {
	if len(value) == 0 {
		return fmt.Errorf("value is empty")
	}
	if len(value) > MaxValueSize {
		return fmt.Errorf("value is larger than %d bytes", MaxValueSize)
	}
	if !json.Valid(value) {
		return fmt.Errorf("value is not valid JSON")
	}
	if !text.Safe(string(value)) {
		return fmt.Errorf("value is not fully printable")
	}
	return nil
}

// StringValue encodes a string as a Value.
func StringValue(s string) Value {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return data
}

// MustValue encodes any Go value as a Value; it panics on values JSON cannot encode.
func MustValue(v interface{}) Value {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// String decodes a value as a JSON string.
func String(v Value) (string, bool) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

// Number decodes a value as a JSON number.
func Number(v Value) (float64, bool) {
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return 0, false
	}
	return f, true
}

// Bool decodes a value as a JSON bool.
func Bool(v Value) (bool, bool) {
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return false, false
	}
	return b, true
}
