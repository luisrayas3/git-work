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

// darkBackground is whether the terminal is dark, as it answered when asked;
// dark until it answers, because most are.
var darkBackground = true

// styleRow is the light wash over the row under the cursor: a shade off the
// background, whichever way the background goes.
func styleRow() lipgloss.Style {
	if darkBackground {
		return lipgloss.NewStyle().Background(lipgloss.Color("236"))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("254"))
}

// styleMark is the cell under the cursor where the cell is a picture and
// not text, a gantt period: a shade of the background a step past the
// wash, under the terminal's own foreground. Reversed, as a list's text
// cell is, a shade glyph or an empty period becomes a block of the
// terminal's foreground, which on a light terminal is black and hides what
// it covers; a shade keeps the glyph as it is, in the one foreground that
// reads on it whichever way the background goes.
func styleMark() lipgloss.Style {
	if darkBackground {
		return lipgloss.NewStyle().Background(lipgloss.Color("240"))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("250"))
}

// styleTitle is the issue's title on show: as large as a terminal allows,
// which is bold, in the terminal's own foreground, over a rule.
var styleTitle = lipgloss.NewStyle().Bold(true)

// styleArchived is the header's archived cell, when the issue is: the one
// word on the page that says what is drawn is out of every default list.
var styleArchived = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)

// styleLink is a value that names another issue: enter follows it.
var styleLink = lipgloss.NewStyle().Underline(true)

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

// prose wraps a body of text to the window, indented one column, each of
// its own lines wrapped on its own so that paragraphs stay paragraphs: a
// description or a comment is read, not scanned, and a cut line is a
// sentence lost. A word longer than the window is broken rather than cut.
func prose(text string, width int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		for _, piece := range strings.Split(ansi.Wrap(line, max(width-1, 1), ""), "\n") {
			lines = append(lines, fit(" "+piece, width))
		}
	}
	return lines
}

// fit cuts a whole rendered line to the window, so nothing ever wraps.
func fit(line string, width int) string {
	if width <= 0 {
		return line
	}
	return truncate(line, width)
}

// fitRight cuts a line to a width from its left, keeping its end.
//
// What is right-aligned is read from the right edge inward, so the end is
// what has to survive: the count on the bottom line, and the last words of
// a refusal that ran long.
func fitRight(line string, width int) string {
	if width <= 0 {
		return ""
	}
	over := ansi.StringWidth(line) - width
	if over <= 0 {
		return line
	}
	if width == 1 {
		return "…"
	}
	return ansi.TruncateLeft(line, over+1, "…")
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
