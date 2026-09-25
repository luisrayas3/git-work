package issue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/git-bug/git-bug/util/text"
)

// Value is a field value as stored in an operation: verbatim JSON.
//
// The entity does not know the schema,
// so it can not decode a value into its kind;
// it keeps the bytes and lets the schema layer above interpret them
// (a string for text and enum kinds, a number, a bool, a list of strings, ...).
// A JSON null is the absence of a value:
// SetField with null clears the field.
type Value = json.RawMessage

// TitleKey is the one field this package knows by name:
// a title is required at creation and can never be cleared,
// because no tool can show an issue without one.
const TitleKey = "title"

// ArchivedKey is the field that hides an issue from every default list.
//
// Archiving is the replicated removal (cli-convention.md):
// `git work issue archive ID` is `set ID '{"archived": true}'`,
// and it is an operation, so it reaches every clone,
// where `rm` only deletes the local ref.
// The package knows the key and nothing else about it:
// what it means is the schema's business.
const ArchivedKey = "archived"

// MaxValueSize bounds a single value, in bytes of JSON.
// Long text belongs in comments.
const MaxValueSize = 64 * 1024

// MaxKeySize bounds a field key or relation kind, in bytes.
const MaxKeySize = 64

// keyPattern is what a field key or a relation kind may look like.
// Deliberately strict: this check is structural and can only ever be loosened,
// because tightening it later would make existing history unreadable.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ValidateKey checks the shape of a field key or relation kind.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is empty")
	}
	if len(key) > MaxKeySize {
		return fmt.Errorf("key is longer than %d bytes", MaxKeySize)
	}
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("key %q must match %s", key, keyPattern.String())
	}
	return nil
}

// ValidateValue checks the shape of a value for the given key.
func ValidateValue(key string, value Value) error {
	if len(value) == 0 {
		return fmt.Errorf("value is empty; use null to clear a field")
	}
	if len(value) > MaxValueSize {
		return fmt.Errorf("value is larger than %d bytes", MaxValueSize)
	}
	if !json.Valid(value) {
		return fmt.Errorf("value is not valid JSON")
	}
	if key == TitleKey {
		title, ok := String(value)
		if !ok {
			return fmt.Errorf("title must be a string")
		}
		if text.Empty(title) {
			return fmt.Errorf("title is empty")
		}
		if !text.SafeOneLine(title) {
			return fmt.Errorf("title has unsafe characters")
		}
	}
	return nil
}

// ValidateItem checks the shape of one item of a list-valued field.
// Null is not an item: it is how SetField clears a whole field.
func ValidateItem(key string, item Value) error {
	if key == TitleKey {
		return fmt.Errorf("title is not a list")
	}
	if len(item) == 0 {
		return fmt.Errorf("item is empty")
	}
	if len(item) > MaxValueSize {
		return fmt.Errorf("item is larger than %d bytes", MaxValueSize)
	}
	if !json.Valid(item) {
		return fmt.Errorf("item is not valid JSON")
	}
	if IsNull(item) {
		return fmt.Errorf("null is not an item")
	}
	return nil
}

// canonical compacts a value, so that equal JSON compares equal as bytes.
func canonical(v Value) Value {
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return v
	}
	return buf.Bytes()
}

// Items decodes the value as a JSON array, item by item, verbatim.
func Items(v Value) ([]Value, bool) {
	var raw []json.RawMessage
	if err := json.Unmarshal(v, &raw); err != nil {
		return nil, false
	}
	items := make([]Value, len(raw))
	for i, r := range raw {
		items[i] = Value(r)
	}
	return items, true
}

// ItemsValue encodes items as a JSON array.
func ItemsValue(items []Value) Value {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(canonical(it))
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// StringValue encodes a string as a Value.
func StringValue(s string) Value {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return data
}

// MustValue encodes any Go value as a Value; it panics on values JSON can not encode.
func MustValue(v interface{}) Value {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// IsNull reports whether the value is the JSON null, meaning "no value".
func IsNull(v Value) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

// String decodes the value as a JSON string.
func String(v Value) (string, bool) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

// Strings decodes the value as a JSON array of strings.
func Strings(v Value) ([]string, bool) {
	var s []string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, false
	}
	return s, true
}
