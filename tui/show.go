package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
	"github.com/git-bug/git-bug/view"
)

// showPage is the `show` view: one issue, its fields, and what was said about
// it.
//
// It is both a view of its own — `git work view show '{"id":"…"}'` — and what
// enter opens from the list, because they are the same page and there is no
// reason for two.
//
// The page is four stops, top to bottom: the title, the comment box right
// under it, the fields table, and the tabs — comments, description, log. The
// cursor opens in the box, because opening an issue to say something about
// it is the common case (doc/design/terminal-renderer.md, revised 2026-09-28).
type showPage struct {
	repo *cache.RepoCache

	id    string
	order []string
	call  *view.Call

	snapshot *issue.Snapshot
	log      []cmdjson.IssueOperation
	// rows is the fields table as drawn, rebuilt on every load
	rows []tableRow

	box     *commentBox
	buttons []button

	// focus indexes positions(); it opens on the box.
	focus int
	// row is the table row under the cursor, kept while the cursor is
	// elsewhere so that coming back to the table comes back to the same row.
	row int
	// tab is the tab drawn under the fields.
	tab tab
	// afterG says the last key was vim's g, so t and T are gt and gT.
	afterG bool
	// warned says leaving has already said a draft would be lost; a second
	// try leaves anyway.
	warned bool

	editor *editor
	help   *help

	// offset is the top of the part of the page that scrolls, which is
	// everything under the comment box.
	offset        int
	width, height int
	status        string
}

// button is one action under the comment box.
//
// There is one today. Actions injected into views (deferred) land here, so a
// view invocation that names "comment and close" gets a second button beside
// the first.
type button struct {
	label string
	press func(p *showPage) tea.Cmd
}

// stopKind is one of the four places the cursor can be.
type stopKind int

const (
	stopTitle stopKind = iota
	stopBox
	stopFields
	stopTabs
)

// tab is one of the three tabs under the fields.
type tab int

const (
	tabComments tab = iota
	tabDescription
	tabLog
	tabCount
)

func (t tab) String() string {
	return [...]string{"comments", "description", "log"}[t]
}

// position is one step of Tab: a stop, and inside the box, which part of it —
// the text, or one of its buttons. The buttons are the box's own, not stops
// of their own, and are in the Tab order so that every terminal can send a
// comment, including one that cannot send ctrl+enter.
type position struct {
	stop   stopKind
	button int // -1 for the text
}

// tableRow is one line of the fields table. A field that links several
// issues is a line per issue, so that each is a link the cursor can stand on.
type tableRow struct {
	key string
	// label is the value as drawn; the key is drawn on a field's first line
	label string
	first bool
	// link is the issue this line names, or ""
	link string
}

func newShowPage(repo *cache.RepoCache, id string, fields []string) (*showPage, error) {
	p := &showPage{repo: repo, id: id, order: fields, width: 80, height: 24}
	if err := p.load(); err != nil {
		return nil, err
	}
	// the id is resolved once, to the whole one, so that a refresh after a
	// prefix was typed still finds the same issue
	p.id = p.snapshot.Id().String()

	// the call this page is, for its first line: whether the command line
	// made it or the list's enter did, it is the same call
	args := map[string]json.RawMessage{"id": mustJSON(p.id)}
	if len(fields) > 0 {
		args["fields"] = mustJSON(fields)
	}
	p.call = &view.Call{Kind: view.KindShow, Args: args}

	p.box = newCommentBox(p.width)
	p.buttons = []button{{label: "Submit comment", press: (*showPage).submitComment}}
	p.focus = 1 // the box's text: positions() always starts title, box
	return p, nil
}

func (p *showPage) load() error {
	snapshot, err := host.IssueSnapshot(p.repo, p.id)
	if err != nil {
		return err
	}
	p.snapshot = snapshot

	entries, err := host.IssueLog(p.repo, p.id)
	if err != nil {
		return err
	}
	p.log = entries

	p.rows = p.tableRows()
	return nil
}

// fieldOrder is which fields are shown, and in which order.
//
// The call may name them. Where it does not, the schema does: the type's
// fields in schema order, which is the order they were authored in. Where
// even that is unknown — a store with no schema, an issue whose type is gone —
// the keys sort, because an arbitrary stable order beats a random one.
func (p *showPage) fieldOrder() []string {
	if len(p.order) > 0 {
		return p.order
	}

	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	if s, err := p.repo.LoadSchema(); err == nil {
		if t, ok := s.Type(typeKey); ok {
			keys := make([]string, 0, len(t.Fields))
			for _, key := range t.FieldKeys() {
				keys = append(keys, key)
			}
			return keys
		}
	}

	return sorted.Keys(p.snapshot.Fields)
}

