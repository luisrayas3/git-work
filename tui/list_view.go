package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// idWidth is the short id's column: the same seven characters every other
// surface prints, so an id read here is an id that can be typed there.
const idWidth = 7

func (p *listPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	header, rows, groups, cursorLine := p.body()

	// one line for the call, one for the header, the rest for the rows, the
	// bottom for whatever is open and the status line
	room := max(p.height-2-len(bottom), 1)

	lines := make([]string, 0, p.height)
	lines = append(lines, callLine(p.call, "", "", p.width), header)
	lines = append(lines, window(&p.top, rows, groups, cursorLine, room)...)
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

// statusLine is the hints on the left and, on the right, the last message
// and the count — the answer to "did that write land?" (bottomLine); what am
// I looking at is the call, on the first line.
func (p *listPage) statusLine() string {
	count := fmt.Sprintf("%d issues", len(p.order))
	if len(p.order) == 1 {
		count = "1 issue"
	}
	if p.filter != "" {
		count = fmt.Sprintf("%d of %d issues · /%s", len(p.order), len(p.rows), p.filter)
	}

	return styleStatus.Render(bottomLine(p.hintLine(), lastAction(p.status, count), p.width))
}

// hintLine is what the keys do where the cursor is (hints.go).
func (p *listPage) hintLine() string {
	switch {
	case p.editor != nil:
		return p.editor.hints()
	case p.filtering != nil:
		return filterHints()
	case p.grabbed >= 0:
		return grabHints(hint{"↑↓", "move"})
	}

	row := p.current()
	if row == nil {
		return hints()
	}
	if key := p.fieldKey(); key != "" {
		pairs := []hint{openHint(len(row.links[key]) > 0)}
		if edit, ok := editHint(fieldKind(p.repo, row.typeKey, key)); ok {
			pairs = append(pairs, edit)
		}
		return hints(pairs...)
	}

	// the tree cell: enter opens the row as the id does, space folds it
	if p.column == p.treeCol() {
		return hints(append([]hint{{"enter", "open"}}, foldHints(p.node())...)...)
	}
	// the id column: enter opens the row, space grabs it to move it
	return hints(hint{"enter", "open"}, hint{"space", "grab"})
}

// body draws the header and every row in the drawing order, says which line
// the cursor is on so the window can be scrolled to it, and which group
// header each line sits under so that header can be kept on screen.
//
// The header describes the layer the cursor's row is on, not the roots': a
// layer draws its own columns in its own widths under its parent, so the
// only header that can be right is the one for the row being read, and it
// changes as tab and shift-tab change level.
func (p *listPage) body() (header string, rows []string, groups []int, cursorLine int) {
	widths := p.widths()
	tree := p.treeRoom()
	heads := groupHeads(p.nodes, p.order)

	cells := []string{pad("id", idWidth)}
	if tree > 0 {
		cells = append(cells, pad("", tree))
	}
	for at, key := range p.columns() {
		cells = append(cells, pad(key, widths[p.cursorLayer()][at]))
	}
	header = styleHeader.Render(fit(strings.Join(cells, " "), p.width))

	cursorLine = 0
	headerAt := -1
	for at, index := range p.order {
		row, node := &p.rows[index], &p.nodes[index]

		// a group is its own level's: the roots section the whole list, and
		// a layer with a `group_by` sections the children under one parent
		if heads[at] {
			line := styleGroup.Render(fit(p.groupLine(node), p.width))
			if node.level == 0 {
				headerAt = len(rows)
			}
			rows = append(rows, line)
			groups = append(groups, headerAt)
		}

		if at == p.cursor {
			cursorLine = len(rows)
		}
		rows = append(rows, p.rowLine(row, node, widths, tree, at == p.cursor, index == p.grabbed))
		groups = append(groups, headerAt)

		for _, line := range p.detailLines(row, node, tree) {
			rows = append(rows, line)
			groups = append(groups, headerAt)
		}
	}

	return header, rows, groups, cursorLine
}

// groupLine is a group header: the value, under the id column and at the
// level's own indent, so that a nested section reads as the parent's and the
// roots' sections still head the whole list.
func (p *listPage) groupLine(node *treeRow) string {
	if node.level == 0 {
		return node.group
	}
	return strings.Repeat(" ", 1+idWidth+1+2*node.level) + node.group
}

// rowLine draws one issue: the short id, the tree cell, then the fields of
// the row's own layer as columns.
//
// The row under the cursor has a light wash across the whole window, which
// says which issue; the cell under the column cursor is reversed within it,
// which says that edit and copy act on that one. The id is a cell like the
// others, and the one the cursor starts on. A cell that links other issues
// is underlined, because enter follows it. The tree cell is the one after
// the id: the level's indent, the fold arrow, and the count of what a fold
// is hiding (treeCell).
func (p *listPage) rowLine(row *listRow, node *treeRow, widths [][]int, tree int, under bool, grabbed bool) string {
	// every piece is styled on its own, the wash included: a style ends in
	// a reset, and a reset inside the row would end the wash with it
	wash := lipgloss.NewStyle()
	if under {
		wash = styleRow()
	}

	layer := p.nest.index(node.level)
	fields, sizes := p.nest.layers[layer].fields, widths[layer]

	parts := make([]string, 0, 2*len(fields)+6)
	id := pad(row.human, idWidth)
	if under && p.column == 0 {
		id = styleCell.Render(id)
	} else {
		id = wash.Faint(true).Render(id)
	}
	parts = append(parts, id)
	used := idWidth

	if tree > 0 {
		cell := pad(treeCell(*node), tree)
		style := wash
		if under && p.column == p.treeCol() {
			style = styleCell
		}
		parts = append(parts, wash.Render(" "), style.Render(cell))
		used += 1 + tree
	}

	for at, key := range fields {
		parts = append(parts, wash.Render(" "))
		text := truncate(row.cells[key], sizes[at])
		gap := strings.Repeat(" ", max(sizes[at]-ansi.StringWidth(text), 0))
		// the cursor cell is reversed over its whole width, padding
		// included, so an empty cell still shows where the cursor is;
		// the underline stays on the text alone
		style, fill := wash, wash
		if under && at+p.firstFieldCol() == p.column {
			style, fill = styleCell, styleCell
		}
		if len(row.links[key]) > 0 {
			style = style.Underline(true)
		}
		parts = append(parts, style.Render(text)+fill.Render(gap))
		used += 1 + sizes[at]
	}
	if under && p.width > used {
		parts = append(parts, wash.Render(strings.Repeat(" ", p.width-used)))
	}

	line := strings.Join(parts, "")
	switch {
	case grabbed && p.blink:
		line = styleGrab.Render("[") + line + styleGrab.Render("]")
	case grabbed:
		line = styleGrab.Render("⟨") + line + styleGrab.Render("⟩")
	case under:
		line = wash.Render("›") + line
	default:
		line = " " + line
	}
	return fit(line, p.width)
}

// detailLines are the dim second line under a row, one per detail field of
// the row's own layer, so that what a row is about can be read without
// opening it.
func (p *listPage) detailLines(row *listRow, node *treeRow, tree int) []string {
	details := p.layer(node.level).details
	if len(details) == 0 {
		return nil
	}

	parts := make([]string, 0, len(details))
	for _, key := range details {
		value := row.cells[key]
		if value == "" {
			continue
		}
		parts = append(parts, key+": "+value)
	}
	if len(parts) == 0 {
		return nil
	}

	indent := strings.Repeat(" ", tree+idWidth+2)
	return []string{styleDim.Render(fit(indent+strings.Join(parts, "  "), p.width))}
}

// treeRoom is the width of the tree cell after the id, nothing where
// `expand` is not bound.
func (p *listPage) treeRoom() int {
	return treeWidth(p.nodes, p.order, p.nest.expanded())
}

// cursorLayer is the layer the header is drawn for: the cursor's row's.
func (p *listPage) cursorLayer() int {
	node := p.node()
	if node == nil {
		return 0
	}
	return p.nest.index(node.level)
}

// widths sizes every layer's columns, one set per layer: a layer's rows are
// measured against each other, so a child's columns are as wide as the
// children need and not as wide as their parents do.
func (p *listPage) widths() [][]int {
	tree := p.treeRoom()
	out := make([][]int, len(p.nest.layers))
	for at := range out {
		out[at] = p.widthsOf(at, tree)
	}
	return out
}

// widthsOf sizes one layer's columns to what is in them, and then to the
// window.
func (p *listPage) widthsOf(layer, tree int) []int {
	fields := p.nest.layers[layer].fields
	widths := make([]int, len(fields))
	for at, key := range fields {
		widths[at] = len([]rune(key))
		for _, index := range p.order {
			if p.nest.index(p.nodes[index].level) != layer {
				continue
			}
			if n := ansi.StringWidth(p.rows[index].cells[key]); n > widths[at] {
				widths[at] = n
			}
		}
	}

	// The budget is the window less the id column, the tree cell, the cursor
	// marker and one space between columns. Over it, the widest column gives
	// way first, so that a long title shrinks before a short status
	// disappears; a column is never capped below that, so a wide window
	// shows a whole title.
	budget := p.width - tree - idWidth - 2 - len(fields)
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

// scroll moves a window of room lines over total so that the cursor's line
// is in it, and no further: top is the first line drawn.
func scroll(top *int, cursorLine, room, total int) {
	if cursorLine < *top {
		*top = cursorLine
	}
	if cursorLine >= *top+room {
		*top = cursorLine - room + 1
	}
	if *top > total-room {
		*top = total - room
	}
	if *top < 0 {
		*top = 0
	}
}

// window is the slice of a body a page draws: scroll moves a window of room
// lines to the cursor's line, and then, in a grouped view, the window's first
// line is the header of the group it opens in — a sticky header, because a
// header scrolled off the top is unreachable, and the rows under it lose the
// only thing that says which group they are in. The sticky line is the header
// as the page drew it, so whatever it carries — the gantt's crosshair, a
// group's tint — comes with it.
//
// groups is parallel to lines: the line each line's group header is drawn on,
// -1 where there is none. The window is room lines whatever happens, the
// sticky header taking one of them, and the cursor's line stays within those
// that are left.
func window(top *int, lines []string, groups []int, cursorLine, room int) []string {
	scroll(top, cursorLine, room, len(lines))

	// a window of one line has none to spare: the cursor's row wins it
	sticky := -1
	if room > 1 {
		sticky = groupHeader(groups, *top)
	}
	if sticky >= 0 && cursorLine <= *top {
		// the rows start one line lower than the window does, so the cursor's
		// row, which the scroll put on its first line, moves down with them
		*top = max(cursorLine-1, 0)
		sticky = groupHeader(groups, *top)
	}

	drawn := make([]string, 0, room)
	first := *top
	if sticky >= 0 {
		drawn = append(drawn, lines[sticky])
		first++
	}
	for at := first; at < min(*top+room, len(lines)); at++ {
		drawn = append(drawn, lines[at])
	}
	return drawn
}

// groupHeader is the group header the line at top sits under, or -1 when the
// view is not grouped, when that line has no header above it, or when it is
// the header itself and so needs no sticky copy of itself.
func groupHeader(groups []int, top int) int {
	if top < 0 || top >= len(groups) || groups[top] == top {
		return -1
	}
	return groups[top]
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
