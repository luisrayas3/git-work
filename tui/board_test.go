package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/rank"
	"github.com/git-bug/git-bug/view"
)

// board builds the board page a set of keyword arguments describes.
func board(t *testing.T, repo *cache.RepoCache, kwargs string) *boardPage {
	t.Helper()

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))

	call, err := view.Parse(view.KindBoard, values)
	require.NoError(t, err)

	page, err := newBoardPage(repo, call)
	require.NoError(t, err)

	// wide enough for the six statuses at the default column width, so that
	// a board the helper draws is the whole board (column_width, kinds.go)
	page.Update(tea.WindowSizeMsg{Width: 210, Height: 30})
	return page
}

// header is the column header line of a drawn board, for asserting the
// order the columns are in.
func header(p *boardPage) string {
	return strings.Split(plainView(p), "\n")[1]
}

// TestBoardHasAColumnPerValueInSchemaOrder: the columns are the field's
// values as the schema orders them, counts in the header, a card in its
// column, and a listed column is drawn when it is empty.
func TestBoardHasAColumnPerValueInSchemaOrder(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "write the renderer", "status": "in-progress"})
	newIssue(t, repo, map[string]any{"title": "read the design", "status": "done"})
	newIssue(t, repo, map[string]any{"title": "ship it", "status": "done"})

	page := board(t, repo, `{"columns":"status"}`)
	drawn := plainView(page)
	head := header(page)

	for _, label := range []string{"Backlog (0)", "To Do (0)", "In Progress (1)", "In Review (0)", "Done (2)", "Canceled (0)"} {
		require.Contains(t, head, label)
	}
	require.Less(t, indexOf(head, "Backlog"), indexOf(head, "To Do"))
	require.Less(t, indexOf(head, "In Progress"), indexOf(head, "Done"))
	require.Contains(t, drawn, "write the")
	require.Contains(t, drawn, "3 issues")
	require.NotContains(t, drawn, "card=", "the cards are on the screen, so the call line does not say them")

	// a card sits under its column: the title is drawn to the right of the
	// column's left edge and left of the next one's
	lines := strings.Split(drawn, "\n")
	var titleLine string
	for _, line := range lines {
		if strings.Contains(line, "write the") {
			titleLine = line
			break
		}
	}
	require.Greater(t, indexOf(titleLine, "write the"), indexOf(head, "In Progress")-2)
	require.Less(t, indexOf(titleLine, "write the"), indexOf(head, "In Review"))
}

// TestBoardValuesOrderAndTrailingColumns: `values` narrows and orders the
// columns; a value it omits that the data has trails, and so does (none)
// for the cards with no value, because a board that drops issues is worse
// than one with a ragged edge.
func TestBoardValuesOrderAndTrailingColumns(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "doing", "status": "in-progress"})
	newIssue(t, repo, map[string]any{"title": "shipped", "status": "done"})
	newIssue(t, repo, map[string]any{"title": "unfiled"})

	page := board(t, repo, `{"columns":"status","values":["done","in-progress"]}`)
	head := header(page)

	require.Less(t, indexOf(head, "Done"), indexOf(head, "In Progress"))
	require.NotContains(t, head, "Backlog")
	require.Contains(t, head, noGroup+" (1)")
	require.Less(t, indexOf(head, "In Progress"), indexOf(head, noGroup))
	require.Contains(t, plainView(page), "unfiled")

	page = board(t, repo, `{"columns":"status","values":["done"]}`)
	head = header(page)
	require.Less(t, indexOf(head, "Done"), indexOf(head, "In Progress"), "the value the data has trails")
	require.Less(t, indexOf(head, "In Progress"), indexOf(head, noGroup))
	require.Contains(t, plainView(page), "values=", "values that were chosen are on the call line")
}

// TestBoardCursorMovesBetweenColumnsAndCards: the cursor starts on the first
// card, left and right skip empty columns, up and down stay in the column.
func TestBoardCursorMovesBetweenColumnsAndCards(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "first", "status": "to-do"})
	second := newIssue(t, repo, map[string]any{"title": "second", "status": "to-do"})
	done := newIssue(t, repo, map[string]any{"title": "done", "status": "done"})

	page := board(t, repo, `{"columns":"status"}`)
	// the default query is last edited first, so the second is on top
	require.Equal(t, second, page.currentId())

	page = send(page, "j").(*boardPage)
	require.Equal(t, first, page.currentId())
	page = send(page, "j").(*boardPage)
	require.Equal(t, first, page.currentId(), "down at the bottom stays")

	// right skips the two empty columns between to-do and done
	page = send(page, "l").(*boardPage)
	require.Equal(t, done, page.currentId())
	page = send(page, "l").(*boardPage)
	require.Equal(t, done, page.currentId(), "right past the last card stays")
	page = send(page, "h").(*boardPage)
	require.Equal(t, second, page.currentId(), "back on the row the cursor was clamped to")
	page = send(page, "k", "ctrl+n", "g").(*boardPage)
	require.Equal(t, second, page.currentId())
	page = send(page, "G").(*boardPage)
	require.Equal(t, first, page.currentId())
}

