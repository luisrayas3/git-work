package issuecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
)

// readArg returns an argument verbatim, or the whole of standard input when it
// is "-": every writer takes its document that way, so that a shell can paste
// it and a program can pipe it.
func readArg(env *execenv.Env, arg string) ([]byte, error) {
	if arg != "-" {
		return []byte(arg), nil
	}
	data, err := io.ReadAll(env.In)
	if err != nil {
		return nil, fmt.Errorf("reading the standard input: %w", err)
	}
	return data, nil
}

// decodeJSON decodes one JSON value and refuses anything after it,
// so that a truncated or doubled document is an error rather than half a write.
// Unknown keys are refused too: a misspelled key is a mistake, never a no-op.
func decodeJSON(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("invalid JSON document: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON document: more than one value")
	}
	return nil
}

// readFields reads a `{"key": value, ...}` argument, as `set` takes it.
func readFields(env *execenv.Env, arg string) (map[string]issue.Value, error) {
	data, err := readArg(env, arg)
	if err != nil {
		return nil, err
	}
	var fields map[string]issue.Value
	if err := decodeJSON(data, &fields); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no field to set")
	}
	return fields, nil
}

// readItems reads a `{"key": [item, ...], ...}` argument,
// as `add` and `remove` take it.
func readItems(env *execenv.Env, arg string) (map[string][]issue.Value, error) {
	data, err := readArg(env, arg)
	if err != nil {
		return nil, err
	}
	var items map[string][]issue.Value
	if err := decodeJSON(data, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no item to change")
	}
	for key, list := range items {
		if len(list) == 0 {
			return nil, fmt.Errorf("field %s has no item", key)
		}
	}
	return items, nil
}

// readBody reads a comment's body, which is text and not JSON.
func readBody(env *execenv.Env, arg string) (string, error) {
	data, err := readArg(env, arg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
