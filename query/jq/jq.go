// Package jq runs jq programs, git-work's query language (3c9c24d, 483dbe2).
//
// There is one evaluator for the whole tool:
// `git work issue PROGRAM` and the Starlark host API's `work.issue.list(program)`
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

// Program is a compiled jq program, safe to run more than once.
type Program struct {
	src  string
	code *gojq.Code
}

// Compile parses and compiles a jq program.
func Compile(src string) (*Program, error) {
	parsed, err := gojq.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("jq: %w", err)
	}
	code, err := gojq.Compile(parsed)
	if err != nil {
		return nil, fmt.Errorf("jq: %w", err)
	}
	return &Program{src: src, code: code}, nil
}

// Source returns the program as it was written.
func (p *Program) Source() string {
	return p.src
}

// Run evaluates the program over one input and collects every value it emits.
//
// The input must already be in the shape encoding/json decodes into
// (map[string]any, []any, string, float64, bool, nil);
// Input converts a Go value into that shape.
func (p *Program) Run(input any) ([]any, error) {
	iter := p.code.Run(input)

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
func Run(src string, input any) ([]any, error) {
	p, err := Compile(src)
	if err != nil {
		return nil, err
	}
	return p.Run(input)
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