// TestBoardEnterOpensTheCard: a card has no cell; enter opens the issue.
func TestBoardEnterOpensTheCard(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := board(t, repo, `{"columns":"status"}`)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	pushed := cmd().(pushMsg)
	require.Equal(t, id, pushed.page.(*showPage).id)
}

// TestBoardGrabMovesAcrossColumnsWithoutARank: moving a card to another
// column is the board's reason to exist and needs no order; dropping it in
// (none) clears the field; dropping it where it was writes nothing.
func TestBoardGrabMovesAcrossColumnsWithoutARank(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "unfiled"})

	page := board(t, repo, `{"columns":"status"}`)
	require.Equal(t, id, page.currentId())

	page = send(page, "space").(*boardPage)
	require.GreaterOrEqual(t, page.grabbed, 0)
	// right moves into the neighbouring column, empty or not
	page = send(page, "l").(*boardPage)
	require.Contains(t, header(page), "In Progress (1)")
	page = send(page, "l", "enter").(*boardPage)
	require.Equal(t, -1, page.grabbed)
	require.Equal(t, "in-review", fieldOf(t, repo, id, "status"))
	require.Equal(t, id, page.currentId(), "the cursor follows the card")
	require.Contains(t, plainView(page), "moved to In Review")

	// dropped where it was picked up: nothing written
	before, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	page = send(page, "space", "l", "h", "space").(*boardPage)
	after, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	require.Len(t, after, len(before))

	// the last column is (none), and a drop there clears the field
	page = send(page, "space", "l", "l", "l", "l", "space").(*boardPage)
	require.Equal(t, "", fieldOf(t, repo, id, "status"))
	require.Equal(t, id, page.currentId())
}

// TestBoardGrabUpAndDownWritesARank: within a column the rank is the order,
// and a drop writes one key between the neighbours — and, across columns,
// both keys in one write.
func TestBoardGrabUpAndDownWritesARank(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "first", "status": "to-do", "rank": "a"})
	second := newIssue(t, repo, map[string]any{"title": "second", "status": "to-do", "rank": "b"})
	third := newIssue(t, repo, map[string]any{"title": "third", "status": "to-do", "rank": "c"})
	other := newIssue(t, repo, map[string]any{"title": "other", "status": "done", "rank": "m"})

	page := board(t, repo, `{"columns":"status"}`)
	require.Equal(t, first, page.currentId(), "the rank orders the column")

	page = send(page, "space", "j", "space").(*boardPage)
	moved := fieldOf(t, repo, first, "rank")
	expected, err := rank.Between("b", "c")
	require.NoError(t, err)
	require.Equal(t, expected, moved)
	require.Equal(t, "b", fieldOf(t, repo, second, "rank"))
	require.Equal(t, "c", fieldOf(t, repo, third, "rank"))
	require.Equal(t, []string{second, first, third}, stackIds(page, 0, 1))

	// across columns, the card lands at its row and gets a rank between its
	// new neighbours; both keys in one call
	page = send(page, "g", "space").(*boardPage)
	for range 3 {
		page = send(page, "l").(*boardPage)
	}
	page = send(page, "j", "space").(*boardPage)
	require.Equal(t, "done", fieldOf(t, repo, second, "status"))
	require.Greater(t, fieldOf(t, repo, second, "rank"), "m")
	require.Equal(t, []string{other, second}, stackIds(page, 0, 4))
}

// TestBoardGrabCanBePutBack: esc is the way out of a move nobody meant.
func TestBoardGrabCanBePutBack(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := board(t, repo, `{"columns":"status"}`)
	page = send(page, "space", "l", "l", "esc").(*boardPage)

	require.Equal(t, -1, page.grabbed)
	require.Equal(t, "to-do", fieldOf(t, repo, id, "status"))
	require.Equal(t, id, page.currentId())
	require.Contains(t, header(page), "To Do (1)")
}

// TestBoardRefreshFollowsTheCardToItsNewColumn: another process moved the
// card the cursor was on, and the cursor is still on it.
func TestBoardRefreshFollowsTheCardToItsNewColumn(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "two", "status": "to-do"})

	page := board(t, repo, `{"columns":"status"}`)
	page = send(page, "j").(*boardPage)
	require.Equal(t, id, page.currentId())

	_, err := host.IssueSet(repo, id, map[string]issue.Value{"status": issue.StringValue("done")}, false)
	require.NoError(t, err)
	updated, _ := page.Update(refreshMsg{})
	page = updated.(*boardPage)

	require.Equal(t, id, page.currentId())
	require.Equal(t, 4, page.col, "done is the fifth column")
	require.Contains(t, header(page), "Done (1)")
}

