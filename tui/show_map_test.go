package tui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/view"
)

// epicShows is the workflow's map (doc/design/show-from-a-view.md): an epic
// opens with every story it holds beside its fields, closed ones included.
const epicShows = `{"epic":{"expand":{"relation":"children","query":"sort_by(.fields.status == \"done\", .fields.title)","fields":["status","priority","assignee"]}}}`

// anEpic is an epic, two stories under it, one done, and a task under the
// open one.
func anEpic(t *testing.T, repo *cache.RepoCache) (epic, open, done, task string) {
	epic = newTyped(t, repo, "epic", map[string]any{"title": "the epic", "status": "in-progress"})
	open = newTyped(t, repo, "story", map[string]any{"title": "open story", "parent": epic, "status": "to-do"})
	done = newTyped(t, repo, "story", map[string]any{"title": "done story", "parent": epic, "status": "done"})
	task = newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": open})
	return epic, open, done, task
}

// pushedShow is the show page a command pushed.
func pushedShow(t *testing.T, cmd tea.Cmd) *showPage {
	t.Helper()
	require.NotNil(t, cmd)
	pushed, ok := cmd().(pushMsg)
	require.True(t, ok, "a page was pushed")
	shown, ok := pushed.page.(*showPage)
	require.True(t, ok, "the page is show")
	return shown
}

// TestEnterOpensTheShowTheTypeMaps is the doc's workflow: a list of epics
// folding their stories; Enter on an epic opens it with the stories beside
// its fields, closed ones too, and Enter on a story opens a bare show that
// still carries the map (V1, V3).
func TestEnterOpensTheShowTheTypeMaps(t *testing.T) {
	repo := testRepo(t)
	epic, open, done, _ := anEpic(t, repo)

	page := list(t, repo, `{"query":"map(select(.fields.type == \"epic\"))","expand":"children","open":true,"show":`+epicShows+`}`)
	page.putCursorOn(epic)
	shown := pushedShow(t, send1(page, "enter"))
	require.Equal(t, epic, shown.id)
	require.Len(t, shown.tables, 1, "the epic's entry")
	shown.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	require.NotEmpty(t, lineWith(shown, open[:idWidth]))
	require.NotEmpty(t, lineWith(shown, done[:idWidth]), "closed ones included")
	drawn := plainView(shown)
	require.Less(t, indexOf(drawn, "open story"), indexOf(drawn, "done story"), "the entry's query orders them")
	require.Equal(t, epic, shown.call.String("id"), "the row gives the id")

	// a story is not listed: a bare show, carrying the map
	page.putCursorOn(open)
	require.Equal(t, open, page.current().id)
	story := pushedShow(t, send1(page, "enter"))
	require.Equal(t, open, story.id)
	require.Empty(t, story.tables, "an unlisted type opens a bare show")
	require.JSONEq(t, epicShows, string(story.call.Raw("show")))
}

// TestEnterReadsTheStoredType: a query that shapes fields.type does not pick
// the entry; the stored issue's type does (V1).
func TestEnterReadsTheStoredType(t *testing.T) {
	repo := testRepo(t)
	epic, open, _, _ := anEpic(t, repo)

	// a story drawn as an epic opens as the story it is
	page := list(t, repo, `{"query":"map(select(.id == \"`+open+`\") | .fields.type = \"epic\")","show":`+epicShows+`}`)
	require.Equal(t, "epic", page.current().cells["type"], "the row is shaped")
	require.Empty(t, pushedShow(t, send1(page, "enter")).tables)

	// and an epic drawn as a story opens as the epic it is
	page = list(t, repo, `{"query":"map(select(.id == \"`+epic+`\") | .fields.type = \"story\")","show":`+epicShows+`}`)
	require.Len(t, pushedShow(t, send1(page, "enter")).tables, 1)

	// a row with no id rings, as it did (query-rows.md, R2)
	page = list(t, repo, `{"query":"[{key: \"a heading\", fields: {type: \"epic\", title: \"a heading\"}}]","show":`+epicShows+`}`)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "the bell")
	_, pushed := cmd().(pushMsg)
	require.False(t, pushed)
}

