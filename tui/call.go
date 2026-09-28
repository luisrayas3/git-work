package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/view"
)

// callLine is the first line of every view: the call it is drawing.
//
// A view is exactly its call (the command is the spec), so the line that
// says what is on the screen is that call: the kind, then its arguments.
// Some are left out because the screen already says them: `fields` is the
// columns and the table, `card` is the cards, and the query goes last without
// its name, being the one argument that is always there and the one that runs long. An
// argument left at its default is dim, so the ones somebody chose stand out.
//
// lead is what follows the kind before the arguments, and replaces the one
// argument it names: `show abc1234` rather than `show id=abc1234`.
func callLine(call *view.Call, lead, leadArg string, width int) string {
	head, query, dim := callParts(call, lead, leadArg)
	if query != "" {
		if dim {
			query = styleDim.Render(query)
		}
		head = append(head, query)
	}
	return fit(strings.Join(head, "  "), width)
}

// callParts is the call line in pieces: the kind, the lead and every named
// argument, rendered; the query as plain text on one line; and whether the
// query is the default.
func callParts(call *view.Call, lead, leadArg string) (head []string, query string, dim bool) {
	head = []string{styleHeader.Render(call.Kind)}
	if lead != "" {
		head = append(head, styleHeader.Render(lead))
	}

	for _, arg := range view.Kinds[call.Kind] {
		if arg.Name == leadArg || arg.Name == "fields" || arg.Name == "card" {
			continue
		}
		raw, ok := call.Args[arg.Name]
		if !ok {
			continue
		}
		defaulted := arg.Default != "" && bytes.Equal(compactJSON(raw), compactJSON(json.RawMessage(arg.Default)))
		if arg.Name == "query" {
			query, dim = argText(raw), defaulted
			continue
		}
		part := arg.Name + "=" + argText(raw)
		if defaulted {
			part = styleDim.Render(part)
		}
		head = append(head, part)
	}
	return head, query, dim
}

// callLines is the call line unfolded, for when the cursor is on it: the
// same first line, less the query, marked and reversed as the cell under
// the cursor is, and under it the query formatted to the window by
// jq.Format — a pipeline one stage per line, and anything else broken only
// where it does not fit — because a jq program folded onto one line is a
// readability nightmare (Luis, 2026-09-28). A program that does not parse
// is cut at its top-level pipes instead, which is the best a scanner can do.
func callLines(call *view.Call, lead, leadArg string, width int) []string {
	head, query, dim := callParts(call, lead, leadArg)
	inner := max(width-2, 10)
	lines := []string{"› " + styleCell.Render(pad(strings.Join(head, "  "), inner))}

	style := styleDim.Faint(false)
	if dim {
		style = styleDim
	}
	var body []string
	if formatted, err := jq.Format(query, inner-2); err == nil {
		body = strings.Split(formatted, "\n")
	} else {
		body = pipeline(query)
	}
	for _, line := range body {
		// a line that still does not fit — one string, one long name — wraps
		for at, wrapped := range strings.Split(ansi.Hardwrap(line, inner-2, false), "\n") {
			indent := "    "
			if at == 0 {
				indent = "  "
			}
			lines = append(lines, fit(indent+style.Render(wrapped), width))
		}
	}
	return lines
}

// pipeline is a jq program cut at its top-level pipes and its own line
// breaks, whitespace within a segment collapsed, every segment after the
// first starting with the pipe it follows: the fallback for a program the
// parser refuses. Pipes inside parentheses, brackets, braces or a string
// are the program's own and are left alone, as is `|=`, which is an
// assignment and not a pipe.
func pipeline(program string) []string {
	var segments []string
	var current strings.Builder
	flush := func() {
		if text := strings.Join(strings.Fields(current.String()), " "); text != "" {
			segments = append(segments, text)
		}
		current.Reset()
	}

	depth := 0
	inString := false
	runes := []rune(program)
	for at := 0; at < len(runes); at++ {
		r := runes[at]
		switch {
		case inString:
			current.WriteRune(r)
			if r == '\\' && at+1 < len(runes) {
				at++
				current.WriteRune(runes[at])
			} else if r == '"' {
				inString = false
			}
		case r == '"':
			inString = true
			current.WriteRune(r)
		case r == '(' || r == '[' || r == '{':
			depth++
			current.WriteRune(r)
		case r == ')' || r == ']' || r == '}':
			depth--
			current.WriteRune(r)
		case r == '\n' && depth == 0:
			flush()
		case r == '|' && depth == 0 && !(at+1 < len(runes) && runes[at+1] == '='):
			flush()
			current.WriteString("| ")
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return segments
}

// command is the whole call as the shell command that draws it, which is
// what the call line unfolds into when the cursor is on it and what copy
// copies there: the command is the spec, so this is the spec, verbatim.
func command(call *view.Call) string {
	args, err := json.Marshal(call.Args)
	if err != nil || len(call.Args) == 0 {
		args = []byte("{}")
	}
	return "git work view " + call.Kind + " " + shellQuote(string(args))
}

// shellQuote is a string as one shell word, in single quotes, which is how
// every recipe in AGENTS.md writes a document argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// argText is an argument as it reads: a string is itself, on one line, and
// anything else is its JSON.
func argText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.Join(strings.Fields(text), " ")
	}
	return string(compactJSON(raw))
}

func compactJSON(raw json.RawMessage) []byte {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return raw
	}
	return out.Bytes()
}
