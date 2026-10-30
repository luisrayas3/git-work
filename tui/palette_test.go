package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// stackOf is a stack over one page, sized.
func stackOf(first page, width, height int) *root {
	stack := &root{pages: []page{first}, width: width, height: height}
	stack.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return stack
}

// drawnStack is what the stack draws, unstyled.
func drawnStack(stack *root) string {
	return ansiPattern.ReplaceAllString(stack.View().Content, "")
}

// run sends one key to the stack, and hands it what the key's command
// answered, as the program would; it returns that answer.
func run(stack *root, spelling string) tea.Msg {
	_, cmd := stack.Update(press(spelling))
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if _, quit := msg.(tea.QuitMsg); quit {
		return msg
	}
	if msg == nil {
		return nil
	}
	_, then := stack.Update(msg)
	if _, pop := msg.(popMsg); pop && then != nil {
		// a pop from the first page is quit, which is what the key did
		if quit, ok := then().(tea.QuitMsg); ok {
			return quit
		}
	}
	return msg
}

// TestHelpIsTheKeymap: one table, generated from the keymap, each action
// with its keys and its letter on the palette; vim's letters are gone, and a
// stray key keeps it open (app.md, A3, A4).
func TestHelpIsTheKeymap(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	page := list(t, repo, "")
	page.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	page = send(page, "?").(*listPage)
	drawn := plainView(page)
	for _, spelling := range []string{
		"pgup", "alt+v", "home", "alt+<", "end", "alt+>", "enter", "ctrl+c", "alt+w", "alt+c", "ctrl+pgdown",
		"ctrl+p", "ctrl+b", "ctrl+s", "ctrl+g", "ctrl+y", "ctrl+q", "alt+q",
		"esc w", "esc c", "esc f", "esc z", "esc n", "esc ?", "esc q",
	} {
		require.Contains(t, drawn, spelling)
	}
	// each alternative is a column: the second keys line up, and so do the
	// alt keys and the palette's letters
	column := func(of, in string) int {
		for _, line := range strings.Split(drawn, "\n") {
			if strings.Contains(line, in) {
				return strings.Index(line, of)
			}
		}
		t.Fatalf("no line with %q", in)
		return -1
	}
	require.Equal(t, column("ctrl+p", "ctrl+p"), column("cmd+c", "copy cell"))
	require.Equal(t, column("alt+v", "page up"), column("alt+w", "copy cell"))
	require.Equal(t, column("esc v", "page up"), column("esc n", "new issue"))
	for _, gone := range []string{"standard", "vim", "emacs", "ctrl+u", "ctrl+d", "gt", "ctrl+enter", "f2"} {
		require.NotContains(t, drawn, gone)
	}

	page = send(page, "x", "right").(*listPage)
	require.NotNil(t, page.help)
	page = send(page, "esc").(*listPage)
	require.Nil(t, page.help)
}

// TestVimLettersAreText: hjkl, g, G, y, p and q no longer move, copy, paste
// or quit; on a list a letter starts the filter with itself (A3).
func TestVimLettersAreText(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "jam"})
	newIssue(t, repo, map[string]any{"title": "kettle"})

	stack := stackOf(list(t, repo, ""), 100, 20)
	require.NotEqual(t, tea.QuitMsg{}, run(stack, "j"), "j quits nothing")
	top := stack.top().(*listPage)
	require.NotNil(t, top.filtering, "a letter starts the filter")
	require.Equal(t, "j", top.filter)
	require.Contains(t, plainView(top), "1 of 2 issues")

	// the filter's own keys: enter keeps it, and typing again adds to it
	run(stack, "enter")
	require.Nil(t, top.filtering)
	run(stack, "a")
	require.Equal(t, "ja", top.filter)
	run(stack, "esc")
	require.Empty(t, top.filter, "esc in the filter clears it")

	// q is text too: no quit
	require.NotEqual(t, tea.QuitMsg{}, run(stack, "q"))
	require.Equal(t, "q", top.filter)
}

// TestABoardAndAGanttFilterAsTyped: the other kinds whose rows are what is
// typed after.
func TestABoardAndAGanttFilterAsTyped(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "write", "status": "to-do"})

	b := stackOf(board(t, repo, `{"columns":"status"}`), 100, 20)
	run(b, "w")
	require.Equal(t, "w", b.top().(*boardPage).filter)

	g := stackOf(gantt(t, repo, `{"start":"start","stop":"due"}`), 100, 20)
	run(g, "w")
	require.Equal(t, "w", g.top().(*ganttPage).filter)
}

