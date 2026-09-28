package tui

import (
	"encoding/json"
	"regexp"
	"strings"
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
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	case "f2":
		return tea.KeyPressMsg{Code: tea.KeyF2}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+shift+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl | tea.ModShift}
	case "alt+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt}
	case "ctrl+pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModCtrl}
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

// TestTheFirstLineIsTheCall: what is on the screen is the call that drew
// it, query first, a default dim and there all the same.
func TestTheFirstLineIsTheCall(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "done"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status"}`)
	first := strings.SplitN(plainView(page), "\n", 2)[0]

	require.True(t, strings.HasPrefix(first, "list"))
	require.Contains(t, first, "query=map(select(.fields.archived != true))")
	require.Contains(t, first, `fields=["title","status"]`)
	require.Contains(t, first, "group_by=status")
	require.NotContains(t, plainView(page), "? for keys · 1 issues · map", "the query left the status line")
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

// TestTheCursorStartsOnTheIdAndCopiesIt is the chat pin (ca81145): the id is
// a column, the cursor starts there, and copying on arrival copies the id.
func TestTheCursorStartsOnTheIdAndCopiesIt(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	require.Equal(t, 0, page.column)

	for _, spelling := range []string{"y", "ctrl+c"} {
		updated, cmd := page.Update(press(spelling))
		require.NotNil(t, cmd, "the clipboard is written by a command, as OSC 52")
		require.Contains(t, plainView(updated), "copied "+id[:7])
	}
}

func TestCopyIsTheCellAndCopyIdIsTheId(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	page := list(t, repo, `{"fields":["title","status"]}`)
	page = send(page, "l", "l").(*listPage)

	for _, spelling := range []string{"ctrl+c", "ctrl+shift+c", "y"} {
		updated, cmd := page.Update(press(spelling))
		require.NotNil(t, cmd)
		require.Contains(t, plainView(updated), "copied status")
	}

	for _, spelling := range []string{"Y", "alt+c"} {
		updated, cmd := page.Update(press(spelling))
		require.NotNil(t, cmd)
		require.Contains(t, plainView(updated), "copied "+id[:7])
	}
}

// TestHelpHasATabPerFamily: each tab is one person's whole set, and moving
// between them does not close the help.
func TestHelpHasATabPerFamily(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	page = send(page, "?").(*listPage)
	drawn := plainView(page)
	for _, spelling := range []string{"standard", "vim", "emacs", "pgup", "home", "ctrl+enter", "f2", "ctrl+c", "alt+c", "ctrl+pgdown", "ctrl+q"} {
		require.Contains(t, drawn, spelling)
	}
	require.NotContains(t, drawn, "ctrl+n")

	page = send(page, "l").(*listPage)
	drawn = plainView(page)
	for _, spelling := range []string{"G", "ctrl+u", " y ", " p ", "gt"} {
		require.Contains(t, drawn, spelling)
	}

	page = send(page, "3").(*listPage)
	drawn = plainView(page)
	for _, spelling := range []string{"ctrl+p", "ctrl+b", "alt+v", "alt+w", "ctrl+s", "ctrl+g"} {
		require.Contains(t, drawn, spelling)
	}

	// a stray key keeps it open; esc closes it
	page = send(page, "x").(*listPage)
	require.NotNil(t, page.help)
	page = send(page, "esc").(*listPage)
	require.Nil(t, page.help)
}

// TestEmacsSearchIsCtrlS: ctrl+s narrows, as / does.
func TestEmacsSearchIsCtrlS(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	page = send(page, "ctrl+s").(*listPage)
	require.NotNil(t, page.filtering)
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
