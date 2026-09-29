package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The glyphs of the chart: a bar's done part and its rest, the envelope a
// parent draws over its children when it has no dates of its own, and the
// dull band of a row with no dates at all. Shades rather than a solid
// block, so that the reversed cell under the cursor still reads as part of
// the bar.
const (
	glyphDone     = "▓"
	glyphRest     = "░"
	glyphEnvelope = "═"
	glyphDateless = "░"
	glyphToday    = "▼"
)

// milestoneTrail is a milestone, an issue with one date and not the other:
// it starts on the date's cell and fades away from it, toward the side its
// missing date would be: left of a stop, right of a start.
var milestoneTrail = []string{"▓", "▓", "▒", "▒", "░", "░"}

// groupColors are the bars' colors, one per group in the order the groups
// first appear, cycled; yellow is left out, being the grab's.
var groupColors = []string{"4", "2", "5", "6", "1", "12", "10", "13", "14", "9"}

func (p *ganttPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	labelWidth, visible := p.layout()
	header := p.headerLines(labelWidth, visible)
	body, cursorLine := p.body(labelWidth, visible)

	// one line for the call, the header, the rest for the chart, the bottom
	// for whatever is open and the status line
	room := max(p.height-1-len(header)-len(bottom), 1)
	scroll(&p.top, cursorLine, room, len(body))

	lines := make([]string, 0, p.height)
	lines = append(lines, callLine(p.call, "", "", p.width))
	lines = append(lines, header...)
	for at := p.top; at < min(p.top+room, len(body)); at++ {
		lines = append(lines, body[at])
	}
	for len(lines) < p.height-len(bottom) {
		lines = append(lines, "")
	}

	return strings.Join(append(lines, bottom...), "\n")
}

// indent is the room the tree takes before the id, nothing when `expand`
// is not bound.
func (p *ganttPage) indent() int {
	return indentOf(p.nodes, p.order, p.expandKey != "")
}

// layout sizes the label column to the labels, up to two fifths of the
// window, gives the chart the rest, and says how many periods fit; the
// chart scrolls sideways by whole periods so that the cursor's is on
// screen, colOffset moving as the board's does.
func (p *ganttPage) layout() (labelWidth, visible int) {
	longest := 0
	for _, index := range p.order {
		longest = max(longest, ansi.StringWidth(p.bars[index].label))
	}
	// the marker, the tree's indent, the id, a space, the label
	labelWidth = 1 + p.indent() + idWidth + 1 + longest
	labelWidth = min(labelWidth, max(p.width*2/5, 1+p.indent()+idWidth+1+4))

	w := periodWidth(p.scale)
	chart := max(p.width-labelWidth-1, w)
	visible = max(chart/w, 1)
	if p.col < p.colOffset {
		p.colOffset = p.col
	}
	if p.col >= p.colOffset+visible {
		p.colOffset = p.col - visible + 1
	}
	p.colOffset = min(max(p.colOffset, 0), max(len(p.periods)-visible, 0))
	return labelWidth, min(visible, len(p.periods))
}

