package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// withDates gives the jira preset's task and story the fields a chart is
// drawn from: the preset has a due date and no span, and the tracker's
// tasks have none either, so a test binds its own.
func withDates(t *testing.T, repo *cache.RepoCache) {
	t.Helper()

	doc, err := schema.ParseDocument([]byte(`
types:
  task:
    fields:
      start: {kind: date, name: Start}
      stop: {kind: date, name: Stop}
      progress: {kind: number, name: Progress}
  story:
    fields:
      start: {kind: date, name: Start}
      stop: {kind: date, name: Stop}
`))
	require.NoError(t, err)
	_, _, err = host.SchemaImport(repo, doc, false, false)
	require.NoError(t, err)
}

// newTyped creates an issue of a type, and returns its id.
func newTyped(t *testing.T, repo *cache.RepoCache, typeKey string, fields map[string]any) string {
	t.Helper()

	values := map[string]issue.Value{"type": issue.StringValue(typeKey)}
	for key, value := range fields {
		values[key] = issue.MustValue(value)
	}
	id, err := host.IssueNew(repo, host.IssueDocument{Fields: values, Body: "the body"})
	require.NoError(t, err)
	return id.String()
}

// gantt builds the gantt page a set of keyword arguments describes, with
// today fixed so that the chart's now marker and its empty extent are
// known.
func gantt(t *testing.T, repo *cache.RepoCache, kwargs string) *ganttPage {
	t.Helper()

	was := today
	today = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { today = was })

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))

	call, err := view.Parse(view.KindGantt, values)
	require.NoError(t, err)

	page, err := newGanttPage(repo, call)
	require.NoError(t, err)

	page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return page
}

// rowOf is the drawn line holding an issue's short id.
func rowOf(p page, id string) string {
	for _, line := range strings.Split(plainView(p), "\n") {
		if strings.Contains(line, id[:idWidth]) {
			return line
		}
	}
	return ""
}

// field reads one stored field back, as JSON.
func field(t *testing.T, repo *cache.RepoCache, id, key string) string {
	t.Helper()
	snap, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	return string(snap.Fields[key])
}

