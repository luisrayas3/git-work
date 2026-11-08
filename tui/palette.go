package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
)

// The palette is the box `esc` opens in the middle of the page
// (doc/design/tui.md, "The palette is a box over the page"): where you are,
// this page's commands, and everywhere's commands, each drawn with its
// letter.
//
// The keys a person needs with no menu are the ones every program has —
// arrows, enter, space, tab, page up and down, home and end — and every other
// command is here, under a letter that means one thing on every page, so the
// menu teaches the keys it lists (doc/design/tui.md, "A letter means one
// thing on every page"). A letter is the command's alt key
// (doc/design/tui.md, "Keys on the palette"): pressed on the palette, it is
// handed to the page as alt+letter, so `esc w` typed slowly and `esc w` typed
// quickly, which arrives as alt+w, are one key and one code path.
//
// A second `esc` is back: off the page, and from the first page out of the
// program, so one stray `esc` cannot end a session (doc/design/tui.md, "Back
// is two presses").
//
// In a text — show's comment box and description editor, new's description
// box — `esc` opens the palette too, over the text, which stays focused
// underneath, and a second `esc` leaves the text, its draft kept. From a
// text the palette offers only the text's commands, and `g` back to it
// (doc/design/tui.md, "The palette over a text").
//
// While a page is editing a one-line input, filtering, grabbing or showing
// the help, `esc` is that state's own and cancels it.

// paletteEntry is one entry on the palette: its key and what it does here.
// The key is a letter but for the palette's own esc, which the box lists
// too (doc/design/tui.md, "Keys on the palette").
type paletteEntry struct {
	key  string
	what string
}

// escBack and escLeave are the palette's second esc as the box lists it: back
// on a page, out of the text over a text.
var (
	escBack  = paletteEntry{key: "esc", what: "back"}
	escLeave = paletteEntry{key: "esc", what: "leave text"}
)

// copyPageKey is the copy key as the breadcrumb line names it, dim at its
// end, on a page's palette only: over a text it rings.
const copyPageKey = "ctrl+c copy page command"

// entry is a chord as the palette lists it, under a page's own wording
// where the page gives one.
func entry(c *chord, what ...string) paletteEntry {
	if len(what) > 0 {
		return paletteEntry{key: c.letter, what: what[0]}
	}
	return paletteEntry{key: c.letter, what: c.what}
}

// everywhere is the palette's last section: the commands that mean the same
// on every page, in this order: search, new, home, run a flow, help, quit;
// then close, which is the palette's own.
func everywhere() []paletteEntry {
	return []paletteEntry{
		entry(keys.search, "search"),
		entry(keys.newIssue, "new"),
		entry(keys.home),
		entry(keys.runFlow),
		entry(keys.help, "help"),
		entry(keys.quit, "quit"),
		entry(keys.closePalette, "close"),
	}
}

// pageCommands is the palette's middle section: what this page answers,
// then edit the query (j) where the kind has one, and reset it (J) where it
// was edited (doc/design/tui.md, "Editing the query"; query_edit.go).
func (r *root) pageCommands() []paletteEntry {
	out := pageCommands(r.top())
	if hasQuery(r.top()) {
		out = append(out, entry(keys.editQuery, "edit query"))
		if _, edited := r.called[len(r.pages)-1]; edited {
			out = append(out, entry(keys.resetQuery, "reset query"))
		}
	}
	return out
}

// pageCommands is what a page answers of its own.
// A show page adds change what is expanded (x), link (l) and unlink (u)
// (doc/design/tui.md, "Changing what is expanded"; doc/design/tui.md,
// "Linking from the palette"; show_palette.go), and on a text box, show's
// or new's, the editor (e; doc/design/views.md, "The external editor").
func pageCommands(p page) []paletteEntry {
	filter := entry(keys.filter, "filter")
	foldAll := entry(keys.foldAll, "fold all")
	openEverything := entry(keys.openEverything, "open everything")
	copyCell := entry(keys.copy)
	copyId := entry(keys.copyId)
	switch p := p.(type) {
	case *listPage:
		out := []paletteEntry{filter}
		if len(p.call.Raw("expand")) > 0 {
			out = append(out, foldAll, openEverything)
		}
		return append(out, copyCell, copyId)
	case *ganttPage:
		out := []paletteEntry{filter}
		if len(p.call.Raw("expand")) > 0 {
			out = append(out, foldAll, openEverything)
		}
		return append(out, copyId)
	case *boardPage:
		return []paletteEntry{filter, copyId}
	case *matrixPage:
		return []paletteEntry{filter, copyCell, entry(keys.copyId, "copy command")}
	case *showPage:
		own := []paletteEntry{entry(keys.expand, "expand"), entry(keys.link, "link"), entry(keys.unlink, "unlink")}
		if p.onComment() {
			own = append(own, entry(keys.deleteComment, "delete comment"))
		}
		if here := p.current().stop; here == stopBox || (here == stopTabs && p.tab == tabDescription) {
			own = append([]paletteEntry{entry(keys.external, "editor")}, own...)
		}
		if p.current().stop == stopSide {
			return append([]paletteEntry{filter, copyCell, copyId}, own...)
		}
		return append([]paletteEntry{copyCell, copyId}, own...)
	case *newPage:
		if p.current().stop == stopBox {
			return []paletteEntry{copyCell, entry(keys.external, "editor")}
		}
		return []paletteEntry{copyCell}
	case *outputPage:
		return []paletteEntry{entry(keys.copy, "copy output")}
	}
	return nil
}

