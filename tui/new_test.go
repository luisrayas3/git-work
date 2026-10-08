package tui

import (
	"context"
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

// newView builds the new page a set of keyword arguments describes.
func newView(t *testing.T, repo *cache.RepoCache, kwargs string) *newPage {
	t.Helper()

	var values map[string]json.RawMessage
	if kwargs != "" {
		require.NoError(t, json.Unmarshal([]byte(kwargs), &values))
	}
	call, err := view.Parse(view.KindNew, values)
	require.NoError(t, err)

	page, err := newNewPage(repo, call)
	require.NoError(t, err)
	page.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return page
}

// typeText types into the open editor's input and accepts it.
func typeText(p page, text string) page {
	for _, r := range text {
		p, _ = p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	p, _ = p.Update(press("enter"))
	return p
}

// TestNewOpensOnTheTitleTyping: with the type decided, a ghost's enter means
// "I want to add one", and the title is what is typed next (C3).
func TestNewOpensOnTheTitleTyping(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"title": "the story", "type": "story"})

	page := newView(t, repo, `{"doc":{"fields":{"type":"task","parent":"`+story+`"},"body":"why"}}`)
	require.NotNil(t, page.editor, "the title's editor is open on arrival")
	require.Equal(t, "title", page.editor.key)

	drawn := plainView(page)
	require.Contains(t, drawn, "new  doc=", "the first line is the call")
	require.Contains(t, drawn, "task  (no title)")
	require.Contains(t, drawn, "the story", "the prefilled parent is drawn as the issue it names")
	require.Contains(t, drawn, "status", "the type's fields are the rows")
	require.Contains(t, drawn, "[ create ]")
	require.Contains(t, drawn, "why", "the body is in the box")
	require.NotContains(t, drawn, "archived", "a draft is not archived")

	page = typeText(page, "do the thing").(*newPage)
	require.Nil(t, page.editor)
	require.Equal(t, "do the thing", page.title())
	require.Contains(t, plainView(page), "task  do the thing")
	require.Equal(t, stopHeader, page.current().stop, "accepting the title leaves the cursor on it")
}

// TestNewOpensOnTheTypeWhenNone: with no type there are no fields to show,
// and the type cell is where the page opens.
func TestNewOpensOnTheTypeWhenNone(t *testing.T) {
	repo := testRepo(t)

	page := newView(t, repo, "")
	require.Nil(t, page.editor)
	require.Equal(t, stopHeader, page.current().stop)
	require.Equal(t, cellType, page.cell)
	require.Empty(t, page.rows)
	require.Contains(t, plainView(page), "(no type)  (no title)")

	// space opens the schema's types; the first is initiative, so task is a
	// few down
	page = send(page, "space").(*newPage)
	require.NotNil(t, page.editor)
	for at, item := range page.editor.picker.items {
		if item.value == "task" {
			page.editor.picker.cursor = at
		}
	}
	page = send(page, "enter").(*newPage)
	require.Equal(t, "task", page.typeKey())
	require.NotEmpty(t, page.rows, "the type's fields appear")
	require.Contains(t, plainView(page), "status")
}

