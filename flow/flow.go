// Package flow reads a flow's Starlark source without running it.
//
// A flow is exactly one function definition and nothing else
// (doc/design/config-entity.md E2):
// its name is the flow's key,
// its docstring is the description,
// and its parameters are the flow's arguments, defaults included.
// `git work flow import` reads all three from the syntax tree,
// so that listing flows never parses a script
// and importing one never executes a line of it.
//
// The runtime that runs a flow is a separate layer;
// it calls Parse for the same three facts and then evaluates the file.
package flow

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"go.starlark.net/syntax"
)

// MaxNameLength bounds a flow name, as the config key it becomes.
const MaxNameLength = 64

// namePattern is what a flow's name must match.
//
// A flow's name is its config key, and a config key of shape flow
// is the slug `^[a-z][a-z0-9_-]*$` (E3);
// a Starlark identifier cannot carry `-`,
// so a flow name is effectively `^[a-z][a-z0-9_]*$`.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Def is what a flow file declares: the whole of it.
//
// Nothing else about a flow is stored,
// because nothing else can be read from the file without running it.
type Def struct {
	// Name is the function's name, and the flow's key.
	Name string `json:"name"`
	// Description is the function's docstring, dedented and trimmed,
	// empty when it has none.
	Description string `json:"description"`
	// Params are the function's parameters, in the order they are declared.
	Params []Param `json:"params"`
}

// Param is one argument of a flow.
//
// Starlark has no annotation syntax,
// so a parameter's type is whatever its default says it is
// and a parameter without one is required (E2).
type Param struct {
	// Name is the parameter's name.
	Name string `json:"name"`
	// Default is the default value as JSON, nil when the parameter is required.
	Default json.RawMessage `json:"default,omitempty"`
	// HasDefault distinguishes no default from a default of JSON null,
	// which `None` is.
	HasDefault bool `json:"-"`
}

// Parse reads a flow file and returns what it declares.
//
// Nothing is executed and nothing is resolved:
// this is the syntax tree alone,
// which is what makes import cheap enough to run on every listing.
// Every refusal names the line it is about,
// because the caller is an agent or a human with a file open.
func Parse(src string) (*Def, error) {
	file, err := syntax.Parse("flow.star", src, 0)
	if err != nil {
		return nil, err
	}

	if len(file.Stmts) == 0 {
		return nil, fmt.Errorf("no function definition: a flow is exactly one def")
	}
	// The first statement is checked first, so that a `load` at the top of a
	// file with a def under it is reported where it is written.
	def, ok := file.Stmts[0].(*syntax.DefStmt)
	if !ok {
		start, _ := file.Stmts[0].Span()
		return nil, fmt.Errorf("line %d: a flow is exactly one def, this is %s",
			start.Line, statementKind(file.Stmts[0]))
	}

	if len(file.Stmts) > 1 {
		start, _ := file.Stmts[1].Span()
		return nil, fmt.Errorf("line %d: a flow is exactly one def, %s is a second top-level statement",
			start.Line, statementKind(file.Stmts[1]))
	}

	name := def.Name.Name
	if len(name) > MaxNameLength {
		return nil, fmt.Errorf("line %d: flow name %q is longer than %d bytes",
			def.Name.NamePos.Line, name, MaxNameLength)
	}
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("line %d: flow name %q must match %s",
			def.Name.NamePos.Line, name, namePattern.String())
	}

	params, err := parseParams(def)
	if err != nil {
		return nil, err
	}

	return &Def{
		Name:        name,
		Description: docstring(def),
		Params:      params,
	}, nil
}

func parseParams(def *syntax.DefStmt) ([]Param, error) {
	params := make([]Param, 0, len(def.Params))
	seen := make(map[string]struct{}, len(def.Params))

	for _, expr := range def.Params {
		start, _ := expr.Span()

		var param Param
		switch p := expr.(type) {
		case *syntax.Ident:
			param = Param{Name: p.Name}

		case *syntax.BinaryExpr:
			if p.Op != syntax.EQ {
				return nil, fmt.Errorf("line %d: unsupported parameter", start.Line)
			}
			ident, ok := p.X.(*syntax.Ident)
			if !ok {
				return nil, fmt.Errorf("line %d: unsupported parameter", start.Line)
			}
			value, err := literalJSON(p.Y)
			if err != nil {
				return nil, fmt.Errorf("line %d: parameter %s: %w", start.Line, ident.Name, err)
			}
			param = Param{Name: ident.Name, Default: value, HasDefault: true}

		case *syntax.UnaryExpr:
			// *args and **kwargs: the host API passes one object of keyword
			// arguments, so a flow whose arguments cannot be listed has no
			// caller (E2, cli-convention.md).
			return nil, fmt.Errorf("line %d: %s parameters are not allowed, a flow's arguments are named",
				start.Line, p.Op.String())

		default:
			return nil, fmt.Errorf("line %d: unsupported parameter", start.Line)
		}

		if _, ok := seen[param.Name]; ok {
			return nil, fmt.Errorf("line %d: duplicate parameter %s", start.Line, param.Name)
		}
		seen[param.Name] = struct{}{}

		params = append(params, param)
	}

	return params, nil
}