// TestBoardSwimlanes: group_by is a lane per value, the ungrouped last, and
// up and down cross the lanes within the column.
func TestBoardSwimlanes(t *testing.T) {
	repo := testRepo(t)
	low := newIssue(t, repo, map[string]any{"title": "low", "status": "to-do", "priority": "low"})
	none := newIssue(t, repo, map[string]any{"title": "none", "status": "to-do"})
	high := newIssue(t, repo, map[string]any{"title": "high", "status": "to-do", "priority": "high"})

	page := board(t, repo, `{"columns":"status","group_by":"priority"}`)
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, "\nhigh"), indexOf(drawn, "\nlow"), "lanes in first appearance order")
	require.Less(t, indexOf(drawn, "\nlow"), indexOf(drawn, "\n"+noGroup), "the ungrouped last")

	require.Equal(t, high, page.currentId())
	page = send(page, "j").(*boardPage)
	require.Equal(t, low, page.currentId())
	page = send(page, "j").(*boardPage)
	require.Equal(t, none, page.currentId())
	page = send(page, "k", "k").(*boardPage)
	require.Equal(t, high, page.currentId())
}

// TestBoardScrollsSideways: columns keep `column_width`, and the board
// scrolls by whole columns to keep the cursor's column on screen. Twenty
// cells in a fifty-wide window is two columns of the six, which is a scroll
// with something on either side of it.
func TestBoardScrollsSideways(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "early", "status": "backlog"})
	newIssue(t, repo, map[string]any{"title": "late", "status": "canceled"})

	page := board(t, repo, `{"columns":"status","column_width":20}`)
	page.Update(tea.WindowSizeMsg{Width: 50, Height: 20})

	head := header(page)
	require.Contains(t, head, "Backlog")
	require.NotContains(t, head, "Canceled")
	require.True(t, strings.HasSuffix(strings.TrimRight(head, " "), "›"), head)

	page = send(page, "l").(*boardPage)
	head = header(page)
	require.Contains(t, head, "Canceled")
	require.NotContains(t, head, "Backlog")
	require.True(t, strings.HasPrefix(head, "‹"), head)
	require.Contains(t, plainView(page), "late")
}

// TestBoardCopiesTheIdAndFilters: copy in any spelling is the id, because a
// card has no cell; / narrows the cards and esc clears it.
func TestBoardCopiesTheIdAndFilters(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "write the renderer", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "read the design", "status": "done"})

	page := board(t, repo, `{"columns":"status"}`)
	for _, spelling := range []string{"y", "ctrl+c", "alt+c", "Y"} {
		updated, cmd := page.Update(press(spelling))
		require.NotNil(t, cmd)
		require.Contains(t, plainView(updated), "copied "+id[:7])
	}

	page = send(page, "/", "r", "e", "n", "d", "enter").(*boardPage)
	drawn := plainView(page)
	require.Contains(t, drawn, "write the")
	require.NotContains(t, drawn, "read the")
	require.Contains(t, drawn, "1 of 2 issues · /rend")

	page = send(page, "esc").(*boardPage)
	require.Contains(t, plainView(page), "read the")
}

