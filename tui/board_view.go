package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/schema"
)

// columnGap is what separates two columns: a dim rule with a space each side.
const columnGap = 3

func (p *boardPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	width, visible := p.layout()
	header := p.headerLines(width, visible)
	body, groups, cursorLine := p.body(width, visible)

	// one line for the call, the header and its rule, the rest for the
	// board, the bottom for whatever is open and the status line
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

// layout sizes the columns to the window, and says how many are drawn.
//
// When every column fits at its minimum width they share the window; when
// they do not, they keep the minimum and the board scrolls sideways by whole
// columns, colOffset moving so that the cursor's column is on screen — the
// sideways twin of the vertical scroll.
func (p *boardPage) layout() (width, visible int) {
	n := len(p.columns)
	if n == 0 {
		return p.width, 0
	}
	if n*minColumnWidth+columnGap*(n-1) <= p.width {
		p.colOffset = 0
		return (p.width - columnGap*(n-1)) / n, n
	}

	width = minColumnWidth
	visible = max((p.width+columnGap)/(width+columnGap), 1)
	if p.col < p.colOffset {
		p.colOffset = p.col
	}
	if p.col >= p.colOffset+visible {
		p.colOffset = p.col - visible + 1
	}
	p.colOffset = min(max(p.colOffset, 0), max(n-visible, 0))
	return width, min(visible, n)
}

// headerLines are the column labels with their counts, and the rule under
// them; ‹ and › at the ends say there are columns off screen.
func (p *boardPage) headerLines(width, visible int) []string {
	counts := make([]int, len(p.columns))
	for _, la := range p.lanes {
		for c, stack := range la.stacks {
			counts[c] += len(stack)
		}
	}

	cells := make([]string, 0, visible)
	rule := make([]string, 0, visible)
	for c := p.colOffset; c < p.colOffset+visible; c++ {
		marker := " "
		if c == p.colOffset && p.colOffset > 0 {
			marker = "‹"
		}
		label := fmt.Sprintf("%s (%d)", p.columns[c].label, counts[c])
		cells = append(cells, marker+styleHeader.Render(pad(label, width-1)))
		rule = append(rule, strings.Repeat("─", width))
	}
	header := strings.Join(cells, styleDim.Render(" │ "))
	if p.colOffset+visible < len(p.columns) {
		header += "›"
	}
	return []string{fit(header, p.width), styleDim.Render(fit(strings.Join(rule, "─┼─"), p.width))}
}

// body draws every lane, the visible columns side by side, says which line
// the cursor's card starts on so the window can be scrolled to it, and which
// lane header each line sits under so that header can be kept on screen.
func (p *boardPage) body(width, visible int) (lines []string, groups []int, cursorLine int) {
	gap := styleDim.Render(" │ ")
	blank := pad("", width)

	for l, la := range p.lanes {
		headerAt := -1
		if p.groupBy != "" {
			headerAt = len(lines)
			lines = append(lines, styleGroup.Render(fit(la.group, p.width)))
			groups = append(groups, headerAt)
		}

		columns := make([][]string, 0, visible)
		height := 0
		for c := p.colOffset; c < p.colOffset+visible; c++ {
			drawn, at := p.stackLines(l, c, width)
			if l == p.lane && c == p.col && at >= 0 {
				cursorLine = len(lines) + at
			}
			columns = append(columns, drawn)
			height = max(height, len(drawn))
		}

		for at := 0; at < height; at++ {
			cells := make([]string, 0, len(columns))
			for _, drawn := range columns {
				if at < len(drawn) {
					cells = append(cells, drawn[at])
				} else {
					cells = append(cells, blank)
				}
			}
			lines = append(lines, fit(strings.Join(cells, gap), p.width))
			groups = append(groups, headerAt)
		}
	}
	return lines, groups, cursorLine
}

// stackLines draws one column of one lane, a card at a time, and says which
// line the cursor's card starts on, or -1.
func (p *boardPage) stackLines(l, c, width int) (lines []string, cursorAt int) {
	cursorAt = -1
	for r, index := range p.lanes[l].stacks[c] {
		under := l == p.lane && c == p.col && r == p.row
		if under {
			cursorAt = len(lines)
		}
		lines = append(lines, p.cardLines(&p.cards[index], width, under, index == p.grabbed)...)
		lines = append(lines, pad("", width))
	}
	return lines, cursorAt
}

// cardLines draws one card: the short id, the title wrapped to the column,
// then each other card field on a line of its own.
//
// The card under the cursor has the light wash over its whole width and its
// id reversed, as the list's row and cell are; the grabbed card's id line
// carries the blinking markers. Every piece is styled on its own, because a
// style ends in a reset and a reset inside the line would end the wash.
func (p *boardPage) cardLines(c *card, width int, under, grabbed bool) []string {
	inner := width - 1
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

	id := pad(c.human, inner)
	if under {
		id = styleCell.Render(id)
	} else {
		id = wash.Faint(true).Render(id)
	}
	lines := []string{marker + id}

	if title, ok := c.cells[schema.TitleKey]; ok && title != "" {
		for _, line := range strings.Split(ansi.Wrap(title, inner, ""), "\n") {
			lines = append(lines, wash.Render(" "+pad(line, inner)))
		}
	}

	for _, key := range p.cardKeys {
		value := c.cells[key]
		if key == schema.TitleKey || value == "" {
			continue
		}
		label := key + ":"
		style := wash
		if c.links[key] {
			style = style.Underline(true)
		}
		text := truncate(value, max(inner-len([]rune(label))-1, 0))
		rest := pad("", max(inner-len([]rune(label))-1-ansi.StringWidth(text), 0))
		lines = append(lines, wash.Render(" ")+wash.Faint(true).Render(label)+wash.Render(" ")+style.Render(text)+wash.Render(rest))
	}
	return lines
}

// bottom is the filter when one is being typed, and the status line, which
// is always the last line of the page.
func (p *boardPage) bottom() []string {
	var lines []string
	if p.filtering != nil {
		lines = []string{fit("/"+p.filtering.View(), p.width)}
	}
	return append(lines, p.statusLine())
}

func (p *boardPage) statusLine() string {
	n := p.count()
	count := fmt.Sprintf("%d issues", n)
	if n == 1 {
		count = "1 issue"
	}
	if p.filter != "" {
		count = fmt.Sprintf("%d of %d issues · /%s", n, len(p.cards), p.filter)
	}

	return styleStatus.Render(bottomLine(p.hintLine(), lastAction(p.status, count), p.width))
}

// hintLine is what the keys do where the cursor is (hints.go). A card has no
// cell: it is opened, grabbed and moved, and its fields are edited on show.
func (p *boardPage) hintLine() string {
	switch {
	case p.filtering != nil:
		return filterHints()
	case p.grabbed >= 0:
		return grabHints(hint{"←→", "column"}, hint{"↑↓", "reorder"})
	case p.current() == nil:
		return hints()
	}
	return hints(hint{"enter", "open"}, hint{"space", "grab"})
}
