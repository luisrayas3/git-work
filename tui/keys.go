package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// One keymap (doc/design/tui.md, "One keymap").
//
// The standard keys and emacs's ctrl and alt keys are read, and nothing
// else: not vim's single letters, because a letter at the top level is worth
// more as text, and typing on a list, a board or a gantt filters it.
// The keys a person needs with no menu are the ones every program has; every
// other command is on the palette `esc` opens, under a letter (palette.go).
//
// A command's letter is also its alt key, by definition (doc/design/tui.md,
// "`esc` is the alt prefix"): a terminal sends alt and a letter as ESC then
// the letter, which is what esc then the letter typed quickly sends, so the
// two are one key on every terminal.
// That is why the letters agree with emacs's alt keys wherever emacs has one:
// w copies, v pages up, < and > go to the first and the last.
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
	shown []string

	binding key.Binding
}

func newChord(what string, keys ...string) *chord {
	c := &chord{what: what, keys: keys}
	c.binding = key.NewBinding(key.WithKeys(keys...))
	return c
}

// on gives the action a letter on the palette, and with it the alt key the
// letter is (doc/design/tui.md, "`esc` is the alt prefix"). A shifted letter is
// also spelled with the modifier by a terminal that reports it rather than
// the character — alt+shift+, for alt+< — so both spellings are bound.
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
var shiftedLetters = map[string]string{"<": ",", ">": ".", "?": "/", "Z": "z", "J": "j"}

func (c *chord) show(spellings ...string) *chord {
	c.shown = spellings
	return c
}

// matches says whether a key press is this action.
func (c *chord) matches(press tea.KeyPressMsg) bool {
	return key.Matches(press, c.binding)
}

