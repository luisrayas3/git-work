package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/git-bug/git-bug/view"
)

// callLine is the first line of every view: the call it is drawing.
//
// A view is exactly its call (the command is the spec), so the line that
// says what is on the screen is that call, spelled the way it was made: the
// kind, then each argument. An argument left at its default is dim and comes
// after the ones somebody chose, and the query is always there, default or
// not, because it is the answer to "why these issues?".
//
// lead is what follows the kind before the arguments, and replaces the one
// argument it names: `show abc1234` rather than `show id=abc1234`.
func callLine(call *view.Call, lead, leadArg string, width int) string {
	parts := []string{styleHeader.Render(call.Kind)}
	if lead != "" {
		parts = append(parts, styleHeader.Render(lead))
	}

	// what somebody chose first, then the defaults, so that a long default
	// query is what the window cuts and not the argument that was typed
	var defaults []string
	for _, arg := range view.Kinds[call.Kind] {
		if arg.Name == leadArg {
			continue
		}
		raw, ok := call.Args[arg.Name]
		if !ok {
			continue
		}
		part := arg.Name + "=" + argText(raw)
		if arg.Default != "" && bytes.Equal(compactJSON(raw), compactJSON(json.RawMessage(arg.Default))) {
			defaults = append(defaults, styleDim.Render(part))
			continue
		}
		parts = append(parts, part)
	}

	return fit(strings.Join(append(parts, defaults...), "  "), width)
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
