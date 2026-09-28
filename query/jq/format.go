package jq

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

// Format lays a jq program out to be read at a width.
//
// It is gofmt's idea and not a pretty-printer's: the program is parsed, and
// every node prints itself on one line where it fits, so what is broken is
// only what has to be, at the node's own seam — a pipe becomes one stage per
// line with the pipe leading, a comma one item per line, a call one argument
// per line, an object one key per line, and if/then/else, try/catch, reduce
// and foreach take the shape their keywords give them. Pipes at the top
// level break whether they fit or not, because a pipeline read as a
// pipeline is the point (Luis, 2026-09-28).
//
// The parser drops comments and the author's own spelling, so this is for
// reading a program, never for rewriting the text someone typed; and a
// program that does not parse is an error, which a half-typed one is.
func Format(src string, width int) (string, error) {
	parsed, err := gojq.Parse(src)
	if err != nil {
		return "", fmt.Errorf("jq: %w", err)
	}
	return strings.Join(query(parsed, max(width, 1), true), "\n"), nil
}

// The layout functions take avail, the columns left on the line where the
// node starts, and return lines indented relative to that column.

func fits(text string, avail int) bool {
	return utf8.RuneCountInString(text) <= avail
}

func indent(lines []string, by int) []string {
	pad := strings.Repeat(" ", by)
	out := make([]string, len(lines))
	for at, line := range lines {
		out[at] = pad + line
	}
	return out
}

// prefix puts first before the first line and aligns the rest under it.
func prefix(lines []string, first string) []string {
	out := make([]string, len(lines))
	pad := strings.Repeat(" ", utf8.RuneCountInString(first))
	for at, line := range lines {
		if at == 0 {
			out[at] = first + line
		} else {
			out[at] = pad + line
		}
	}
	return out
}

// lead is prefix with a two-column continuation, the shape of a pipe stage:
// the mark on the first line and the rest two in.
func lead(lines []string, first string) []string {
	out := make([]string, len(lines))
	for at, line := range lines {
		if at == 0 {
			out[at] = first + line
		} else {
			out[at] = "  " + line
		}
	}
	return out
}

func suffixLast(lines []string, text string) []string {
	if len(lines) == 0 {
		return []string{text}
	}
	lines[len(lines)-1] += text
	return lines
}

// query is a whole query: its module header, imports and definitions on
// lines of their own, then the body.
func query(q *gojq.Query, avail int, root bool) []string {
	var out []string
	if q.Meta != nil {
		out = append(out, "module "+q.Meta.String()+";")
	}
	for _, im := range q.Imports {
		out = append(out, strings.TrimSpace(im.String()))
	}
	for _, def := range q.FuncDefs {
		out = append(out, funcDef(def, avail)...)
	}

	body := *q
	body.Meta, body.Imports, body.FuncDefs = nil, nil, nil
	return append(out, expression(&body, avail, root)...)
}

// expression is a query without its definitions: a term, or two queries
// around an operator.
func expression(q *gojq.Query, avail int, root bool) []string {
	if q.Term != nil {
		return term(q.Term, avail)
	}
	one := q.String()
	switch q.Op {
	case gojq.OpPipe:
		if !root && fits(one, avail) {
			return []string{one}
		}
		return pipe(q, avail)
	case gojq.OpComma:
		if fits(one, avail) {
			return []string{one}
		}
		return comma(q, avail)
	}

	if fits(one, avail) {
		return []string{one}
	}
	// any other operator: the left side, then the operator leading the right
	op := q.Op.String() + " "
	out := expression(q.Left, avail, false)
	return append(out, prefix(expression(q.Right, avail-utf8.RuneCountInString(op), false), op)...)
}

// pipe is a chain of stages, one per line, the pipe leading every stage but
// the first; `as $x` stays with the stage that binds it.
func pipe(q *gojq.Query, avail int) []string {
	var out []string
	for first := true; ; first = false {
		stageAvail := avail - 2
		if first {
			stageAvail = avail
		}
		stage := expression(q.Left, stageAvail, false)
		if len(q.Patterns) > 0 {
			as := " as " + q.Patterns[0].String()
			for _, p := range q.Patterns[1:] {
				as += " ?// " + p.String()
			}
			stage = suffixLast(stage, as)
		}
		if first {
			out = append(out, stage...)
		} else {
			out = append(out, lead(stage, "| ")...)
		}

		next := q.Right
		if next.Term == nil && next.Op == gojq.OpPipe && next.Meta == nil && len(next.Imports) == 0 && len(next.FuncDefs) == 0 {
			q = next
			continue
		}
		return append(out, lead(query(next, avail-2, false), "| ")...)
	}
}

// comma is one item per line, the comma ending every line but the last.
func comma(q *gojq.Query, avail int) []string {
	var items []*gojq.Query
	for q.Term == nil && q.Op == gojq.OpComma && len(q.FuncDefs) == 0 {
		items = append([]*gojq.Query{q.Right}, items...)
		q = q.Left
	}
	items = append([]*gojq.Query{q}, items...)

	var out []string
	for at, item := range items {
		lines := query(item, avail, false)
		if at < len(items)-1 {
			lines = suffixLast(lines, ",")
		}
		out = append(out, lines...)
	}
	return out
}

