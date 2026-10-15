package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
)

// The palette is the panel `esc` opens at the foot of the page
// (doc/design/app.md, A1, A4, 2026-10-15): where you are, this page's
// commands, and everywhere's commands, each drawn with its letter.
//
// The keys a person needs with no menu are the ones every program has —
// arrows, enter, space, tab, page up and down, home and end — and every other
// command is here, under a letter that means one thing on every page, so the
// menu teaches the keys it lists. A letter is the command's alt key (A2):
// pressed on the palette, it is handed to the page as alt+letter, so `esc w`
// typed slowly and `esc w` typed quickly, which arrives as alt+w, are one
// key and one code path.
//
// A second `esc` is back: off the page, and from the first page out of the
// program, so one stray `esc` still cannot end a session. That was the call
// line's double press (terminal-renderer.md, 2026-09-28), and the call line
// is the palette's first section now.
//
// While a page is editing, filtering, grabbing or showing the help, `esc` is
// that state's own and cancels it; the palette opens only on a page at rest.

// paletteEntry is one entry on the palette: its letter and what it does here.
type paletteEntry struct {
	letter string
	what   string
}

// entry is a chord as the palette lists it, under a page's own wording
// where the page gives one.
func entry(c *chord, what ...string) paletteEntry {
	if len(what) > 0 {
		return paletteEntry{letter: c.letter, what: what[0]}
	}
	return paletteEntry{letter: c.letter, what: c.what}
}

// everywhere is the palette's last section: the commands that mean the same
// on every page. Search (s), home (h) and run a flow (r) join it with the
// features they name (A5, A8).
func everywhere() []paletteEntry {
	return []paletteEntry{
		entry(keys.newIssue, "new"),
		entry(keys.help, "help"),
		entry(keys.quit, "quit"),
	}
}

// pageCommands is the palette's middle section: what this page answers.
// Change what is expanded (e), link (l) and unlink (u) join it with the
// features they name (A6, A7), as entries here.
func pageCommands(p page) []paletteEntry {
	filter := entry(keys.filter, "filter")
	foldAll := entry(keys.foldAll, "fold all")
	copyCell := entry(keys.copy)
	copyId := entry(keys.copyId)
	switch p := p.(type) {
	case *listPage:
		out := []paletteEntry{filter}
		if len(p.call.Raw("expand")) > 0 {
			out = append(out, foldAll)
		}
		return append(out, copyCell, copyId)
	case *ganttPage:
		out := []paletteEntry{filter}
		if len(p.call.Raw("expand")) > 0 {
			out = append(out, foldAll)
		}
		return append(out, copyId)
	case *boardPage:
		return []paletteEntry{filter, copyId}
	case *matrixPage:
		return []paletteEntry{filter, copyCell, entry(keys.copyId, "copy command")}
	case *showPage:
		if p.current().stop == stopSide {
			return []paletteEntry{filter, copyCell, copyId}
		}
		return []paletteEntry{copyCell, copyId}
	case *newPage:
		return []paletteEntry{copyCell}
	}
	return nil
}

// atRest says the page reads its own keys: nothing is being edited,
// filtered or grabbed, and the help is not open. Only then is esc the
// palette, and a letter text to filter by.
func atRest(p page) bool {
	switch p := p.(type) {
	case *listPage:
		return p.help == nil && p.editor == nil && p.filtering == nil && p.grabbed < 0
	case *boardPage:
		return p.help == nil && p.filtering == nil && p.grabbed < 0
	case *ganttPage:
		return p.help == nil && p.filtering == nil && p.grabbed < 0
	case *matrixPage:
		return p.help == nil && p.filtering == nil
	case *showPage:
		return p.help == nil && p.editor == nil && p.filtering == nil && p.grab == nil &&
			!p.editingDesc && !p.inText()
	case *newPage:
		return p.help == nil && p.editor == nil && !p.inText()
	}
	return true
}

// typingFilters says a printable key starts this page's filter (A3): a list,
// a board, a gantt, the kinds whose rows are what is typed after.
func typingFilters(p page) (start func(), ok bool) {
	switch p := p.(type) {
	case *listPage:
		return p.startFilter, true
	case *boardPage:
		return p.startFilter, true
	case *ganttPage:
		return p.startFilter, true
	}
	return nil, false
}

// printable is a key that types a character: no ctrl, alt or cmd with it,
// and not space, which edits.
func printable(press tea.KeyPressMsg) bool {
	return press.Text != "" && press.Text != " " &&
		press.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// letter is the palette letter a key press is, if any chord has it.
func letter(press tea.KeyPressMsg) (string, bool) {
	if !printable(press) {
		return "", false
	}
	for _, c := range helpRows() {
		if c.letter != "" && c.letter == press.Text {
			return c.letter, true
		}
	}
	return "", false
}

// altOf is the key a letter is (A2).
func altOf(letter string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(letter)[0], Mod: tea.ModAlt}
}

// repoOf is the store the page draws, for a command the stack runs itself.
func repoOf(p page) *cache.RepoCache {
	switch p := p.(type) {
	case *listPage:
		return p.repo
	case *boardPage:
		return p.repo
	case *ganttPage:
		return p.repo
	case *matrixPage:
		return p.repo
	case *showPage:
		return p.repo
	case *newPage:
		return p.repo
	}
	return nil
}

// showsOf is the `show` map the page opens issues by, passed on to the
// creator so that what it opens is opened the same way (show_map.go).
func showsOf(p page) json.RawMessage {
	if p, ok := p.(*newPage); ok {
		return p.shows
	}
	call, _, _ := p.Call()
	return call.Raw("show")
}