// TestNewCreatesOnEnter is the whole point: nothing in the store until
// Create, and then exactly what issue new would have written.
func TestNewCreatesOnEnter(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"title": "the story", "type": "story"})
	before := len(repo.Issues().AllIds())

	page := newView(t, repo, `{"doc":{"fields":{"type":"task","parent":"`+story+`"},"body":"why"}}`)
	page = typeText(page, "do the thing").(*newPage)

	// a field edited in the draft: status, the first row
	page = send(page, "down", "space").(*newPage)
	require.NotNil(t, page.editor)
	require.Equal(t, "status", page.editor.key)
	for at, item := range page.editor.picker.items {
		if item.value == "in-progress" {
			page.editor.picker.cursor = at
		}
	}
	page = send(page, "enter").(*newPage)
	require.Equal(t, `"in-progress"`, string(page.fields["status"]))
	require.Len(t, repo.Issues().AllIds(), before, "nothing is written until Create")

	// tab walks to the button: fields → box → create
	page = send(page, "tab", "tab").(*newPage)
	require.Equal(t, stopCreate, page.current().stop)
	require.Contains(t, plainView(page), "enter: create")

	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd)
	msg, ok := cmd().(createdMsg)
	require.True(t, ok, "enter on Create answers with the created issue")

	require.Len(t, repo.Issues().AllIds(), before+1)
	doc, err := host.IssueGet(repo, msg.id)
	require.NoError(t, err)
	require.Equal(t, `"do the thing"`, string(doc.Fields["title"]))
	require.Equal(t, `"task"`, string(doc.Fields["type"]))
	require.Equal(t, `"`+story+`"`, string(doc.Fields["parent"]))
	require.Equal(t, `"in-progress"`, string(doc.Fields["status"]))
	require.Equal(t, "why", doc.Comments[0].Message)
	_, has := doc.Fields["priority"]
	require.False(t, has, "a field left empty is not stored: the form invents no default (C5)")
}

// TestNewStandaloneBecomesShow: with nothing underneath, Create puts show on
// the new issue in the draft's place, and the id is the program's answer.
func TestNewStandaloneBecomesShow(t *testing.T) {
	repo := testRepo(t)

	draft := newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	stack := &root{pages: []page{draft}, width: 100, height: 40}
	stack.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	page := typeText(draft, "alone").(*newPage)
	// header → fields → box → create
	page = send(page, "tab", "tab", "tab").(*newPage)
	require.Equal(t, stopCreate, page.current().stop)
	_, cmd := stack.Update(press("enter"))
	require.NotNil(t, cmd)
	stack.Update(cmd())

	require.Len(t, stack.pages, 1)
	shown, ok := stack.pages[0].(*showPage)
	require.True(t, ok, "show took the draft's place")
	require.Equal(t, "alone", shown.snapshot.Title())
	require.Equal(t, `"`+shown.id+`"`, string(stack.answer))
	require.Contains(t, ansiPattern.ReplaceAllString(stack.View().Content, ""), "created "+shown.id[:7])
}

// TestNewFromAViewPopsBack: the view underneath reloads and stands on the
// new issue, which is the ghost's promise kept (C4).
func TestNewFromAViewPopsBack(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "first"})

	under := list(t, repo, "")
	stack := &root{pages: []page{under}, width: 100, height: 40}
	stack.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	draft := newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	stack.Update(pushMsg{page: draft})
	require.Len(t, stack.pages, 2)

	typed := typeText(draft, "second").(*newPage)
	typed = send(typed, "tab", "tab", "tab").(*newPage)
	_, cmd := stack.Update(press("enter"))
	require.NotNil(t, cmd)
	stack.Update(cmd())

	require.Len(t, stack.pages, 1)
	require.Nil(t, stack.answer, "from a view, the view is the command and nothing is answered")
	require.Equal(t, "second", under.current().cells["title"], "the cursor is on the new issue")
	require.Contains(t, under.status, "created")
	require.NotContains(t, under.status, "not in this view")

	// a query that leaves the new issue out says so instead
	narrow := list(t, repo, `{"query":"map(select(.fields.title == \"first\"))"}`)
	stack = &root{pages: []page{narrow}, width: 100, height: 40}
	draft = newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	stack.Update(pushMsg{page: draft})
	typed = typeText(draft, "third").(*newPage)
	typed = send(typed, "tab", "tab", "tab").(*newPage)
	_, cmd = stack.Update(press("enter"))
	stack.Update(cmd())
	require.Contains(t, narrow.status, "not in this view")
}

// TestNewRefusesWhatThePlannerRefuses: a draft without a type or a title is
// named in the status line, the cursor on the cell it names (C3).
func TestNewRefusesWhatThePlannerRefuses(t *testing.T) {
	repo := testRepo(t)

	page := newView(t, repo, "")
	page.focusStop(stopCreate)
	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "the bell")
	require.Contains(t, page.status, "type")
	require.Equal(t, stopHeader, page.current().stop)
	require.Equal(t, cellType, page.cell)

	page = newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	page.editor = nil
	page.focusStop(stopCreate)
	_, cmd = page.Update(press("enter"))
	require.NotNil(t, cmd)
	require.Contains(t, page.status, "title")
	require.Equal(t, cellTitle, page.cell)
}

