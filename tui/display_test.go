package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
)

// key is a Jira key longer than the seven cells of a short hash, so that a
// column cut to seven would show another key.
const jiraKey = "LONGPROJ-12345"

// keyedRepo is the test store with git-work.display.id = jira.
func keyedRepo(t *testing.T) *cache.RepoCache {
	t.Helper()
	repo := testRepo(t)
	require.NoError(t, repo.LocalConfig().StoreString(cache.DisplayIdKey, "jira"))
	return repo
}

// newKeyed creates an issue carrying alias:jira, and returns its id.
func newKeyed(t *testing.T, repo *cache.RepoCache, alias string, fields map[string]any) string {
	t.Helper()
	values := map[string]issue.Value{"type": issue.StringValue("task")}
	for k, v := range fields {
		values[k] = issue.MustValue(v)
	}
	id, err := host.IssueNew(repo, host.IssueDocument{Fields: values, Body: "b", Aliases: map[string]string{"jira": alias}})
	require.NoError(t, err)
	return id.String()
}

// clipped is the text a copy command puts on the clipboard: the OSC 52 half
// of setClipboard, read without running the machine's clipboard tool.
func clipped(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	require.NotNil(t, cmd)
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			// SetClipboard is first and is a message alone; the tool is not run
			m := c()
			if reflect.TypeOf(m).Name() == "setClipboardMsg" {
				return fmt.Sprint(m)
			}
			break
		}
		t.Fatal("no clipboard write in the batch")
	}
	require.Equal(t, "setClipboardMsg", reflect.TypeOf(msg).Name())
	return fmt.Sprint(msg)
}