// TestBoardCardShowsItsFields: a card is the id, the title wrapped to the
// column, and each other card field on a line of its own.
func TestBoardCardShowsItsFields(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "the story", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "a title long enough to wrap onto a second line", "status": "to-do", "priority": "high", "parent": story})

	page := board(t, repo, `{"columns":"status","card":["title","priority","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	drawn := plainView(page)

	require.Contains(t, drawn, "priority: high")
	require.Contains(t, drawn, "parent: "+story[:7])
	require.Contains(t, drawn, "a title long enough")
	require.NotContains(t, drawn, "a title long enough to wrap onto a second line", "the title wraps")
}

// TestBoardColumnsComeFromTheTypesOnTheBoard: an iteration's statuses are
// not columns of a board of tasks, and a drop the schema refuses is a status
// line and nothing else.
func TestBoardColumnsComeFromTheTypesOnTheBoard(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})
	iteration, err := host.IssueNew(repo, host.IssueDocument{Fields: map[string]issue.Value{
		"type": issue.StringValue("iteration"), "title": issue.StringValue("sprint 1"), "status": issue.StringValue("active"),
	}})
	require.NoError(t, err)

	page := board(t, repo, `{"columns":"status","query":"map(select(.fields.type == \"task\"))"}`)
	require.NotContains(t, header(page), "Active")

	page = board(t, repo, `{"columns":"status"}`)
	labels := strings.Join(columnLabels(page), " ")
	require.Contains(t, labels, "Active", "an iteration on the board brings its columns")
	require.Less(t, indexOf(labels, "Canceled"), indexOf(labels, "Active"), "the first type's order wins")

	// the task cannot go where an iteration can: the schema says so, and
	// the card stays where it was
	page.putCursorOn(id)
	page = send(page, "space", "G").(*boardPage)
	for range 8 {
		page = send(page, "l").(*boardPage)
	}
	page = send(page, "space").(*boardPage)
	require.Contains(t, plainView(page), "not in the schema")
	require.Equal(t, "to-do", fieldOf(t, repo, id, "status"))
	require.Equal(t, id, page.currentId())
	require.Equal(t, "active", fieldOf(t, repo, iteration.String(), "status"))
}

// TestGroupedBoardKeepsTheLaneHeaderOnTop: a window too small for the board
// still opens on the header of the lane its first line is in, whether the
// cursor is on that lane's first card or deeper in it, because a lane header
// scrolled off the top cannot be reached and the cards under it lose the
// lane they are in.
func TestGroupedBoardKeepsTheLaneHeaderOnTop(t *testing.T) {
	repo := testRepo(t)
	for _, priority := range []string{"low", "medium", "high"} {
		// the titles say nothing of the priority, so only the lane's own
		// header can put it on the screen
		for _, title := range []string{"one", "two"} {
			newIssue(t, repo, map[string]any{"title": title, "status": "to-do", "priority": priority})
		}
	}

	page := board(t, repo, `{"columns":"status","group_by":"priority"}`)
	page.Update(tea.WindowSizeMsg{Width: 140, Height: 14})
	require.Len(t, page.lanes, 3)

	// the middle lane, and the two cards in it in the order they are drawn
	lane := page.lanes[1]
	var stack []int
	for _, cards := range lane.stacks {
		if len(cards) > 0 {
			stack = cards
		}
	}
	require.Len(t, stack, 2)
	first, second := page.cards[stack[0]].human, page.cards[stack[1]].human

	// down to the last card of the last lane: the window opens in the middle
	// of the lane above, so its header is the first body line over cards
	// that are not the ones it starts with
	page = send(page, "j", "j", "j", "j", "j").(*boardPage)
	drawn := plainView(page)
	lines := strings.Split(drawn, "\n")
	require.Contains(t, lines[3], lane.group, "the window opens on the lane's header")
	require.NotContains(t, drawn, first, "the lane's first card is above the window")
	require.Contains(t, drawn, second, "its second is in it")
	require.Contains(t, drawn, page.currentId()[:idWidth], "the cursor's card is on screen")

	// and back up onto that lane's first card, the header is still above it
	page = send(page, "k", "k", "k").(*boardPage)
	require.Equal(t, page.cards[stack[0]].id, page.currentId())
	lines = strings.Split(plainView(page), "\n")
	require.Contains(t, lines[3], lane.group, "the lane's header is the first body line")
	require.Contains(t, lines[4], first, "the cursor's card is right under it")
}

func columnLabels(p *boardPage) []string {
	labels := make([]string, 0, len(p.columns))
	for _, column := range p.columns {
		labels = append(labels, column.label)
	}
	return labels
}

func stackIds(p *boardPage, lane, col int) []string {
	ids := make([]string, 0)
	for _, index := range p.lanes[lane].stacks[col] {
		ids = append(ids, p.cards[index].id)
	}
	return ids
}

// TestBoardColumnWidthIsAnArgument: a column is 32 cells by default, wide
// enough that a title reads as a title, so six statuses no longer fit the
// 140-wide window that fit them at the old 20-cell minimum and the board
// scrolls instead; `column_width` is how the narrower column is asked for,
// and only a width somebody chose reaches the call line (10f676e).
func TestBoardColumnWidthIsAnArgument(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "early", "status": "backlog"})

	page := board(t, repo, `{"columns":"status"}`)
	page.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	require.Len(t, page.columns, 6)

	width, visible := page.layout()
	require.Equal(t, 32, width)
	require.Equal(t, 4, visible)
	head := header(page)
	require.True(t, strings.HasSuffix(strings.TrimRight(head, " "), "›"), head)
	require.NotContains(t, plainView(page), "column_width", "the default stays off the call line")

	page = board(t, repo, `{"columns":"status","column_width":20}`)
	page.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	_, visible = page.layout()
	require.Equal(t, 6, visible, "six 20-cell columns and their gaps fit 140")
	head = header(page)
	require.Contains(t, head, "Backlog")
	require.Contains(t, head, "Canceled")
	require.False(t, strings.HasSuffix(strings.TrimRight(head, " "), "›"), head)
	require.Contains(t, plainView(page), "column_width=20")
}
