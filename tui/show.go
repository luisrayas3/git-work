package tui

import (
	"encoding/json"
	"fmt"
	"slices"
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
// The page is four stops, top to bottom: the header (type, title, archived),
// the fields table, the comment box with its buttons inside it, and the tabs
// — description, comments, log. The cursor opens in the box, because opening
// an issue to say something about it is the common case
// (doc/design/terminal-renderer.md, revised 2026-09-28).
type showPage struct {
	repo *cache.RepoCache

	id    string
	order []string
	call  *view.Call

	snapshot *issue.Snapshot
	log      []cmdjson.IssueOperation
	// rows is the fields table as drawn, rebuilt on every load
	rows []tableRow
	// children are the call's sections of issues pointing at this one,
	// drawn as rows after the fields (show_children.go)
	children []view.Children

	box     *commentBox
	buttons []button

	// focus is the stop the cursor is in; it opens on the box.
	focus stopKind
	// button is where the cursor is inside the box: -1 for the text, else
	// the button it is on in the footer.
	button int
	// cell is the header cell under the cursor, kept while the cursor is
	// elsewhere, like row; it starts on the title.
	cell int
	// row is the table row under the cursor, kept while the cursor is
	// elsewhere so that coming back to the table comes back to the same row.
	row int
	// tab is the tab drawn under the box.
	tab tab
	// afterG says the last key was vim's g, so t and T are gt and gT.
	afterG bool
	// warned says leaving has already said a draft would be lost; a second
	// try leaves anyway.
	warned bool

	editor *editor
	help   *help

	// offset is the top of the part of the page that scrolls, which is
	// everything under the header.
	offset        int
	width, height int
	status        string
}

// button is one action in the comment box's footer.
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
	stopHeader stopKind = iota
	stopFields
	stopBox
	stopTabs
)

// The header's cells: the three built-in fields, which every type has and
// which are not rows of the table.
const (
	cellType = iota
	cellTitle
	cellArchived
	headerCells
)

// tab is one of the three tabs under the box.
type tab int

const (
	tabDescription tab = iota
	tabComments
	tabLog
	tabCount
)

func (t tab) String() string {
	return [...]string{"description", "comments", "log"}[t]
}

// position is where the cursor is: a stop, and inside the box, which part of
// it — the text, or one of its buttons. The buttons are the box's own and not
// stops: Tab skips the block whole, and down from the text's last line is
// how the footer is reached, so that sending a comment is down, enter on
// every terminal there is.
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
	// derived marks a row of a children section: the other side of a
	// relation, which is not a field and so is never edited
	derived bool
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
	p.focus, p.button, p.cell = stopBox, -1, cellTitle
	return p, nil
}