// TestEveryKindOpensByTheMap: the board's card and the gantt's bar open as
// the list's row does; the matrix's list inherits the map, and its rows open
// by it (V1, V3, Settled 3).
func TestEveryKindOpensByTheMap(t *testing.T) {
	repo := testRepo(t)
	epic, _, _, _ := anEpic(t, repo)

	b := board(t, repo, `{"columns":"status","show":`+epicShows+`}`)
	b.putCursorOn(epic)
	require.Equal(t, epic, b.currentId())
	require.Len(t, pushedShow(t, send1(b, "enter")).tables, 1, "a board's card")

	g := gantt(t, repo, `{"start":"due","stop":"due","show":`+epicShows+`}`)
	g.putCursorOn(epic)
	require.Equal(t, epic, g.current().id)
	require.Len(t, pushedShow(t, send1(g, "enter")).tables, 1, "a gantt's bar")

	m := matrix(t, repo, `{"rows":"type","columns":"status","query":"map(select(.fields.type == \"epic\"))","show":`+epicShows+`}`)
	put(t, m, "Epic", "In Progress")
	_, cmd := m.Update(press("enter"))
	require.NotNil(t, cmd)
	opened := cmd().(pushMsg).page.(*listPage)
	require.JSONEq(t, epicShows, string(opened.call.Raw("show")), "the list takes the matrix's map")
	opened.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	opened.putCursorOn(epic)
	require.Len(t, pushedShow(t, send1(opened, "enter")).tables, 1, "and opens by it")

	// a matrix with no map opens a list with none
	m = matrix(t, repo, `{"rows":"type","columns":"status","query":"map(select(.fields.type == \"epic\"))"}`)
	put(t, m, "Epic", "In Progress")
	_, cmd = m.Update(press("enter"))
	require.False(t, cmd().(pushMsg).page.(*listPage).call.Has("show"))
}

// TestTheMapIsCarried: from the epic's page, a side table's row, then a
// relation cell back to the epic, then the creator, each open by the same
// map, and Esc back up the stack returns to pages that keep their tables
// (V3).
func TestTheMapIsCarried(t *testing.T) {
	repo := testRepo(t)
	epic, open, _, _ := anEpic(t, repo)

	epics := list(t, repo, `{"query":"map(select(.fields.type == \"epic\"))","show":`+epicShows+`}`)
	stack := &root{pages: []page{epics}, width: 160, height: 50}
	stack.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	epics.putCursorOn(epic)
	pushInto(t, stack, "enter")
	first := stack.top().(*showPage)
	require.Len(t, first.tables, 1)

	// a side table's row: the open story, bare, carrying the map
	send(first, "tab")
	require.Equal(t, open, first.sideRowAt(first.sideCurrent()).id)
	pushInto(t, stack, "enter")
	story := stack.top().(*showPage)
	require.Equal(t, open, story.id)
	require.Empty(t, story.tables)

	// a relation cell: the story's parent, the epic, with its table again
	onField(t, story, "parent")
	pushInto(t, stack, "enter")
	again := stack.top().(*showPage)
	require.Equal(t, epic, again.id)
	require.Len(t, again.tables, 1, "a relation cell opens by the map")

	// the ghost's creator carries it: its relation cells open by it
	send(again, "tab")
	again.side = len(again.sideItems()) - 1
	require.True(t, again.sideCurrent().ghost)
	pushInto(t, stack, "enter")
	draft := stack.top().(*newPage)
	require.JSONEq(t, epicShows, string(draft.shows))
	require.Len(t, pushedShow(t, draft.follow(epic)).tables, 1, "the parent the draft names")

	// Esc back up the stack: every page kept its tables
	back(stack)
	require.Len(t, stack.top().(*showPage).tables, 1)
	back(stack)
	require.Equal(t, open, stack.top().(*showPage).id)
	back(stack)
	require.Equal(t, epic, stack.top().(*showPage).id)
	require.Len(t, stack.top().(*showPage).tables, 1, "back on the first page, its table kept")
}