// TestEscOpensThePalette: where you are, the whole call, this page's
// commands and everywhere's, each with its letter, in a box in the middle of
// the page; the page keeps its first lines and its height (A1, 2026-10-29).
func TestEscOpensThePalette(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	stack := stackOf(list(t, repo, `{"group_by":"status"}`), 300, 20)
	require.Nil(t, run(stack, "esc"), "the first esc does not quit")
	require.True(t, stack.palette)

	drawn := drawnStack(stack)
	lines := strings.Split(drawn, "\n")
	require.Len(t, lines, 20, "the page keeps its height")
	require.True(t, strings.HasPrefix(lines[0], "list  group_by=status"), "the first line is still the call")
	require.Contains(t, drawn, "│ list ", "the stack, one page deep")
	// a box, in the middle: the page is drawn around it
	top := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "╭") })
	require.Greater(t, top, 3, "the box is not at the top")
	require.Greater(t, strings.Index(lines[top], "╭"), 0, "the box is not at the left edge")
	require.True(t, strings.HasPrefix(lines[0], "list"), "the page is still drawn above it")
	// the call in full: the kind once, in the breadcrumb, then each
	// argument a line, and the query a section of its own, unfolded as the
	// pipeline it is (2026-10-30)
	require.Equal(t, 1, strings.Count(drawn, "│ list "), "the kind is said once")
	require.Contains(t, drawn, "│ group_by    status ", "each argument named in the left column")
	require.Contains(t, drawn, "│ query       sort_by(.edit_time.lamport, .edit_time.timestamp)")
	require.Contains(t, drawn, "│             | reverse")
	for _, entry := range []string{"f filter", "w copy cell", "c copy id", "j edit query", "n new", "? help", "q quit"} {
		require.Contains(t, drawn, entry)
	}
	require.NotContains(t, drawn, "z fold all", "nothing nests here")
	require.NotContains(t, drawn, "J reset query", "the query is the call's")
	require.Contains(t, lines[19], "esc: back")

	// narrow, it still keeps its height, and the box fits the window
	stack.Update(tea.WindowSizeMsg{Width: 30, Height: 20})
	narrow := strings.Split(drawnStack(stack), "\n")
	require.Len(t, narrow, 20)
	for _, line := range narrow {
		require.LessOrEqual(t, ansi.StringWidth(line), 30, line)
	}
}

// TestTheSecondEscIsBack: from the first page it quits; from a page over
// another it is that page's back, the palette closed (A1).
func TestTheSecondEscIsBack(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	stack := stackOf(list(t, repo, ""), 100, 20)
	stack.top().(*listPage).putCursorOn(id)
	pushInto(t, stack, "enter")
	require.Len(t, stack.pages, 2)

	run(stack, "esc")
	require.True(t, stack.palette)
	require.Contains(t, drawnStack(stack), "list › show "+id[:7], "the stack as a breadcrumb")
	run(stack, "esc")
	require.False(t, stack.palette)
	require.Len(t, stack.pages, 1, "back off the issue")

	run(stack, "esc")
	require.IsType(t, tea.QuitMsg{}, run(stack, "esc"), "back from the first page quits")

	// esc esc typed quickly, which arrives as one key, is back too
	stack = stackOf(list(t, repo, ""), 100, 20)
	require.IsType(t, tea.QuitMsg{}, run(stack, "alt+esc"))
}

