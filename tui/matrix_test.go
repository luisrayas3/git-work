package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/view"
)

// tasksOnly is the query every matrix here runs: the fixtures hold stories
// and iterations too, and they are the axes, not the rows.
const tasksOnly = `map(select(.fields.type == \"task\"))`

// matrix builds the matrix page a set of keyword arguments describes.
func matrix(t *testing.T, repo *cache.RepoCache, kwargs string) *matrixPage {
	t.Helper()

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))

	call, err := view.Parse(view.KindMatrix, values)
	require.NoError(t, err)

	page, err := newMatrixPage(repo, call)
	require.NoError(t, err)

	page.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return page
}

// twoByTwo is the fixture most of these run on: two stories, two sprints,
// and three tasks, two of them in one cell.
func twoByTwo(t *testing.T, repo *cache.RepoCache) (alpha, beta, first, second string) {
	t.Helper()

	alpha = newIssue(t, repo, map[string]any{"type": "story", "title": "alpha"})
	beta = newIssue(t, repo, map[string]any{"type": "story", "title": "beta"})
	first = newIssue(t, repo, map[string]any{"type": "iteration", "title": "sprint 1"})
	second = newIssue(t, repo, map[string]any{"type": "iteration", "title": "sprint 2"})

	newIssue(t, repo, map[string]any{"title": "one", "parent": alpha, "iteration": first, "estimate": 3})
	newIssue(t, repo, map[string]any{"title": "two", "parent": alpha, "iteration": first, "estimate": 2})
	newIssue(t, repo, map[string]any{"title": "three", "parent": beta, "iteration": second, "estimate": 5})
	return alpha, beta, first, second
}

// pick finds the one label a needle names: the whole label when one is it,
// else the first that holds it, so that "High" is High and not Highest.
func pick(t *testing.T, labels []string, needle string) int {
	t.Helper()

	loose := -1
	for at, label := range labels {
		if label == needle {
			return at
		}
		if loose < 0 && strings.Contains(label, needle) {
			loose = at
		}
	}
	if loose < 0 {
		t.Fatalf("no label named %q among %v", needle, labels)
	}
	return loose
}

// matrixRowOf and matrixColOf are where a row and a column are, by label.
func matrixRowOf(t *testing.T, p *matrixPage, needle string) int {
	t.Helper()

	labels := make([]string, len(p.drawn))
	for at, drawn := range p.drawn {
		if drawn.kind == rowData {
			labels[at] = drawn.label
		}
	}
	return pick(t, labels, needle)
}

func matrixColOf(t *testing.T, p *matrixPage, needle string) int {
	t.Helper()

	labels := make([]string, len(p.cols))
	for at, c := range p.cols {
		labels[at] = p.colAxis.values[c].label
	}
	return pick(t, labels, needle)
}

// cellOf is the cell where a row and a column meet, as it draws.
func cellOf(t *testing.T, p *matrixPage, row, column string) string {
	t.Helper()
	return p.cellText(p.drawn[matrixRowOf(t, p, row)].cells[matrixColOf(t, p, column)])
}

// put moves the cursor onto the cell a row and a column meet in.
func put(t *testing.T, p *matrixPage, row, column string) {
	t.Helper()
	p.row, p.col = matrixRowOf(t, p, row), matrixColOf(t, p, column)
}

// TestMatrixSumsTheValueField: a cell is the sum of the number named, over
// the issues whose two axis values meet there; a cell with no issues is
// blank, and a cell with issues is a number even when nothing adds up.
func TestMatrixSumsTheValueField(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)
	newIssue(t, repo, map[string]any{"title": "no estimate"})

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)

	require.Equal(t, "5", cellOf(t, page, "alpha", "sprint 1"))
	require.Equal(t, "", cellOf(t, page, "alpha", "sprint 2"), "a cell with no issues is blank")
	require.Equal(t, "5", cellOf(t, page, "beta", "sprint 2"))
	// an issue with no number is still an issue, so its cell reads 0
	require.Equal(t, "0", cellOf(t, page, noGroup, noGroup))
}

// TestMatrixCountsWithNoValueField: `value` absent is a different view, not
// a different default — the cells count their issues.
func TestMatrixCountsWithNoValueField(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","query":"`+tasksOnly+`"}`)

	require.Equal(t, "2", cellOf(t, page, "alpha", "sprint 1"))
	require.Equal(t, "1", cellOf(t, page, "beta", "sprint 2"))
	require.Equal(t, "", cellOf(t, page, "beta", "sprint 1"))
}