// TestGanttDrawsBarsBetweenStartAndStop: a row per issue, a bar over the
// periods from its start to its stop, the header the month and the
// weeks, and the call line naming the two dates.
func TestGanttDrawsBarsBetweenStartAndStop(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	a := newIssue(t, repo, map[string]any{"title": "write the renderer", "start": "2026-09-07", "stop": "2026-09-20"})
	b := newIssue(t, repo, map[string]any{"title": "ship it", "start": "2026-09-21", "stop": "2026-09-21"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","query":"sort_by(.fields.start)"}`)
	drawn := plainView(page)
	lines := strings.Split(drawn, "\n")

	require.Contains(t, lines[0], "start=start")
	require.Contains(t, lines[0], "stop=stop")
	require.Contains(t, lines[1], "Sep 2026")
	require.Contains(t, lines[2], "id")
	for _, day := range []string{" 7 ", "14 ", "21 "} {
		require.Contains(t, lines[2], day)
	}
	require.Contains(t, lines[3], "┼")
	require.Contains(t, lines[3], "▼", "today's period is marked on the rule")

	require.Equal(t, 3, len(page.periods), "the chart is the data's extent, in weeks")
	require.Contains(t, rowOf(page, a), "write the renderer")
	require.Contains(t, rowOf(page, a), strings.Repeat("▓", 6), "two weeks of three cells")
	require.NotContains(t, rowOf(page, a), strings.Repeat("▓", 7))
	require.Contains(t, rowOf(page, b), strings.Repeat("▓", 3))
	require.NotContains(t, rowOf(page, b), strings.Repeat("▓", 4))
	require.Contains(t, drawn, "2 issues")
}

// TestGanttCursorIsACell: the cursor opens on the first row where its bar
// starts, left and right move it a period, up and down a row with the
// period kept, and it never leaves the chart.
func TestGanttCursorIsACell(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "first", "start": "2026-09-14", "stop": "2026-09-27"})
	newIssue(t, repo, map[string]any{"title": "second", "start": "2026-09-07", "stop": "2026-09-13"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","query":"sort_by(.fields.title)"}`)
	require.Equal(t, 0, page.cursor)
	require.Equal(t, 1, page.col, "the first row's bar starts on the second week")

	send(page, "right")
	require.Equal(t, 2, page.col)
	send(page, "down")
	require.Equal(t, 1, page.cursor)
	require.Equal(t, 2, page.col, "the column is kept across rows")
	send(page, "right", "right")
	require.Equal(t, 2, page.col, "the chart ends")
	send(page, "left", "left", "left", "left")
	require.Equal(t, 0, page.col)
	send(page, "up", "up")
	require.Equal(t, 0, page.cursor)
}

// TestGanttGrabShiftsTheBar: grabbed, the bar moves a period a step — on
// its first cell the start, on its last the stop, between them both — and
// the drop writes what moved in one call; dropped where it was picked up,
// nothing is written.
func TestGanttGrabShiftsTheBar(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "the task", "start": "2026-09-07", "stop": "2026-09-20"})

	page := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	require.Equal(t, 0, page.col)

	// on the first cell: only the start moves
	send(page, "space", "right", "enter")
	require.Equal(t, `"2026-09-14"`, field(t, repo, id, "start"))
	require.Equal(t, `"2026-09-20"`, field(t, repo, id, "stop"))
	require.Equal(t, "moved", page.status)
	require.Equal(t, 0, page.col, "the chart shrank to the bar and the cursor followed its start")

	// one cell: right grows the stop
	send(page, "space", "right", "enter")
	require.Equal(t, `"2026-09-14"`, field(t, repo, id, "start"))
	require.Equal(t, `"2026-09-27"`, field(t, repo, id, "stop"))

	// one cell: left grows the start
	send(page, "left", "space", "left", "enter")
	require.Equal(t, `"2026-09-07"`, field(t, repo, id, "start"))
	require.Equal(t, 3, len(page.periods))
	require.Equal(t, 0, page.col)

	// in the middle: both move and the bar keeps its length
	send(page, "right", "space", "right", "right", "enter")
	require.Equal(t, `"2026-09-21"`, field(t, repo, id, "start"))
	require.Equal(t, `"2026-10-11"`, field(t, repo, id, "stop"))

	// on the last cell: only the stop moves, down to one cell, and from
	// there left grows the start
	send(page, "right", "right", "space", "left", "left", "left", "enter")
	require.Equal(t, `"2026-09-14"`, field(t, repo, id, "start"))
	require.Equal(t, `"2026-09-27"`, field(t, repo, id, "stop"))

	// dropped where it was: nothing written
	log, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	send(page, "space", "right", "left", "enter")
	after, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	require.Equal(t, len(log), len(after))
}

// TestGanttGrabCanBePutBack: esc puts a grabbed bar back on its stored
// dates, and nothing is written.
func TestGanttGrabCanBePutBack(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "the task", "start": "2026-09-07", "stop": "2026-09-20"})

	page := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	before := plainView(page)
	send(page, "right", "space", "right", "right")
	require.NotEqual(t, before, plainView(page))
	send(page, "esc")
	require.Equal(t, -1, page.grabbed)
	require.Equal(t, before, plainView(page))
	require.Equal(t, `"2026-09-07"`, field(t, repo, id, "start"))
}

// TestGanttMilestonesAndDatelessRows: one date is a milestone, and a row
// with none has nothing to move.
func TestGanttMilestonesAndDatelessRows(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	stone := newIssue(t, repo, map[string]any{"title": "the release", "start": "2026-09-14"})
	bare := newIssue(t, repo, map[string]any{"title": "someday"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","query":"sort_by(.fields.title)"}`)
	require.Contains(t, rowOf(page, stone), "◆")
	require.NotContains(t, rowOf(page, bare), "◆")
	require.NotContains(t, rowOf(page, bare), "▓")

	require.Equal(t, bare, page.current().id)
	send(page, "space", "right")
	require.Equal(t, "no dates: nothing to move", page.status)
	send(page, "esc", "down", "space", "right", "enter")
	require.Equal(t, `"2026-09-21"`, field(t, repo, stone, "start"))
	require.Equal(t, "", field(t, repo, stone, "stop"))
}

// TestGanttProgressFillsTheBar: with `progress` bound the bar is done for
// that fraction of its cells and rest for the remainder.
func TestGanttProgressFillsTheBar(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "half", "start": "2026-09-07", "stop": "2026-09-20", "progress": 0.5})

	page := gantt(t, repo, `{"start":"start","stop":"stop","progress":"progress"}`)
	require.Contains(t, rowOf(page, id), strings.Repeat("▓", 3)+strings.Repeat("░", 3))
}

