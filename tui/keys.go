package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// family is one of the three sets of muscle memory the renderer reads.
//
// All three are read at once, with no mode: whoever sits down already knows
// one of them. They exist as a type only so that `?` can show each person
// their own set, one tab each, instead of three spellings in every row.
type family int

const (
	standard family = iota
	vim
	emacs
	families
)

func (f family) String() string {
	return [...]string{"standard", "vim", "emacs"}[f]
}

// chord is one action's keys, per family.
//
// A family with no keys of its own reads the standard ones, which every
// family also reads, so every tab of the help is the whole set.
type chord struct {
	what string
	keys [families][]string
	// shown replaces the keys in the help where a family's key is not one the
	// program reads: the terminal's own paste arrives as a paste, not a key.
	shown [families]string

	binding key.Binding
}

func newChord(what string, std, vi, em []string) *chord {
	c := &chord{what: what, keys: [families][]string{std, vi, em}}

	all := make([]string, 0, len(std)+len(vi)+len(em))
	seen := map[string]bool{}
	for _, set := range c.keys {
		for _, k := range set {
			if !seen[k] {
				seen[k] = true
				all = append(all, k)
			}
		}
	}
	c.binding = key.NewBinding(key.WithKeys(all...))
	return c
}

func (c *chord) show(f family, spelling string) *chord {
	c.shown[f] = spelling
	return c
}

// matches says whether a key press is this action in any family.
func (c *chord) matches(press tea.KeyPressMsg) bool {
	return key.Matches(press, c.binding)
}

// spelling is how a family's keys read in the help.
func (c *chord) spelling(f family) string {
	if c.shown[f] != "" {
		return c.shown[f]
	}
	set := c.keys[f]
	if len(set) == 0 {
		set = c.keys[standard]
	}

	parts := make([]string, 0, len(set))
	for _, k := range set {
		// two spellings of one key are one key to a person reading
		if _, doubled := doubles[k]; doubled {
			continue
		}
		parts = append(parts, readable(k))
	}
	return strings.Join(parts, " ")
}

// doubles are the second spellings of a shifted key: a terminal sends either
// the character or the modifier, so both are bound, and one is shown.
var doubles = map[string]struct{}{
	"alt+shift+,": {},
	"alt+shift+.": {},
	"shift+g":     {},
	"shift+y":     {},
	"shift+z":     {},
	" ":           {},
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

func one(k ...string) []string { return k }

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
	nextTab, previousTab  *chord
	help                  *chord
	back                  *chord
	quit                  *chord

	// cancel is the text widgets' own: it is read before the widget sees the
	// key, so it must not be a key a person types text with.
	cancel *chord
}