// textCommands are the palette's commands from a text, before close: write
// the text in git's editor (doc/design/views.md, "The external editor").
// None of their letters is one of the
// textarea's alt keys, so that `alt+<letter>` and `esc <letter>` are one key
// in the text too (doc/design/tui.md, "The palette over a text").
func textCommands() []paletteEntry {
	return []paletteEntry{entry(keys.external, "editor")}
}

// isTextCommand says a letter is one of textCommands.
func isTextCommand(letter string) bool {
	for _, c := range textCommands() {
		if c.key == letter {
			return true
		}
	}
	return false
}

// inText says the cursor is in a text, a box whose keys are text: show's
// comment box or description editor, new's description box. Its esc opens
// the palette, as a page at rest does.
func inText(p page) bool {
	switch p := p.(type) {
	case *showPage:
		return p.help == nil && p.editor == nil && p.filtering == nil && p.grab == nil &&
			p.typingIn() != nil
	case *newPage:
		return p.help == nil && p.editor == nil && p.inText()
	}
	return false
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
	case *homePage:
		// typed into, its esc clears the text first, as a picker's does
		// (doc/design/tui.md, "Pickers are typed into")
		return p.help == nil && p.picker.text() == ""
	case *outputPage:
		return p.help == nil
	}
	return true
}

// typingFilters says a printable key starts this page's filter
// (doc/design/tui.md, "Typing filters"): a list, a board, a gantt, the kinds
// whose rows are what is typed after.
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

// altOf is the key a letter is (doc/design/tui.md, "`esc` is the alt
// prefix").
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
	case *homePage:
		return p.repo
	case *outputPage:
		return p.repo
	}
	return nil
}

// showsOf is the `shows` the page opens issues by, passed on to the
// creator so that what it opens is opened the same way (show_map.go).
func showsOf(p page) json.RawMessage {
	if p, ok := p.(*newPage); ok {
		return p.shows
	}
	call, _, _ := p.Call()
	return call.Raw("shows")
}

// openCreator is `n`: the creator on an empty draft, over the page, its type
// the first thing chosen (doc/design/views.md, "The creator page").
func openCreator(p page) tea.Cmd {
	repo := repoOf(p)
	if repo == nil {
		return bell()
	}
	return openNew(repo, showsOf(p), host.IssueDocument{})
}