// headerLines are the chart's header: the coarse labels, the month over
// days and weeks and the year over months and quarters, said where they
// change; the periods' own labels, with the id column's header; and the
// rule, today's period marked on it. ‹ and › say there are periods off
// screen.
func (p *ganttPage) headerLines(labelWidth, visible int) []string {
	w := periodWidth(p.scale)
	chart := p.width - labelWidth - 1

	fine := make([]string, 0, visible)
	rule := []rune(strings.Repeat("─", chart))
	now := p.index(today())

	// the coarse labels: where the label changes, and over the first period
	type mark struct {
		at           int
		whole, short string
	}
	var marks []mark
	last := ""
	for i := p.colOffset; i < p.colOffset+visible; i++ {
		label, over, short := periodLabel(p.periods[i], p.scale)
		if i == p.colOffset || over != last {
			marks = append(marks, mark{at: (i - p.colOffset) * w, whole: over, short: short})
		}
		last = over
		fine = append(fine, fmt.Sprintf("%*s ", w-1, label))
		if i == now {
			rule[(i-p.colOffset)*w] = []rune(glyphToday)[0]
		}
	}
	// the first period's label gives way to a change right after it, which
	// is the one worth reading; the first one written is whole, and a
	// change after it short unless the year changed too
	if len(marks) > 1 && marks[1].at <= len([]rune(marks[0].whole)) {
		marks = marks[1:]
	}
	coarse := make([]rune, 0, chart)
	for n, m := range marks {
		text := m.whole
		if n > 0 && m.whole[len(m.whole)-4:] == marks[n-1].whole[len(marks[n-1].whole)-4:] {
			text = m.short
		}
		if m.at < len(coarse)+1 && n > 0 {
			continue
		}
		for len(coarse) < m.at {
			coarse = append(coarse, ' ')
		}
		coarse = append(coarse, []rune(text)...)
	}

	sep := "│"
	if p.colOffset > 0 {
		sep = "‹"
	}
	more := ""
	if p.colOffset+visible < len(p.periods) {
		more = "›"
	}

	first := pad("", labelWidth) + styleDim.Render(sep) + styleDim.Render(fit(string(coarse), chart))
	idHeader := styleHeader.Render(pad(strings.Repeat(" ", 1+p.indent())+"id", labelWidth))
	labels := make([]string, 0, len(fine)+1)
	for n, label := range fine {
		style := styleHeader
		if p.colOffset+n == p.col {
			style = style.Background(styleRow().GetBackground())
		}
		labels = append(labels, style.Render(label))
	}
	labels = append(labels, strings.Repeat(" ", max(chart-len(more)-len(fine)*w, 0)))
	second := idHeader + styleDim.Render(sep) + strings.Join(labels, "") + more
	third := styleDim.Render(strings.Repeat("─", labelWidth) + "┼" + string(rule))
	return []string{fit(first, p.width), fit(second, p.width), fit(third, p.width)}
}

// body draws every row, and says which line the cursor's row is on.
func (p *ganttPage) body(labelWidth, visible int) (lines []string, cursorLine int) {
	colors := p.groupColors()
	w := periodWidth(p.scale)
	cross := labelWidth + 1 + (p.col-p.colOffset)*w
	group := ""
	for at, index := range p.order {
		node := &p.nodes[index]
		// a group is the root's: its children follow it into the group
		if p.groupBy != "" && node.level == 0 && node.group != group {
			group = node.group
			header := styleGroup
			if tint, ok := colors[group]; ok {
				header = header.Foreground(tint)
			}
			// the crosshair runs through the header too, unbroken
			text := pad(group, max(ansi.StringWidth(group), cross+w))
			line := header.Render(ansi.Cut(text, 0, cross)) +
				header.Background(styleRow().GetBackground()).Render(ansi.Cut(text, cross, cross+w))
			if rest := ansi.Cut(text, cross+w, p.width); rest != "" {
				line += header.Render(rest)
			}
			lines = append(lines, fit(line, p.width))
		}
		if at == p.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, p.rowLine(index, labelWidth, visible, at == p.cursor, index == p.grabbed, colors[group]))
	}
	return lines, cursorLine
}

// groupColors gives each group but the ungrouped a color, in the order the
// groups' roots are stored rather than drawn, so a filter keeps them.
func (p *ganttPage) groupColors() map[string]color.Color {
	colors := map[string]color.Color{}
	if p.groupBy == "" {
		return colors
	}
	for _, node := range p.nodes {
		if _, seen := colors[node.group]; seen || node.level != 0 || node.group == noGroup {
			continue
		}
		colors[node.group] = lipgloss.Color(groupColors[len(colors)%len(groupColors)])
	}
	return colors
}