// tableRows is every field but the title, which has a stop of its own; a
// relation is drawn as the issues it names.
func (p *showPage) tableRows() []tableRow {
	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	known := newKinds(p.repo)

	var out []tableRow
	for _, key := range p.fieldOrder() {
		if key == schema.TitleKey {
			continue
		}
		if isRelation(known.of(typeKey, key)) {
			value, _ := decodeValue(p.snapshot.Fields[key])
			ids := linkIds(value)
			for at, id := range ids {
				out = append(out, tableRow{key: key, label: linkLabel(p.repo, id), first: at == 0, link: id})
			}
			if len(ids) > 0 {
				continue
			}
		}
		out = append(out, tableRow{key: key, label: plain(p.snapshot.Fields[key]), first: true})
	}
	return out
}

// positions is every step of Tab, top to bottom.
func (p *showPage) positions() []position {
	out := []position{{stop: stopTitle, button: -1}, {stop: stopBox, button: -1}}
	for at := range p.buttons {
		out = append(out, position{stop: stopBox, button: at})
	}
	if len(p.rows) > 0 {
		out = append(out, position{stop: stopFields, button: -1})
	}
	return append(out, position{stop: stopTabs, button: -1})
}

func (p *showPage) current() position {
	all := p.positions()
	p.focus = min(max(p.focus, 0), len(all)-1)
	return all[p.focus]
}

// inText says the cursor is in the comment box's text, where keys are typing.
func (p *showPage) inText() bool {
	here := p.current()
	return here.stop == stopBox && here.button < 0
}

// currentRow is the table row under the cursor, when the cursor is in the
// table.
func (p *showPage) currentRow() *tableRow {
	if p.current().stop != stopFields || len(p.rows) == 0 {
		return nil
	}
	p.row = min(max(p.row, 0), len(p.rows)-1)
	return &p.rows[p.row]
}

// field is the field under the cursor, or "" where the cursor is on no field.
func (p *showPage) field() string {
	if p.current().stop == stopTitle {
		return schema.TitleKey
	}
	if row := p.currentRow(); row != nil {
		return row.key
	}
	return ""
}

// moveFocus steps through positions, and gives the box the keyboard only
// while the cursor is in its text.
func (p *showPage) moveFocus(by int) {
	all := p.positions()
	p.focusOn((p.focus + by + len(all)) % len(all))
}

func (p *showPage) focusOn(at int) {
	here := p.positions()[at]
	p.focus = at
	p.box.focus(here.stop == stopBox && here.button < 0)
	p.warned = false
	if here.stop == stopTabs {
		// the strip comes to the top, so the tab under it has the room
		p.offset = p.tabBarLine()
	}
}

// focusStop moves to a stop; in the box, to its text or one of its buttons.
func (p *showPage) focusStop(stop stopKind, button int) {
	for at, pos := range p.positions() {
		if pos.stop == stop && (stop != stopBox || pos.button == button) {
			p.focusOn(at)
			return
		}
	}
}

// switchTab shows the next or the previous tab, where the cursor is.
func (p *showPage) switchTab(by int) {
	p.tab = tab((int(p.tab) + by + int(tabCount)) % int(tabCount))
	p.offset = min(p.offset, p.tabBarLine())
}

func (p *showPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.box.resize(p.width)
		return p, nil

	case tea.BackgroundColorMsg:
		p.box.restyle()
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case refreshMsg:
		if err := p.load(); err != nil {
			p.status = err.Error()
		}
		return p, nil

	case tea.KeyPressMsg:
		return p.key(msg)

	case tea.PasteMsg:
		if p.editor == nil && !p.inText() {
			return p, p.paste(msg.Content)
		}
	case tea.ClipboardMsg:
		switch {
		case p.editor != nil:
			p.editor.paste(msg.Content)
			return p, nil
		case p.inText():
			p.box.area.InsertString(msg.Content)
			return p, nil
		default:
			return p, p.paste(msg.Content)
		}
	}

	// a widget that asked for a command gets the answer to it
	if p.editor != nil {
		return p.updateEditor(msg)
	}
	if p.inText() {
		return p, p.box.Update(msg)
	}
	return p, nil
}

