package tui

import (
	"charm.land/bubbles/v2/key"
)

// keymap is every key the renderer reads.
//
// Each motion is bound three ways at once — arrows, vi letters, and the
// emacs/readline control keys — because the three sets are muscle memory for
// three different people and none of them conflict. The help lists all three
// spellings, so nobody has to guess which one this program chose.
type keymap struct {
	up      key.Binding
	down    key.Binding
	left    key.Binding
	right   key.Binding
	pageUp  key.Binding
	pageDn  key.Binding
	top     key.Binding
	bottom  key.Binding
	open    key.Binding
	back    key.Binding
	edit    key.Binding
	comment key.Binding
	yank    key.Binding
	filter  key.Binding
	grab    key.Binding
	help    key.Binding
	quit    key.Binding
	submit  key.Binding
	cancel  key.Binding
}

var keys = keymap{
	up:     key.NewBinding(key.WithKeys("up", "k", "ctrl+p"), key.WithHelp("↑/k/ctrl+p", "up")),
	down:   key.NewBinding(key.WithKeys("down", "j", "ctrl+n"), key.WithHelp("↓/j/ctrl+n", "down")),
	left:   key.NewBinding(key.WithKeys("left", "h", "ctrl+b"), key.WithHelp("←/h/ctrl+b", "previous column")),
	right:  key.NewBinding(key.WithKeys("right", "l", "ctrl+f"), key.WithHelp("→/l/ctrl+f", "next column")),
	pageUp: key.NewBinding(key.WithKeys("pgup", "ctrl+u", "alt+v"), key.WithHelp("pgup/ctrl+u/alt+v", "page up")),
	pageDn: key.NewBinding(key.WithKeys("pgdown", "ctrl+d", "ctrl+v"), key.WithHelp("pgdown/ctrl+d/ctrl+v", "page down")),
	// alt+< and alt+> are shift keys, and a terminal spells them either as
	// the character or as the modifier, so both spellings are bound.
	top: key.NewBinding(key.WithKeys("home", "g", "alt+<", "alt+shift+,"),
		key.WithHelp("home/g/alt+<", "first")),
	bottom: key.NewBinding(key.WithKeys("end", "G", "shift+g", "alt+>", "alt+shift+."),
		key.WithHelp("end/G/alt+>", "last")),
	open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "accept")),
	back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit the field")),
	comment: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
	yank:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank the id")),
	filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	grab:    key.NewBinding(key.WithKeys(" ", "space"), key.WithHelp("space", "grab and drop a row")),
	help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "this help")),
	quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q/ctrl+c", "quit")),
	submit:  key.NewBinding(key.WithKeys("ctrl+s", "ctrl+d"), key.WithHelp("ctrl+s/ctrl+d", "submit")),
	cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// helpLines is the overlay `?` draws: every key, with all three spellings.
func helpLines() []string {
	rows := []struct {
		binding key.Binding
		what    string
	}{
		{keys.up, "move up"},
		{keys.down, "move down"},
		{keys.left, "previous column"},
		{keys.right, "next column"},
		{keys.pageUp, "page up"},
		{keys.pageDn, "page down"},
		{keys.top, "first row"},
		{keys.bottom, "last row"},
		{keys.open, "accept what is typed"},
		{keys.back, "back, or clear the filter"},
		{keys.edit, "edit the field under the cursor"},
		{keys.comment, "write a comment"},
		{keys.yank, "yank the id to the clipboard"},
		{keys.filter, "filter the rows"},
		{keys.grab, "grab a row, then drop it (needs rank)"},
		{keys.help, "this help"},
		{keys.quit, "quit"},
	}

	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, styleHeader.Render("keys"), "")
	for _, row := range rows {
		spellings := ""
		for at, k := range row.binding.Keys() {
			if at > 0 {
				spellings += " "
			}
			spellings += k
		}
		lines = append(lines, "  "+pad(spellings, 26)+row.what)
	}
	lines = append(lines, "", styleDim.Render("  any key closes this"))
	return lines
}
