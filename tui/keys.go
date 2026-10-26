package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// One keymap (doc/design/app.md, A3, 2026-10-15).
//
// The standard keys and emacs's ctrl and alt keys are read, and nothing
// else: vim's single letters went, because a letter at the top level is
// worth more as text, and typing on a list, a board or a gantt filters it.
// The keys a person needs with no menu are the ones every program has; every
// other command is on the palette `esc` opens, under a letter (palette.go).
//
// A command's letter is also its alt key, by definition (A2): a terminal
// sends alt+x as ESC then x, which is what esc then x typed quickly sends,
// so the two are one key on every terminal. That is why the letters agree
// with emacs's alt keys wherever emacs has one: w copies, v pages up, < and >
// go to the first and the last.
//
// Bubble Tea v2 asks every terminal for the kitty keyboard protocol's key
// disambiguation on its own, which removes the wait after a lone ESC and
// tells shift+enter from enter where the terminal has it; no key here means
// anything different because of it.

// chord is one action: its keys, and its letter on the palette.
type chord struct {
	what string
	keys []string
	// letter is the action's key on the palette, and so its alt key; empty
	// for an action that has none
	letter string
	// shown replaces the keys in the help where they are not keys the
	// program reads: the terminal's own paste arrives as a paste, not a key.
	shown string

	binding key.Binding
}

func newChord(what string, keys ...string) *chord {
	c := &chord{what: what, keys: keys}
	c.binding = key.NewBinding(key.WithKeys(keys...))
	return c
}

// on gives the action a letter on the palette, and with it the alt key the
// letter is (A2). A shifted letter is also spelled with the modifier by a
// terminal that reports it rather than the character — alt+shift+, for
// alt+< — so both spellings are bound.
func (c *chord) on(letter string) *chord {
	c.letter = letter
	all := append([]string{}, c.keys...)
	all = append(all, "alt+"+letter)
	if shifted, ok := shiftedLetters[letter]; ok {
		all = append(all, "alt+shift+"+shifted)
	}
	c.binding = key.NewBinding(key.WithKeys(all...))
	return c
}

// shiftedLetters are the palette's letters typed with shift, as the key
// under them.
var shiftedLetters = map[string]string{"<": ",", ">": ".", "?": "/", "Z": "z"}

func (c *chord) show(spelling string) *chord {
	c.shown = spelling
	return c
}

// matches says whether a key press is this action.
func (c *chord) matches(press tea.KeyPressMsg) bool {
	return key.Matches(press, c.binding)
}

// spelling is how the action's keys read in the help: its keys, then its
// alt key where it has a letter.
func (c *chord) spelling() string {
	if c.shown != "" {
		return c.shown
	}
	parts := make([]string, 0, len(c.keys)+1)
	for _, k := range c.keys {
		// two spellings of one key are one key to a person reading
		if _, doubled := doubles[k]; doubled {
			continue
		}
		parts = append(parts, readable(k))
	}
	if c.letter != "" {
		parts = append(parts, "alt+"+c.letter)
	}
	return strings.Join(parts, " ")
}

// doubles are the second spellings of a key: a terminal sends either, so
// both are bound, and one is shown.
var doubles = map[string]struct{}{
	" ": {},
	// the ctrl+shift and cmd+shift copies are the same copy, for a terminal
	// that hands them over rather than keeping them for itself
	"ctrl+shift+c":  {},
	"super+shift+c": {},
	"ctrl+shift+v":  {},
	"super+shift+v": {},
}

// readable is a key the way a person writes it: super is the key a Mac calls
// cmd, and the one a Mac user is looking for.
func readable(k string) string {
	return strings.ReplaceAll(k, "super+", "cmd+")
}

// keymap is every key the renderer reads.
type keymap struct {
	up, down, left, right *chord
	pageUp, pageDn        *chord
	top, bottom           *chord
	next, previous        *chord
	act, edit, newline    *chord
	copy, copyId, paste   *chord
	filter                *chord
	grab                  *chord
	foldAll               *chord
	openEverything        *chord
	expand, link, unlink  *chord
	nextTab, previousTab  *chord
	newIssue, search      *chord
	home, runFlow         *chord
	help                  *chord
	back                  *chord
	quit                  *chord

	// cancel is the text widgets' own: it is read before the widget sees the
	// key, so it must not be a key a person types text with.
	cancel *chord
}