// rowLine draws one row: the marker, the tree's indent, the short id and
// the label, then the chart, a cell per period.
//
// The row under the cursor has the light wash over its width and the cell
// under the cursor reversed, as a list's row and cell are, and the cursor's
// period has the wash down every row, a crosshair; the grabbed bar
// is drawn in the grab colour with the blinking marker. Every piece is
// styled on its own, because a style ends in a reset and a reset inside
// the line would end the wash. A bar is drawn in its group's tint, when it
// has one.
func (p *ganttPage) rowLine(index, labelWidth, visible int, under, grabbed bool, tint color.Color) string {
	b, node := &p.bars[index], &p.nodes[index]
	w := periodWidth(p.scale)
	indent := p.indent()

	wash := lipgloss.NewStyle()
	if under {
		wash = styleRow()
	}

	marker := " "
	switch {
	case grabbed && p.blink:
		marker = styleGrab.Render("[")
	case grabbed:
		marker = styleGrab.Render("⟨")
	case under:
		marker = wash.Render("›")
	}

	parts := []string{marker}
	prefix := ""
	if indent > 0 {
		prefix = nestPrefix(*node)
		parts = append(parts, wash.Render(prefix))
	}
	parts = append(parts, wash.Faint(true).Render(pad(b.human, indent+idWidth-len([]rune(prefix)))))
	room := max(labelWidth-1-indent-idWidth-1, 0)
	parts = append(parts, wash.Render(" "+pad(b.label, room)))
	parts = append(parts, wash.Faint(true).Render("│"))

	first, last, own, ok := p.span(index)
	milestone := own && b.hasStart != b.hasStop
	cells := (last - first + 1) * w
	done := cells
	if b.hasProgress {
		done = int(min(max(b.progress, 0), 1)*float64(cells) + 0.5)
	}
	barStyle := wash
	switch {
	case grabbed:
		barStyle = wash.Foreground(lipgloss.Color("3")).Bold(true)
	case tint != nil:
		barStyle = wash.Foreground(tint)
	}
	// the band of a row with no dates
	bandStyle := barStyle.Faint(true)
	if tint != nil && !grabbed {
		bandStyle = barStyle.Foreground(dull(tint))
	}
	for i := p.colOffset; i < p.colOffset+visible; i++ {
		var text strings.Builder
		style := barStyle
		switch {
		case !ok:
			text.WriteString(strings.Repeat(glyphDateless, w))
			style = bandStyle
		case milestone:
			for k := 0; k < w; k++ {
				text.WriteString(milestoneGlyph((i-first)*w+k, w, b.hasStart))
			}
		case i < first || i > last:
			text.WriteString(strings.Repeat(" ", w))
		case !own:
			text.WriteString(strings.Repeat(glyphEnvelope, w))
		default:
			for k := 0; k < w; k++ {
				if (i-first)*w+k < done {
					text.WriteString(glyphDone)
				} else {
					text.WriteString(glyphRest)
				}
			}
		}
		switch {
		case under && i == p.col:
			style = styleCell
		case i == p.col:
			style = style.Background(styleRow().GetBackground())
		}
		parts = append(parts, style.Render(text.String()))
	}

	used := labelWidth + 1 + visible*w
	if under && p.width > used {
		parts = append(parts, wash.Render(strings.Repeat(" ", p.width-used)))
	}
	return fit(strings.Join(parts, ""), p.width)
}

// milestoneGlyph is the character at pos cells from the start of a
// milestone's period: the trail starts on the period's first cell for a
// start, its last for a stop, and fades away from it into the neighbors.
func milestoneGlyph(pos, w int, start bool) string {
	distance := pos
	if !start {
		distance = w - 1 - pos
	}
	if distance >= 0 && distance < len(milestoneTrail) {
		return milestoneTrail[distance]
	}
	return " "
}

// dull is a group's tint toward the background, for the band of a row
// with no dates: short of the terminal's faint, which washes the hue out,
// so the group still tells.
func dull(tint color.Color) color.Color {
	if darkBackground {
		return lipgloss.Darken(tint, 0.35)
	}
	return lipgloss.Lighten(tint, 0.35)
}

// bottom is the filter when one is being typed, and the status line, which
// is always the last line of the page.
func (p *ganttPage) bottom() []string {
	var lines []string
	if p.filtering != nil {
		lines = []string{fit("/"+p.filtering.View(), p.width)}
	}
	return append(lines, p.statusLine())
}

func (p *ganttPage) statusLine() string {
	n := p.count()
	count := fmt.Sprintf("%d issues", n)
	if n == 1 {
		count = "1 issue"
	}
	if p.filter != "" {
		count = fmt.Sprintf("%d of %d issues · /%s", n, len(p.bars), p.filter)
	}

	left := p.status
	if left == "" {
		left = "? keys"
	}
	return styleStatus.Render(fit(left+" · "+count, p.width))
}