// TestGanttGrabUpAndDownNeedsARank: reordering rows is a rank, so without
// one it says so; with one the drop writes a key between the neighbours,
// alongside the dates when both moved.
func TestGanttGrabUpAndDownNeedsARank(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	first := newIssue(t, repo, map[string]any{"title": "first", "start": "2026-09-07", "stop": "2026-09-13", "rank": "a"})
	second := newIssue(t, repo, map[string]any{"title": "second", "start": "2026-09-07", "stop": "2026-09-13", "rank": "b"})

	page := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	send(page, "space", "down")
	require.Equal(t, "no rank: cannot reorder", page.status)
	send(page, "esc")

	page = gantt(t, repo, `{"start":"start","stop":"stop","rank":"rank"}`)
	require.Equal(t, first, page.current().id)
	send(page, "down", "space", "up", "right", "enter")
	require.Equal(t, second, page.current().id)
	require.Equal(t, 0, page.cursor, "second is now first")
	rank := field(t, repo, second, "rank")
	require.Less(t, rank, `"a"`)
	require.Equal(t, `"2026-09-07"`, field(t, repo, second, "start"))
	require.Equal(t, `"2026-09-20"`, field(t, repo, second, "stop"), "a one-cell bar grows to the right")
	require.Equal(t, "moved", page.status)

	// the rank and the date were one drop: one operation per key, one commit
	log, err := host.IssueLog(repo, second)
	require.NoError(t, err)
	require.Equal(t, 3, len(log), "the create, the stop and the rank")
}

// TestGanttNestsRowsUnderRows: with `expand` the rows are a tree, a parent
// with no dates of its own draws the envelope of its children's, z folds
// it — the envelope staying — and tab and shift-tab walk the levels.
func TestGanttNestsRowsUnderRows(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "start": "2026-09-07", "stop": "2026-09-13", "parent": story})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "start": "2026-09-21", "stop": "2026-09-27", "parent": story})

	page := gantt(t, repo, `{"start":"start","stop":"stop","expand":"children","query":"map(select(.fields.type == \"story\"))"}`)
	drawn := plainView(page)
	require.Contains(t, drawn, "3 issues")
	require.Contains(t, rowOf(page, story), "▾")
	require.Contains(t, rowOf(page, story), strings.Repeat("═", 9), "the envelope spans the three weeks")
	require.Contains(t, rowOf(page, one), "  "+one[:idWidth], "a child is indented")
	require.Contains(t, rowOf(page, one), "▓▓▓")
	require.Equal(t, story, page.current().id)
	require.Equal(t, 0, page.col, "the envelope starts the chart")

	send(page, "z")
	drawn = plainView(page)
	require.Contains(t, drawn, "1 issue")
	require.Contains(t, rowOf(page, story), "▸")
	require.Contains(t, rowOf(page, story), strings.Repeat("═", 9), "folded, the envelope stays")
	require.Equal(t, "", rowOf(page, two))

	send(page, "tab")
	require.Equal(t, one, page.current().id, "tab unfolds and enters")
	send(page, "down")
	require.Equal(t, two, page.current().id)
	send(page, "shift+tab")
	require.Equal(t, story, page.current().id)
}

// TestGanttScrollsSideways: when the periods do not fit the chart scrolls
// by whole periods to keep the cursor's on screen, and the header says
// there is more.
func TestGanttScrollsSideways(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "long", "start": "2026-01-05", "stop": "2026-12-20"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","scale":"day"}`)
	page.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Contains(t, strings.Split(plainView(page), "\n")[2], "›")
	require.NotContains(t, strings.Split(plainView(page), "\n")[2], "‹")

	for range 40 {
		send(page, "right")
	}
	lines := strings.Split(plainView(page), "\n")
	require.Contains(t, lines[2], "‹")
	require.Greater(t, page.colOffset, 0)
	require.Contains(t, lines[1], "Feb 2026")
}

