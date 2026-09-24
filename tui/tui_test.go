package tui

import (
	"encoding/json"
	"regexp"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/view"
)

// The models are driven here without a terminal: Update takes messages and
// View returns a string, so everything but the drawing is testable as data.

// testRepo is a store with an identity and the jira preset, which is what a
// view needs to have any fields to show at all.
func testRepo(t *testing.T) *cache.RepoCache {
	t.Helper()

	repo := repository.CreateGoGitTestRepo(t, false)
	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	identity, err := backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, backend.SetUserIdentity(identity))

	_, _, err = host.SchemaInit(backend, "jira", false)
	require.NoError(t, err)

	return backend
}

// newIssue creates a task, and returns its id.
func newIssue(t *testing.T, repo *cache.RepoCache, fields map[string]any) string {
	t.Helper()

	values := map[string]issue.Value{"type": issue.StringValue("task")}
	for key, value := range fields {
		values[key] = issue.MustValue(value)
	}

	id, err := host.IssueNew(repo, host.IssueDocument{Fields: values, Body: "the body"})
	require.NoError(t, err)
	return id.String()
}

// list builds the list page a set of keyword arguments describes.
func list(t *testing.T, repo *cache.RepoCache, kwargs string) *listPage {
	t.Helper()

	var values map[string]json.RawMessage
	if kwargs != "" {
		require.NoError(t, json.Unmarshal([]byte(kwargs), &values))
	}

	call, err := view.Parse(view.KindList, values)
	require.NoError(t, err)

	page, err := newListPage(repo, call)
	require.NoError(t, err)

	page.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return page
}

// press is one keystroke, spelled the way a binding spells it.
func press(spelling string) tea.KeyPressMsg {
	switch spelling {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	runes := []rune(spelling)
	return tea.KeyPressMsg{Code: runes[0], Text: spelling}
}

// send types a series of keys into a page and returns what it became.
func send(p page, spellings ...string) page {
	for _, spelling := range spellings {
		p, _ = p.Update(press(spelling))
	}
	return p
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// plainView is what the page draws, with the styling taken back off, because
// a test is about what it says and not about how it is coloured.
func plainView(p page) string {
	return ansiPattern.ReplaceAllString(p.View(), "")
}

func TestListDrawsTheIssues(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "write the renderer", "status": "in-progress"})
	newIssue(t, repo, map[string]any{"title": "read the design", "status": "done"})

	page := list(t, repo, `{"fields":["title","status"]}`)
	drawn := plainView(page)

	require.Contains(t, drawn, "title")
	require.Contains(t, drawn, "status")
	require.Contains(t, drawn, "write the renderer")
	require.Contains(t, drawn, "read the design")
	require.Contains(t, drawn, "in-progress")
	require.Contains(t, drawn, "2 issues")
}

func TestCursorMoves(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "first"})
	second := newIssue(t, repo, map[string]any{"title": "second"})

	page := list(t, repo, "")
	// the default listing is last edited first, so the second issue is on top
	require.Equal(t, second, page.currentId())

	page = send(page, "j").(*listPage)
	require.Equal(t, first, page.currentId())

	page = send(page, "k").(*listPage)
	require.Equal(t, second, page.currentId())

	// and the three spellings are one key: the arrows, the vi letters and the
	// readline controls all move the same cursor
	page = send(page, "down").(*listPage)
	require.Equal(t, first, page.currentId())
	page = send(page, "ctrl+p").(*listPage)
	require.Equal(t, second, page.currentId())
	page = send(page, "ctrl+n").(*listPage)
	require.Equal(t, first, page.currentId())

	page = send(page, "G").(*listPage)
	require.Equal(t, first, page.currentId())
	page = send(page, "g").(*listPage)
	require.Equal(t, second, page.currentId())
}

func TestGroupsHaveAHeaderAndTheUngroupedComeLast(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "filed", "status": "done"})
	newIssue(t, repo, map[string]any{"title": "unfiled"})

	page := list(t, repo, `{"group_by":"status","fields":["title"]}`)
	drawn := plainView(page)

	require.Contains(t, drawn, "done")
	require.Contains(t, drawn, noGroup)
	require.Less(t, indexOf(drawn, "done"), indexOf(drawn, noGroup),
		"the issues nobody has filed come last")
}

func TestFilterHidesRows(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "write the renderer"})
	newIssue(t, repo, map[string]any{"title": "read the design"})

	page := list(t, repo, "")
	page = send(page, "/", "r", "e", "n", "d", "enter").(*listPage)

	drawn := plainView(page)
	require.Contains(t, drawn, "write the renderer")
	require.NotContains(t, drawn, "read the design")
	require.Contains(t, drawn, "1 of 2 issues")

	// esc puts them back
	page = send(page, "esc").(*listPage)
	require.Contains(t, plainView(page), "read the design")
}

// TestRefreshKeepsTheCursorOnTheSameIssue is what makes a live view usable:
// somebody else's write must not move what you were about to edit.
func TestRefreshKeepsTheCursorOnTheSameIssue(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "first"})
	newIssue(t, repo, map[string]any{"title": "second"})

	page := list(t, repo, "")
	page = send(page, "j").(*listPage)
	was := page.currentId()

	// another process writes: a new issue, which the default listing puts on
	// top, which would move the cursor if it were an index alone
	newIssue(t, repo, map[string]any{"title": "third"})
	updated, _ := page.Update(refreshMsg{})

	page = updated.(*listPage)
	require.Equal(t, was, page.currentId())
	require.Contains(t, plainView(page), "third")
}

func TestYankSaysSo(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	updated, cmd := page.Update(press("y"))

	require.NotNil(t, cmd, "the clipboard is written by a command, as OSC 52")
	require.Contains(t, plainView(updated), "yanked "+id[:7])
}

func TestHelpListsEverySpelling(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	drawn := plainView(send(page, "?"))

	for _, spelling := range []string{"ctrl+p", "ctrl+n", "ctrl+b", "ctrl+f", "pgup", "alt+v", "G", "home"} {
		require.Contains(t, drawn, spelling)
	}

	// any key closes it
	require.NotContains(t, plainView(send(page, "?", "x")), "ctrl+b")
}

// TestNestingIsRefused is the one thing the table promises and the renderer
// does not do: it is parsed, and then refused by name.
func TestNestingIsRefused(t *testing.T) {
	repo := testRepo(t)
	renderer := &Renderer{}

	call, err := view.Parse(view.KindList, map[string]json.RawMessage{"expand": json.RawMessage(`"parent"`)})
	require.NoError(t, err)

	_, err = renderer.Render(t.Context(), repo, call)
	require.ErrorContains(t, err, "not nest rows yet (84dfbde)")
	require.ErrorContains(t, err, "expand")
}

func TestBoardAndGanttNameTheRenderer(t *testing.T) {
	repo := testRepo(t)
	renderer := &Renderer{}

	kwargs := map[string]map[string]json.RawMessage{
		view.KindBoard: {"columns": json.RawMessage(`"status"`)},
		view.KindGantt: {"start": json.RawMessage(`"due"`), "stop": json.RawMessage(`"due"`)},
	}

	for _, kind := range []string{view.KindBoard, view.KindGantt} {
		call, err := view.Parse(kind, kwargs[kind])
		require.NoError(t, err)

		_, err = renderer.Render(t.Context(), repo, call)
		require.ErrorContains(t, err, "does not draw a "+kind+" yet (84dfbde)")
	}
}

func indexOf(haystack, needle string) int {
	for at := 0; at+len(needle) <= len(haystack); at++ {
		if haystack[at:at+len(needle)] == needle {
			return at
		}
	}
	return -1
}
