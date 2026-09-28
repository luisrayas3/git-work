package tui

import (
	"fmt"
	"strings"
)

// idWidth is the short id's column: the same seven characters every other
// surface prints, so an id read here is an id that can be typed there.
const idWidth = 7

func (p *listPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	header, rows, cursorLine := p.body()

	// one line for the call, one for the header, the rest for the rows, the
	// bottom for whatever is open and the status line
	room := max(p.height-2-len(bottom), 1)
	p.scroll(cursorLine, room, len(rows))

	lines := make([]string, 0, p.height)
	lines = append(lines, callLine(p.call, "", "", p.width), header)
	for at := p.top; at < min(p.top+room, len(rows)); at++ {
		lines = append(lines, rows[at])
	}
	for len(lines) < p.height-len(bottom) {
		lines = append(lines, "")
	}

	return strings.Join(append(lines, bottom...), "\n")
}

// bottom is whatever is open over the rows, plus the status line, which is
// always the last line of the page.
func (p *listPage) bottom() []string {
	var lines []string
	switch {
	case p.editor != nil:
		lines = p.editor.View(p.width)
	case p.filtering != nil:
		lines = []string{fit("/"+p.filtering.View(), p.width)}
	}
	return append(lines, p.statusLine())
}

// statusLine is the last message and the count, the answer to "did that
// write land?"; what am I looking at is the call, on the first line.
func (p *listPage) statusLine() string {
	count := fmt.Sprintf("%d issues", len(p.order))
	if p.filter != "" {
		count = fmt.Sprintf("%d of %d issues, filter %q", len(p.order), len(p.rows), p.filter)
	}

	left := p.status
	if left == "" {
		left = "? for keys"
	}

	line := fmt.Sprintf("%s · %s", left, count)
	return styleStatus.Render(fit(line, p.width))
}

// body draws the header and every row in the drawing order, and says which
// line the cursor is on so the window can be scrolled to it.
func (p *listPage) body() (header string, rows []string, cursorLine int) {
	widths := p.widths()

	cells := make([]string, 0, len(p.fields)+1)
	cells = append(cells, pad("id", idWidth))
	for at, key := range p.fields {
		cells = append(cells, pad(key, widths[at]))
	}
	header = styleHeader.Render(fit(strings.Join(cells, " "), p.width))

	cursorLine = 0
	group := ""
	for at, index := range p.order {
		row := &p.rows[index]

		if p.groupBy != "" && row.group != group {
			group = row.group
			rows = append(rows, styleGroup.Render(fit(group, p.width)))
		}

		if at == p.cursor {
			cursorLine = len(rows)
		}
		rows = append(rows, p.rowLine(row, widths, at == p.cursor, index == p.grabbed))

		for _, line := range p.detailLines(row) {
			rows = append(rows, line)
		}
	}

	return header, rows, cursorLine
}

// rowLine draws one issue: the short id, then the fields as columns.
//
// The cell under the column cursor is reversed, which is what says that edit
// and copy act on that one and not the row. The id is a cell like the others,
// and the one the cursor starts on.
func (p *listPage) rowLine(row *listRow, widths []int, under bool, grabbed bool) string {
	cells := make([]string, 0, len(p.fields)+1)
	id := pad(row.human, idWidth)
	if under && p.column == 0 {
		cells = append(cells, styleCell.Render(id))
	} else {
		cells = append(cells, styleDim.Render(id))
	}

	for at, key := range p.fields {
		cell := pad(plainValue(row.fields[key]), widths[at])
		if under && at+1 == p.column {
			cell = styleCell.Render(cell)
		}
		cells = append(cells, cell)
	}

	line := strings.Join(cells, " ")
	switch {
	case grabbed && p.blink:
		line = styleGrab.Render("[") + line + styleGrab.Render("]")
	case grabbed:
		line = styleGrab.Render("⟨") + line + styleGrab.Render("⟩")
	case under:
		line = "›" + line
	default:
		line = " " + line
	}
	return line
}

// detailLines are the dim second line under a row, one per detail field, so
// that what a row is about can be read without opening it.
func (p *listPage) detailLines(row *listRow) []string {
	if len(p.details) == 0 {
		return nil
	}

	parts := make([]string, 0, len(p.details))
	for _, key := range p.details {
		value := plainValue(row.fields[key])
		if value == "" {
			continue
		}
		parts = append(parts, key+": "+value)
	}
	if len(parts) == 0 {
		return nil
	}

	indent := strings.Repeat(" ", idWidth+2)
	return []string{styleDim.Render(fit(indent+strings.Join(parts, "  "), p.width))}
}

// widths sizes the field columns to what is in them, and then to the window.
func (p *listPage) widths() []int {
	widths := make([]int, len(p.fields))
	for at, key := range p.fields {
		widths[at] = len([]rune(key))
		for _, index := range p.order {
			if n := len([]rune(plainValue(p.rows[index].fields[key]))); n > widths[at] {
				widths[at] = n
			}
		}
	}

	// The budget is the window less the id column, the cursor marker and one
	// space between columns. Over it, the widest column gives way first, so
	// that a long title shrinks before a short status disappears; a column is
	// never capped below that, so a wide window shows a whole title.
	budget := p.width - idWidth - 2 - len(p.fields)
	for budget > 0 && sum(widths) > budget {
		widest := 0
		for at := range widths {
			if widths[at] > widths[widest] {
				widest = at
			}
		}
		if widths[widest] <= 3 {
			break
		}
		widths[widest]--
	}

	return widths
}

// scroll moves the window so the cursor's line is in it, and no further.
func (p *listPage) scroll(cursorLine, room, total int) {
	if cursorLine < p.top {
		p.top = cursorLine
	}
	if cursorLine >= p.top+room {
		p.top = cursorLine - room + 1
	}
	if p.top > total-room {
		p.top = total - room
	}
	if p.top < 0 {
		p.top = 0
	}
}

// rowsPerPage is what a page key moves by: the rows that fit, at least one.
func (p *listPage) rowsPerPage() int {
	return max(p.height-4, 1)
}

func sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
