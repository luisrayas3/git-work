package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// minMatrixColumn is the narrowest a column of the matrix goes before it
// scrolls sideways instead: enough for a short number and a cut header.
const minMatrixColumn = 10

// matrixGap is the space between two columns.
const matrixGap = 1

func (p *matrixPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	bottom := p.bottom()
	labels, width, visible := p.layout()
	header := p.headerLines(labels, width, visible)
	body, groups, cursorLine := p.body(labels, width, visible)

	// one line for the call, the header and its rule, the rest for the
	// matrix, the bottom for whatever is open and the status line
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

// layout sizes the row-label column and the data columns, and says how many
// data columns are drawn.
//
// The row labels are sticky: a matrix whose row labels have scrolled off the
// left is a grid of numbers nobody can read. The totals column is pinned to
// the right of whatever is drawn, for the same reason. What scrolls is the
// data columns, by whole columns, as the board's do (A10).
func (p *matrixPage) layout() (labels, width, visible int) {
	labels = len([]rune(p.axesLabel()))
	for _, row := range p.drawn {
		if n := ansi.StringWidth(row.label); n > labels {
			labels = n
		}
	}
	labels = min(max(labels, minMatrixColumn), max(p.width/3, minMatrixColumn))

	n := len(p.cols) + 1 // the totals column is one more
	room := max(p.width-labels-matrixGap, minMatrixColumn)
	if n*minMatrixColumn+matrixGap*(n-1) <= room {
		p.colOffset = 0
		return labels, (room - matrixGap*(n-1)) / n, len(p.cols)
	}

	width = minMatrixColumn
	// one of the columns that fit is the totals column, which is always drawn
	visible = max((room+matrixGap)/(width+matrixGap)-1, 1)
	if p.col < p.colOffset {
		p.colOffset = p.col
	}
	if p.col < len(p.cols) && p.col >= p.colOffset+visible {
		p.colOffset = p.col - visible + 1
	}
	p.colOffset = min(max(p.colOffset, 0), max(len(p.cols)-visible, 0))
	return labels, width, min(visible, len(p.cols))
}

// axesLabel is the header's corner cell: the two axes, which is what names
// what the rows and the columns are.
func (p *matrixPage) axesLabel() string {
	return p.rowsKey + " \\ " + p.columnsKey
}

// headerLines are the column labels and the rule under them; ‹ and › at the
// ends say there are columns off screen.
func (p *matrixPage) headerLines(labels, width, visible int) []string {
	corner := pad(p.axesLabel(), labels)
	if p.colOffset > 0 {
		corner = pad(truncate(p.axesLabel(), labels-1), labels-1) + "‹"
	}

	cells := []string{styleHeader.Render(corner)}
	for c := p.colOffset; c < p.colOffset+visible; c++ {
		cells = append(cells, styleHeader.Render(padLeft(p.colAxis.values[p.cols[c]].label, width)))
	}
	cells = append(cells, styleDim.Render(padLeft("total", width)))

	header := strings.Join(cells, strings.Repeat(" ", matrixGap))
	if p.colOffset+visible < len(p.cols) {
		header += "›"
	}
	return []string{
		fit(header, p.width),
		styleDim.Render(fit(strings.Repeat("─", p.width), p.width)),
	}
}

// body draws every group's rows and says which line the cursor is on, so the
// window can be scrolled to it, and which group header line each line sits
// under (-1 for none), so the window can keep that header on its top line.
func (p *matrixPage) body(labels, width, visible int) (lines []string, groups []int, cursorLine int) {
	group := -2
	header := -1
	for at, row := range p.drawn {
		if p.groupBy != "" && row.kind != rowGrand && row.group != group {
			group = row.group
			header = len(lines)
			lines = append(lines, styleGroup.Render(fit(p.groups[group].label, p.width)))
			groups = append(groups, header)
		}
		if at == p.row {
			cursorLine = len(lines)
		}
		lines = append(lines, p.rowLine(row, labels, width, visible, at == p.row))
		groups = append(groups, header)
	}
	return lines, groups, cursorLine
}

// rowLine draws one row: its label, then a cell per drawn column, then the
// row's own total.
//
// A totals row and the totals column are dim, because they are derived and
// the data is what the eye should land on. The row under the cursor has the
// wash across the window and the cell under it is reversed, as a list's row
// and cell are; every piece is styled on its own, because a style ends in a
// reset and a reset inside the line would end the wash.
func (p *matrixPage) rowLine(row drawnRow, labels, width, visible int, under bool) string {
	wash := lipgloss.NewStyle()
	if under {
		wash = styleRow()
	}
	derived := row.kind != rowData

	label := wash.Faint(derived).Render(pad(row.label, labels))
	parts := []string{label}
	gap := wash.Render(strings.Repeat(" ", matrixGap))

	cell := func(sum cellSum, at int, dim bool) {
		text := padLeft(p.cellText(sum), width)
		style := wash.Faint(dim)
		if under && at == p.col {
			style = styleCell
		}
		parts = append(parts, gap, style.Render(text))
	}
	for c := p.colOffset; c < p.colOffset+visible; c++ {
		cell(row.cells[c], c, derived)
	}
	cell(row.total, len(p.cols), true)

	line := strings.Join(parts, "")
	used := labels + (visible+1)*(width+matrixGap)
	if under && p.width > used+1 {
		line += wash.Render(strings.Repeat(" ", p.width-used-1))
	}
	if under {
		line = wash.Render("›") + line
	} else {
		line = " " + line
	}
	return fit(line, p.width)
}

// bottom is the filter when one is being typed, and the status line, which
// is always the last line of the page.
func (p *matrixPage) bottom() []string {
	var lines []string
	if p.filtering != nil {
		lines = []string{fit("/"+p.filtering.View(), p.width)}
	}
	return append(lines, p.statusLine())
}

func (p *matrixPage) statusLine() string {
	count := fmt.Sprintf("%d issues", p.count)
	if p.count == 1 {
		count = "1 issue"
	}
	if p.filter != "" {
		count = fmt.Sprintf("%d×%d of %d×%d · /%s",
			len(p.rows), len(p.cols), len(p.rowAxis.values), len(p.colAxis.values), p.filter)
	}

	return styleStatus.Render(bottomLine(p.hintLine(), lastAction(p.status, count), p.width))
}

// hintLine is what the keys do where the cursor is (hints.go).
//
// Space is never named: a sum is not a value, and it rings. What enter opens
// is the cell's issues, and on a total the cell with the clause it dropped
// dropped too — the row, the column, or the lot.
func (p *matrixPage) hintLine() string {
	if p.filtering != nil {
		return filterHints()
	}
	if p.row >= len(p.drawn) {
		return hints()
	}

	data, whole := p.drawn[p.row].kind == rowData, p.col >= len(p.cols)
	what := "open issues"
	switch {
	case data && whole:
		what = "open row"
	case !data && !whole:
		what = "open column"
	case !data && whole:
		what = "open all"
	}
	return hints(hint{"enter", what}, hint{"/", "narrow axes"})
}

// padLeft fits a string to a width, right-aligned, because a column of
// numbers is read down its last digit and a header has to sit over it.
func padLeft(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}