// literalJSON converts a default value to JSON, refusing anything computed.
//
// A default is read, never evaluated,
// so it has to be a literal: a string, a number, a bool, None,
// or a list or dict of those.
func literalJSON(expr syntax.Expr) (json.RawMessage, error) {
	value, err := literalValue(expr)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func literalValue(expr syntax.Expr) (any, error) {
	switch e := expr.(type) {
	case *syntax.Literal:
		switch e.Token {
		case syntax.STRING:
			return e.Value, nil
		case syntax.INT, syntax.FLOAT:
			return e.Value, nil
		default:
			return nil, fmt.Errorf("a default must be a literal, %s is not", e.Raw)
		}

	case *syntax.Ident:
		switch e.Name {
		case "True":
			return true, nil
		case "False":
			return false, nil
		case "None":
			return nil, nil
		default:
			return nil, fmt.Errorf("a default must be a literal, %s is a name", e.Name)
		}

	case *syntax.UnaryExpr:
		return signedNumber(e)

	case *syntax.ParenExpr:
		return literalValue(e.X)

	case *syntax.ListExpr:
		list := make([]any, 0, len(e.List))
		for _, item := range e.List {
			value, err := literalValue(item)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		return list, nil

	case *syntax.DictExpr:
		// A JSON object's keys are strings, so a dict's have to be too;
		// an integer-keyed dict would not survive the round trip.
		dict := make(map[string]any, len(e.List))
		for _, entry := range e.List {
			pair, ok := entry.(*syntax.DictEntry)
			if !ok {
				return nil, fmt.Errorf("a default dict entry must be key: value")
			}
			literal, ok := pair.Key.(*syntax.Literal)
			if !ok || literal.Token != syntax.STRING {
				return nil, fmt.Errorf("a default dict key must be a string literal")
			}
			key, _ := literal.Value.(string)
			if _, ok := dict[key]; ok {
				return nil, fmt.Errorf("a default dict repeats the key %q", key)
			}
			value, err := literalValue(pair.Value)
			if err != nil {
				return nil, err
			}
			dict[key] = value
		}
		return dict, nil

	default:
		return nil, fmt.Errorf("a default must be a literal, not an expression")
	}
}

// signedNumber reads `-1` and `+1`, which the parser gives as a unary operator
// over a literal rather than as a negative literal.
func signedNumber(e *syntax.UnaryExpr) (any, error) {
	literal, ok := e.X.(*syntax.Literal)
	if !ok || (literal.Token != syntax.INT && literal.Token != syntax.FLOAT) {
		return nil, fmt.Errorf("a default must be a literal, not an expression")
	}

	switch e.Op {
	case syntax.PLUS:
		return literal.Value, nil
	case syntax.MINUS:
		switch v := literal.Value.(type) {
		case int64:
			return -v, nil
		case *big.Int:
			return new(big.Int).Neg(v), nil
		case float64:
			return -v, nil
		}
	}
	return nil, fmt.Errorf("a default must be a literal, not an expression")
}

// docstring returns the function's docstring, dedented and trimmed.
//
// It is the first statement of the body when that statement is a string,
// which is Python's rule and Starlark's convention.
func docstring(def *syntax.DefStmt) string {
	if len(def.Body) == 0 {
		return ""
	}
	stmt, ok := def.Body[0].(*syntax.ExprStmt)
	if !ok {
		return ""
	}
	literal, ok := stmt.X.(*syntax.Literal)
	if !ok || literal.Token != syntax.STRING {
		return ""
	}
	text, ok := literal.Value.(string)
	if !ok {
		return ""
	}
	return dedent(text)
}

// dedent strips the common indentation of a multi-line docstring.
//
// The first line of a `"""…"""` carries no indentation of its own,
// since it follows the quotes,
// so the common prefix is computed over the rest.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		return strings.TrimSpace(s)
	}

	indent := -1
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || width < indent {
			indent = width
		}
	}

	if indent > 0 {
		for i, line := range lines[1:] {
			if len(line) >= indent {
				lines[i+1] = line[indent:]
			} else {
				lines[i+1] = strings.TrimLeft(line, " \t")
			}
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// statementKind names a statement for an error message.
func statementKind(stmt syntax.Stmt) string {
	switch s := stmt.(type) {
	case *syntax.AssignStmt:
		return "an assignment"
	case *syntax.BranchStmt:
		return "a " + s.Token.String()
	case *syntax.DefStmt:
		return "a def"
	case *syntax.ExprStmt:
		return "an expression"
	case *syntax.ForStmt:
		return "a for loop"
	case *syntax.WhileStmt:
		return "a while loop"
	case *syntax.IfStmt:
		return "an if"
	case *syntax.LoadStmt:
		return "a load"
	case *syntax.ReturnStmt:
		return "a return"
	default:
		return "not a def"
	}
}