// TestNewDocIsCheckedAtTheCall: a document the planner would refuse is
// refused before anything draws, as children and expand are (C1).
func TestNewDocIsCheckedAtTheCall(t *testing.T) {
	repo := testRepo(t)

	_, err := host.View(context.Background(), repo, nil, view.KindNew,
		map[string]json.RawMessage{"doc": json.RawMessage(`{"fields":{"type":"task","colour":"red"}}`)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "colour")

	_, err = host.View(context.Background(), repo, nil, view.KindNew,
		map[string]json.RawMessage{"doc": json.RawMessage(`{"fields":{"type":"task","status":"nope"}}`)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nope")

	// a good one reaches the renderer, which here is none
	_, err = host.View(context.Background(), repo, nil, view.KindNew,
		map[string]json.RawMessage{"doc": json.RawMessage(`{"fields":{"type":"task","status":"to-do"}}`)})
	require.ErrorIs(t, err, view.ErrNoTerminal)

	// and no type is no check: the page is where one is picked
	_, err = host.View(context.Background(), repo, nil, view.KindNew,
		map[string]json.RawMessage{"doc": json.RawMessage(`{"fields":{"status":"to-do"}}`)})
	require.ErrorIs(t, err, view.ErrNoTerminal)
}

// TestNewEscAsksTwiceWithADraft: back from a draft drops it, so a draft is
// warned about once, as show's are.
func TestNewEscAsksTwiceWithADraft(t *testing.T) {
	repo := testRepo(t)

	page := newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	page.editor = nil
	_, cmd := page.Update(press("esc"))
	require.NotNil(t, cmd)
	require.IsType(t, popMsg{}, cmd(), "nothing typed: back is back")

	page = newView(t, repo, `{"doc":{"fields":{"type":"task"}}}`)
	page = typeText(page, "half").(*newPage)
	_, cmd = page.Update(press("esc"))
	require.Nil(t, cmd)
	require.Contains(t, page.status, "esc again")
	_, cmd = page.Update(press("esc"))
	require.NotNil(t, cmd)
	require.IsType(t, popMsg{}, cmd())
}

// TestNewRetypeKeepsSharedFields: a type picked wrong is one key from right,
// so the values the new type also has stay and the rest are counted (C3).
func TestNewRetypeKeepsSharedFields(t *testing.T) {
	repo := testRepo(t)
	sprint := newIssue(t, repo, map[string]any{"title": "sprint 1", "type": "iteration"})

	page := newView(t, repo, `{"doc":{"fields":{"type":"task","status":"to-do","iteration":"`+sprint+`"}}}`)
	page.editor = nil
	page.focusStop(stopHeader)
	page.cell = cellType
	page = send(page, "space").(*newPage)
	for at, item := range page.editor.picker.items {
		if item.value == "epic" {
			page.editor.picker.cursor = at
		}
	}
	page = send(page, "enter").(*newPage)

	require.Equal(t, "epic", page.typeKey())
	require.Equal(t, `"to-do"`, string(page.fields["status"]), "epic has a status too")
	_, has := page.fields["iteration"]
	require.False(t, has, "an epic has no iteration")
	require.Contains(t, page.status, "1 dropped")
}

// TestNewFieldsNarrowTheRows: `fields` keeps show's meaning on new.
func TestNewFieldsNarrowTheRows(t *testing.T) {
	repo := testRepo(t)

	page := newView(t, repo, `{"doc":{"fields":{"type":"task"}},"fields":["estimate","status"]}`)
	require.Len(t, page.rows, 2)
	require.Equal(t, "estimate", page.rows[0].key)
	require.Equal(t, "status", page.rows[1].key)
}
