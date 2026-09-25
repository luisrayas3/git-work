package issuecmd

import (
	"fmt"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
)

// readFields reads a `{"key": value, ...}` argument, as `set` takes it.
func readFields(env *execenv.Env, arg string) (map[string]issue.Value, error) {
	data, err := execenv.ReadLiteralOrStdin(env, arg)
	if err != nil {
		return nil, err
	}
	var fields map[string]issue.Value
	if err := host.DecodeStrict(data, &fields); err != nil {
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
	data, err := execenv.ReadLiteralOrStdin(env, arg)
	if err != nil {
		return nil, err
	}
	var items map[string][]issue.Value
	if err := host.DecodeStrict(data, &items); err != nil {
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
	data, err := execenv.ReadLiteralOrStdin(env, arg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