var keys = keymap{
	up:     newChord("up", "up", "ctrl+p"),
	down:   newChord("down", "down", "ctrl+n"),
	left:   newChord("previous column / tab", "left", "ctrl+b"),
	right:  newChord("next column / tab", "right", "ctrl+f"),
	pageUp: newChord("page up", "pgup").on("v"),
	pageDn: newChord("page down", "pgdown", "ctrl+v"),
	top:    newChord("first", "home").on("<"),
	bottom: newChord("last", "end").on(">"),

	next:        newChord("next stop", "tab"),
	previous:    newChord("previous stop", "shift+tab"),
	nextTab:     newChord("next tab", "ctrl+pgdown"),
	previousTab: newChord("previous tab", "ctrl+pgup"),

	// Enter opens and space edits (doc/design/terminal-renderer.md,
	// 2026-10-02): enter opens the issue under the cursor, or the one a link
	// names, and sends a comment being typed; space edits the cell under the
	// cursor, starts typing in the comment box, and on what has no cell — an
	// id, a card, a bar — grabs. ctrl+enter and f2 are gone, because
	// ctrl+enter is enter on every terminal without the kitty keyboard
	// protocol, and a key that is one key here and another there is not a key
	// the renderer reads (2026-09-28).
	act:  newChord("open issue, follow link; typing: send comment", "enter"),
	edit: newChord("edit cell (a link: change it); on the box: write", "space", " "),
	// A newline in a comment: alt+enter is escape, enter, which every
	// terminal sends; shift+enter only where the terminal tells it from enter.
	newline: newChord("typing: newline in comment", "alt+enter", "shift+enter"),

	// Copy and paste are the terminal's first: cmd+c and ctrl+shift+c copy
	// what the mouse selected, and cmd+v and ctrl+shift+v paste, arriving as
	// a bracketed paste rather than a key. Where the terminal hands a copy
	// key over instead of keeping it, it copies the cell under the cursor;
	// ctrl+c is that key everywhere, because no terminal keeps it.
	copy:   newChord("copy cell", "ctrl+c", "super+c", "ctrl+shift+c", "super+shift+c").on("w"),
	copyId: newChord("copy id").on("c"),
	// A paste key that reaches the program asks the terminal for its
	// clipboard over OSC 52. ctrl+v stays emacs's page down: a standard
	// user's ctrl+v is the terminal's paste, or ^V, and never a paste here.
	paste: newChord("paste into field", "super+v", "ctrl+shift+v", "super+shift+v", "ctrl+y").
		show("terminal paste, ctrl+y"),

	// Typing filters a list, a board or a gantt too (A3); `/` starts the
	// filter with nothing typed.
	filter: newChord("filter (or type)", "/", "ctrl+s").on("f"),
	grab:   newChord("on id, card, bar: grab / drop (move it)", "space", " "),
	// One nested row is folded with space on its arrow, the cell after its
	// id; the whole tree at once is a palette command, `Z` being text now
	// (A3). Tab and shift-tab, the next and previous stop, are into the
	// first child and up to the parent.
	foldAll: newChord("fold / unfold every row (nested)").on("z"),
	// Open everything opens the leaves too, each to an empty child table
	// and its ghost, which fold all leaves folded (app.md A4, Two folds;
	// create.md C8). Palette only, as fold all is.
	openEverything: newChord("open every row, leaves too (nested)").on("Z"),
	// Show's own: what its issue's type is expanded by, for this page and
	// the ones it opens, and a link added or removed, inverses written to
	// the other issue (A6, A7; show_palette.go).
	expand:   newChord("show: change what is expanded").on("e"),
	link:     newChord("show: link to an issue").on("l"),
	unlink:   newChord("show: unlink an issue").on("u"),
	newIssue: newChord("new issue").on("n"),
	// search is the finder over every issue; enter on one opens it
	// (doc/design/app.md, A5)
	search: newChord("search issues").on("s"),
	// home is back down the stack to the home page, or the home flow run
	// where the stack did not start from one; run a flow picks any stored
	// flow and runs it (A8, home.go).
	home:    newChord("home").on("h"),
	runFlow: newChord("run a flow").on("r"),
	help:    newChord("help", "?").on("?"),
	// esc opens the palette, and a second esc is back: out of a filter, out
	// of an issue, and from the first page out of the program (A1).
	back: newChord("commands; twice: back (first page: quit)", "esc", "ctrl+g"),
	// ctrl+q is the quit nothing swallows, at once, from anywhere.
	quit: newChord("quit now", "ctrl+q").on("q"),

	cancel: newChord("cancel", "esc", "ctrl+g"),
}

// helpRows is the order the help lists the actions in.
func helpRows() []*chord {
	return []*chord{
		keys.up, keys.down, keys.left, keys.right,
		keys.pageUp, keys.pageDn, keys.top, keys.bottom,
		keys.next, keys.previous,
		keys.act, keys.edit, keys.grab, keys.newline,
		keys.copy, keys.copyId, keys.paste,
		keys.filter, keys.foldAll, keys.openEverything, keys.expand, keys.link, keys.unlink, keys.nextTab, keys.previousTab,
		keys.search, keys.newIssue, keys.home, keys.runFlow, keys.back, keys.help, keys.quit,
	}
}

// help is the overlay `?` opens: the keymap, generated from it.
type help struct{}

// Update says when the help is closed: on a key that means "out", and on
// nothing else, so that a stray key does not throw away what a person was
// reading.
func (h *help) Update(press tea.KeyPressMsg) (closed bool) {
	return keys.back.matches(press) || keys.quit.matches(press) || keys.help.matches(press)
}

// View is one row per action: its keys, its letter on the palette, and what
// it does.
func (h *help) View(width int) string {
	lines := []string{styleHeader.Render("keys"), ""}
	for _, row := range helpRows() {
		letter := ""
		if row.letter != "" {
			letter = "esc " + row.letter
		}
		lines = append(lines, fit("  "+pad(row.spelling(), 32)+pad(letter, 8)+row.what, width))
	}
	lines = append(lines, "",
		styleDim.Render(fit("  esc x is alt+x · esc ?: close", width)))
	return strings.Join(lines, "\n")
}

// bell is the terminal's own blink: what a key that has nothing to do here
// answers with, alongside the status line saying why.
func bell() tea.Cmd {
	return tea.Raw("\a")
}
