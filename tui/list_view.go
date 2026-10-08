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

// indentWidth is what one level of nesting moves a child table right by:
// the whole table, header and rows, as one unit
// (doc/design/terminal-renderer.md, Nesting).
const indentWidth = 2

func (p *listPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	body := p.body()

	// one line for the call, one for the header, the rest for the rows, the
	// bottom for whatever is open and the status line
	room := max(p.height-2-len(bottom), 1)

	lines := make([]string, 0, p.height)
	lines = append(lines, callLine(p.call, "", "", p.width), body.header)
	lines = append(lines, window(&p.top, body.lines, body.sticky, body.cursorLine, room)...)
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
	if p.node().ghost {
		return hints(hint{"enter", "new issue"})
	}
	// a row that stands for no issue opens nothing and edits nothing: what
	// is left is a link to follow, its fold and its grab (R2)
	none := row.id == ""
	if key := p.fieldKey(); key != "" {
		linked := len(row.links[key]) > 0
		var pairs []hint
		if !none || linked {
			pairs = append(pairs, openHint(linked))
		}
		if edit, ok := editHint(fieldKind(p.repo, row.typeKey, key)); ok && !none {
			pairs = append(pairs, edit)
		}
		return hints(pairs...)
	}

	open := []hint{{"enter", "open"}}
	if none {
		open = nil
	}
	// the tree cell: enter opens the row as the id does, space folds it
	if p.column == p.treeCol() {
		return hints(append(open, foldHints(p.node())...)...)
	}
	// the id column: enter opens the row, space grabs it to move it
	return hints(append(open, hint{"space", "grab"})...)
}

// listBody is the page's body as drawn: the top table's header, which is
// fixed, and the lines under it — rows, group lines, detail lines and the
// headers of the child tables — with, for every line, the header line it is
// read under, and the line the cursor's row is on.
type listBody struct {
	header     string
	lines      []string
	sticky     []int
	cursorLine int
}

// body draws the body (listBody).
//
// The list is a table of tables (doc/design/terminal-renderer.md, Nesting).
// The roots are the top table, whose header is the page's second line and
// never changes. The rows under an opened parent are a child table: indented
// as one unit, headed by its own header line above its first row, its
// columns the layer's own in the layer's widths. The pre-order puts a
// parent's visible children right after it, so the first child is the row
// whose predecessor is its parent, and that is where the header goes.
//
// sticky is, per line, the header it must not be read without: a root's is
// the group line it is under, when the roots are grouped, because the top
// header is always on screen; a nested line's is its table's header, because
// a row read without its header is a row of numbers. A header is its own
// sticky, which is how window knows not to pin a copy of a line that is
// already drawn.
func (p *listPage) body() listBody {
	widths := p.widths()
	tree := p.treeRoom()
	heads := groupHeads(p.nodes, p.order)

	body := listBody{header: p.tableHeader(0, widths, tree)}
	add := func(line string, under int) {
		body.lines = append(body.lines, line)
		body.sticky = append(body.sticky, under)
	}

	rootGroup := -1
	tableHeader := map[string]int{} // the header line of the table under each parent
	for at, index := range p.order {
		row, node := &p.rows[index], &p.nodes[index]

		under := rootGroup
		if node.level > 0 {
			if at > 0 && p.nodes[p.order[at-1]].key == node.parent {
				tableHeader[node.parent] = len(body.lines)
				add(p.tableHeader(node.level, widths, tree), len(body.lines))
			}
			under = tableHeader[node.parent]
		}

		// a group is its own level's: the roots section the whole list, and
		// a layer with a `group_by` sections the table under one parent
		if heads[at] {
			line := styleGroup.Render(fit(p.groupLine(node), p.width))
			if node.level == 0 {
				rootGroup = len(body.lines)
				under = rootGroup
				add(line, rootGroup)
			} else {
				add(line, under)
			}
		}

		if at == p.cursor {
			body.cursorLine = len(body.lines)
		}
		add(p.rowLine(row, node, widths, tree, at == p.cursor, index == p.grabbed), under)
		for _, line := range p.detailLines(row, node, tree) {
			add(line, under)
		}
	}

	return body
}

// tableHeader is the header line of the tables at one level: the id, the
// tree cell and the layer's fields, each over its column, at the level's
// indent. Level 0 is the top table's, the page's fixed header.
func (p *listPage) tableHeader(level int, widths [][]int, tree int) string {
	cells := []string{indent(level) + " " + pad("id", idWidth)}
	if tree > 0 {
		cells = append(cells, pad("", tree))
	}
	sizes := widths[p.nest.family(level)]
	for at, key := range p.layer(level).fields {
		cells = append(cells, pad(key, sizes[at]))
	}
	return styleHeader.Render(fit(strings.Join(cells, " "), p.width))
}

// indent is the left margin of the tables at one level.
func indent(level int) string {
	return strings.Repeat(" ", indentWidth*level)
}

// groupLine is a group header: the value, at its table's indent, so that a
// nested section reads as part of the table it sections and the roots'
// sections still head the whole list.
func (p *listPage) groupLine(node *treeRow) string {
	return indent(node.level) + node.group
}