// drawnLine is the drawn line holding some text.
func drawnLine(t *testing.T, drawn, text string) string {
	t.Helper()
	for _, line := range strings.Split(drawn, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no line holds %q in\n%s", text, drawn)
	return ""
}

// TestListDrawsKeysWhole: with a namespace set, the id column is the key,
// never cut, and as wide as the widest key, so a hash beside it lines up
// (doc/design/alias-ids.md A3).
func TestListDrawsKeysWhole(t *testing.T) {
	repo := keyedRepo(t)
	newKeyed(t, repo, jiraKey, map[string]any{"title": "keyed"})
	local := newIssue(t, repo, map[string]any{"title": "local"})

	page := list(t, repo, `{"fields":["title"]}`)
	drawn := plainView(page)
	keyLine, localLine := drawnLine(t, drawn, "keyed"), drawnLine(t, drawn, "local")
	require.Contains(t, keyLine, jiraKey+" ")
	require.Contains(t, localLine, local[:7])
	require.Equal(t, cellAt(keyLine, "keyed"), cellAt(localLine, "local"), "the titles line up")
	require.Equal(t, cellAt(keyLine, "keyed"), cellAt(drawnLine(t, drawn, "title"), "title"), "under the header")
}

// cellAt is the cell a text starts at on a drawn line.
func cellAt(line, text string) int {
	return ansi.StringWidth(line[:strings.Index(line, text)])
}

// TestIdStyleDimsTheFallback: an alias is drawn at full strength and the
// hash an issue fell back to dim; with no namespace every id is dim, as it
// always was (A3).
func TestIdStyleDimsTheFallback(t *testing.T) {
	repo := keyedRepo(t)
	keyed := newKeyed(t, repo, jiraKey, map[string]any{"title": "keyed"})
	local := newIssue(t, repo, map[string]any{"title": "local"})

	base := lipgloss.NewStyle()
	require.False(t, idStyle(repo, base, keyed, jiraKey).GetFaint())
	require.True(t, idStyle(repo, base, local, local[:7]).GetFaint())

	plain := testRepo(t)
	require.True(t, idStyle(plain, base, keyed, keyed[:7]).GetFaint())

	// and the drawn list carries it: the hash's line is faint where the id is
	page := list(t, repo, `{"fields":["title"]}`)
	page = send(page, "l").(*listPage) // the cursor off the id column
	raw := page.View()
	faint := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*2(?:;[0-9]+)*m` + local[:7])
	require.Regexp(t, faint, raw, "the hash is dim")
	require.NotRegexp(t, regexp.MustCompile(`\x1b\[(?:[0-9]+;)*2(?:;[0-9]+)*m`+jiraKey), raw, "the key is not")
}

// TestCopyCopiesWhatIsShown: the id copy keys copy the key where it is drawn
// and the whole hash where the hash is, and a link copies its key (A7).
func TestCopyCopiesWhatIsShown(t *testing.T) {
	repo := keyedRepo(t)
	story := newKeyed(t, repo, jiraKey, map[string]any{"type": "story", "title": "the story"})
	task := newIssue(t, repo, map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	require.Equal(t, task, page.current().id)
	require.Contains(t, drawnLine(t, plainView(page), "the task"), jiraKey+" the story", "a link is drawn by its key")
	for _, spelling := range []string{"alt+c", "ctrl+c"} {
		_, cmd := page.Update(press(spelling))
		require.Equal(t, task, clipped(t, cmd), "the hash is copied whole")
	}

	page = send(page, "l", "l").(*listPage) // onto parent
	_, cmd := page.Update(press("ctrl+c"))
	require.Equal(t, jiraKey, clipped(t, cmd), "a link copies the jiraKey it shows")

	stories := list(t, repo, `{"fields":["title"],"query":"map(select(.fields.type == \"story\"))"}`)
	_, cmd = stories.Update(press("alt+c"))
	require.Equal(t, jiraKey, clipped(t, cmd))
	require.Equal(t, "copied "+jiraKey, stories.status)

	// what is copied is accepted back as an id
	back, err := repo.Issues().ResolvePrefixOrAlias(jiraKey)
	require.NoError(t, err)
	require.Equal(t, story, back.Id().String())

	// show: its call line leads with the jiraKey, and its id copy is the jiraKey
	shown := show(t, repo, story, nil)
	require.True(t, strings.HasPrefix(plainView(shown), "show  "+jiraKey), plainView(shown))
	updated, cmd := shown.Update(press("alt+c"))
	require.Equal(t, jiraKey, clipped(t, cmd))
	require.Contains(t, plainView(updated), "copied "+jiraKey)
}

// TestPickerDrawsKeys: a relation's picker lists the issues as links are
// drawn, the key first, and writes the full id.
func TestPickerDrawsKeys(t *testing.T) {
	repo := keyedRepo(t)
	story := newKeyed(t, repo, jiraKey, map[string]any{"type": "story", "title": "the story"})
	newIssue(t, repo, map[string]any{"title": "the task"})

	page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
	page = send(page, "l", "l", "space").(*listPage)
	require.NotNil(t, page.editor)
	require.Equal(t, story, page.editor.picker.items[0].value)
	require.Equal(t, jiraKey+" the story", page.editor.picker.items[0].label)
}

// TestBoardAndGanttDrawKeysWhole: a card's id and a gantt label's are the
// key, uncut, and copy copies it.
func TestBoardAndGanttDrawKeysWhole(t *testing.T) {
	repo := keyedRepo(t)
	withDates(t, repo)
	newKeyed(t, repo, jiraKey, map[string]any{"title": "keyed", "status": "to-do", "start": "2026-09-28"})

	b := board(t, repo, `{"columns":"status"}`)
	require.Contains(t, plainView(b), jiraKey)
	_, cmd := b.Update(press("alt+c"))
	require.Equal(t, jiraKey, clipped(t, cmd))

	g := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	require.Contains(t, drawnLine(t, plainView(g), "keyed"), jiraKey+" keyed")
	_, cmd = g.Update(press("alt+c"))
	require.Equal(t, jiraKey, clipped(t, cmd))
}

// TestOrderStaysOnTheHash: the (rank, id) tie-break reads the id, never what
// is drawn, so a setting cannot move a row (A8).
func TestOrderStaysOnTheHash(t *testing.T) {
	repo := keyedRepo(t)
	var ids []string
	for i, alias := range []string{"PROJ-3", "PROJ-2", "PROJ-1"} {
		ids = append(ids, newKeyed(t, repo, alias, map[string]any{"title": fmt.Sprint("t", i), "rank": "m"}))
	}
	sort.Strings(ids)

	page := list(t, repo, `{"fields":["title"]}`)
	var drawn []string
	for _, index := range page.order {
		if !page.nodes[index].ghost {
			drawn = append(drawn, page.rows[index].id)
		}
	}
	require.Equal(t, ids, drawn)
}