// breadcrumbLine is the palette's first line: the breadcrumb, and on a
// page's palette the copy key dim at the line's end, beside the call it
// copies. Where the line cannot hold both, the breadcrumb is cut from its
// left, the top page's step last to go; the copy key is dropped rather than
// cut into that step (doc/design/tui.md, "Where you are").
func breadcrumbLine(pages []page, copyKey bool, width int) string {
	crumb := breadcrumb(pages)
	if !copyKey {
		return fitRight(crumb, width)
	}
	call, lead, _ := pages[len(pages)-1].Call()
	step := call.Kind
	if lead != "" {
		step += " " + lead
	}
	keyWidth := ansi.StringWidth(copyPageKey)
	room := width - keyWidth - 2 // two spaces at least before the key
	need := ansi.StringWidth(step)
	if ansi.StringWidth(crumb) > room {
		need++ // the … the cut breadcrumb starts with
	}
	if room < need {
		return fitRight(crumb, width)
	}
	crumb = fitRight(crumb, room)
	gap := width - ansi.StringWidth(crumb) - keyWidth
	return crumb + strings.Repeat(" ", gap) + styleDim.Render(copyPageKey)
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
// commands from column col, wrapped to the window.
func section(name string, commands []paletteEntry, col, width int) []string {
	if len(commands) == 0 {
		return nil
	}
	label := pad(name, col)
	indent := strings.Repeat(" ", col)
	var lines []string
	line := label
	used := ansi.StringWidth(label)
	for at, c := range commands {
		item := styleHeader.Render(c.key) + " " + c.what
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

// paletteLines is the box's inside: where you are — the stack, whose last
// step is the top page's kind and lead, then the rest of its call, one
// argument a line, and its query in a section of its own — then this page's
// commands and everywhere's, everywhere's ending in esc, all of it width
// wide (doc/design/tui.md, "Where you are"). Every name,
// an argument's or a section's, is in the left column, and what it names
// starts in the one column after it.
func (r *root) paletteLines(width, room int) []string {
	top := r.top()
	col := r.labelWidth()
	commands := append(section("this page", r.pageCommands(), col, width),
		section("everywhere", append(everywhere(), escBack), col, width)...)
	if inText(top) {
		commands = section("this text", append(textCommands(), entry(keys.closePalette, "close"), escLeave), col, width)
	}
	head := r.callHead(col, width)
	query := paletteQuery(top, col, width)

	// the query gives up its last lines where room cannot hold all of it
	fixed := len(head) + 1 + len(commands) // the blank over the commands
	if len(query) > 0 {
		fixed++ // the blank over the query
	}
	if spare := room - fixed; len(query) > spare {
		query = cutQuery(query, max(spare, 1), col, width)
	}
	lines := head
	if len(query) > 0 {
		lines = append(append(lines, ""), query...)
	}
	lines = append(lines, "")
	return append(lines, commands...)
}

// callHead is the palette's first lines: the breadcrumb (breadcrumbLine),
// then every argument
// of the top page's call but its lead and its query, one a line, its name in
// the left column and its value from col, as the query's is. The breadcrumb
// ends in the kind and the lead, so they are not said twice. An
// argument too long for the box wraps rather than being cut: here is where
// it is read in full.
func (r *root) callHead(col, width int) []string {
	call, _, leadArg := r.top().Call()
	args, _, _ := callParts(call, leadArg)
	lines := []string{breadcrumbLine(r.pages, !inText(r.top()), width)}
	for _, arg := range args {
		label := arg.render(arg.name) + strings.Repeat(" ", col-ansi.StringWidth(arg.name))
		for _, wrapped := range strings.Split(ansi.Hardwrap(arg.value, width-col, false), "\n") {
			lines = append(lines, fit(label+arg.render(wrapped), width))
			label = strings.Repeat(" ", col)
		}
	}
	return lines
}

// labelWidth is the palette's left column, where every name is: wide enough
// for the section names, and for the longest argument name the top page's
// call draws, with two spaces after it.
func (r *root) labelWidth() int {
	call, _, leadArg := r.top().Call()
	args, _, _ := callParts(call, leadArg)
	width := len("everywhere  ")
	for _, arg := range args {
		width = max(width, ansi.StringWidth(arg.name)+2)
	}
	return width
}

// queryMaxLines is the most of a query the palette draws: enough for the
// query a person typed, short of a flow's whole program, whose tail is
// summed up as the lines left out (doc/design/tui.md, "Where you are").
// The query editor holds all
// of it.
const queryMaxLines = 12

// paletteQuery is the palette's query section: the top page's query
// formatted, under the label query, dim where it is the default, at most
// queryMaxLines long. A kind with no query has no section.
func paletteQuery(p page, col, width int) []string {
	call, _, leadArg := p.Call()
	_, query, dim := callParts(call, leadArg)
	if query == "" {
		return nil
	}
	style := styleDim.Faint(false)
	if dim {
		style = styleDim
	}
	body := queryLines(call.String("query"), width-col)
	lines := make([]string, 0, len(body))
	label := pad("query", col)
	for _, line := range body {
		lines = append(lines, fit(label+style.Render(line), width))
		label = strings.Repeat(" ", col)
	}
	return cutQuery(lines, queryMaxLines, col, width)
}

// cutQuery keeps a query section's first lines, n at most, the last of them
// saying how many more there are.
func cutQuery(lines []string, n, col, width int) []string {
	if len(lines) <= n {
		return lines
	}
	kept := append([]string{}, lines[:n-1]...)
	more := fmt.Sprintf("… %d more lines", len(lines)-n+1)
	return append(kept, fit(strings.Repeat(" ", col)+styleDim.Render(more), width))
}

// paletteWidth is the widest the box's inside is drawn: wide enough for a
// formatted query, narrow enough to read as a box over the page and not a
// page of its own.
const paletteWidth = 80

// stylePalette is the palette's box.
var stylePalette = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

// withPalette draws the palette as a box in the middle of the page, over a
// status line of its own (doc/design/tui.md, "The palette is a box over the
// page"): the page is in sight around it, so
// the palette reads as a menu over the page, and not as the page's foot
// changing under it. The line has no hints, the box listing its own keys,
// and keeps the page's last message on its right (doc/design/tui.md, "Key
// hints are on the bottom line only").
func (r *root) withPalette(content string) string {
	inner, room := r.boxRoom(content)
	return r.withBox(content, r.paletteLines(inner, room), "", r.top().Status())
}

// boxRoom is the inside of a box drawn over the page: how wide, and how many
// lines it can hold.
func (r *root) boxRoom(content string) (width, room int) {
	height := max(r.height, strings.Count(content, "\n")+1)
	width = max(min(paletteWidth, r.width-stylePalette.GetHorizontalFrameSize()-2), 10)
	room = max(height-1-stylePalette.GetVerticalFrameSize(), 1)
	return width, room
}

// withBox draws inside, in a box, in the middle of the page, and the status
// line under both, hints on its left and right on its right: the palette,
// the search, the flow picker and the query editor are drawn so.
func (r *root) withBox(content string, inside []string, hints, right string) string {
	lines := strings.Split(content, "\n")
	height := max(r.height, len(lines))
	page := lines[:max(len(lines)-1, 0)] // the page's status line is the box's
	for len(page) < height-1 {
		page = append(page, "")
	}
	box := strings.Split(stylePalette.Render(strings.Join(inside, "\n")), "\n")
	return strings.Join(append(overlay(page, box, r.width), bottomLine(hints, right, r.width)), "\n")
}

// overlay draws box over the middle of page, which keeps its lines and its
// height: what is under the box is hidden, what is around it stays.
func overlay(page, box []string, width int) []string {
	boxWidth := 0
	for _, line := range box {
		boxWidth = max(boxWidth, ansi.StringWidth(line))
	}
	x := max((width-boxWidth)/2, 0)
	y := max((len(page)-len(box))/2, 0)
	out := append([]string{}, page...)
	for at, line := range box {
		if y+at >= len(out) {
			break
		}
		under := out[y+at]
		left := pad(ansi.Truncate(under, x, ""), x)
		right := ansi.TruncateLeft(under, x+boxWidth, "")
		out[y+at] = left + "\x1b[m" + pad(line, boxWidth) + "\x1b[m" + right
	}
	return out
}

// paletteKey is a key with the palette open: esc goes back, a letter runs
// its command, the copy key copies the call as the shell command that draws
// the page, and anything else closes the palette — a direction, enter or tab
// does only that, and every other key then means what it means on the page.
// Over a text, the second esc goes back out of the text, and the letters are
// the text's and `g`, every other one, ctrl+c too, ringing.
func (r *root) paletteKey(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keys.back.matches(press):
		r.palette = false
		return r, r.back()
	case keys.copy.matches(press) && press.Mod&tea.ModAlt == 0:
		if inText(r.top()) {
			// over a text the palette offers only the text's commands
			return r, bell()
		}
		r.palette = false
		return r, tea.Batch(tea.SetClipboard(commandOf(r.top())), r.say("copied the command"))
	}
	if l, ok := letter(press); ok {
		if inText(r.top()) && !isTextCommand(l) {
			if l != keys.closePalette.letter {
				return r, bell()
			}
			r.palette = false
			return r, nil
		}
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

// back is the page's own back, which the second esc is: out of a text, its
// draft kept, out of a filter, out of an issue — a draft warned about first —
// and from the first page out of the program.
func (r *root) back() tea.Cmd {
	top, cmd := r.top().Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	r.pages[len(r.pages)-1] = top
	return cmd
}

// textKey is a key in a text before the page sees it: esc opens the palette
// over the text, and esc esc typed quickly, one key, leaves the text. It
// answers false for every other key, which is the text's.
func (r *root) textKey(press tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keys.back.matches(press):
		r.palette = true
		return nil, true
	case press.String() == "alt+esc":
		return r.back(), true
	}
	return nil, false
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
	case keys.search.matches(press):
		return r.openSearch(), true
	case keys.home.matches(press):
		return r.goHome(), true
	case keys.runFlow.matches(press):
		return r.openFlowPicker(), true
	case keys.editQuery.matches(press):
		return r.openQueryEditor(), true
	case keys.resetQuery.matches(press):
		return r.resetQuery(), true
	case keys.closePalette.matches(press):
		// the palette is closed already, or was never open
		return nil, true
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
