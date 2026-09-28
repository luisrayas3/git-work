package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/view"
)

// callLine is the first line of every view: the call it is drawing.
//
// A view is exactly its call (the command is the spec), so the line that
// says what is on the screen is that call: the kind, then its arguments.
// Two are left out because the screen already says them: `fields` is the
// columns and the table, and the query goes last without its name, being
// the one argument that is always there and the one that runs long. An
// argument left at its default is dim, so the ones somebody chose stand out.
//
// lead is what follows the kind before the arguments, and replaces the one
// argument it names: `show abc1234` rather than `show id=abc1234`.
func callLine(call *view.Call, lead, leadArg string, width int) string {
	parts := []string{styleHeader.Render(call.Kind)}
	if lead != "" {
		parts = append(parts, styleHeader.Render(lead))
	}

	var query string
	for _, arg := range view.Kinds[call.Kind] {
		if arg.Name == leadArg || arg.Name == "fields" {
			continue
		}
		raw, ok := call.Args[arg.Name]
		if !ok {
			continue
		}
		defaulted := arg.Default != "" && bytes.Equal(compactJSON(raw), compactJSON(json.RawMessage(arg.Default)))

		part := arg.Name + "=" + argText(raw)
		if arg.Name == "query" {
			part = argText(raw)
		}
		if defaulted {
			part = styleDim.Render(part)
		}
		if arg.Name == "query" {
			query = part
			continue
		}
		parts = append(parts, part)
	}
	if query != "" {
		parts = append(parts, query)
	}

	return fit(strings.Join(parts, "  "), width)
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

// commandLines is the command wrapped to the window, the cursor's mark on
// the first line, every line reversed as the cell under the cursor is.
func commandLines(call *view.Call, width int) []string {
	inner := max(width-2, 10)
	var lines []string
	for at, line := range strings.Split(ansi.Hardwrap(command(call), inner, false), "\n") {
		marker := "  "
		if at == 0 {
			marker = "› "
		}
		lines = append(lines, marker+styleCell.Render(pad(line, inner)))
	}
	return lines
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