// TestAPaletteLetterIsItsAltKey: a letter on the palette is the page's alt
// key, so esc then w and alt+w copy the same cell (A2).
func TestAPaletteLetterIsItsAltKey(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	stack := stackOf(list(t, repo, `{"fields":["title","status"]}`), 100, 20)
	send(stack.top(), "right", "right")

	run(stack, "esc")
	_, cmd := stack.Update(press("w"))
	require.NotNil(t, cmd)
	require.False(t, stack.palette, "a command closes the palette")
	require.Contains(t, plainView(stack.top()), "copied status")

	run(stack, "esc")
	_, cmd = stack.Update(press("c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(stack.top()), "copied "+id[:7])

	// alt+f, or esc f, is the filter, empty
	run(stack, "esc")
	run(stack, "f")
	require.NotNil(t, stack.top().(*listPage).filtering)
	run(stack, "esc")

	// a letter that is no command rings and leaves the palette open
	run(stack, "esc")
	_, cmd = stack.Update(press("x"))
	require.Equal(t, bell()(), cmd())
	require.True(t, stack.palette)

	// a direction closes it, and only that
	cursor := stack.top().(*listPage).cursor
	run(stack, "down")
	require.False(t, stack.palette)
	require.Equal(t, cursor, stack.top().(*listPage).cursor)

	// g closes it, and only that; alt+g, which esc g typed quickly is, does
	// nothing on the page, and a g typed there is still text
	run(stack, "esc")
	require.Nil(t, run(stack, "g"))
	require.False(t, stack.palette)
	require.Nil(t, run(stack, "alt+g"))
	require.False(t, stack.palette)
	require.Nil(t, stack.top().(*listPage).filtering)
	require.Equal(t, cursor, stack.top().(*listPage).cursor)
	run(stack, "g")
	require.Equal(t, "g", stack.top().(*listPage).filter)
	run(stack, "esc")

	// esc q quits, as alt+q does
	run(stack, "esc")
	require.IsType(t, tea.QuitMsg{}, run(stack, "q"))
	require.IsType(t, tea.QuitMsg{}, run(stackOf(list(t, repo, ""), 100, 20), "alt+q"))

	// ctrl+c on the palette copies the command that drew the page
	run(stack, "esc")
	_, cmd = stack.Update(press("ctrl+c"))
	require.NotNil(t, cmd)
	require.Contains(t, plainView(stack.top()), "copied the command")
}

// TestNewFromThePalette: n opens the creator over any page, on the type.
func TestNewFromThePalette(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	stack := stackOf(list(t, repo, ""), 100, 20)
	run(stack, "esc")
	run(stack, "n")
	draft, ok := stack.top().(*newPage)
	require.True(t, ok, "the creator is pushed")
	require.Equal(t, cellType, draft.cell, "the type is chosen first")
	require.Len(t, stack.pages, 2)
}

// TestEscWhileEditingCancelsOnly: an open editor, a filter being typed and
// the help each take esc for their own, and the palette stays shut (A1).
func TestEscWhileEditingCancelsOnly(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	stack := stackOf(list(t, repo, `{"fields":["title","status"]}`), 100, 20)
	send(stack.top(), "right", "right")
	run(stack, "space")
	require.NotNil(t, stack.top().(*listPage).editor)
	run(stack, "esc")
	require.Nil(t, stack.top().(*listPage).editor)
	require.False(t, stack.palette)

	run(stack, "/")
	run(stack, "esc")
	require.Nil(t, stack.top().(*listPage).filtering)
	require.False(t, stack.palette)

	run(stack, "?")
	run(stack, "esc")
	require.Nil(t, stack.top().(*listPage).help)
	require.False(t, stack.palette)
}

// TestAShowPagesCommands: on show, the filter is a side table's, and is
// listed only there.
func TestAShowPagesCommands(t *testing.T) {
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	shown, err := showCall(t, repo, `{"id":"`+id+`"}`)
	require.NoError(t, err)
	stack := stackOf(shown, 100, 30)
	run(stack, "esc")
	drawn := drawnStack(stack)
	require.Contains(t, drawn, "show "+id[:7])
	require.Contains(t, drawn, "c copy id")
	require.NotContains(t, drawn, "f filter")
}

// TestThePaletteCutsALongQuery: a query longer than twelve lines shows its
// first eleven and how many more there are (2026-10-30).
func TestThePaletteCutsALongQuery(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})

	stages := make([]string, 0, 20)
	for range 20 {
		stages = append(stages, ".")
	}
	query, _ := json.Marshal(strings.Join(stages, " | "))
	stack := stackOf(list(t, repo, `{"query":`+string(query)+`}`), 200, 60)
	run(stack, "esc")
	drawn := drawnStack(stack)
	require.Equal(t, 11, strings.Count(drawn, "│ query       .")+strings.Count(drawn, "│             | ."))
	require.Contains(t, drawn, "… 9 more lines")
}

// TestEditTheQuery: esc j opens the query in the palette, enter draws the
// page from what was typed, a query that does not draw keeps the editor and
// says why, and esc J puts back the call's own (A10).
func TestEditTheQuery(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one"})
	newIssue(t, repo, map[string]any{"title": "two"})

	stack := stackOf(list(t, repo, `{"query":"map(select(.fields.title == \"one\"))"}`), 120, 40)
	require.Contains(t, plainView(stack.top()), "1 issue")
	run(stack, "esc")
	run(stack, "j")
	require.NotNil(t, stack.query)
	require.False(t, stack.palette)
	require.Contains(t, drawnStack(stack), `map(select(.fields.title == "one"))`, "the query, ready to edit")
	require.Contains(t, drawnStack(stack), "enter: draw")

	// enter with nothing changed draws nothing new
	run(stack, "enter")
	require.Nil(t, stack.query)
	require.Empty(t, stack.called)

	// a query that does not parse: the editor stays, saying why
	run(stack, "alt+j")
	stack.query.box.area.SetValue("map(")
	run(stack, "enter")
	require.NotNil(t, stack.query)
	require.NotEmpty(t, stack.query.failed)
	require.Contains(t, plainView(stack.top()), "1 issue", "the page is as it was")

	// cleared, it is the default: every issue
	stack.query.box.area.SetValue("")
	run(stack, "enter")
	require.Nil(t, stack.query)
	require.Contains(t, plainView(stack.top()), "2 issues")
	call, _, _ := stack.top().Call()
	require.Contains(t, command(call), "sort_by", "the copied command is the call drawn")

	run(stack, "esc")
	require.Contains(t, drawnStack(stack), "J reset query")
	run(stack, "J")
	require.Contains(t, plainView(stack.top()), "1 issue", "the call's own query again")
	require.Empty(t, stack.called)
	run(stack, "esc")
	require.NotContains(t, drawnStack(stack), "J reset query")

	// a show has no query, and no j
	run(stack, "esc")
	pushInto(t, stack, "enter")
	require.IsType(t, &showPage{}, stack.top())
	run(stack, "esc")
	require.NotContains(t, drawnStack(stack), "j edit query")
	run(stack, "up") // closes the palette, and only that
	_, cmd := stack.Update(press("alt+j"))
	require.Nil(t, stack.query)
	require.NotNil(t, cmd)
}