// openCreator is `n`: the creator on an empty draft, over the page, its type
// the first thing chosen (doc/design/create.md).
func openCreator(p page) tea.Cmd {
	repo := repoOf(p)
	if repo == nil {
		return bell()
	}
	return openNew(repo, showsOf(p), host.IssueDocument{})
}

// paletteHints is the status line while the palette is open.
func paletteHints() string {
	return hints(hint{"letter", "run"}, hint{"esc", "back"}, hint{"ctrl+c", "copy command"})
}

// breadcrumb is the stack, oldest first: each page's kind and what follows
// it on its first line.
func breadcrumb(pages []page) string {
	parts := make([]string, 0, len(pages))
	for _, p := range pages {
		call, lead, _ := p.Call()
		part := call.Kind
		if lead != "" {
			part += " " + lead
		}
		parts = append(parts, part)
	}
	last := len(parts) - 1
	head := strings.Join(parts[:last], " › ")
	if head != "" {
		head = styleDim.Render(head + " › ")
	}
	return head + styleHeader.Render(parts[last])
}

// section is one of the palette's command lines: its name, then its
// commands, wrapped to the window.
func section(name string, commands []paletteEntry, width int) []string {
	if len(commands) == 0 {
		return nil
	}
	label := pad(name, 12)
	indent := strings.Repeat(" ", 12)
	var lines []string
	line := label
	used := ansi.StringWidth(label)
	for at, c := range commands {
		item := styleHeader.Render(c.letter) + " " + c.what
		w := ansi.StringWidth(item)
		if at > 0 {
			if used+3+w > width {
				lines = append(lines, line)
				line, used = indent, len(indent)
			} else {
				line += "   "
				used += 3
			}
		}
		line += item
		used += w
	}
	return append(lines, fit(line, width))
}

// paletteLines is the panel: a rule, where you are — the stack, and the top
// page's whole call, its query formatted — then this page's commands and
// everywhere's. The call gives up its query's last lines where the window
// cannot hold all of it.
func (r *root) paletteLines(room int) []string {
	top := r.top()
	call, lead, leadArg := top.Call()
	where := callLines(call, lead, leadArg, r.width)
	commands := append(section("this page", pageCommands(top), r.width),
		section("everywhere", everywhere(), r.width)...)

	fixed := 2 + len(commands) // the rule and the breadcrumb
	if spare := room - fixed; len(where) > spare {
		where = where[:max(spare, 1)]
	}
	lines := []string{
		styleRule.Render(strings.Repeat("─", max(r.width, 1))),
		fit(breadcrumb(r.pages), r.width),
	}
	lines = append(lines, where...)
	return append(lines, commands...)
}

// withPalette draws the palette over the foot of the page, above a status
// line of its own: the page keeps its first lines, the call among them.
func (r *root) withPalette(content string) string {
	lines := strings.Split(content, "\n")
	height := max(r.height, len(lines))
	panel := r.paletteLines(height - 1)
	keep := max(height-1-len(panel), 0)
	if keep > len(lines)-1 {
		keep = len(lines) - 1
	}
	out := append([]string{}, lines[:max(keep, 0)]...)
	for len(out) < height-1-len(panel) {
		out = append(out, "")
	}
	out = append(out, panel...)
	return strings.Join(append(out, bottomLine(paletteHints(), "", r.width)), "\n")
}

// paletteKey is a key with the palette open: esc goes back, a letter runs
// its command, the copy key copies the call as the shell command that draws
// the page, and anything else closes the palette — a direction, enter or tab
// does only that, and every other key then means what it means on the page.
func (r *root) paletteKey(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keys.back.matches(press):
		r.palette = false
		return r, r.back()
	case keys.copy.matches(press) && press.Mod&tea.ModAlt == 0:
		call, _, _ := r.top().Call()
		r.palette = false
		return r, tea.Batch(tea.SetClipboard(command(call)), r.say("copied the command"))
	}
	if l, ok := letter(press); ok {
		r.palette = false
		return r.Update(altOf(l))
	}
	if printable(press) {
		// a letter that is no command: the palette stays, and says so
		return r, bell()
	}
	r.palette = false
	switch {
	case keys.act.matches(press), keys.next.matches(press), keys.previous.matches(press),
		keys.up.matches(press), keys.down.matches(press), keys.left.matches(press), keys.right.matches(press):
		return r, nil
	}
	return r.Update(press)
}

// back is the page's own back, which the second esc is: out of a filter,
// out of an issue — a draft warned about first — and from the first page
// out of the program.
func (r *root) back() tea.Cmd {
	top, cmd := r.top().Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r.pages[len(r.pages)-1] = top
	return cmd
}

// restKey is a key on a page at rest, before the page sees it: esc opens
// the palette, the commands that are the stack's run here, and on a list, a
// board or a gantt a character starts the filter with it. It answers false
// for a key the page reads itself.
func (r *root) restKey(press tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keys.back.matches(press):
		r.palette = true
		return nil, true
	case press.String() == "alt+esc":
		// esc esc typed quickly, which a terminal sends as one key
		return r.back(), true
	case keys.quit.matches(press):
		return tea.Quit, true
	case keys.newIssue.matches(press):
		return openCreator(r.top()), true
	case keys.help.matches(press), keys.filter.matches(press):
		return nil, false
	}
	if start, ok := typingFilters(r.top()); ok && printable(press) {
		start()
		top, cmd := r.top().Update(press)
		r.pages[len(r.pages)-1] = top
		return cmd, true
	}
	return nil, false
}