// TestMatrixRelationAxesAreDrawnAsTitles: a relation axis is the issue it
// names, short id and title, and is ordered by the title rather than by the
// hash the label opens with.
func TestMatrixRelationAxesAreDrawnAsTitles(t *testing.T) {
	repo := testRepo(t)
	alpha, beta, first, second := twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	drawn := plainView(page)

	require.Contains(t, drawn, alpha[:idWidth])
	require.Contains(t, drawn, "alpha")
	require.Contains(t, drawn, "sprint 1")
	require.NotContains(t, drawn, alpha, "the whole hash is never drawn")

	require.Equal(t, []string{"alpha", "beta"},
		[]string{page.rowAxis.values[0].sort, page.rowAxis.values[1].sort})
	require.Equal(t, []string{"sprint 1", "sprint 2"},
		[]string{page.colAxis.values[0].sort, page.colAxis.values[1].sort})
	require.Equal(t, []string{beta, second},
		[]string{page.rowAxis.values[1].value, page.colAxis.values[1].value})

	// the header's corner names the two axes
	require.Contains(t, strings.Split(drawn, "\n")[1], `parent \ iteration`)
	require.NotContains(t, drawn, first[:idWidth]+" "+second[:idWidth])
}

// TestMatrixNoneIsLastOnBothAxes: the issues with no value on an axis get
// their own row and column, last, because a matrix that drops issues is
// worse than one with a ragged edge.
func TestMatrixNoneIsLastOnBothAxes(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)
	newIssue(t, repo, map[string]any{"title": "unfiled", "estimate": 7})

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)

	require.True(t, page.rowAxis.values[len(page.rowAxis.values)-1].none)
	require.True(t, page.colAxis.values[len(page.colAxis.values)-1].none)
	require.Equal(t, noGroup, page.rowAxis.values[len(page.rowAxis.values)-1].label)
	require.Equal(t, "7", cellOf(t, page, noGroup, noGroup))
}

// TestMatrixTotals: a totals row along the bottom, a totals column down the
// right, and the grand total in the corner, all of what is drawn.
func TestMatrixTotals(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)

	last := page.drawn[len(page.drawn)-1]
	require.Equal(t, rowGrand, last.kind)
	require.Equal(t, "10", page.cellText(last.total), "the grand total is every issue's estimate")
	require.Equal(t, "5", page.cellText(last.cells[0]), "the column totals")

	for _, row := range page.drawn {
		if row.kind == rowData && strings.Contains(row.label, "alpha") {
			require.Equal(t, "5", page.cellText(row.total), "the row totals")
		}
	}
	require.Contains(t, plainView(page), "total")
}

// TestMatrixGroupsAreBlocksOfRows: group_by splits the rows into blocks
// under one column header, each block ending in its own subtotal, the grand
// total last.
func TestMatrixGroupsAreBlocksOfRows(t *testing.T) {
	repo := testRepo(t)
	alpha := newIssue(t, repo, map[string]any{"type": "story", "title": "alpha"})
	sprint := newIssue(t, repo, map[string]any{"type": "iteration", "title": "sprint 1"})
	newIssue(t, repo, map[string]any{"title": "one", "parent": alpha, "iteration": sprint, "estimate": 3, "status": "done"})
	newIssue(t, repo, map[string]any{"title": "two", "parent": alpha, "iteration": sprint, "estimate": 2, "status": "to-do"})

	page := matrix(t, repo,
		`{"rows":"parent","columns":"iteration","value":"estimate","group_by":"status","query":"`+tasksOnly+`"}`)

	require.Len(t, page.groups, 2)
	subtotals := 0
	for _, row := range page.drawn {
		if row.kind == rowSubtotal {
			subtotals++
		}
	}
	require.Equal(t, 2, subtotals, "one subtotal per block")
	require.Equal(t, rowGrand, page.drawn[len(page.drawn)-1].kind)
	require.Equal(t, "5", page.cellText(page.drawn[len(page.drawn)-1].total))

	drawn := plainView(page)
	require.Contains(t, drawn, "done")
	require.Contains(t, drawn, "to-do")
	// one column header, shared by every block
	require.Equal(t, 1, strings.Count(drawn, `parent \ iteration`))
}

