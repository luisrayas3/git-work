package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
// The page is five stops, top to bottom: the title, the comment box right
// under it, the fields table, and the description and log tabs. The cursor
// opens in the box, because opening an issue to say something about it is
// the common case (doc/design/terminal-renderer.md, revised 2026-09-27).
type showPage struct {
	repo *cache.RepoCache

	id    string
	order []string
	call  *view.Call

	snapshot *issue.Snapshot
	log      []cmdjson.IssueOperation

	box     *commentBox
	buttons []button

	// focus indexes positions(); it opens on the box.
	focus int
	// row is the field under the cursor, kept while the cursor is elsewhere
	// so that coming back to the table comes back to the same row.
	row int
	// tab is the tab drawn under the fields: stopDescription or stopLog.
	tab stopKind
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

// stopKind is one of the five places the cursor can be.
type stopKind int

const (
	stopTitle stopKind = iota
	stopBox
	stopFields
	stopDescription
	stopLog
)

// position is one step of Tab: a stop, and inside the box, which part of it —
// the text, or one of its buttons. The buttons are the box's own, not stops
// of their own, and are in the Tab order so that every terminal can send a
// comment, including one that cannot send ctrl+enter.
type position struct {
	stop   stopKind
	button int // -1 for the text
}

func newShowPage(repo *cache.RepoCache, id string, fields []string) (*showPage, error) {
	p := &showPage{repo: repo, id: id, order: fields, width: 80, height: 24, tab: stopDescription}
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

// tableFields are the rows of the fields table: every field but the title,
// which has a stop of its own.
func (p *showPage) tableFields() []string {
	out := make([]string, 0, len(p.fieldOrder()))
	for _, key := range p.fieldOrder() {
		if key != schema.TitleKey {
			out = append(out, key)
		}
	}
	return out
}

// positions is every step of Tab, top to bottom.
func (p *showPage) positions() []position {
	out := []position{{stop: stopTitle, button: -1}, {stop: stopBox, button: -1}}
	for at := range p.buttons {
		out = append(out, position{stop: stopBox, button: at})
	}
	if len(p.tableFields()) > 0 {
		out = append(out, position{stop: stopFields, button: -1})
	}
	return append(out,
		position{stop: stopDescription, button: -1},
		position{stop: stopLog, button: -1})
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

// field is the field under the cursor, or "" where the cursor is on no field.
func (p *showPage) field() string {
	switch p.current().stop {
	case stopTitle:
		return schema.TitleKey
	case stopFields:
		fields := p.tableFields()
		p.row = min(max(p.row, 0), len(fields)-1)
		return fields[p.row]
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

	if here.stop == stopDescription || here.stop == stopLog {
		// focus follows selection, as on any tab strip: the tab the cursor
		// is on is the tab drawn, and it is scrolled to the top
		p.tab = here.stop
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

// switchTab shows the other tab, and takes the cursor with it when the
// cursor was on the tab strip.
func (p *showPage) switchTab() {
	next := stopLog
	if p.tab == stopLog {
		next = stopDescription
	}
	here := p.current().stop
	if here == stopDescription || here == stopLog {
		p.focusStop(next, -1)
		return
	}
	p.tab = next
	p.offset = min(p.offset, p.tabBarLine())
}

func (p *showPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.box.resize(p.width)
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
		if press.String() == "t" || press.String() == "T" {
			p.switchTab()
			return p, nil
		}
	}

	here := p.current()
	switch {
	case keys.back.matches(press), keys.quit.matches(press):
		return p.leave()

	case keys.next.matches(press):
		p.moveFocus(1)
	case keys.previous.matches(press):
		p.moveFocus(-1)
	case keys.nextTab.matches(press), keys.previousTab.matches(press):
		p.switchTab()

	case keys.down.matches(press):
		p.down(here)
	case keys.up.matches(press):
		p.up(here)
	case keys.left.matches(press), keys.right.matches(press):
		p.sideways(here, keys.right.matches(press))

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
		if here.stop == stopBox {
			return p, p.buttons[here.button].press(p)
		}

	case keys.copyId.matches(press):
		p.status = "copied " + p.id
		return p, tea.SetClipboard(p.id)
	case keys.copy.matches(press):
		return p, p.copyHere(here)
	case keys.paste.matches(press):
		p.status = "asking the terminal for its clipboard"
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
		if p.row < len(p.tableFields())-1 {
			p.row++
			return
		}
		p.focusStop(p.tab, -1)
	case stopDescription, stopLog:
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
	case stopDescription, stopLog:
		if p.offset > p.tabBarLine() {
			p.offset--
			return
		}
		if len(p.tableFields()) == 0 {
			p.focusStop(stopBox, 0)
			return
		}
		p.row = len(p.tableFields()) - 1
		p.focusStop(stopFields, -1)
	}
}

// sideways moves between the buttons, and between the tabs.
func (p *showPage) sideways(here position, right bool) {
	switch {
	case here.stop == stopBox && right && here.button < len(p.buttons)-1:
		p.moveFocus(1)
	case here.stop == stopBox && !right && here.button > 0:
		p.moveFocus(-1)
	case here.stop == stopDescription && right, here.stop == stopLog && !right:
		p.switchTab()
	}
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
	case keys.nextTab.matches(press), keys.previousTab.matches(press):
		p.switchTab()
		return p, nil
	case keys.cancel.matches(press):
		if p.box.draft() == "" {
			return p.leave()
		}
		// out of the box, onto the button that sends it, draft kept
		p.moveFocus(1)
		p.status = "draft kept: enter sends it, esc twice goes back without it"
		return p, nil
	}
	return p, p.box.Update(press)
}

// leave goes back to the view that opened the issue, once a draft has been
// warned about: it is the one thing on the page the store does not have.
func (p *showPage) leave() (page, tea.Cmd) {
	if p.box.draft() != "" && !p.warned {
		p.warned = true
		p.status = "a comment is being written: esc again drops it"
		return p, nil
	}
	// the list is underneath, where it was; with nothing underneath the
	// stack quits, which is what `git work view show` should do.
	return p, func() tea.Msg { return popMsg{} }
}

func (p *showPage) submitComment() tea.Cmd {
	body := p.box.draft()
	if body == "" {
		p.status = "nothing to send: the comment is empty"
		return bell()
	}

	if _, err := host.IssueCommentNew(p.repo, p.id, body); err != nil {
		p.status = err.Error()
		return nil
	}
	p.box.area.Reset()
	p.warned = false
	p.status = "commented: it is on the log tab"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	return nil
}

func (p *showPage) editHere(here position) tea.Cmd {
	switch here.stop {
	case stopBox:
		return p.buttons[here.button].press(p)
	case stopDescription:
		p.status = "the description is edited with git work issue comment edit, for now"
		return bell()
	case stopLog:
		p.status = "the log is what happened: it is not edited"
		return bell()
	}
	return p.startEdit(p.field(), nil)
}

func (p *showPage) copyHere(here position) tea.Cmd {
	var what, value string
	switch here.stop {
	case stopTitle, stopFields:
		what = p.field()
		value = plain(p.snapshot.Fields[what])
	case stopDescription:
		what = "the description"
		if len(p.snapshot.Comments) > 0 {
			value = p.snapshot.Comments[0].Message
		}
	default:
		return bell()
	}
	if strings.TrimSpace(value) == "" {
		p.status = what + " is empty: nothing copied"
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
		p.status = "the clipboard is empty"
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
func (p *showPage) topLines(here position) []string {
	snap := p.snapshot
	typeKey, _ := issue.String(snap.Fields[schema.TypeKey])

	title := styleHeader.Render(snap.Title())
	marker := " "
	if here.stop == stopTitle {
		title = styleCell.Render(snap.Title())
		marker = "›"
	}
	head := marker + title
	if typeKey != "" {
		head += "  " + styleDim.Render("("+typeKey+")")
	}

	lines := []string{callLine(p.call, snap.Id().Human(), "id", p.width), fit(head, p.width)}
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
	hint := styleDim.Render("ctrl+enter sends · tab moves on")
	return append(lines, fit(" "+strings.Join(cells, " ")+"  "+hint, p.width), "")
}

// body is the part that scrolls, unwindowed: the fields table, the tab
// strip, and the tab drawn. It says which line the field under the cursor is
// on, or -1 when the cursor is not in the table.
func (p *showPage) body(here position) ([]string, int) {
	lines, cursorLine := p.fieldLines(here)
	lines = append(lines, tabStrip(p.tab, here.stop == p.tab, p.width)...)
	if p.tab == stopLog {
		return append(lines, p.logLines()...), cursorLine
	}
	return append(lines, p.descriptionLines()...), cursorLine
}

// tabBarLine is where the tab strip starts in body: what a tab scrolls to
// when the cursor moves onto it.
func (p *showPage) tabBarLine() int {
	lines, _ := p.fieldLines(position{stop: stopTitle})
	return len(lines)
}

// fieldLines is the fields table: a key column and a value column, the row
// under the cursor marked while the cursor is in the table.
func (p *showPage) fieldLines(here position) ([]string, int) {
	fields := p.tableFields()
	if len(fields) == 0 {
		return nil, -1
	}

	cursorLine := -1
	lines := []string{styleDim.Render(fit(" "+pad("field", 16)+"value", p.width))}
	for at, key := range fields {
		marker := " "
		value := plain(p.snapshot.Fields[key])
		if here.stop == stopFields && at == p.row {
			marker = "›"
			cursorLine = len(lines)
			value = styleCell.Render(pad(value, max(len([]rune(value)), 1)))
		}
		lines = append(lines, fit(marker+pad(key, 16)+value, p.width))
	}
	return append(lines, ""), cursorLine
}

// tabStrip draws the two tabs as tabs: boxes on a rule, the drawn one open
// into what is under it, its name reversed while the cursor is on it.
func tabStrip(active stopKind, focused bool, width int) []string {
	tabs := []struct {
		stop  stopKind
		label string
	}{{stopDescription, "description"}, {stopLog, "log"}}

	var top, middle, bottom strings.Builder
	top.WriteString(" ")
	middle.WriteString(" ")
	bottom.WriteString("─")
	used := 1
	for _, tab := range tabs {
		span := len(tab.label) + 2
		label := styleDim.Render(tab.label)
		if tab.stop == active {
			label = styleHeader.Render(tab.label)
			if focused {
				label = styleCell.Render(tab.label)
			}
		}
		top.WriteString("╭" + strings.Repeat("─", span) + "╮")
		middle.WriteString("│ " + label + " │")
		if tab.stop == active {
			bottom.WriteString("┘" + strings.Repeat(" ", span) + "└")
		} else {
			bottom.WriteString("┴" + strings.Repeat("─", span) + "┴")
		}
		used += span + 2
	}
	if width > used {
		bottom.WriteString(strings.Repeat("─", width-used))
	}
	return []string{fit(top.String(), width), fit(middle.String(), width), fit(bottom.String(), width)}
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

// logLines is what happened to the issue, in the order it happened: the
// comments in full, as they read now, and every other operation on a line.
// One timeline rather than a comments pane and a history pane, because what
// was said and what was done answer the same question.
func (p *showPage) logLines() []string {
	comments := make(map[string]issue.Comment, len(p.snapshot.Comments))
	for _, comment := range p.snapshot.Comments {
		comments[comment.TargetId().String()] = comment
	}

	var lines []string
	for _, entry := range p.log {
		when := time.Unix(entry.UnixTime, 0).Format("2006-01-02 15:04")
		switch entry.Type {
		case "noop":
			continue
		case "add-comment":
			message := ""
			if comment, ok := comments[entry.Id]; ok {
				message = comment.Message
			}
			lines = append(lines, "", styleHeader.Render(fit(fmt.Sprintf(" %s  %s", entry.Author.Name, when), p.width)))
			for _, line := range strings.Split(message, "\n") {
				lines = append(lines, fit(" "+line, p.width))
			}
			lines = append(lines, "")
			continue
		}
		lines = append(lines, styleDim.Render(fit(fmt.Sprintf(" %s  %s  %s", when, entry.Author.Name, opSummary(entry)), p.width)))
	}
	if len(lines) == 0 {
		return []string{styleDim.Render(" (nothing yet)")}
	}
	return lines
}

func (p *showPage) statusLine() string {
	left := p.status
	if left == "" {
		left = "? for keys"
		if p.inText() {
			left = "tab leaves the box"
		}
	}
	return styleStatus.Render(fit(left, p.width))
}

// opSummary is one operation in one line, the way a person would say it.
func opSummary(entry cmdjson.IssueOperation) string {
	var op struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
		Item  json.RawMessage `json:"item"`
	}
	if err := decodeInto(entry.Op, &op); err != nil {
		return entry.Type
	}

	switch entry.Type {
	case "create":
		return "created it"
	case "edit-comment":
		return "edited a comment"
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
