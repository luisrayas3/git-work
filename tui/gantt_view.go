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
// missing date would be: left of a stop, right of a start. Past the trail
// that side is the dateless band, from today on.
var milestoneTrail = []string{"▓", "▓", "▒", "▒", "░", "░"}

// overdueTint is the band between a stop that has gone by and today, which
// is the one thing on a chart worth a color of its own: red, outranking the
// group's tint, because a date already missed is what the eye is looking
// for and the group is readable from the row's neighbours (2026-10-02).
var overdueTint = lipgloss.Color("1")

// cellStyle says which of a row's four ways to draw one cell is meant.
type cellStyle int

const (
	// cellBlank is the chart showing through: before a row can start, after
	// its stop, outside what it covers.
	cellBlank cellStyle = iota
	// cellBar is the bar itself, or a milestone's trail.
	cellBar
	// cellBand is the dull band of what is not planned: a row with no
	// dates, or a milestone's open side.
	cellBand
	// cellOverdue is the band from a stop already past to today.
	cellOverdue
)

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
	body, groups, cursorLine := p.body(labelWidth, visible)

	// one line for the call, the header, the rest for the chart, the bottom
	// for whatever is open and the status line
	room := max(p.height-1-len(header)-len(bottom), 1)

	lines := make([]string, 0, p.height)
	lines = append(lines, callLine(p.call, "", "", p.width))
	lines = append(lines, header...)
	lines = append(lines, window(&p.top, body, groups, cursorLine, room)...)
	for len(lines) < p.height-len(bottom) {
		lines = append(lines, "")
	}

	return strings.Join(append(lines, bottom...), "\n")
}

// treeRoom is the width of the tree cell after the id — the deepest level's
// indent, then the arrow and its count — nothing when `expand` is not bound.
// The gantt keeps the indent in its label column, because its rows share one
// time axis and cannot be tables of their own; it is measured over every
// row, hidden or drawn, so that folding moves nothing (nest.go).
func (p *ganttPage) treeRoom() int {
	if !p.nest.expanded() {
		return 0
	}
	return indentWidth*deepest(p.nodes) + treeWidth(p.nodes, true)
}

// layout sizes the label column to the labels, up to two fifths of the
// window, gives the chart the rest, and says how many periods fit; the
// chart scrolls sideways by whole periods so that the cursor's is on
// screen, colOffset moving as the board's does.
func (p *ganttPage) layout() (labelWidth, visible int) {
	labelWidth, visible = p.labelWidth(), p.capacity()
	if p.col < 0 {
		// the arrow cell is not a period: the chart stays where it was
		p.colOffset = min(max(p.colOffset, 0), max(len(p.periods)-visible, 0))
		return labelWidth, min(visible, len(p.periods))
	}
	if p.col < p.colOffset {
		p.colOffset = p.col
	}
	if p.col >= p.colOffset+visible {
		p.colOffset = p.col - visible + 1
	}
	p.colOffset = min(max(p.colOffset, 0), max(len(p.periods)-visible, 0))
	return labelWidth, min(visible, len(p.periods))
}

// labelWidth is the label column's: the marker, the tree's indent, the
// id, a space and the label, up to two fifths of the window.
func (p *ganttPage) labelWidth() int {
	longest := 0
	for _, index := range p.order {
		longest = max(longest, ansi.StringWidth(p.bars[index].label))
	}
	labelWidth := 1 + p.treeRoom() + idWidth + 1 + longest
	return min(labelWidth, max(p.width*2/5, 1+p.treeRoom()+idWidth+1+4))
}

