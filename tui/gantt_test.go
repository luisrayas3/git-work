package tui

import (
	"encoding/json"
	"image/color"
	"regexp"
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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"sort_by(.fields.start)"}`)
	drawn := plainView(page)
	lines := strings.Split(drawn, "\n")

	require.Contains(t, lines[0], "start=start")
	require.Contains(t, lines[0], "stop=stop")
	require.Contains(t, lines[1], "Sep 2026")
	require.Contains(t, lines[2], "id")
	for _, week := range []string{"37 ", "38 ", "39 "} {
		require.Contains(t, lines[2], week)
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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"sort_by(.fields.title)"}`)
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
	send(page, "left", "left", "left")
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
	require.Equal(t, "2026-09-07", page.periods[0].Format(time.DateOnly), "the chart grew back to the bar")
	require.Equal(t, 0, page.col)

	// in the middle: both move and the bar keeps its length
	send(page, "right", "space", "right", "right", "enter")
	require.Equal(t, `"2026-09-21"`, field(t, repo, id, "start"))
	require.Equal(t, `"2026-10-11"`, field(t, repo, id, "stop"))

	// on the last cell: only the stop moves, down to one cell, and from
	// there left grows the start
	send(page, "right", "space", "left", "left", "left", "enter")
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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07"}`)
	before := plainView(page)
	send(page, "right", "space", "right", "right")
	require.NotEqual(t, before, plainView(page))
	send(page, "esc")
	require.Equal(t, -1, page.grabbed)
	require.Equal(t, before, plainView(page))
	require.Equal(t, `"2026-09-07"`, field(t, repo, id, "start"))
}

