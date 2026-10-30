// Package jq runs jq programs, git-work's query language (3c9c24d, 483dbe2).
//
// There is one evaluator for the whole tool:
// `git work issue list PROGRAM` and the Starlark host API's `work.issue.list(program)`
// call this package over the same JSON,
// so a program written at the shell means the same thing inside a flow.
//
// gojq is the implementation:
// it is jq's language without jq's C library,
// and it works on the values encoding/json decodes into.
package jq

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/itchyny/gojq"
)

// Every program runs with the schema bound as `$schema`, the document
// `git work schema export --format json` prints, and three functions over it
// (doc/design/schema-in-jq.md, J1, J2): what a value means is read from the
// schema the program runs under, not written into the program.
//
// The functions are ordinary jq, so they read as such. Each is null wherever
// the schema says nothing about the value (J3): an unset field, a type
// without it, a value the schema does not list, a multi-enum's list. A
// program that defines a function of the same name keeps its own, since the
// helpers come before its definitions and gojq resolves a name to its last.
const schemaHelpers = `
def ordinal(f):
  .fields[f] as $v
  | if ($v | type) != "string" then null
    else [$schema.types[.fields.type | strings].fields[f].values[]?.id] | index($v)
    end;
def category(f):
  .fields[f] as $v
  | if ($v | type) != "string" then null
    else first($schema.types[.fields.type | strings].fields[f].values[]? | select(.id == $v) | .category // null) // null
    end;
def done: . == "completed" or . == "canceled";
.`

// helperDefs is schemaHelpers parsed, once.
var helperDefs = func() []*gojq.FuncDef {
	parsed, err := gojq.Parse(schemaHelpers)
	if err != nil {
		panic(err)
	}
	return parsed.FuncDefs
}()

// Program is a compiled jq program, safe to run more than once.
type Program struct {
	code *gojq.Code
}

// Compile parses and compiles a jq program, with `$schema` declared and the
// schema's functions defined (J1, J2).
func Compile(src string) (*Program, error) {
	parsed, err := gojq.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("jq: %w", err)
	}
	// put before the program's own, not parsed with it, so that the
	// program's error positions are its own
	parsed.FuncDefs = append(append([]*gojq.FuncDef{}, helperDefs...), parsed.FuncDefs...)
	code, err := gojq.Compile(parsed, gojq.WithVariables([]string{"$schema"}))
	if err != nil {
		return nil, fmt.Errorf("jq: %w", err)
	}
	return &Program{code: code}, nil
}

// Run evaluates the program over one input, with schema as `$schema`, and
// collects every value it emits.
//
// The input and the schema must already be in the shape encoding/json
// decodes into (map[string]any, []any, string, float64, bool, nil);
// Input converts a Go value into that shape. A nil schema is a program run
// with no schema, where every helper reads null.
func (p *Program) Run(input, schema any) ([]any, error) {
	iter := p.code.Run(input, schema)

	var out []any
	for {
		v, ok := iter.Next()
		if !ok {
			return out, nil
		}
		if err, isErr := v.(error); isErr {
			// `halt` with no value ends the stream rather than failing it,
			// which is how jq itself reads it.
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				return out, nil
			}
			return nil, fmt.Errorf("jq: %w", err)
		}
		out = append(out, v)
	}
}

// Run compiles and evaluates a program in one call.
func Run(src string, input, schema any) ([]any, error) {
	p, err := Compile(src)
	if err != nil {
		return nil, err
	}
	return p.Run(input, schema)
}

// Input converts any Go value into the shape a program runs over,
// by way of its JSON encoding:
// the program sees exactly what `--format json` prints.
func Input(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