// TestThePageAfterCreateOpensByTheMap: the creator a view's map reached puts
// show on the new issue by that map when it stands alone (V3).
func TestThePageAfterCreateOpensByTheMap(t *testing.T) {
	repo := testRepo(t)
	draft := newView(t, repo, `{"doc":{"fields":{"type":"epic"}}}`)
	draft.shows = json.RawMessage(epicShows)
	typed := typeText(draft, "a new epic").(*newPage)
	typed = send(typed, "tab", "tab", "tab").(*newPage)
	for typed.focus != stopCreate {
		typed = send(typed, "tab").(*newPage)
	}
	_, cmd := typed.Update(press("enter"))
	require.NotNil(t, cmd)
	created, ok := cmd().(createdMsg)
	require.True(t, ok)
	require.Len(t, created.shown.(*showPage).tables, 1)
}

// TestADirectShowTakesTheMap: show's own `show` is for the pages it opens,
// never for the shown issue (V3, Settled 2).
func TestADirectShowTakesTheMap(t *testing.T) {
	repo := testRepo(t)
	epic, open, _, _ := anEpic(t, repo)

	// an epic shown directly with an epic entry: not applied to itself
	direct, err := showCall(t, repo, `{"id":"`+epic+`","show":`+epicShows+`}`)
	require.NoError(t, err)
	require.Empty(t, direct.tables, "never for itself")
	require.Contains(t, plainView(direct), "show=", "the call line is the call")

	// a story shown directly: its parent opens by the map
	story, err := showCall(t, repo, `{"id":"`+open+`","show":`+epicShows+`}`)
	require.NoError(t, err)
	onField(t, story, "parent")
	shown := pushedShow(t, send1(story, "enter"))
	require.Equal(t, epic, shown.id)
	require.Len(t, shown.tables, 1)
}

// TestAShowEntryIsCheckedAsShowIs: what show refuses, an entry refuses,
// naming the type; the shape in view.Parse, the names in view.CheckSchema
// (V2).
func TestAShowEntryIsCheckedAsShowIs(t *testing.T) {
	repo := testRepo(t)
	for _, c := range []struct{ shows, want string }{
		{`{"saga":{}}`, "show names type saga, which the schema does not have; the types are"},
		{`{"epic":{"expand":"nephews"}}`, "show epic: view show: expand"},
		{`{"epic":{"expand":{"relation":"children","fields":["colour"]}}}`, "colour"},
	} {
		call, err := view.Parse(view.KindList, kwargs(t, `{"show":`+c.shows+`}`))
		require.NoError(t, err, c.shows)
		require.ErrorContains(t, checkCall(repo, call), c.want)
	}
	for _, c := range []struct{ shows, want string }{
		{`{"epic":{"id":"abc"}}`, "epic takes no id"},
		{`{"epic":{"colour":"red"}}`, "epic takes no argument colour, an entry takes fields (defaulted), expand (optional)"},
		{`{"epic":{"expand":{"relation":"children","group_by":"status"}}}`, "takes no group_by"},
		{`{"epic":"children"}`, "epic is an object of show arguments"},
		{`["epic"]`, "is an object of type keys"},
	} {
		_, err := view.Parse(view.KindBoard, kwargs(t, `{"columns":"status","show":`+c.shows+`}`))
		require.ErrorContains(t, err, c.want, c.shows)
	}
}

// send1 is one key into a page, and the command it answered with.
func send1(p page, spelling string) tea.Cmd {
	_, cmd := p.Update(press(spelling))
	return cmd
}

// pushInto sends a key to the stack's top page and pushes what it opened.
func pushInto(t *testing.T, stack *root, spelling string) {
	t.Helper()
	_, cmd := stack.Update(press(spelling))
	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(pushMsg)
	require.True(t, ok, "a page was pushed, not %T", msg)
	stack.Update(msg)
}

// onField puts show's cursor on the fields table's row of a key.
func onField(t *testing.T, p *showPage, key string) {
	t.Helper()
	p.focusStop(stopFields)
	for at, row := range p.rows {
		if row.key == key && row.first {
			p.row = at
			return
		}
	}
	t.Fatalf("no %s row", key)
}

// back is Esc on the stack top page, and the pop it answers with.
func back(stack *root) {
	_, cmd := stack.Update(press("esc"))
	if cmd != nil {
		stack.Update(cmd())
	}
}