// capacity is how many periods the window has room for: the chart less one
// cell, which is the › that says there are periods off screen, so that a
// chart whose periods divide its width exactly still has somewhere to say so.
func (p *ganttPage) capacity() int {
	w := periodWidth(p.scale)
	return max(max(p.width-p.labelWidth()-2, w)/w, 1)
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
	idHeader := styleHeader.Render(pad(" id", labelWidth))
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

// body draws every row, says which line the cursor's row is on, and which
// group header each line sits under so that header can be kept on screen.
func (p *ganttPage) body(labelWidth, visible int) (lines []string, groups []int, cursorLine int) {
	colors := p.groupColors()
	w := periodWidth(p.scale)
	cross := labelWidth + 1 + (p.col-p.colOffset)*w
	if p.col < 0 {
		// the cursor is on the arrow cell, which is in the label column:
		// nothing in the chart is crossed
		cross = p.width
	}
	group := ""
	headerAt := -1
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
			text := pad(group, max(ansi.StringWidth(group), min(cross+w, p.width)))
			line := header.Render(ansi.Cut(text, 0, cross)) +
				header.Background(styleRow().GetBackground()).Render(ansi.Cut(text, cross, cross+w))
			if rest := ansi.Cut(text, cross+w, p.width); rest != "" {
				line += header.Render(rest)
			}
			headerAt = len(lines)
			lines = append(lines, fit(line, p.width))
			groups = append(groups, headerAt)
		}
		if at == p.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, p.rowLine(index, labelWidth, visible, at == p.cursor, index == p.grabbed, colors[group]))
		groups = append(groups, headerAt)
	}
	return lines, groups, cursorLine
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
// The row under the cursor has the light wash over its width, as a list's
// row has, the cell under the cursor a stronger shade (styleMark), and the cursor's
// period has the wash down every row, a crosshair; the grabbed bar
// is drawn in the grab colour with the blinking marker. Every piece is
// styled on its own, because a style ends in a reset and a reset inside
// the line would end the wash. A bar is drawn in its group's tint, when it
// has one.
func (p *ganttPage) rowLine(index, labelWidth, visible int, under, grabbed bool, tint color.Color) string {
	b, node := &p.bars[index], &p.nodes[index]
	w := periodWidth(p.scale)
	indent := p.treeRoom()

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

	if node.ghost {
		// the ghost is a place to add, drawn dim, with no bar
		parts := []string{marker, wash.Faint(true).Render(pad(b.human, idWidth))}
		if indent > 0 {
			parts = append(parts, wash.Render(" "), wash.Render(pad("", indent)))
		}
		room := max(labelWidth-1-indent-idWidth-1, 0)
		parts = append(parts, wash.Faint(true).Render(" "+pad(b.label, room)), wash.Faint(true).Render("│"))
		return fit(strings.Join(parts, ""), p.width)
	}

	parts := []string{marker}
	parts = append(parts, wash.Faint(true).Render(pad(b.human, idWidth)))
	if indent > 0 {
		cell := pad(strings.Repeat(" ", indentWidth*node.level)+treeCell(*node), indent)
		style := wash
		if under && p.col < 0 {
			style = styleMark()
		}
		parts = append(parts, wash.Render(" "), style.Render(cell))
	}
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
	// the band of a row with no dates, and of a milestone's open side
	bandStyle := barStyle.Faint(true)
	if tint != nil && !grabbed {
		bandStyle = barStyle.Foreground(dull(tint))
	}
	overdueStyle := bandStyle
	if !grabbed {
		overdueStyle = barStyle.Foreground(overdueTint)
	}
	styleOf := func(kind cellStyle) lipgloss.Style {
		switch kind {
		case cellBand:
			return bandStyle
		case cellOverdue:
			return overdueStyle
		}
		return barStyle
	}
	// on the cursor's period the cell is marked on the cursor's row and
	// washed on every other; the mark drops the tint, the grab's colour and
	// the faint, so what it covers is the terminal's foreground on the mark
	cursor := func(i int, style lipgloss.Style) lipgloss.Style {
		switch {
		case under && i == p.col:
			return styleMark()
		case i == p.col:
			return style.Background(styleRow().GetBackground())
		}
		return style
	}
	now := p.nowCol()
	at := first
	if milestone {
		at = p.milestoneCol(b)
	}
	for i := p.colOffset; i < p.colOffset+visible; i++ {
		var text strings.Builder
		style := barStyle
		switch {
		case !ok:
			// a row with no dates at all could start at any point from now
			// on: its band begins at today and runs to the chart's end
			if i < now {
				text.WriteString(strings.Repeat(" ", w))
				break
			}
			text.WriteString(strings.Repeat(glyphDateless, w))
			style = bandStyle
		case milestone:
			// a cell at a time: the trail is the bar's, the rest the band's
			for k := 0; k < w; k++ {
				glyph, kind := milestoneGlyph(i, k, w, at, now, b.hasStart)
				parts = append(parts, cursor(i, styleOf(kind)).Render(glyph))
			}
			continue
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
		parts = append(parts, cursor(i, style).Render(text.String()))
	}

	used := labelWidth + 1 + visible*w
	if under && p.width > used {
		parts = append(parts, wash.Render(strings.Repeat(" ", p.width-used)))
	}
	return fit(strings.Join(parts, ""), p.width)
}

// milestoneGlyph is cell k of period i on a row with one date: the trail
// starts on the date's period — its first cell for a start, its last for a
// stop — and fades away from it into the neighboring periods, the side its
// missing date leaves open, and past the trail that side is the band.
//
// The band of a missing start begins at today (`04248c5`): a row with no
// start has not started, so the chart says it could start from now on
// rather than painting the weeks it has already been possible in. A stop
// that has gone by draws its marker all the same, and the band from it to
// today in the overdue tint, which is the row saying it is late. A missing
// stop is open-ended as it always was: the band runs from the start to the
// chart's edge, because work with no end date has not been given one.
func milestoneGlyph(i, k, w, at, now int, start bool) (glyph string, kind cellStyle) {
	distance := (i-at)*w + k
	if !start {
		distance = (at-i)*w + (w - 1 - k)
	}
	switch {
	case distance < 0:
		// the far side of the date: nothing, unless a stop has gone by and
		// the band from it to today says so
		if !start && i <= now {
			return glyphDateless, cellOverdue
		}
		return " ", cellBlank
	case distance < len(milestoneTrail):
		return milestoneTrail[distance], cellBar
	case start || i >= now:
		return glyphDateless, cellBand
	}
	return " ", cellBlank
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
		count = fmt.Sprintf("%d of %d issues · /%s", n, p.total(), p.filter)
	}

	return styleStatus.Render(bottomLine(p.hintLine(), lastAction(p.status, count), p.width))
}

// hintLine is what the keys do where the cursor is (hints.go).
//
// A bar with no dates has nothing to shift — dragAlong refuses it — so space
// is named there only where a bound rank still gives the grab something to
// do, and the grabbed line drops the shift for the same reason.
func (p *ganttPage) hintLine() string {
	switch {
	case p.filtering != nil:
		return filterHints()
	case p.grabbed >= 0:
		b := &p.bars[p.grabbed]
		var moves []hint
		if b.hasStart || b.hasStop {
			moves = append(moves, hint{"←→", "shift"})
		}
		moves = append(moves, hint{"↑↓", "reorder"})
		return grabHints(moves...)
	}

	b := p.current()
	if b == nil {
		return hints()
	}
	if isGhost(b.id) {
		return hints(hint{"enter", "new issue"})
	}
	// the arrow cell: enter opens the row as it does anywhere, space folds
	if p.col < 0 {
		return hints(append([]hint{{"enter", "open"}}, foldHints(p.node())...)...)
	}
	pairs := []hint{{"enter", "open"}}
	if b.hasStart || b.hasStop {
		pairs = append(pairs, hint{"space", "grab bar"})
	} else {
		pairs = append(pairs, hint{"space", "grab"})
	}
	return hints(pairs...)
}