// TestGanttScalesAndExtent: `scale` picks the period and `from`/`to` the
// window, a bad date is refused at once, and a stored time keeps its clock
// when it is shifted.
func TestGanttScalesAndExtent(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "the task", "start": "2026-02-10", "stop": "2026-08-01"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","scale":"month"}`)
	require.Equal(t, 7, len(page.periods))
	lines := strings.Split(plainView(page), "\n")
	require.Contains(t, lines[1], "2026")
	require.Contains(t, lines[2], "Feb")
	require.Contains(t, lines[2], "Aug")

	page = gantt(t, repo, `{"start":"start","stop":"stop","scale":"quarter","from":"2026-01-01","to":"2026-12-31"}`)
	require.Equal(t, 4, len(page.periods))
	require.Contains(t, strings.Split(plainView(page), "\n")[2], "Q1")
	require.Contains(t, strings.Split(plainView(page), "\n")[0], "from=2026-01-01")

	call, err := view.Parse(view.KindGantt, map[string]json.RawMessage{
		"start": json.RawMessage(`"start"`), "stop": json.RawMessage(`"stop"`), "from": json.RawMessage(`"someday"`),
	})
	require.NoError(t, err)
	_, err = newGanttPage(repo, call)
	require.ErrorContains(t, err, "from")
	require.ErrorContains(t, err, "not a date")

	require.Equal(t, "2026-09-15T10:00:00Z", shiftText("2026-09-08T10:00:00Z", "week", 1))
	require.Equal(t, "2026-12-31", shiftText("2026-01-31", "month", 11))
	require.Equal(t, "2026-07-01", periodStart(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "quarter").Format(time.DateOnly))
	require.Equal(t, "2026-09-21", periodStart(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC), "week").Format(time.DateOnly))
}

// TestGanttGroupsEnterAndCopy: group_by starts a section, enter opens the
// row's issue, copy copies its id and a paste has nowhere to go.
func TestGanttGroupsEnterAndCopy(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "the task", "start": "2026-09-07", "stop": "2026-09-13", "status": "in-progress"})
	newIssue(t, repo, map[string]any{"title": "the other", "start": "2026-09-07", "stop": "2026-09-13", "status": "done"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","group_by":"status","query":"sort_by(.fields.status)"}`)
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, "done"), indexOf(drawn, "in-progress"))

	send(page, "down")
	require.Equal(t, id, page.current().id)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	msg := cmd()
	pushed, ok := msg.(pushMsg)
	require.True(t, ok)
	shown, ok := pushed.page.(*showPage)
	require.True(t, ok)
	require.Equal(t, id, shown.id)

	send(page, "ctrl+c")
	require.Equal(t, "copied "+id, page.status)
	page.Update(tea.PasteMsg{Content: "x"})
	require.Equal(t, "nothing to paste into", page.status)
}

// TestGanttGroupsByRelationTitleAndTint: a relation's group is headed by the
// issue it names, and each group's bars get their own color.
func TestGanttGroupsByRelationTitleAndTint(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	north := newTyped(t, repo, "story", map[string]any{"title": "north"})
	south := newTyped(t, repo, "story", map[string]any{"title": "south"})
	newTyped(t, repo, "task", map[string]any{"title": "one", "start": "2026-09-07", "stop": "2026-09-13", "parent": north})
	newTyped(t, repo, "task", map[string]any{"title": "two", "start": "2026-09-14", "stop": "2026-09-20", "parent": south})

	page := gantt(t, repo, `{"start":"start","stop":"stop","group_by":"parent","query":"map(select(.fields.type == \"task\"))"}`)
	drawn := plainView(page)
	require.Contains(t, drawn, "\n"+north[:idWidth]+" north\n")
	require.Contains(t, drawn, "\n"+south[:idWidth]+" south\n")

	colors := page.groupColors()
	require.Len(t, colors, 2)
	require.NotEqual(t, colors[north[:idWidth]+" north"], colors[south[:idWidth]+" south"])
}

// TestGanttRefreshKeepsTheCursorOnTheIssue: another writer moves the bar,
// the refresh keeps the cursor on it, and a grab in flight is let go.
func TestGanttRefreshKeepsTheCursorOnTheIssue(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	first := newIssue(t, repo, map[string]any{"title": "first", "start": "2026-09-07", "stop": "2026-09-13"})
	second := newIssue(t, repo, map[string]any{"title": "second", "start": "2026-09-14", "stop": "2026-09-20"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","query":"sort_by(.fields.start)"}`)
	send(page, "down")
	require.Equal(t, second, page.current().id)

	_, err := host.IssueSet(repo, second, map[string]issue.Value{"start": issue.StringValue("2026-08-31")}, false)
	require.NoError(t, err)
	send(page, "space")
	page.Update(refreshMsg{})
	require.Equal(t, second, page.current().id)
	require.Equal(t, 0, page.cursor, "second now starts first")
	require.Equal(t, -1, page.grabbed)
	require.Equal(t, "store changed: grab released", page.status)
	require.Equal(t, first, page.bars[page.order[1]].id)
}
