// Package view is the argument table of every view kind, and the seam a
// renderer plugs into.
//
// A view is a module, not a surface (doc/design/cli-convention.md):
// `work.view.board(columns="status")` and
// `git work view board '{"columns":"status"}'`
// are the same call, with the same keywords and the same defaults,
// because both parse against the one table in kinds.go.
//
// The command *is* the view: a call's whole input is one JSON object of
// keyword arguments, `query` included, so nothing is piped in and nothing
// intermediate is printed out (decided 2026-09-24). A view reads the store
// itself, draws, and returns what the user answered.
//
// What draws it is a Renderer: the terminal one (package `tui`, `84dfbde`) and
// the browser one (`8b06191`). A kind no renderer draws yet fails naming the
// renderer, so a view is never a command on one surface and not the other.
// There are no field roles on the schema: a flow's script names the fields it
// means when it calls the view (`d56e6f1`, `f4bac00`).
package view

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/cache"
)

// ErrNoTerminal is what a view with nowhere to draw hits.
//
// It is not an error about the call: the call is fine, there is just no
// surface, and the two ways out are both in the message. It lives here
// because the command and host.View both answer with it, and a view that
// could not be drawn has to say the same thing whoever asked for it.
var ErrNoTerminal = errors.New("a view needs a terminal; run it in one, or with --gui")

// ErrNoGui is what --gui hits until the browser renderer exists (8b06191).
var ErrNoGui = errors.New("the gui renderer is not built yet (8b06191)")

// Call is one parsed view call: the kind, and its arguments with the
// defaults applied.
//
// It is what a renderer is handed, and the only thing it is handed besides the
// repository, so that everything a view can be told is checked in one place
// rather than in each renderer.
type Call struct {
	Kind string
	Args map[string]json.RawMessage
}

// Renderer draws a call and blocks until the user is done with it.
//
// The return value is the user's answer — nothing, today, for every kind —
// so that the deferred questions to the user (choose, confirm, ask) can
// return what the user answered.
type Renderer interface {
	Render(ctx context.Context, repo *cache.RepoCache, call *Call) (json.RawMessage, error)
}

// Parse checks a call against its kind's table and applies the defaults.
//
// Everything a renderer can be told is checked here, in the one place both
// surfaces reach, so that a bad view fails where it was written rather than in
// the renderer of whichever surface ran it first.
func Parse(kind string, kwargs map[string]json.RawMessage) (*Call, error) {
	args, ok := Kinds[kind]
	if !ok {
		return nil, fmt.Errorf("no view named %s, the views are %s", kind, strings.Join(KindNames(), ", "))
	}

	allowed := make(map[string]Arg, len(args))
	for _, arg := range args {
		allowed[arg.Name] = arg
	}
	for name := range kwargs {
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("view %s takes no argument %s, it takes %s",
				kind, name, argNames(args))
		}
	}

	out := make(map[string]json.RawMessage, len(args))
	for _, arg := range args {
		raw, given := kwargs[arg.Name]
		if given && isNull(raw) {
			// An explicit null is how a script says "use the default",
			// which is the absence of a value, not a value of null.
			given = false
		}

		if !given {
			switch arg.Tier {
			case Required:
				return nil, fmt.Errorf("view %s needs %s: %s", kind, arg.Name, arg.Doc)
			case Defaulted:
				if arg.hasDefault() {
					out[arg.Name] = json.RawMessage(arg.Default)
				}
			}
			continue
		}

		checked, err := arg.check(raw)
		if err != nil {
			return nil, fmt.Errorf("view %s: %s %w", kind, arg.Name, err)
		}
		out[arg.Name] = checked
	}

	return &Call{Kind: kind, Args: out}, nil
}

// Has reports whether an argument was given or defaulted into place.
func (c *Call) Has(name string) bool {
	_, ok := c.Args[name]
	return ok
}

// Raw returns an argument's JSON, nil when it is absent.
func (c *Call) Raw(name string) json.RawMessage {
	return c.Args[name]
}

// String returns a string argument, empty when it is absent.
func (c *Call) String(name string) string {
	raw, ok := c.Args[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// Strings returns a list argument, nil when it is absent.
func (c *Call) Strings(name string) []string {
	raw, ok := c.Args[name]
	if !ok {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	return list
}

// Int returns a whole-number argument, zero when it is absent.
func (c *Call) Int(name string) int {
	raw, ok := c.Args[name]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// check validates one value against its row of the table,
// and returns it compacted, so that equal arguments are equal bytes.
func (a Arg) check(raw json.RawMessage) (json.RawMessage, error) {
	switch a.Kind {
	case FieldKey, Id:
		s, err := asString(raw)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("is empty, it names %s", a.Doc)
		}

	case String, Query:
		if _, err := asString(raw); err != nil {
			return nil, err
		}

	case Enum:
		s, err := asString(raw)
		if err != nil {
			return nil, err
		}
		if !contains(a.Allowed, s) {
			return nil, fmt.Errorf("is %s, which is not one of %s", s, strings.Join(a.Allowed, ", "))
		}

	case FieldKeys, StringList:
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("is a list of strings, not %s", jsonKind(raw))
		}
		for _, item := range list {
			if a.Kind == FieldKeys && strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("has an empty field key")
			}
		}

	case Int:
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, fmt.Errorf("is a whole number, not %s", jsonKind(raw))
		}

	default:
		return nil, fmt.Errorf("has an unknown kind %s, which is a bug in the table", a.Kind)
	}

	return compact(raw), nil
}

func asString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("is a string, not %s", jsonKind(raw))
	}
	return s, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"
}

func compact(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// jsonKind names what a value is, for an error a reader can act on.
func jsonKind(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "something that is not JSON"
	}
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case string:
		return "a string"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	default:
		return "a number"
	}
}