// spellings are how the action's keys read in the help, one per
// alternative; its alt key is the help's own column, not one of these.
func (c *chord) spellings() []string {
	if c.shown != nil {
		return c.shown
	}
	parts := make([]string, 0, len(c.keys))
	for _, k := range c.keys {
		// two spellings of one key are one key to a person reading
		if _, doubled := doubles[k]; doubled {
			continue
		}
		parts = append(parts, readable(k))
	}
	return parts
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
	external              *chord
	copy, copyId, paste   *chord
	filter                *chord
	grab                  *chord
	foldAll               *chord
	openEverything        *chord
	expand, link, unlink  *chord
	deleteComment         *chord
	editQuery, resetQuery *chord
	nextTab, previousTab  *chord
	newIssue, search      *chord
	home, runFlow         *chord
	closePalette          *chord
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

	// Enter opens and space edits (doc/design/tui.md, "Enter opens, space
	// edits"): enter opens the issue under the cursor, or the one a link
	// names, and sends a comment being typed; space edits the cell under the
	// cursor, starts typing in the comment box, and on what has no cell — an
	// id, a card, a bar — grabs. Not ctrl+enter, which is enter on every
	// terminal without the kitty keyboard protocol: a key that is one key
	// here and another there is not a key the renderer reads.
	act:  newChord("open issue, follow link; typing: send comment", "enter"),
	edit: newChord("edit cell (a link: change it); on the box: write", "space", " "),
	// A newline in a comment: alt+enter is escape, enter, which every
	// terminal sends; shift+enter only where the terminal tells it from enter.
	newline: newChord("typing: newline in comment", "alt+enter", "shift+enter"),
	// A box's text written in git's editor: show's comment box and its
	// description, new's description (doc/design/views.md, "The external
	// editor"). alt+e is no textarea key, so it works from inside the text
	// as well as on the box.
	external: newChord("on a text box: write it in the editor").on("e"),

	// Copy and paste are the terminal's first: cmd+c and ctrl+shift+c copy
	// what the mouse selected, and cmd+v and ctrl+shift+v paste, arriving as
	// a bracketed paste rather than a key. Where the terminal hands a copy
	// key over instead of keeping it, it copies the cell under the cursor;
	// ctrl+c is that key everywhere, because no terminal keeps it.
	copy:   newChord("copy cell", "ctrl+c", "super+c", "ctrl+shift+c", "super+shift+c").on("w"),
	copyId: newChord("copy id").on("c"),
	// A paste key that reaches the program asks the terminal for its
	// clipboard over OSC 52. ctrl+v is emacs's page down: a standard
	// user's ctrl+v is the terminal's paste, or ^V, and never a paste here.
	paste: newChord("paste into field", "super+v", "ctrl+shift+v", "super+shift+v", "ctrl+y").
		show("terminal paste", "ctrl+y"),

	// Typing filters a list, a board or a gantt too (doc/design/tui.md,
	// "Typing filters"); `/` starts the filter with nothing typed.
	filter: newChord("filter (or type)", "/", "ctrl+s").on("f"),
	grab:   newChord("on id, card, bar: grab / drop (move it)", "space", " "),
	// One nested row is folded with space on its arrow, the cell after its
	// id; the whole tree at once is a palette command, a letter on a page
	// being text (doc/design/tui.md, "A letter means one thing on every
	// page"). Tab and
	// shift-tab, the next and previous stop, are into the first child and up
	// to the parent.
	foldAll: newChord("fold / unfold every row (nested)").on("z"),
	// Open everything opens the leaves too, each to an empty child table
	// and its ghost, which fold all leaves folded (doc/design/views.md, "A
	// leaf unfolds to an empty table"). Palette only, as fold all is.
	openEverything: newChord("open every row, leaves too (nested)").on("Z"),
	// Show's own: what its issue's type is expanded by, for this page and
	// the ones it opens, and a link added or removed, inverses written to
	// the other issue (doc/design/tui.md, "Changing what is expanded";
	// doc/design/tui.md, "Linking from the palette"; show_palette.go).
	expand: newChord("show: change what is expanded").on("x"),
	link:   newChord("show: link to an issue").on("l"),
	unlink: newChord("show: unlink an issue").on("u"),
	// Delete is final and replicated, so it is a chord, never one stray
	// key, and asks nothing first: there is no confirmation in the renderer
	// (doc/design/tui.md, "Deleting a comment").
	deleteComment: newChord("show: delete the comment under the cursor (comments tab)").on("d"),
	// The query is edited in the palette and the page drawn again from it;
	// the shifted letter undoes it, as Z is z's further step
	// (doc/design/tui.md, "Editing the query"; query_edit.go).
	editQuery:  newChord("edit the query").on("j"),
	resetQuery: newChord("reset the query to the call's").on("J"),
	newIssue:   newChord("new issue").on("n"),
	// search is the finder over every issue; enter on one opens it
	// (doc/design/tui.md, "The search")
	search: newChord("search issues").on("s"),
	// home is back down the stack to the home page, or the home flow run
	// where the stack did not start from one; run a flow picks any stored
	// flow and runs it (doc/design/flows.md, "How a page runs a flow";
	// home.go).
	home:    newChord("home").on("h"),
	runFlow: newChord("run a flow").on("r"),
	help:    newChord("help", "?").on("?"),
	// g closes the palette and does nothing else, as emacs's C-g quits what
	// is half typed; esc g typed quickly is alt+g, which nothing on a page
	// binds (doc/design/tui.md, "Keys on the palette").
	closePalette: newChord("close the palette").on("g"),
	// esc opens the palette, and a second esc is back: out of a filter, out
	// of an issue, and from the first page out of the program
	// (doc/design/tui.md, "Back is two presses").
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
		keys.act, keys.edit, keys.grab, keys.newline, keys.external,
		keys.copy, keys.copyId, keys.paste,
		keys.filter, keys.foldAll, keys.openEverything, keys.expand, keys.link, keys.unlink, keys.deleteComment, keys.editQuery, keys.resetQuery, keys.nextTab, keys.previousTab,
		keys.search, keys.newIssue, keys.home, keys.runFlow, keys.closePalette, keys.back, keys.help, keys.quit,
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

// View is one row per action: its keys, each alternative in a column of its
// own, its alt key, its letter on the palette, and what it does, under dim
// column headers that say which is which; the page's bottom line is under it.
//
// The bottom line is the one place a key is hinted
// (doc/design/tui.md, "Key hints are on the bottom line only"),
// and esc is the key it names: the first esc closes the help, so nothing
// drawn past it is ever read.
func (h *help) View(width, height int) string {
	rows := helpRows()
	var widths []int
	altWidth := ansi.StringWidth("alt")
	for _, row := range rows {
		for i, k := range row.spellings() {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(k))
		}
		if row.letter != "" {
			altWidth = max(altWidth, ansi.StringWidth("alt+"+row.letter))
		}
	}
	const gap = 2
	keysWidth := 0
	for _, w := range widths {
		keysWidth += w + gap
	}
	keysWidth = max(keysWidth, ansi.StringWidth("keys")+gap)
	letterWidth := max(ansi.StringWidth("esc x"), ansi.StringWidth("palette"))

	headers := "  " + pad("keys", keysWidth) + pad("alt", altWidth+gap) +
		pad("palette", letterWidth+gap) + "action"
	lines := []string{styleHeader.Render("help"), "", styleDim.Render(fit(headers, width))}
	for _, row := range rows {
		var keysCell strings.Builder
		spelled := row.spellings()
		for i, w := range widths {
			k := ""
			if i < len(spelled) {
				k = spelled[i]
			}
			keysCell.WriteString(pad(k, w+gap))
		}
		alt, letter := "", ""
		if row.letter != "" {
			alt, letter = "alt+"+row.letter, "esc "+row.letter
		}
		line := "  " + pad(keysCell.String(), keysWidth) +
			pad(alt, altWidth+gap) + pad(letter, letterWidth+gap) + row.what
		lines = append(lines, fit(line, width))
	}

	// the table is cut before the bottom line is: the line is the way out,
	// and a window too short for the table can be made taller
	if height > 1 && len(lines) > height-1 {
		lines = lines[:height-1]
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	bottom := styleStatus.Render(bottomLine(hintText(hint{"esc", "close"}), "", width))
	return strings.Join(append(lines, bottom), "\n")
}

// bell is the terminal's own blink: what a key that has nothing to do here
// answers with, alongside the status line saying why.
func bell() tea.Cmd {
	return tea.Raw("\a")
}