// TestGanttMilestonesAndDatelessRows: one date is a milestone, and a row
// with none is a dull band with nothing to move.
func TestGanttMilestonesAndDatelessRows(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	stone := newIssue(t, repo, map[string]any{"title": "the release", "start": "2026-09-14"})
	bare := newIssue(t, repo, map[string]any{"title": "someday"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","to":"2026-10-05","query":"sort_by(.fields.title)"}`)
	require.Equal(t, 3, page.nowCol(), "today is the chart's fourth week")
	require.Contains(t, rowOf(page, stone), "│   ▓▓▒▒░░", "a start fades to the right")
	require.NotContains(t, rowOf(page, bare), "▓")
	require.Contains(t, rowOf(page, bare), "│"+strings.Repeat(" ", 9)+strings.Repeat("░", 6),
		"no dates: the band begins at today and runs to the chart's end")

	require.Equal(t, bare, page.current().id)
	send(page, "space", "right")
	require.Equal(t, "no dates: nothing to move", page.status)
	send(page, "esc", "down", "space", "right", "enter")
	require.Equal(t, `"2026-09-21"`, field(t, repo, stone, "start"))
	require.Equal(t, "", field(t, repo, stone, "stop"))
}

// TestGanttARowWithNoStartBandsFromToday: a row that has not started could
// start at any point from now on, so its band begins at today's period and
// runs to its stop; a stop already past draws its marker and a short band
// back from the stop to today, in the overdue tint (`04248c5`).
func TestGanttARowWithNoStartBandsFromToday(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	soon := newIssue(t, repo, map[string]any{"title": "not started", "stop": "2026-10-26"})
	late := newIssue(t, repo, map[string]any{"title": "overdue", "stop": "2026-09-14"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","to":"2026-11-02","query":"sort_by(.fields.title)"}`)
	require.Equal(t, 3, page.nowCol())

	// nothing before today, then the band, then the trail into the stop
	require.Contains(t, rowOf(page, soon),
		"│"+strings.Repeat(" ", 9)+strings.Repeat("░", 9)+"░░▒"+"▒▓▓"+"   ")
	// the stop that has gone by keeps its marker, and the band runs back
	// from it to today rather than from the chart's edge
	require.Contains(t, rowOf(page, late), "│░░▒▒▓▓"+strings.Repeat("░", 6)+"   ")

	// the cells between a stop that has gone by and today are the overdue
	// ones; the rest of the row is the band and the trail
	_, kind := milestoneGlyph(2, 0, 3, 1, 3, false)
	require.Equal(t, cellOverdue, kind, "a week after the stop, before today")
	_, kind = milestoneGlyph(3, 2, 3, 1, 3, false)
	require.Equal(t, cellOverdue, kind, "today's own period")
	_, kind = milestoneGlyph(4, 0, 3, 1, 3, false)
	require.Equal(t, cellBlank, kind, "past today: a stop is a stop")
	_, kind = milestoneGlyph(1, 2, 3, 1, 3, false)
	require.Equal(t, cellBar, kind, "the stop's own marker")

	// the chart's own extent counts a missing start as today
	first, last, _, ok := page.extent(page.order[1])
	require.True(t, ok)
	require.Equal(t, late, page.bars[page.order[1]].id)
	require.Equal(t, "2026-09-14", first.Format(time.DateOnly), "the stop that has gone by")
	require.Equal(t, "2026-09-28", last.Format(time.DateOnly), "today")
}

// TestGanttProgressFillsTheBar: with `progress` bound the bar is done for
// that fraction of its cells and rest for the remainder.
func TestGanttProgressFillsTheBar(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "half", "start": "2026-09-07", "stop": "2026-09-20", "progress": 0.5})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","progress":"progress"}`)
	require.Contains(t, rowOf(page, id), strings.Repeat("▓", 3)+strings.Repeat("░", 3))
}

// TestGanttGrabUpAndDownWritesARank: reordering rows is a rank, and the drop
// writes a key between the neighbours, alongside the dates when both moved.
func TestGanttGrabUpAndDownWritesARank(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	first := newIssue(t, repo, map[string]any{"title": "first", "start": "2026-09-07", "stop": "2026-09-13", "rank": "a"})
	second := newIssue(t, repo, map[string]any{"title": "second", "start": "2026-09-07", "stop": "2026-09-13", "rank": "b"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07"}`)
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
// with no dates of its own draws the envelope of its children's, folded or
// not; the arrow is the cell after the id, which ← reaches from the first
// period and space folds, and tab and shift-tab walk the levels, shift-tab
// onto the parent's arrow.
func TestGanttNestsRowsUnderRows(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "start": "2026-09-07", "stop": "2026-09-13", "parent": story})
	two := newTyped(t, repo, "task", map[string]any{"title": "two", "start": "2026-09-21", "stop": "2026-09-27", "parent": story})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","expand":"children","query":"map(select(.fields.type == \"story\"))"}`)
	drawn := plainView(page)
	require.Contains(t, drawn, "1 issue", "it opens folded")
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▸ 2", "the arrow follows the id, with the count")
	require.Contains(t, rowOf(page, story), strings.Repeat("═", 9), "folded, the envelope stays")
	require.Equal(t, "", rowOf(page, two))
	require.Equal(t, story, page.current().id)
	require.Equal(t, 0, page.col, "the envelope starts the chart")

	// ← from the first period is the arrow cell, where space unfolds
	send(page, "left", "space")
	drawn = plainView(page)
	require.Equal(t, -1, page.col)
	require.Contains(t, drawn, "3 issues")
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▾")
	require.Contains(t, rowOf(page, one), one[:idWidth]+"   ", "a child is indented")
	require.Contains(t, rowOf(page, one), "▓▓▓")

	send(page, "tab")
	require.Equal(t, one, page.current().id, "tab unfolds and enters")
	send(page, "down")
	require.Equal(t, two, page.current().id)
	send(page, "right", "shift+tab")
	require.Equal(t, story, page.current().id)
	require.Equal(t, -1, page.col, "shift-tab lands on the parent's arrow")
}

// TestGanttScrollsSideways: when the periods do not fit the chart scrolls
// by whole periods to keep the cursor's on screen, and the header says
// there is more.
func TestGanttScrollsSideways(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "long", "start": "2026-01-05", "stop": "2026-12-20"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-01-05","scale":"day"}`)
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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-02-01","scale":"month"}`)
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

	fine, coarse, _ := periodLabel(time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC), "week")
	require.Equal(t, "53", fine, "the ISO week, which 2026 has 53 of")
	require.Equal(t, "Dec 2026", coarse)
	fine, _, _ = periodLabel(time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC), "week")
	require.Equal(t, "1", fine)
}

// TestGanttGroupsEnterAndCopy: group_by starts a section, enter opens the
// row's issue, copy copies its id and a paste has nowhere to go.
func TestGanttGroupsEnterAndCopy(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	id := newIssue(t, repo, map[string]any{"title": "the task", "start": "2026-09-07", "stop": "2026-09-13", "status": "done"})
	newIssue(t, repo, map[string]any{"title": "the other", "start": "2026-09-07", "stop": "2026-09-13", "status": "in-progress"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"status","query":"sort_by(.fields.status)"}`)
	drawn := plainView(page)
	// the groups in schema order, whatever order the query gave
	require.Less(t, indexOf(drawn, "in-progress"), indexOf(drawn, "done"))

	// over the in-progress group's ghost (ghost.go)
	send(page, "down", "down")
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
	// the clipboard gets the whole id, the message the short one
	require.Equal(t, "copied "+id[:7], page.status)
	page.Update(tea.PasteMsg{Content: "x"})
	require.Equal(t, "nothing to paste into", page.status)
}

// TestGanttMilestonesTrailAwayFromTheirDate: a start fades from its date to
// its right, a stop to its left, into the neighboring periods, and the band
// runs on from the fade to the chart's edge on that side.
func TestGanttMilestonesTrailAwayFromTheirDate(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "the span", "start": "2026-09-07", "stop": "2026-10-25"})
	begins := newIssue(t, repo, map[string]any{"title": "begins", "start": "2026-09-14"})
	ends := newIssue(t, repo, map[string]any{"title": "ends", "stop": "2026-09-28"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07"}`)
	require.True(t, strings.HasSuffix(rowOf(page, begins), "│   ▓▓▒▒░░"+strings.Repeat("░", 12)), rowOf(page, begins))
	// the stop is today's own period, so there is no band before the trail:
	// a row with no start has not started, and today is where it could
	require.True(t, strings.HasSuffix(strings.TrimRight(rowOf(page, ends), " "), "│"+strings.Repeat(" ", 6)+"░░▒▒▓▓"), rowOf(page, ends))
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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"parent","query":"map(select(.fields.type == \"task\"))"}`)
	drawn := plainView(page)
	lines := strings.Split(drawn, "\n")
	cross := strings.Index(lines[2], "│") + 1 + 3
	require.Contains(t, lines, pad(north[:idWidth]+" north", cross), "the header runs to the crosshair's period")
	require.Contains(t, lines, pad(south[:idWidth]+" south", cross))

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

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"sort_by(.fields.start)"}`)
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

