package issuecmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/entities/issue"
)

// parseValue turns a command-line argument into a field value.
//
// Anything that parses as JSON is taken as JSON,
// so `true`, `3`, `null`, `["a","b"]` and `"quoted"` mean what they say;
// anything else is a string.
// That makes the common case short (`set <id> status closed`)
// and the precise case possible (`set <id> estimate 3` versus `'"3"'`).
func parseValue(arg string) issue.Value {
	trimmed := strings.TrimSpace(arg)
	if json.Valid([]byte(trimmed)) {
		return issue.Value(trimmed)
	}
	return issue.StringValue(arg)
}

// parseAssignments turns repeated `key=value` flags into fields.
func parseAssignments(assignments []string) (map[string]issue.Value, error) {
	if len(assignments) == 0 {
		return nil, nil
	}
	fields := make(map[string]issue.Value, len(assignments))
	for _, a := range assignments {
		key, value, ok := strings.Cut(a, "=")
		if !ok {
			return nil, fmt.Errorf("no \"=\" in key=value field %q", a)
		}
		if err := issue.ValidateKey(key); err != nil {
			return nil, err
		}
		fields[key] = parseValue(value)
	}
	return fields, nil
}