// TestMatrixEnterOpensTheCellsIssues: a sum you cannot open is a number you
// have to trust, so enter pushes the list its query selects — the predicate,
// which re-runs, not the ids that are in the cell today.
func TestMatrixEnterOpensTheCellsIssues(t *testing.T) {
	repo := testRepo(t)
	alpha, _, first, _ := twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	put(t, page, "alpha", "sprint 1")

	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	pushed := cmd().(pushMsg).page.(*listPage)

	require.Contains(t, pushed.query, alpha)
	require.Contains(t, pushed.query, first)
	require.Len(t, pushed.order, 2, "the two tasks that made the 5")
	require.Equal(t, []string{"title", "parent", "iteration", "estimate"}, pushed.fields)

	drawn := plainView(pushed)
	require.Contains(t, drawn, "one")
	require.Contains(t, drawn, "two")
	require.NotContains(t, drawn, "three")

	// the totals column opens the whole row: both of alpha's sprints
	page.col = len(page.cols)
	_, cmd = page.Update(press("enter"))
	whole := cmd().(pushMsg).page.(*listPage)
	require.Contains(t, whole.query, alpha)
	require.NotContains(t, whole.query, first, "a total drops the clause it totals")
}

// TestMatrixNoneCellSelectsTheIssuesWithNoValue: (none) is null, not a value.
func TestMatrixNoneCellSelectsTheIssuesWithNoValue(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)
	newIssue(t, repo, map[string]any{"title": "unfiled"})

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","query":"`+tasksOnly+`"}`)
	put(t, page, noGroup, noGroup)

	_, cmd := page.Update(press("enter"))
	pushed := cmd().(pushMsg).page.(*listPage)
	require.Contains(t, pushed.query, "== null")
	require.Len(t, pushed.order, 1)
	require.Contains(t, plainView(pushed), "unfiled")
}

// TestMatrixSpaceRings: a cell is a sum, and a sum is not a value.
func TestMatrixSpaceRings(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	updated, cmd := page.Update(press("space"))
	require.NotNil(t, cmd, "the bell is a command")
	require.Contains(t, plainView(updated), "a sum is not editable")
}