var keys = keymap{
	up:     newChord("up", one("up"), one("k"), one("ctrl+p")),
	down:   newChord("down", one("down"), one("j"), one("ctrl+n")),
	left:   newChord("previous column / tab", one("left"), one("h"), one("ctrl+b")),
	right:  newChord("next column / tab", one("right"), one("l"), one("ctrl+f")),
	pageUp: newChord("page up", one("pgup"), one("ctrl+u"), one("alt+v")),
	pageDn: newChord("page down", one("pgdown"), one("ctrl+d"), one("ctrl+v")),
	// alt+< and alt+> are shift keys, and a terminal spells them either as
	// the character or as the modifier, so both spellings are bound.
	top:    newChord("first", one("home"), one("g"), one("alt+<", "alt+shift+,")),
	bottom: newChord("last", one("end"), one("G", "shift+g"), one("alt+>", "alt+shift+.")),

	next:     newChord("next stop", one("tab"), nil, nil),
	previous: newChord("previous stop", one("shift+tab"), nil, nil),
	// the tab keys of browsers and editors; vim's gt and gT are read by the
	// show page itself, because g alone is already a key
	nextTab: newChord("next tab", one("ctrl+pgdown"), nil, nil).
		show(vim, "gt ctrl+pgdown"),
	previousTab: newChord("previous tab", one("ctrl+pgup"), nil, nil).
		show(vim, "gT ctrl+pgup"),

	// Enter opens and space edits (doc/design/terminal-renderer.md,
	// 2026-10-02): enter opens the issue under the cursor, or the one a link
	// names, and sends a comment being typed; space edits the cell under the
	// cursor, starts typing in the comment box, and on what has no cell — an
	// id, a card, a bar — grabs. ctrl+enter and f2 are gone, because
	// ctrl+enter is enter on every terminal without the kitty keyboard
	// protocol, and a key that is one key here and another there is not a key
	// the renderer reads (2026-09-28).
	act:  newChord("open issue, follow link; typing: send comment", one("enter"), nil, nil),
	edit: newChord("edit cell (a link: change it); on the box: write", one("space", " "), nil, nil),
	// A newline in a comment: alt+enter is escape, enter, which every
	// terminal sends; shift+enter only where the terminal tells it from enter.
	newline: newChord("typing: newline in comment", one("alt+enter", "shift+enter"), nil, nil),

	// Copy and paste are the terminal's first: cmd+c and ctrl+shift+c copy
	// what the mouse selected, and cmd+v and ctrl+shift+v paste, arriving as
	// a bracketed paste rather than a key. Where the terminal hands a copy
	// key over instead of keeping it, it copies the cell under the cursor;
	// ctrl+c is that key everywhere, because no terminal keeps it.
	copy: newChord("copy cell",
		one("ctrl+c", "super+c", "ctrl+shift+c", "super+shift+c"), one("y"), one("alt+w")),
	copyId: newChord("copy id", one("alt+c"), one("Y", "shift+y"), nil),
	// A paste key that reaches the program asks the terminal for its
	// clipboard over OSC 52. ctrl+v stays emacs's page down: a standard
	// user's ctrl+v is the terminal's paste, or ^V, and never a paste here.
	paste: newChord("paste into field",
		one("super+v", "ctrl+shift+v", "super+shift+v"), one("p"), one("ctrl+y")).
		show(standard, "terminal paste (cmd+v, ctrl+shift+v)"),

	filter: newChord("filter", one("/"), one("/"), one("ctrl+s")),
	grab:   newChord("on id, card, bar: grab / drop (move it)", one("space", " "), nil, nil),
	// One nested row is folded with space on its arrow, the cell after its
	// id, so `z` is gone (2026-10-02); `Z` is the whole tree at once, and
	// tab and shift-tab, the next and previous stop, are into the first
	// child and up to the parent.
	foldAll: newChord("fold / unfold every row (nested)", one("Z", "shift+z"), nil, nil),
	help:    newChord("help", one("?"), nil, nil),
	// Back is always back: out of a filter, out of an issue, and from the
	// first view, twice, out of the program (doc/design/terminal-renderer.md).
	back: newChord("back (twice at top: quit)", one("esc"), one("esc", "q"), one("esc", "ctrl+g")),
	// ctrl+q is the quit nothing swallows, at once, from anywhere.
	quit: newChord("quit now", one("ctrl+q"), nil, nil),

	cancel: newChord("cancel", one("esc"), one("esc"), one("esc", "ctrl+g")),
}

// helpRows is the order the help lists the actions in.
func helpRows() []*chord {
	return []*chord{
		keys.up, keys.down, keys.left, keys.right,
		keys.pageUp, keys.pageDn, keys.top, keys.bottom,
		keys.next, keys.previous,
		keys.act, keys.edit, keys.grab, keys.newline,
		keys.copy, keys.copyId, keys.paste,
		keys.filter, keys.foldAll, keys.nextTab, keys.previousTab,
		keys.back, keys.help, keys.quit,
	}
}

// help is the overlay `?` opens: the keys, one tab per family.
type help struct {
	tab family
}

// Update moves between the tabs, and says when the help is closed.
//
// The keys that close it are the ones that would mean "out" in any family;
// anything else is ignored, so that a stray key does not throw away the tab
// a person was reading.
func (h *help) Update(press tea.KeyPressMsg) (closed bool) {
	switch {
	case keys.right.matches(press), keys.next.matches(press):
		h.tab = (h.tab + 1) % families
	case keys.left.matches(press), keys.previous.matches(press):
		h.tab = (h.tab + families - 1) % families
	case press.String() == "1", press.String() == "2", press.String() == "3":
		h.tab = family(press.String()[0] - '1')
	case keys.back.matches(press), keys.quit.matches(press), keys.help.matches(press):
		return true
	}
	return false
}

func (h *help) View(width int) string {
	tabs := make([]string, 0, families)
	for f := family(0); f < families; f++ {
		label := " " + f.String() + " "
		if f == h.tab {
			label = styleCell.Render(label)
		} else {
			label = styleDim.Render(label)
		}
		tabs = append(tabs, label)
	}

	lines := []string{styleHeader.Render("keys") + "  " + strings.Join(tabs, " "), ""}
	for _, row := range helpRows() {
		lines = append(lines, fit("  "+pad(row.spelling(h.tab), 30)+row.what, width))
	}
	lines = append(lines, "",
		styleDim.Render(fit("  ←/→ tab 1-3: switch · esc ?: close", width)))
	return strings.Join(lines, "\n")
}

// bell is the terminal's own blink: what a key that has nothing to do here
// answers with, alongside the status line saying why.
func bell() tea.Cmd {
	return tea.Raw("\a")
}
