package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// showCall builds the show page a set of keyword arguments describes, the
// way the command and a flow reach it, at a width of 140.
func showCall(t *testing.T, repo *cache.RepoCache, kwargs string) (*showPage, error) {
	t.Helper()

	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))
	call, err := view.Parse(view.KindShow, values)
	require.NoError(t, err)
	if err := checkCall(repo, call); err != nil {
		return nil, err
	}

	page, err := newShowView(repo, call)
	if err != nil {
		return nil, err
	}
	page.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	return page, nil
}

// checkCall is host.View's check, which the command and a flow pass before
// anything draws.
func checkCall(repo *cache.RepoCache, call *view.Call) error {
	s, err := repo.LoadSchema()
	if err != nil {
		return err
	}
	return view.CheckSchema(call, s)
}

// lineWith is the first drawn line holding a text, or "".
func lineWith(p page, text string) string {
	for _, line := range strings.Split(plainView(p), "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

// storyWithTasks is a story, two tasks under it and a subtask, and a task
// elsewhere.
func storyWithTasks(t *testing.T, repo *cache.RepoCache) (story, one, two, sub string) {
	story = newTyped(t, repo, "story", map[string]any{"title": "the story", "status": "to-do"})
	one = newTyped(t, repo, "task", map[string]any{"title": "task one", "parent": story, "status": "in-progress"})
	two = newTyped(t, repo, "task", map[string]any{"title": "task two", "parent": story})
	sub = newTyped(t, repo, "subtask", map[string]any{"title": "a subtask", "parent": story})
	newTyped(t, repo, "task", map[string]any{"title": "not ours"})
	return story, one, two, sub
}

// TestSideTableBesideTheFields: a table per element of expand, beside the
// fields, its first line level with the first field; the heading is the
// relation as given, then the header and a row per issue the relation
// reaches, in the store's order, the archived left out
// (doc/design/show-side-table.md, S2 and S4).
func TestSideTableBesideTheFields(t *testing.T) {
	repo := testRepo(t)
	story, one, two, sub := storyWithTasks(t, repo)
	gone := newTyped(t, repo, "task", map[string]any{"title": "archived", "parent": story})
	_, err := host.IssueSet(repo, gone, map[string]issue.Value{"archived": issue.MustValue(true)}, false)
	require.NoError(t, err)

	page, err := showCall(t, repo, `{"id":"`+story+`","expand":[{"relation":"children","fields":["status"]},"blocks"]}`)
	require.NoError(t, err)

	first := lineWith(page, "status ")
	require.Contains(t, first, "│  children", "the heading is level with the first field")
	require.Contains(t, lineWith(page, one[:idWidth]), "task one")
	require.Contains(t, lineWith(page, one[:idWidth]), "in-progress")
	require.Contains(t, lineWith(page, one[:idWidth]), "│", "beside the fields")
	require.NotEmpty(t, lineWith(page, sub[:idWidth]), "every type whose parent names it")
	require.Empty(t, lineWith(page, gone[:idWidth]), "the archived are left out")
	require.Empty(t, lineWith(page, "not ours"))
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, two[:idWidth]), "the store's order")
	require.Contains(t, drawn, "│  blocks")
	require.Empty(t, lineWith(page, "  blocks "), "a stored relation a table draws is not a field row too")

	// include_archive brings the archived back
	page, err = showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"children","include_archive":true}}`)
	require.NoError(t, err)
	require.NotEmpty(t, lineWith(page, gone[:idWidth]))

	// and the query narrows, as a layer's does
	page, err = showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"children","query":"map(select(.fields.type == \"task\"))"}}`)
	require.NoError(t, err)
	require.Empty(t, lineWith(page, sub[:idWidth]))
	require.NotEmpty(t, lineWith(page, one[:idWidth]))
}

