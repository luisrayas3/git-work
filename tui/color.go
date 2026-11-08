package tui

import (
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/git-bug/git-bug/schema"
)

// ansiColors is each value of the built-in `color` as the terminal's own
// palette index, SGR 31–36 and 91–96, never a 256-color or a truecolor one,
// so that a color follows the user's theme
// (doc/design/schema.md, "Color is a built-in a type switches on").
var ansiColors = map[string]string{
	"red":            "1",
	"green":          "2",
	"yellow":         "3",
	"blue":           "4",
	"magenta":        "5",
	"cyan":           "6",
	"bright-red":     "9",
	"bright-green":   "10",
	"bright-yellow":  "11",
	"bright-blue":    "12",
	"bright-magenta": "13",
	"bright-cyan":    "14",
}

// brightPair is a palette color's other half, normal for bright and bright
// for normal, ANSI 1–6 against 9–14; any other color is its own pair.
func brightPair(c color.Color) color.Color {
	for n := 1; n <= 6; n++ {
		normal, bright := lipgloss.Color(strconv.Itoa(n)), lipgloss.Color(strconv.Itoa(n+8))
		switch c {
		case normal:
			return bright
		case bright:
			return normal
		}
	}
	return c
}

// issueColor is the terminal color a `color` value draws in, and false for
// no value and for anything that is not one of schema.Colors, which a read
// takes as it is (doc/design/schema.md, "Validate on write, lenient on
// read") and draws plain.
func issueColor(value any) (color.Color, bool) {
	id, _ := value.(string)
	index, ok := ansiColors[id]
	if !ok || !schema.IsColor(id) {
		return nil, false
	}
	return lipgloss.Color(index), true
}