// TestGanttOpensOnToday: with neither `from` nor `to` the chart opens with
// today's period the left-most and the cursor on it, the past a scroll to
// the left and the future filling the window; `from` pins the left edge.
func TestGanttOpensOnToday(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "past", "start": "2026-09-07", "stop": "2026-09-13"})

	page := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	lines := strings.Split(plainView(page), "\n")
	require.Contains(t, lines[2], "‹40 41 42", "today's week first, the past off to the left")
	require.Contains(t, lines[3], "┼▼")
	require.Equal(t, "2026-09-28", page.periods[page.col].Format(time.DateOnly))
	require.Equal(t, page.col, page.colOffset)
	require.Greater(t, len(page.periods)-page.colOffset, 30, "the future fills the window")

	send(page, "left", "left", "left")
	require.Equal(t, 0, page.col, "the past is a scroll away")

	page = gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07"}`)
	require.Equal(t, 0, page.colOffset)
	require.Equal(t, 1, len(page.periods))
}

// TestGanttCursorCellReadsOnEitherBackground: the cell under the cursor is
// a shade of the background with the terminal's own foreground, never
// reversed, which on a light terminal was a black block over the glyph; on
// a bar, a milestone and an empty period alike.
func TestGanttCursorCellReadsOnEitherBackground(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	bar := newIssue(t, repo, map[string]any{"title": "a bar", "start": "2026-09-07", "stop": "2026-09-20"})
	stone := newIssue(t, repo, map[string]any{"title": "b stone", "start": "2026-09-14"})

	was := darkBackground
	t.Cleanup(func() { darkBackground = was })

	for _, dark := range []bool{false, true} {
		darkBackground = dark
		mark := "48;5;250m"
		if dark {
			mark = "48;5;240m"
		}
		page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","to":"2026-10-11","query":"sort_by(.fields.title)"}`)
		for _, at := range []struct {
			name        string
			row, col    int
			id, covered string
		}{
			{"a bar", 0, 0, bar, "▓▓▓"},
			{"an empty period", 0, 3, bar, "   "},
			{"a milestone", 1, 1, stone, "▓▓▒"},
		} {
			page.cursor, page.col = at.row, at.col
			var line string
			for _, l := range strings.Split(page.View(), "\n") {
				if strings.Contains(ansiPattern.ReplaceAllString(l, ""), at.id[:idWidth]) {
					line = l
				}
			}
			// what the mark covers, a milestone being styled a cell at a time
			var covered strings.Builder
			for _, m := range regexp.MustCompile(regexp.QuoteMeta("\x1b["+mark)+"([^\x1b]*)").FindAllStringSubmatch(line, -1) {
				covered.WriteString(m[1])
			}
			require.Equal(t, at.covered, covered.String(), "dark=%v, on %s: %q", dark, at.name, line)
			require.NotRegexp(t, `\x1b\[(\d+;)*7m`, line, "dark=%v, on %s: nothing reversed", dark, at.name)
		}
	}
}