func (p *showPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.help != nil:
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
	case p.editor != nil:
		return p.updateEditor(press)
	case p.inText():
		return p.textKey(press)
	}

	// vim's gt and gT: g has already done what g does, scroll to the top,
	// which a new tab does anyway
	if p.afterG {
		p.afterG = false
		switch press.String() {
		case "t":
			p.switchTab(1)
			return p, nil
		case "T":
			p.switchTab(-1)
			return p, nil
		}
	}

	here := p.current()
	switch {
	case keys.back.matches(press):
		return p.leave()

	case keys.next.matches(press):
		p.moveFocus(1)
	case keys.previous.matches(press):
		p.moveFocus(-1)
	case keys.nextTab.matches(press):
		p.switchTab(1)
	case keys.previousTab.matches(press):
		p.switchTab(-1)

	case keys.down.matches(press):
		p.down(here)
	case keys.up.matches(press):
		p.up(here)
	case keys.left.matches(press):
		p.sideways(here, -1)
	case keys.right.matches(press):
		p.sideways(here, 1)

	case keys.pageUp.matches(press):
		p.offset = max(0, p.offset-max(p.height-4, 1))
	case keys.pageDn.matches(press):
		p.offset += max(p.height-4, 1)
	case keys.top.matches(press):
		p.offset = 0
		p.afterG = press.String() == "g"
	case keys.bottom.matches(press):
		p.offset = 1 << 30 // the view clamps it to the last screenful

	case keys.edit.matches(press):
		return p, p.editHere(here)
	case keys.open.matches(press):
		return p, p.openHere(here)

	case keys.copyId.matches(press):
		p.status = "copied " + p.id
		return p, tea.SetClipboard(p.id)
	case keys.copy.matches(press):
		return p, p.copyHere(here)
	case keys.paste.matches(press):
		p.status = "reading clipboard…"
		return p, tea.ReadClipboard

	case keys.help.matches(press):
		p.help = &help{}
	}

	return p, nil
}

// down is within a stop first, and to the next stop at its edge, so that the
// directions alone walk the whole page.
func (p *showPage) down(here position) {
	switch here.stop {
	case stopTitle:
		p.focusStop(stopBox, -1)
	case stopBox:
		// from a button, past the others, to what is under the box
		p.moveFocus(len(p.buttons) - here.button)
	case stopFields:
		if p.row < len(p.rows)-1 {
			p.row++
			return
		}
		p.focusStop(stopTabs, -1)
	case stopTabs:
		p.offset++
	}
}

func (p *showPage) up(here position) {
	switch here.stop {
	case stopBox:
		p.focusStop(stopBox, -1)
	case stopFields:
		if p.row > 0 {
			p.row--
			return
		}
		p.focusStop(stopBox, 0)
	case stopTabs:
		if p.offset > p.tabBarLine() {
			p.offset--
			return
		}
		if len(p.rows) == 0 {
			p.focusStop(stopBox, 0)
			return
		}
		p.row = len(p.rows) - 1
		p.focusStop(stopFields, -1)
	}
}

// sideways moves between the buttons when there are several to move
// between, and switches the tab everywhere else: left and right are the tab
// keys of this page.
func (p *showPage) sideways(here position, by int) {
	if here.stop == stopBox && len(p.buttons) > 1 {
		if at := here.button + by; at >= 0 && at < len(p.buttons) {
			p.moveFocus(by)
		}
		return
	}
	p.switchTab(by)
}

// textKey is a key typed with the cursor in the comment box, where every
// letter is text: only the keys no one types prose with do anything else.
func (p *showPage) textKey(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.submit.matches(press):
		return p, p.submitComment()
	case keys.next.matches(press):
		p.moveFocus(1)
		return p, nil
	case keys.previous.matches(press):
		p.moveFocus(-1)
		return p, nil
	case keys.nextTab.matches(press):
		p.switchTab(1)
		return p, nil
	case keys.previousTab.matches(press):
		p.switchTab(-1)
		return p, nil
	case keys.cancel.matches(press):
		if p.box.draft() == "" {
			return p.leave()
		}
		// out of the box, onto the button that sends it, draft kept
		p.moveFocus(1)
		p.status = "draft kept"
		return p, nil
	}
	return p, p.box.Update(press)
}

// leave goes back to the view that opened the issue, once a draft has been
// warned about: it is the one thing on the page the store does not have.
func (p *showPage) leave() (page, tea.Cmd) {
	if p.box.draft() != "" && !p.warned {
		p.warned = true
		p.status = "esc again drops draft"
		return p, nil
	}
	// the list is underneath, where it was; with nothing underneath, the
	// stack asks for a second back before it quits
	return p, func() tea.Msg { return popMsg{} }
}

