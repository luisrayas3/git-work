package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The styles, all of them, so that changing how the renderer looks is one
// file and not a hunt.
var (
	styleHeader = lipgloss.NewStyle().Bold(true)
	styleGroup  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleCursor = lipgloss.NewStyle().Bold(true)
	styleCell   = lipgloss.NewStyle().Reverse(true)
	styleStatus = lipgloss.NewStyle().Faint(true)
	styleGrab   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
)

// noGroup is what a row with no value for the grouping field is filed under.
const noGroup = "(none)"

// plain renders a stored field value the way a person reads it.
//
// The value is whatever JSON the operation carried, because the entity does
// not know the schema (entities/issue): a string is itself, an enum is its
// value id, which is what the store holds, a list is its items joined, and
// null is nothing at all rather than the word "null".
func plain(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return plainValue(value)
}

func plainValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, plainValue(item))
		}
		return strings.Join(items, ", ")
	case map[string]any:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	default:
		return fmt.Sprint(v)
	}
}

// truncate cuts a string to a width, marking that it was cut.
//
// Width is what the terminal shows and not what the string holds:
// a style is escape codes and a wide rune is two columns,
// so both are measured rather than counted.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
}

// pad fits a string to a width, truncating or padding with spaces.
func pad(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// fit cuts a whole rendered line to the window, so nothing ever wraps.
func fit(line string, width int) string {
	if width <= 0 {
		return line
	}
	return truncate(line, width)
}

// decodeValue reads a stored value as the Go value JSON decodes it into,
// which is what the widgets take: they are given a value, not its bytes.
func decodeValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	err := json.Unmarshal(raw, &value)
	return value, err
}

// decodeInto reads a piece of JSON into a shape, for the op log's summary.
func decodeInto(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, into)
}