// TestSideTableUnderWhenNarrow: in a window too narrow for both, the tables
// are drawn under the fields at the window's width; the keys do not change.
func TestSideTableUnderWhenNarrow(t *testing.T) {
	repo := testRepo(t)
	story, one, _, _ := storyWithTasks(t, repo)

	page, err := showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"children","fields":["status"]}}`)
	require.NoError(t, err)
	page.Update(tea.WindowSizeMsg{Width: 40, Height: 50})

	require.NotContains(t, plainView(page), "│  children")
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, "status "), indexOf(drawn, " children"), "under the fields")
	require.NotEmpty(t, lineWith(page, one[:idWidth]))

	send(page, "tab")
	require.Equal(t, stopSide, page.current().stop)
	require.Contains(t, lineWith(page, "›"), one[:idWidth])
}

// TestSideTableTabTraversal: tab and shift-tab are the only way between the
// fields and the side tables; up and down leave either for the header above
// and the box below (S5).
func TestSideTableTabTraversal(t *testing.T) {
	repo := testRepo(t)
	story, one, _, _ := storyWithTasks(t, repo)
	page, err := showCall(t, repo, `{"id":"`+story+`","expand":"children"}`)
	require.NoError(t, err)

	require.Equal(t, stopFields, page.current().stop, "the page opens on the fields")
	send(page, "tab")
	require.Equal(t, stopSide, page.current().stop)
	require.Equal(t, one, page.sideRowAt(page.sideCurrent()).id, "on the first side row")
	send(page, "shift+tab")
	require.Equal(t, stopFields, page.current().stop)

	// down past the last field is the box, not the side tables
	for range page.rows {
		send(page, "down")
	}
	require.Equal(t, stopBox, page.current().stop)
	// up from the box is the fields' last row, as before
	send(page, "up")
	require.Equal(t, stopFields, page.current().stop)
	require.Equal(t, len(page.rows)-1, page.row)

	// on the side: up from the first is the header, down past the last the box
	send(page, "tab")
	send(page, "up")
	require.Equal(t, stopHeader, page.current().stop)
	send(page, "tab", "tab")
	require.Equal(t, stopSide, page.current().stop)
	for range page.sideItems() {
		send(page, "down")
	}
	require.Equal(t, stopBox, page.current().stop)
	send(page, "shift+tab")
	require.Equal(t, stopSide, page.current().stop, "shift-tab from the box is the side tables")
}

// TestSideTableLeftRightSwitchTabs: left and right stay the tab keys on every
// stop but the header, the side tables included.
func TestSideTableLeftRightSwitchTabs(t *testing.T) {
	repo := testRepo(t)
	story, _, _, _ := storyWithTasks(t, repo)
	page, err := showCall(t, repo, `{"id":"`+story+`","expand":"children"}`)
	require.NoError(t, err)

	for _, stop := range []stopKind{stopFields, stopSide, stopBox, stopTabs} {
		page.focusStop(stop)
		was := page.tab
		send(page, "l")
		require.NotEqual(t, was, page.tab, "right on stop %d", stop)
		send(page, "h")
		require.Equal(t, was, page.tab, "left on stop %d", stop)
		require.Equal(t, stop, page.current().stop)
	}

	page.focusStop(stopHeader)
	was := page.cell
	send(page, "l")
	require.NotEqual(t, was, page.cell, "the header walks its cells")
}

// TestSideTableScrollsWithinItsCap: past the taller of the fields and twelve
// lines the side column scrolls by itself, its last line saying what is out
// of sight, the heading and header kept on top once scrolled off (S4).
func TestSideTableScrollsWithinItsCap(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	var ids []string
	for i := range 30 {
		ids = append(ids, newTyped(t, repo, "task", map[string]any{"title": fmt.Sprintf("task %02d", i), "parent": story}))
	}

	page, err := showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"children","query":"sort_by(.fields.title)"}}`)
	require.NoError(t, err)
	limit := max(len(page.rows), sideCap)

	window, _ := page.sideWindow(80, limit, page.current())
	require.Len(t, window, limit)
	require.Contains(t, ansiPattern.ReplaceAllString(window[len(window)-1], ""), "↓", "the last line says what is below")
	require.Empty(t, lineWith(page, "task 29"), "the last task is out of sight")

	send(page, "tab")
	for range 29 {
		send(page, "down")
	}
	require.Equal(t, stopSide, page.current().stop)
	require.Contains(t, lineWith(page, "›"), "task 29", "the window follows the cursor")
	drawn := plainView(page)
	require.Contains(t, drawn, "│  children", "the heading is kept once scrolled off")
	require.Less(t, indexOf(drawn, "│  children"), indexOf(drawn, "task 29"))
	require.Contains(t, drawn, "↑")
	require.Empty(t, lineWith(page, "task 00 "), "the first task scrolled off")
}