func funcDef(def *gojq.FuncDef, avail int) []string {
	one := def.String()
	if fits(one, avail) {
		return []string{one}
	}
	head := "def " + def.Name
	if len(def.Args) > 0 {
		head += "(" + strings.Join(def.Args, "; ") + ")"
	}
	out := []string{head + ":"}
	out = append(out, indent(query(def.Body, avail-2, false), 2)...)
	return suffixLast(out, ";")
}

// term is one term with its suffixes, which stay on its last line.
func term(t *gojq.Term, avail int) []string {
	one := t.String()
	if fits(one, avail) {
		return []string{one}
	}

	var suffix strings.Builder
	for _, s := range t.SuffixList {
		suffix.WriteString(s.String())
	}
	bare := *t
	bare.SuffixList = nil
	return suffixLast(core(&bare, avail), suffix.String())
}

// core is a term without its suffixes, broken at its own seam.
func core(t *gojq.Term, avail int) []string {
	one := t.String()
	if fits(one, avail) {
		return []string{one}
	}
	switch t.Type {
	case gojq.TermTypeQuery:
		out := []string{"("}
		out = append(out, indent(query(t.Query, avail-2, false), 2)...)
		return append(out, ")")
	case gojq.TermTypeFunc:
		return call(t.Func, avail)
	case gojq.TermTypeObject:
		return object(t.Object, avail)
	case gojq.TermTypeArray:
		if t.Array.Query == nil {
			return []string{"[]"}
		}
		out := []string{"["}
		out = append(out, indent(query(t.Array.Query, avail-2, false), 2)...)
		return append(out, "]")
	case gojq.TermTypeIf:
		return ifThen(t.If, avail)
	case gojq.TermTypeTry:
		out := prefix(query(t.Try.Body, avail-4, false), "try ")
		if t.Try.Catch != nil {
			out = append(out, prefix(query(t.Try.Catch, avail-6, false), "catch ")...)
		}
		return out
	case gojq.TermTypeReduce:
		r := t.Reduce
		return loop("reduce "+r.Query.String()+" as "+r.Pattern.String()+" (", avail, r.Start, r.Update, nil)
	case gojq.TermTypeForeach:
		f := t.Foreach
		return loop("foreach "+f.Query.String()+" as "+f.Pattern.String()+" (", avail, f.Start, f.Update, f.Extract)
	case gojq.TermTypeLabel:
		out := []string{"label " + t.Label.Ident}
		return append(out, lead(query(t.Label.Body, avail-2, false), "| ")...)
	}
	// a string, a number, an index, a format: one line is all it has
	return []string{one}
}

// call is a function with one argument per line, `;` ending every line but
// the last.
func call(f *gojq.Func, avail int) []string {
	if len(f.Args) == 0 {
		return []string{f.Name}
	}
	out := []string{f.Name + "("}
	for at, arg := range f.Args {
		lines := query(arg, avail-2, false)
		if at < len(f.Args)-1 {
			lines = suffixLast(lines, ";")
		}
		out = append(out, indent(lines, 2)...)
	}
	return append(out, ")")
}

// object is one key per line, the comma ending every line but the last.
func object(o *gojq.Object, avail int) []string {
	out := []string{"{"}
	for at, kv := range o.KeyVals {
		key := kv.Key
		switch {
		case kv.KeyString != nil:
			key = kv.KeyString.String()
		case kv.KeyQuery != nil:
			key = "(" + kv.KeyQuery.String() + ")"
		}
		lines := []string{key}
		if kv.Val != nil {
			key += ": "
			lines = prefix(query(kv.Val, avail-2-utf8.RuneCountInString(key), false), key)
		}
		if at < len(o.KeyVals)-1 {
			lines = suffixLast(lines, ",")
		}
		out = append(out, indent(lines, 2)...)
	}
	return append(out, "}")
}

// ifThen is the shape the keywords give it: each condition ends its line
// with then, each branch is indented under its keyword, and end closes.
func ifThen(i *gojq.If, avail int) []string {
	out := suffixLast(prefix(query(i.Cond, avail-3, false), "if "), " then")
	out = append(out, indent(query(i.Then, avail-2, false), 2)...)
	for _, elif := range i.Elif {
		out = append(out, suffixLast(prefix(query(elif.Cond, avail-5, false), "elif "), " then")...)
		out = append(out, indent(query(elif.Then, avail-2, false), 2)...)
	}
	if i.Else != nil {
		out = append(out, "else")
		out = append(out, indent(query(i.Else, avail-2, false), 2)...)
	}
	return append(out, "end")
}

// loop is reduce and foreach: the header on its line, then the clauses
// indented, `;` ending every one but the last.
func loop(head string, avail int, clauses ...*gojq.Query) []string {
	out := []string{head}
	var present []*gojq.Query
	for _, c := range clauses {
		if c != nil {
			present = append(present, c)
		}
	}
	for at, c := range present {
		lines := query(c, avail-2, false)
		if at < len(present)-1 {
			lines = suffixLast(lines, ";")
		}
		out = append(out, indent(lines, 2)...)
	}
	return append(out, ")")
}