func (p *showPage) Call() (*view.Call, string, string) {
	return p.call, p.snapshot.Id().Human(), "id"
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

	p.rows = append(p.tableRows(), p.childRows()...)
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

// tableRows is every field but the three built-ins, which are the header; a
// relation is drawn as the issues it names.
func (p *showPage) tableRows() []tableRow {
	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	known := newKinds(p.repo)

	var out []tableRow
	for _, key := range p.fieldOrder() {
		if key == schema.TitleKey || key == schema.TypeKey || key == issue.ArchivedKey {
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
		label := plain(p.snapshot.Fields[key])
		if isPerson(known.of(typeKey, key)) {
			value, _ := decodeValue(p.snapshot.Fields[key])
			label = personText(p.repo, value)
		}
		out = append(out, tableRow{key: key, label: label, first: true})
	}
	return out
}

// archived is whether the issue is, as its field says.
func (p *showPage) archived() bool {
	value, _ := decodeValue(p.snapshot.Fields[issue.ArchivedKey])
	archived, _ := value.(bool)
	return archived
}

// stops is every step of Tab, top to bottom: the table is one only while it
// has rows.
func (p *showPage) stops() []stopKind {
	out := []stopKind{stopHeader}
	if len(p.rows) > 0 {
		out = append(out, stopFields)
	}
	return append(out, stopBox, stopTabs)
}

func (p *showPage) current() position {
	if p.focus == stopFields && len(p.rows) == 0 {
		p.focus, p.button = stopBox, -1
	}
	button := -1
	if p.focus == stopBox {
		p.button = min(max(p.button, -1), len(p.buttons)-1)
		button = p.button
	}
	return position{stop: p.focus, button: button}
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
	if p.current().stop == stopHeader {
		return [...]string{schema.TypeKey, schema.TitleKey, issue.ArchivedKey}[p.cell]
	}
	if row := p.currentRow(); row != nil && !row.derived {
		return row.key
	}
	return ""
}

// moveFocus steps through the stops, into the box's text when it reaches the
// box.
func (p *showPage) moveFocus(by int) {
	all := p.stops()
	at := slices.Index(all, p.current().stop)
	p.focusStop(all[(at+by+len(all))%len(all)], -1)
}

// focusStop moves to a stop; in the box, to its text or one of its buttons.
// The box gets the keyboard only while the cursor is in its text.
func (p *showPage) focusStop(stop stopKind, button int) {
	if stop == stopFields && len(p.rows) == 0 {
		stop, button = stopBox, -1
	}
	p.focus, p.button = stop, button
	here := p.current()
	p.box.focus(here.stop == stopBox && here.button < 0)
	p.warned = false
	if stop == stopTabs {
		// the strip comes to the top, so the tab under it has the room
		p.offset = p.tabBarLine()
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

	case keys.act.matches(press):
		return p, p.act(here)

	case keys.copyId.matches(press):
		p.status = "copied " + p.id
		return p, setClipboard(p.id)
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
// directions alone walk the whole page. In the box's text it is the text's
// own key, until its last line (textKey).
func (p *showPage) down(here position) {
	switch here.stop {
	case stopHeader:
		p.focusStop(stopFields, -1)
	case stopFields:
		if p.row < len(p.rows)-1 {
			p.row++
			return
		}
		p.focusStop(stopBox, -1)
	case stopBox:
		// from the footer, past the box, to the tabs
		p.focusStop(stopTabs, -1)
	case stopTabs:
		p.offset++
	}
}

func (p *showPage) up(here position) {
	switch here.stop {
	case stopFields:
		if p.row > 0 {
			p.row--
			return
		}
		p.focusStop(stopHeader, -1)
	case stopBox:
		// from the footer, back into the text
		p.focusStop(stopBox, -1)
	case stopTabs:
		if p.offset > p.tabBarLine() {
			p.offset--
			return
		}
		p.focusStop(stopBox, 0)
	}
}

// upFromText leaves the box upward: to the last row of the table, or to the
// header when there is no table.
func (p *showPage) upFromText() {
	if len(p.rows) == 0 {
		p.focusStop(stopHeader, -1)
		return
	}
	p.row = len(p.rows) - 1
	p.focusStop(stopFields, -1)
}

// sideways walks the header's cells, moves between the footer's buttons when
// there are several, and switches the tab everywhere else: left and right
// are the tab keys of this page.
func (p *showPage) sideways(here position, by int) {
	switch {
	case here.stop == stopHeader:
		p.cell = min(max(p.cell+by, 0), headerCells-1)
	case here.stop == stopBox && here.button >= 0 && len(p.buttons) > 1:
		p.button = min(max(here.button+by, 0), len(p.buttons)-1)
	default:
		p.switchTab(by)
	}
}

// textKey is a key typed with the cursor in the comment box, where every
// letter is text: only the keys no one types prose with do anything else,
// and up and down leave the text at its edges.
func (p *showPage) textKey(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
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
		// out of the text, onto the button that sends it, draft kept
		p.focusStop(stopBox, 0)
		p.status = "draft kept"
		return p, nil
	case press.String() == "down" || press.String() == "ctrl+n":
		if p.box.onLastLine() {
			p.focusStop(stopBox, 0)
			return p, nil
		}
	case press.String() == "up" || press.String() == "ctrl+p":
		if p.box.onFirstLine() {
			p.upFromText()
			return p, nil
		}
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

// act is enter, the one action key: it presses the button under the cursor,
// or edits the field under it — a bool, archived included, flips at once,
// and a relation's picker opens on "go to" the issue the line names, so
// enter, enter follows the link (doc/design/terminal-renderer.md, 2026-09-29).
func (p *showPage) act(here position) tea.Cmd {
	switch here.stop {
	case stopBox:
		return p.buttons[here.button].press(p)
	case stopTabs:
		return nil
	}
	cmd := p.startEdit(p.field(), nil)
	if row := p.currentRow(); row != nil && row.link != "" && p.editor != nil {
		p.editor.goToFirst(row.link)
	}
	return cmd
}

// follow opens the issue a link names, over this one.
func (p *showPage) follow(id string) tea.Cmd {
	shown, err := newShowPage(p.repo, id, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

func (p *showPage) copyHere(here position) tea.Cmd {
	var what, value string
	switch here.stop {
	case stopHeader:
		what = p.field()
		value = plain(p.snapshot.Fields[what])
		if what == issue.ArchivedKey && !p.archived() {
			value = ""
		}
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
	return setClipboard(value)
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
		said := fieldKey + " set"
		if fieldKey == issue.ArchivedKey {
			said = "archived"
			if was {
				said = "unarchived"
			}
		}
		p.write(fieldKey, issue.MustValue(!was), said)
		return nil
	}

	ed, refusal, err := editable(p.repo, p.id, typeKey, fieldKey, current)
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

	if id := ed.goTo(); id != "" {
		return p, p.follow(id)
	}
	if refusal := ed.refusal(); refusal != "" {
		p.status = refusal
		return p, bell()
	}
	value, err := ed.Value()
	if err != nil {
		p.status = err.Error()
		return p, nil
	}
	p.write(ed.key, value, ed.key+" set")
	return p, nil
}

// write sets one field, and says what it did in the status line.
func (p *showPage) write(fieldKey string, value issue.Value, said string) {
	if _, err := host.IssueSet(p.repo, p.id, map[string]issue.Value{fieldKey: value}, false); err != nil {
		p.status = err.Error()
		return
	}
	p.status = said
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
	body, from, to := p.body(here)

	var bottom []string
	if p.editor != nil {
		bottom = p.editor.View(p.width)
	}
	bottom = append(bottom, p.statusLine())

	room := max(p.height-len(top)-len(bottom), 1)
	if from >= 0 {
		// what the cursor is on is on the screen, however far down
		if to >= p.offset+room {
			p.offset = to - room + 1
		}
		if from < p.offset {
			p.offset = from
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

// topLines is what never scrolls: the call, and the header under it.
func (p *showPage) topLines(here position) []string {
	lines := []string{callLine(p.call, p.snapshot.Id().Human(), "id", p.width), ""}
	lines = append(lines, p.headerLines(here)...)
	return append(lines, "")
}

// headerLines is the three built-in fields as three cells: the type, dim,
// left of the title as the list has it; the title bold over a rule as long
// as the two of them, which is the heading a terminal's one size of text
// allows; and archived as a checkbox, ticked in the warning colour when the
// issue is and dim when it is not, so that the toggle enter flips is always
// in sight (Luis, 2026-09-28).
func (p *showPage) headerLines(here position) []string {
	snap := p.snapshot
	on := here.stop == stopHeader
	focused := func(cell int) bool { return on && p.cell == cell }

	typeKey, _ := issue.String(snap.Fields[schema.TypeKey])
	if typeKey == "" {
		typeKey = "(no type)"
	}
	title := truncate(snap.Title(), max(p.width-ansi.StringWidth(typeKey)-16, 1))

	cells := []string{styleDim.Render(typeKey), styleTitle.Render(title)}
	if focused(cellType) {
		cells[0] = styleCell.Render(typeKey)
	}
	if focused(cellTitle) {
		cells[1] = styleCell.Bold(true).Render(title)
	}
	box := "[ ] archived"
	style := styleDim
	if p.archived() {
		box, style = "[x] archived", styleArchived
	}
	if focused(cellArchived) {
		style = styleCell
	}
	cells = append(cells, style.Render(box))

	marker := " "
	if on {
		marker = "›"
	}
	head := marker + " " + strings.Join(cells, "  ")
	rule := "  " + styleDim.Render(strings.Repeat("━", ansi.StringWidth(typeKey)+2+max(ansi.StringWidth(title), 1)))
	return []string{fit(head, p.width), fit(rule, p.width)}
}

// body is the part that scrolls, unwindowed: the fields table, the comment
// box with its footer, the tab strip, and the tab drawn. It says which lines
// the cursor is on, first to last, or -1 when it is on none of them: a table
// row is one line, the box is all of its own.
func (p *showPage) body(here position) (lines []string, from, to int) {
	from, to = -1, -1

	lines, rowLine := p.fieldLines(here)
	if here.stop == stopFields {
		from, to = rowLine, rowLine
	}

	boxStart := len(lines)
	lines = append(lines, p.box.View(p.width)...)
	lines = append(lines, p.footerLine(here), "")
	if here.stop == stopBox {
		from, to = boxStart, len(lines)-2
	}

	lines = append(lines, tabStrip(p.tab, here.stop == stopTabs, p.width)...)
	switch p.tab {
	case tabComments:
		lines = append(lines, p.commentLines()...)
	case tabDescription:
		lines = append(lines, p.descriptionLines()...)
	case tabLog:
		lines = append(lines, p.logLines()...)
	}
	return lines, from, to
}

// tabBarLine is where the tab strip starts in body: what the page scrolls to
// when the cursor moves onto the tabs.
func (p *showPage) tabBarLine() int {
	lines, _ := p.fieldLines(position{stop: stopHeader})
	return len(lines) + len(p.box.View(p.width)) + 2
}

// footerLine is the box's buttons, under its text: the one under the cursor
// reversed, and, while the cursor is in the text, the two keys that reach
// them.
func (p *showPage) footerLine(here position) string {
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
	line := " " + strings.Join(cells, " ")
	if here.stop == stopBox && here.button < 0 {
		line += "  " + styleDim.Render("↓ enter sends")
	}
	return fit(line, p.width)
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
		lines = append(lines, prose(comment.Message, p.width)...)
	}
	return lines
}

// descriptionLines is the issue's body, the first comment, which it always
// has, wrapped: a description is read, not scanned.
func (p *showPage) descriptionLines() []string {
	if len(p.snapshot.Comments) == 0 {
		return []string{styleDim.Render(" (no description)")}
	}
	body := p.snapshot.Comments[0]
	lines := []string{styleDim.Render(fit(fmt.Sprintf(" %s  %s", body.Author.DisplayName(), body.FormatTimeRel()), p.width))}
	if strings.TrimSpace(body.Message) == "" {
		return append(lines, styleDim.Render(" (no description)"))
	}
	return append(lines, prose(body.Message, p.width)...)
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
			left = "↓ enter: send · tab: leave box"
		}
		if row := p.currentRow(); row != nil && p.editor == nil {
			typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
			if kind, _ := fieldKind(p.repo, typeKey, row.key); isRelation(kind) {
				left = relationHint(row.link != "")
			}
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