// TestSideTableKeys: enter opens the row's issue as a bare show, space grabs
// it and a drop writes the rank, the drawn order kept; copy copies the id,
// and `/` narrows the rows (S5).
func TestSideTableKeys(t *testing.T) {
	repo := testRepo(t)
	story, one, two, sub := storyWithTasks(t, repo)
	page, err := showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"children","fields":["status"]}}`)
	require.NoError(t, err)
	send(page, "tab")

	// enter opens the row
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	pushed, ok := cmd().(pushMsg)
	require.True(t, ok)
	require.Equal(t, one, pushed.page.(*showPage).id)
	require.Empty(t, pushed.page.(*showPage).tables, "a bare show")

	// copy copies the row's id, either key
	_, cmd = page.Update(press("ctrl+c"))
	require.NotNil(t, cmd)
	require.Equal(t, "copied "+one[:idWidth], page.status)
	_, cmd = page.Update(press("alt+c"))
	require.NotNil(t, cmd)
	require.Equal(t, "copied "+one[:idWidth], page.status)

	// space grabs; left and right ring; down carries; space drops: one is
	// now under two, the rows above it ranked so the screen reads as drawn
	send(page, "space")
	require.NotNil(t, page.grab)
	_, cmd = page.Update(press("l"))
	require.NotNil(t, cmd, "the bell")
	require.Equal(t, "stays in its table", page.status)
	send(page, "down", "space")
	require.Nil(t, page.grab)
	require.Contains(t, page.status, "rank set")
	require.NotEqual(t, "null", field(t, repo, one, "rank"))
	require.NotEqual(t, "null", field(t, repo, two, "rank"), "the row drawn above was ranked first")
	drawn := plainView(page)
	require.Less(t, indexOf(drawn, two[:idWidth]), indexOf(drawn, one[:idWidth]))
	require.Less(t, indexOf(drawn, one[:idWidth]), indexOf(drawn, sub[:idWidth]), "unranked keep the store's order, last")
	require.Equal(t, one, page.sideRowAt(page.sideCurrent()).id, "the cursor stays on the row dropped")

	// a carry past the table's edge stops there; esc puts it back
	page.side = 0
	send(page, "space", "up", "up")
	require.Equal(t, 0, page.side)
	send(page, "esc")
	require.Nil(t, page.grab)

	// the filter narrows the side rows; esc clears it
	send(page, "/", "s", "u", "b")
	require.Empty(t, lineWith(page, two[:idWidth]))
	require.NotEmpty(t, lineWith(page, sub[:idWidth]))
	send(page, "enter")
	require.Nil(t, page.filtering)
	require.Equal(t, "sub", page.filter)
	send(page, "/", "esc")
	require.Equal(t, "", page.filter)
	require.NotEmpty(t, lineWith(page, two[:idWidth]))
}

// TestSideTableGhost: a table over an inverse ends in a ghost whose draft
// sets the stored field to this issue's id, and the type where one type
// holds that field; a stored relation of the shown issue has none (S6).
func TestSideTableGhost(t *testing.T) {
	repo := testRepo(t)
	story, _, _, _ := storyWithTasks(t, repo)

	page, err := showCall(t, repo, `{"id":"`+story+`","expand":"children"}`)
	require.NoError(t, err)
	send(page, "tab")
	items := page.sideItems()
	page.side = len(items) - 1
	require.True(t, page.sideCurrent().ghost)
	require.Contains(t, lineWith(page, "›"), ghostLabel)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	created, ok := cmd().(pushMsg).page.(*newPage)
	require.True(t, ok)
	require.Equal(t, issue.StringValue(story), created.fields["parent"])
	_, typed := created.fields[schema.TypeKey]
	require.False(t, typed, "a task's parent and a subtask's: two types, no type filled")

	// on a task, children are subtasks alone: the type is filled
	task := page.sideRowAt(&page.sideItems()[0]).id
	sub, err := showCall(t, repo, `{"id":"`+task+`","expand":"children"}`)
	require.NoError(t, err)
	require.Equal(t, issue.StringValue("subtask"), sub.tables[0].ghost.Fields[schema.TypeKey])

	// blocked_by is read through blocks, a set: the ghost holds a set
	blocked, err := showCall(t, repo, `{"id":"`+story+`","expand":"blocked_by"}`)
	require.NoError(t, err)
	require.Equal(t, issue.MustValue([]string{story}), blocked.tables[0].ghost.Fields["blocks"])

	// a stored relation of the shown issue: no ghost, and `(none)` stands
	stored, err := showCall(t, repo, `{"id":"`+story+`","expand":"blocks"}`)
	require.NoError(t, err)
	require.Nil(t, stored.tables[0].ghost)
	require.Contains(t, plainView(stored), noGroup)
	send(stored, "tab")
	_, cmd = stored.Update(press("enter"))
	require.NotNil(t, cmd, "the bell on (none)")

	// what the ghost created lands under the cursor when the creator pops
	made := newTyped(t, repo, "task", map[string]any{"title": "made here", "parent": story})
	page.focusStop(stopFields)
	page.Update(createdMsg{id: made})
	require.Equal(t, stopSide, page.current().stop)
	require.Equal(t, made, page.sideRowAt(page.sideCurrent()).id)
	require.Equal(t, "created "+made[:idWidth], page.status)
}

// TestSideTableIsLive: the tables are read again on every refresh, the
// cursor kept on its row.
func TestSideTableIsLive(t *testing.T) {
	repo := testRepo(t)
	story, _, two, _ := storyWithTasks(t, repo)
	page, err := showCall(t, repo, `{"id":"`+story+`","expand":"children"}`)
	require.NoError(t, err)
	send(page, "tab", "down")
	require.Equal(t, two, page.sideRowAt(page.sideCurrent()).id)

	early := newTyped(t, repo, "task", map[string]any{"title": "ranked first", "parent": story, "rank": "0"})
	page.Update(refreshMsg{})
	require.NotEmpty(t, lineWith(page, early[:idWidth]))
	require.Equal(t, two, page.sideRowAt(page.sideCurrent()).id, "the cursor stays on its row")
}

// TestSideTableRefusesBeforeDrawing: what the schema does not have is refused
// by host.View, the way every surface reaches it, naming what exists.
func TestSideTableRefusesBeforeDrawing(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	for kwargs, says := range map[string]string{
		`{"relation":"nephews"}`:                                 "task/parent (inverse children)",
		`{"relation":"children","fields":["colour"]}`:            "colour",
		`[ "children", {"relation":"blocks","fields":["nope"]}]`: "table 2 names field nope",
	} {
		_, err := host.View(t.Context(), repo, nil, view.KindShow, map[string]json.RawMessage{
			"id":     mustJSON(story),
			"expand": json.RawMessage(kwargs),
		})
		require.ErrorContains(t, err, "view show: expand", kwargs)
		require.ErrorContains(t, err, says, kwargs)
	}

	_, err := host.View(t.Context(), repo, nil, view.KindShow, map[string]json.RawMessage{
		"id":       mustJSON(story),
		"children": json.RawMessage(`[{"relation":"children"}]`),
	})
	require.ErrorContains(t, err, "takes no argument children")
}