func (p *showPage) submitComment() tea.Cmd {
	body := p.box.draft()
	if body == "" {
		p.status = "empty comment"
		return bell()
	}

	if _, err := host.IssueCommentNew(p.repo, p.id, body); err != nil {
		p.status = err.Error()
		return nil
	}
	p.box.area.Reset()
	p.warned = false
	p.tab = tabComments
	p.status = "commented"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	return nil
}

// openHere presses a button, or follows the link under the cursor.
func (p *showPage) openHere(here position) tea.Cmd {
	if here.stop == stopBox {
		return p.buttons[here.button].press(p)
	}
	row := p.currentRow()
	if row == nil || row.link == "" {
		return nil
	}
	shown, err := newShowPage(p.repo, row.link, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

func (p *showPage) editHere(here position) tea.Cmd {
	switch here.stop {
	case stopBox:
		return p.buttons[here.button].press(p)
	case stopTabs:
		p.status = "not editable"
		return bell()
	}
	return p.startEdit(p.field(), nil)
}

func (p *showPage) copyHere(here position) tea.Cmd {
	var what, value string
	switch here.stop {
	case stopTitle:
		what, value = "title", p.snapshot.Title()
	case stopFields:
		row := p.currentRow()
		what, value = row.key, plain(p.snapshot.Fields[row.key])
		if row.link != "" {
			// a link copies the id it names, which is what a command takes
			value = row.link
		}
	case stopTabs:
		if p.tab != tabDescription {
			return bell()
		}
		what = "description"
		if len(p.snapshot.Comments) > 0 {
			value = p.snapshot.Comments[0].Message
		}
	default:
		return bell()
	}
	if strings.TrimSpace(value) == "" {
		p.status = what + " empty"
		return bell()
	}
	p.status = "copied " + what
	return tea.SetClipboard(value)
}

func (p *showPage) paste(text string) tea.Cmd {
	fieldKey := p.field()
	if fieldKey == "" {
		return bell()
	}
	if strings.TrimSpace(text) == "" {
		p.status = "clipboard empty"
		return bell()
	}
	return p.startEdit(fieldKey, &text)
}

// startEdit opens the widget the schema says this field takes, with pasted
// text in it when there is some; a stop that is not a field rings the bell.
func (p *showPage) startEdit(fieldKey string, pasted *string) tea.Cmd {
	if fieldKey == "" {
		return bell()
	}
	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	current, _ := decodeValue(p.snapshot.Fields[fieldKey])

	kind, known := fieldKind(p.repo, typeKey, fieldKey)
	if known && kind == schema.KindBool && pasted == nil {
		was, _ := current.(bool)
		p.write(fieldKey, issue.MustValue(!was))
		return nil
	}

	ed, refusal, err := editable(p.repo, typeKey, fieldKey, current)
	switch {
	case err != nil:
		p.status = err.Error()
		return bell()
	case refusal != "":
		p.status = refusal
		return bell()
	}
	if pasted != nil {
		if refusal := ed.paste(*pasted); refusal != "" {
			p.status = refusal
			return bell()
		}
	}
	ed.issueId = p.id
	p.editor = ed
	p.status = ""
	return nil
}

func (p *showPage) updateEditor(msg tea.Msg) (page, tea.Cmd) {
	done, cancelled, cmd := p.editor.Update(msg)
	if !done {
		return p, cmd
	}

	ed := p.editor
	p.editor = nil
	if cancelled {
		return p, nil
	}

	value, err := ed.Value()
	if err != nil {
		p.status = err.Error()
		return p, nil
	}
	p.write(ed.key, value)
	return p, nil
}

func (p *showPage) write(fieldKey string, value issue.Value) {
	if _, err := host.IssueSet(p.repo, p.id, map[string]issue.Value{fieldKey: value}, false); err != nil {
		p.status = err.Error()
		return
	}
	p.status = fieldKey + " set"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
}

func (p *showPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	here := p.current()
	top := p.topLines(here)
	body, cursorLine := p.body(here)

	var bottom []string
	if p.editor != nil {
		bottom = p.editor.View(p.width)
	}
	bottom = append(bottom, p.statusLine())

	room := max(p.height-len(top)-len(bottom), 1)
	if cursorLine >= 0 {
		// the row under the cursor is on the screen, however far down
		if cursorLine < p.offset {
			p.offset = cursorLine
		}
		if cursorLine >= p.offset+room {
			p.offset = cursorLine - room + 1
		}
	}
	if p.offset > len(body)-room {
		p.offset = max(0, len(body)-room)
	}

	lines := make([]string, 0, p.height)
	lines = append(lines, top...)
	for at := p.offset; at < min(p.offset+room, len(body)); at++ {
		lines = append(lines, body[at])
	}
	for len(lines) < p.height-len(bottom) {
		lines = append(lines, "")
	}

	return strings.Join(append(lines, bottom...), "\n")
}

// topLines is what never scrolls: the call, the title, and the box under it
// with its buttons, because the box is where the page opens and it must not
// move away.
//
// A terminal has one size of text, so the title is made to read as a
// heading the other ways: bold, in the accent colour, over a rule as long as
// it is, with a blank line on either side.
func (p *showPage) topLines(here position) []string {
	snap := p.snapshot
	typeKey, _ := issue.String(snap.Fields[schema.TypeKey])

	title := truncate(snap.Title(), max(p.width-4, 1))
	marker, drawn := " ", styleTitle.Render(title)
	if here.stop == stopTitle {
		marker, drawn = "›", styleCell.Bold(true).Render(title)
	}
	head := marker + " " + drawn
	if typeKey != "" {
		head += "  " + styleDim.Render(typeKey)
	}
	rule := "  " + styleTitle.Render(strings.Repeat("━", max(ansi.StringWidth(title), 1)))

	lines := []string{
		callLine(p.call, snap.Id().Human(), "id", p.width),
		"",
		fit(head, p.width),
		fit(rule, p.width),
		"",
	}
	lines = append(lines, p.box.View(p.width)...)

	cells := make([]string, 0, len(p.buttons))
	for at, b := range p.buttons {
		label := "[ " + b.label + " ]"
		if here.stop == stopBox && here.button == at {
			label = styleCell.Render(label)
		} else {
			label = styleDim.Render(label)
		}
		cells = append(cells, label)
	}
	hint := styleDim.Render("ctrl+enter sends")
	return append(lines, fit(" "+strings.Join(cells, " ")+"  "+hint, p.width), "")
}

// body is the part that scrolls, unwindowed: the fields table, the tab
// strip, and the tab drawn. It says which line the row under the cursor is
// on, or -1 when the cursor is not in the table.
func (p *showPage) body(here position) ([]string, int) {
	lines, cursorLine := p.fieldLines(here)
	lines = append(lines, tabStrip(p.tab, here.stop == stopTabs, p.width)...)
	switch p.tab {
	case tabComments:
		lines = append(lines, p.commentLines()...)
	case tabDescription:
		lines = append(lines, p.descriptionLines()...)
	case tabLog:
		lines = append(lines, p.logLines()...)
	}
	return lines, cursorLine
}

// tabBarLine is where the tab strip starts in body: what the page scrolls to
// when the cursor moves onto the tabs.
func (p *showPage) tabBarLine() int {
	lines, _ := p.fieldLines(position{stop: stopTitle})
	return len(lines)
}

// fieldLines is the fields table: the key, then the value, a link
// underlined; the row under the cursor marked while the cursor is in it.
func (p *showPage) fieldLines(here position) ([]string, int) {
	if len(p.rows) == 0 {
		return nil, -1
	}

	cursorLine := -1
	lines := make([]string, 0, len(p.rows)+1)
	for at, row := range p.rows {
		key := ""
		if row.first {
			key = row.key
		}
		value := row.label
		style := styleDim.Faint(false)
		if row.link != "" {
			style = styleLink
		}
		marker := " "
		if here.stop == stopFields && at == p.row {
			marker = "›"
			cursorLine = len(lines)
			style = styleCell.Underline(row.link != "")
			value = pad(value, max(ansi.StringWidth(value), 1))
		}
		lines = append(lines, fit(marker+styleDim.Render(pad(key, 16))+style.Render(value), p.width))
	}
	return append(lines, ""), cursorLine
}

// tabStrip draws the tabs as tabs: boxes on a rule, the drawn one open into
// what is under it, its name reversed while the cursor is on the strip.
func tabStrip(active tab, focused bool, width int) []string {
	var top, middle, bottom strings.Builder
	top.WriteString(" ")
	middle.WriteString(" ")
	bottom.WriteString("─")
	used := 1
	for t := tab(0); t < tabCount; t++ {
		name := t.String()
		span := len(name) + 2
		label := styleDim.Render(name)
		if t == active {
			label = styleHeader.Render(name)
			if focused {
				label = styleCell.Render(name)
			}
		}
		top.WriteString("╭" + strings.Repeat("─", span) + "╮")
		middle.WriteString("│ " + label + " │")
		if t == active {
			bottom.WriteString("┘" + strings.Repeat(" ", span) + "└")
		} else {
			bottom.WriteString("┴" + strings.Repeat("─", span) + "┴")
		}
		used += span + 2
	}
	if width > used {
		bottom.WriteString(strings.Repeat("─", width-used))
	}
	hint := ""
	if focused {
		hint = "  " + styleDim.Render("←→")
	}
	return []string{fit(top.String(), width), fit(middle.String()+hint, width), fit(bottom.String(), width)}
}

// commentLines is what was said about the issue, after its body: each
// comment in full, as it reads now, newest first.
//
// Newest is last in the timeline, which is the operations' causal order, not
// the comments' wall-clock times: those are for display only, never for
// ordering (entities/issue).
func (p *showPage) commentLines() []string {
	if len(p.snapshot.Comments) <= 1 {
		return []string{styleDim.Render(" (no comments)")}
	}
	var lines []string
	said := p.snapshot.Comments[1:]
	for at := len(said) - 1; at >= 0; at-- {
		comment := said[at]
		if at < len(said)-1 {
			lines = append(lines, "")
		}
		lines = append(lines, styleHeader.Render(fit(fmt.Sprintf(" %s  %s", comment.Author.DisplayName(), comment.FormatTimeRel()), p.width)))
		for _, line := range strings.Split(comment.Message, "\n") {
			lines = append(lines, fit(" "+line, p.width))
		}
	}
	return lines
}

// descriptionLines is the issue's body, the first comment, which it always
// has.
func (p *showPage) descriptionLines() []string {
	if len(p.snapshot.Comments) == 0 {
		return []string{styleDim.Render(" (no description)")}
	}
	body := p.snapshot.Comments[0]
	lines := []string{styleDim.Render(fit(fmt.Sprintf(" %s  %s", body.Author.DisplayName(), body.FormatTimeRel()), p.width))}
	if strings.TrimSpace(body.Message) == "" {
		return append(lines, styleDim.Render(" (no description)"))
	}
	for _, line := range strings.Split(body.Message, "\n") {
		lines = append(lines, fit(" "+line, p.width))
	}
	return lines
}

// logLines is what was done to the issue, one operation to a line, the
// latest first; a comment is its first line, the comments tab being where it
// is read.
func (p *showPage) logLines() []string {
	var lines []string
	for at := len(p.log) - 1; at >= 0; at-- {
		entry := p.log[at]
		if entry.Type == "noop" {
			continue
		}
		when := time.Unix(entry.UnixTime, 0).Format("2006-01-02 15:04")
		lines = append(lines, fit(fmt.Sprintf(" %s  %s  %s",
			styleDim.Render(when), entry.Author.Name, opSummary(entry)), p.width))
	}
	if len(lines) == 0 {
		return []string{styleDim.Render(" (empty)")}
	}
	return lines
}

func (p *showPage) statusLine() string {
	left := p.status
	if left == "" {
		left = "? keys"
		if p.inText() {
			left = "tab: leave box"
		}
	}
	return styleStatus.Render(fit(left, p.width))
}

// opSummary is one operation in one line, the way a person would say it.
func opSummary(entry cmdjson.IssueOperation) string {
	var op struct {
		Key     string          `json:"key"`
		Value   json.RawMessage `json:"value"`
		Item    json.RawMessage `json:"item"`
		Message string          `json:"message"`
	}
	if err := decodeInto(entry.Op, &op); err != nil {
		return entry.Type
	}

	switch entry.Type {
	case "create":
		return "created"
	case "add-comment":
		return "commented: " + truncate(strings.SplitN(op.Message, "\n", 2)[0], 60)
	case "edit-comment":
		return "edited comment"
	case "set-field":
		if value := plain(op.Value); value != "" {
			return fmt.Sprintf("set %s to %s", op.Key, value)
		}
		return "cleared " + op.Key
	case "add-value":
		return fmt.Sprintf("added %s to %s", plain(op.Item), op.Key)
	case "remove-value":
		return fmt.Sprintf("removed %s from %s", plain(op.Item), op.Key)
	}
	if op.Key != "" {
		return entry.Type + " " + op.Key
	}
	return entry.Type
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