// rowLine draws one issue in its table: the table's indent, the cursor
// marker, the short id, the tree cell, then the fields of the row's own
// layer as columns in the layer's widths.
//
// The row under the cursor has a light wash across the whole window, which
// says which issue; the cell under the column cursor is reversed within it,
// which says that edit and copy act on that one. The id is a cell like the
// others, and the one the cursor starts on. A cell that links other issues
// is underlined, because enter follows it. The tree cell is the one after
// the id: the fold arrow and the count of what a fold is hiding (treeCell).
func (p *listPage) rowLine(row *listRow, node *treeRow, widths [][]int, tree int, under bool, grabbed bool) string {
	// every piece is styled on its own, the wash included: a style ends in
	// a reset, and a reset inside the row would end the wash with it
	wash := lipgloss.NewStyle()
	if under {
		wash = styleRow()
	}
	if node.ghost {
		// the ghost is drawn dim whole: a place to add, not an issue
		wash = wash.Faint(true)
	}

	fields, sizes := p.layer(node.level).fields, widths[p.nest.family(node.level)]

	parts := make([]string, 0, 2*len(fields)+8)
	margin := indent(node.level)
	switch {
	case grabbed && p.blink:
		parts = append(parts, margin, styleGrab.Render("["))
	case grabbed:
		parts = append(parts, margin, styleGrab.Render("⟨"))
	case under:
		parts = append(parts, wash.Render(margin+"›"))
	default:
		parts = append(parts, margin+" ")
	}
	used := len(margin) + 1

	id := pad(row.human, idWidth)
	if under && p.column == 0 {
		id = styleCell.Render(id)
	} else {
		id = wash.Faint(true).Render(id)
	}
	parts = append(parts, id)
	used += idWidth

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
	switch {
	case grabbed:
		parts = append(parts, styleGrab.Render(map[bool]string{true: "]", false: "⟩"}[p.blink]))
	case under && p.width > used:
		parts = append(parts, wash.Render(strings.Repeat(" ", p.width-used)))
	}

	return fit(strings.Join(parts, ""), p.width)
}

// detailLines are the dim second line under a row, one per detail field of
// the row's own layer, so that what a row is about can be read without
// opening it. They start under the row's first field, in its table.
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

	margin := strings.Repeat(" ", fieldsStart(node.level, tree))
	return []string{styleDim.Render(fit(margin+strings.Join(parts, "  "), p.width))}
}

// fieldsStart is the column a table's first field starts in: the indent,
// the marker, the id, the tree cell and the spaces between them.
func fieldsStart(level, tree int) int {
	start := indentWidth*level + 1 + idWidth + 1
	if tree > 0 {
		start += tree + 1
	}
	return start
}

// treeRoom is the width of the tree cell after the id, nothing where
// `expand` is not bound. It is measured over every row, hidden or drawn, so
// that folding moves nothing.
func (p *listPage) treeRoom() int {
	return treeWidth(p.nodes, p.nest.expanded())
}

// widths sizes every layer's columns, one set per layer family: the rows of
// one layer are measured against each other, wherever in the tree they are
// drawn and whether or not a fold hides them, so that every table of that
// layer has the same columns at the same widths and unfolding moves nothing.
func (p *listPage) widths() [][]int {
	tree := p.treeRoom()
	out := make([][]int, len(p.nest.layers))
	for family := range out {
		out[family] = p.widthsOf(family, tree)
	}
	return out
}

// widthsOf sizes one layer family's columns to what is in them, and then to
// the window at the deepest indent the family is drawn at.
func (p *listPage) widthsOf(family, tree int) []int {
	fields := p.nest.layers[family].fields
	widths := make([]int, len(fields))
	for at, key := range fields {
		widths[at] = len([]rune(key))
	}
	deepest := 0
	for index := range p.rows {
		level := p.nodes[index].level
		if p.nest.family(level) != family {
			continue
		}
		deepest = max(deepest, level)
		for at, key := range fields {
			if n := ansi.StringWidth(p.rows[index].cells[key]); n > widths[at] {
				widths[at] = n
			}
		}
	}

	// The budget is the window less the table's indent, the id column, the
	// tree cell, the cursor marker and one space between columns. Over it,
	// the widest column gives way first, so that a long title shrinks before
	// a short status disappears; a column is never capped below that, so a
	// wide window shows a whole title.
	budget := p.width - fieldsStart(deepest, tree) - len(fields) + 1
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
// lines to the cursor's line, and then the window's first line is the header
// the line it opens on is read under — a group's, or a child table's — a
// sticky header, because a header scrolled off the top is unreachable, and
// the rows under it lose the only thing that says what they are. The sticky
// line is the header as the page drew it, so whatever it carries — the
// gantt's crosshair, a group's tint — comes with it.
//
// sticky is parallel to lines: the line each line's header is drawn on, -1
// where there is none. The window is room lines whatever happens, the sticky
// header taking one of them, and the cursor's line stays within those that
// are left.
func window(top *int, lines []string, sticky []int, cursorLine, room int) []string {
	scroll(top, cursorLine, room, len(lines))

	// a window of one line has none to spare: the cursor's row wins it
	pinned := -1
	if room > 1 {
		pinned = stickyHeader(sticky, *top)
	}
	if pinned >= 0 && cursorLine <= *top {
		// the rows start one line lower than the window does, so the cursor's
		// row, which the scroll put on its first line, moves down with them
		*top = max(cursorLine-1, 0)
		pinned = stickyHeader(sticky, *top)
	}

	drawn := make([]string, 0, room)
	first := *top
	if pinned >= 0 {
		drawn = append(drawn, lines[pinned])
		first++
	}
	for at := first; at < min(*top+room, len(lines)); at++ {
		drawn = append(drawn, lines[at])
	}
	return drawn
}

// stickyHeader is the header the line at top is read under, or -1 when that
// line has none, or is the header itself and so needs no copy of itself.
func stickyHeader(sticky []int, top int) int {
	if top < 0 || top >= len(sticky) || sticky[top] == top {
		return -1
	}
	return sticky[top]
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