// TestGanttCrosshairFollowsTheAnsweredBackground: the terminal's answer to
// the background query, reaching the root, decides the crosshair's shade:
// the period's wash down the other rows, the cursor row's across it and the
// period's label are a light shade on a light terminal and a dark one on a
// dark terminal, never the other's.
func TestGanttCrosshairFollowsTheAnsweredBackground(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{"title": "a bar", "start": "2026-09-07", "stop": "2026-09-20"})
	newIssue(t, repo, map[string]any{"title": "b other", "start": "2026-09-14", "stop": "2026-09-27"})

	was := darkBackground
	t.Cleanup(func() { darkBackground = was })

	for _, answer := range []struct {
		name        string
		bg          color.Color
		wash, wrong string
	}{
		{"light", color.RGBA{0xfd, 0xf6, 0xe3, 0xff}, "48;5;254m", "48;5;236m"},
		{"dark", color.RGBA{0x1e, 0x1e, 0x1e, 0xff}, "48;5;236m", "48;5;254m"},
	} {
		g := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","to":"2026-10-11","query":"sort_by(.fields.title)"}`)
		r := &root{pages: []page{g}, width: 120, height: 30}
		r.Update(tea.BackgroundColorMsg{Color: answer.bg})

		drawn := g.View()
		require.Contains(t, drawn, answer.wash, "on a %s terminal", answer.name)
		require.NotContains(t, drawn, answer.wrong, "on a %s terminal", answer.name)
	}
}

// TestGroupedGanttKeepsTheGroupHeaderOnTop: a window too small for the chart
// still opens on the header of the group its first row is in, whether the
// cursor is on that group's first row or deep in it, and the header keeps
// the crosshair it is drawn with.
func TestGroupedGanttKeepsTheGroupHeaderOnTop(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	for _, status := range []string{"to-do", "in-progress", "done"} {
		for _, title := range []string{"one", "two", "three"} {
			newIssue(t, repo, map[string]any{
				"title": status + " " + title, "status": status,
				"start": "2026-09-07", "stop": "2026-09-13",
			})
		}
	}

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"status","query":"sort_by(.fields.status)"}`)
	page.Update(tea.WindowSizeMsg{Width: 120, Height: 11})

	// the second group, the row it starts on, and the header line as it is
	// drawn where nothing scrolls it: the crosshair runs through it
	second := 0
	for at, index := range page.order {
		if page.nodes[index].group != page.nodes[page.order[0]].group {
			second = at
			break
		}
	}
	require.Greater(t, second, 0, "three groups of three")
	group := page.nodes[page.order[second]].group
	firstRow := page.bars[page.order[second]].human
	header := ""
	for _, line := range strings.Split(plainView(page), "\n") {
		if strings.HasPrefix(line, group) {
			header = line
		}
	}
	require.Greater(t, len(header), len(group), "the crosshair pads the header")

	// scrolled to the end, the window opens in the middle of the second
	// group: its header is the first body line, its first row is above
	page = send(page, "G").(*ganttPage)
	lines := strings.Split(plainView(page), "\n")
	require.Equal(t, header, lines[4], "the window opens on the group's header, crosshair and all")
	require.NotContains(t, lines[5], firstRow, "and not on the group's first row")
	require.Contains(t, plainView(page), page.bars[page.order[page.cursor]].human,
		"the cursor's row is on screen")

	// and back up onto that first row, the header is still the line above it
	for page.cursor > second {
		page = send(page, "k").(*ganttPage)
	}
	lines = strings.Split(plainView(page), "\n")
	require.Equal(t, header, lines[4], "the group's header is the first body line")
	require.Contains(t, lines[5], firstRow, "the cursor's row is right under it")
}