// TestMatrixCopy: copy takes the number under the cursor, and copy-id — a
// cell has none — takes the command that lists the cell's issues.
func TestMatrixCopy(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	put(t, page, "alpha", "sprint 1")

	updated, cmd := page.Update(press("ctrl+c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(updated), "copied 5")

	updated, cmd = page.Update(press("alt+c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(updated), "copied the cell's command")
}

// TestMatrixFilterNarrowsTheAxes: hiding issues would change every sum on
// the screen while the totals still read as totals, so the filter narrows
// the rows and the columns — and leaves whole the axis nothing matches.
func TestMatrixFilterNarrowsTheAxes(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	page = send(page, "/", "a", "l", "p", "h", "a", "enter").(*matrixPage)

	require.Len(t, page.rows, 1, "one row matches")
	require.Len(t, page.cols, 2, "no column matches, so the columns are left whole")
	require.Contains(t, plainView(page), "1×2 of 2×2")

	// the totals are of what is drawn
	require.Equal(t, "5", page.cellText(page.drawn[len(page.drawn)-1].total))

	page = send(page, "esc").(*matrixPage)
	require.Len(t, page.rows, 2)
	require.Equal(t, "10", page.cellText(page.drawn[len(page.drawn)-1].total))
}

// TestMatrixScrollsSidewaysByWholeColumns: the row labels and the totals
// column stay put, and the cursor's column is brought on screen.
func TestMatrixScrollsSidewaysByWholeColumns(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "alpha"})
	for _, name := range []string{"sprint 1", "sprint 2", "sprint 3", "sprint 4", "sprint 5", "sprint 6"} {
		sprint := newIssue(t, repo, map[string]any{"type": "iteration", "title": name})
		newIssue(t, repo, map[string]any{"title": name + " work", "parent": story, "iteration": sprint, "estimate": 1})
	}

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	page.Update(tea.WindowSizeMsg{Width: 50, Height: 20})

	require.Contains(t, plainView(page), "›", "there are columns off the right")
	require.Equal(t, 0, page.colOffset)

	page.col = 5
	drawn := plainView(page)
	_, _, visible := page.layout()
	require.Greater(t, page.colOffset, 0, "the cursor's column is brought on screen")
	require.Less(t, page.col, page.colOffset+visible)
	require.Contains(t, drawn, "‹")
	require.Contains(t, drawn, "total", "the totals column is pinned")
	require.Contains(t, drawn, "alpha", "the row labels are sticky")
}

// TestMatrixRefreshKeepsTheCursorOnTheSameCell: somebody else's write must
// not move the cell you were about to open, so the cursor is kept by the row
// and column values rather than by their places.
func TestMatrixRefreshKeepsTheCursorOnTheSameCell(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	put(t, page, "beta", "sprint 2")
	wasRow, wasCol := page.cursorKeys()
	require.Equal(t, 1, page.row)

	// another process opens a story and a sprint that both sort first
	early := newIssue(t, repo, map[string]any{"type": "story", "title": "aardvark"})
	sprint := newIssue(t, repo, map[string]any{"type": "iteration", "title": "sprint 0"})
	newIssue(t, repo, map[string]any{"title": "new", "parent": early, "iteration": sprint, "estimate": 1})

	updated, _ := page.Update(refreshMsg{})
	page = updated.(*matrixPage)

	require.Len(t, page.rowAxis.values, 3)
	require.Equal(t, 2, page.row, "the row moved down, the cursor stayed on beta")
	row, col := page.cursorKeys()
	require.Equal(t, wasRow, row)
	require.Equal(t, wasCol, col)
	require.Equal(t, "5", page.cellText(page.cell()))
}

// TestMatrixEnumAxisFollowsSchemaOrder: an enum axis is the field's values
// as the schema orders them, empty columns included, as a board's are.
func TestMatrixEnumAxisFollowsSchemaOrder(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "done", "priority": "high"})
	newIssue(t, repo, map[string]any{"title": "two", "status": "to-do", "priority": "low"})

	page := matrix(t, repo, `{"rows":"priority","columns":"status","query":"`+tasksOnly+`"}`)

	labels := make([]string, 0, len(page.colAxis.values))
	for _, value := range page.colAxis.values {
		labels = append(labels, value.label)
	}
	require.Equal(t, []string{"Backlog", "To Do", "In Progress", "In Review", "Done", "Canceled"}, labels)
	require.Equal(t, "1", cellOf(t, page, "High", "Done"))
	require.Equal(t, "", cellOf(t, page, "High", "Backlog"))

	// column_values orders and narrows what is drawn empty
	page = matrix(t, repo, `{"rows":"priority","columns":"status","column_values":["done"],"query":"`+tasksOnly+`"}`)
	require.Equal(t, "Done", page.colAxis.values[0].label)
	require.Equal(t, "To Do", page.colAxis.values[1].label, "a value the data has trails")
	require.Len(t, page.colAxis.values, 2)
}

// TestMatrixMultiValuedAxisDoubleCounts: an issue with two values on an axis
// lands on both rows, and the total says so; dividing the number between
// them would invent data the store does not have.
func TestMatrixMultiValuedAxisDoubleCounts(t *testing.T) {
	repo := testRepo(t)
	sprint := newIssue(t, repo, map[string]any{"type": "iteration", "title": "sprint 1"})
	newIssue(t, repo, map[string]any{"title": "both", "iteration": sprint,
		"labels": []any{"cli", "tui"}, "estimate": 4})

	page := matrix(t, repo, `{"rows":"labels","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)

	require.Equal(t, "4", cellOf(t, page, "cli", "sprint 1"))
	require.Equal(t, "4", cellOf(t, page, "tui", "sprint 1"))
	require.Equal(t, "8", page.cellText(page.drawn[len(page.drawn)-1].total))

	// and the cell's query is a membership, not an equality
	put(t, page, "cli", "sprint 1")
	_, cmd := page.Update(press("enter"))
	pushed := cmd().(pushMsg).page.(*listPage)
	require.Contains(t, pushed.query, "index(")
	require.Len(t, pushed.order, 1)
}

// TestMatrixIsOnTheCallLine: the two axes and the number are arguments the
// screen does not spell out as such, so the call line names them.
func TestMatrixIsOnTheCallLine(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	first := strings.SplitN(plainView(page), "\n", 2)[0]

	require.True(t, strings.HasPrefix(first, "matrix"), first)
	require.Contains(t, first, "rows=parent")
	require.Contains(t, first, "columns=iteration")
	require.Contains(t, first, "value=estimate")
}
