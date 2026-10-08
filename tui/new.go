package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
	"github.com/git-bug/git-bug/view"
)

// newPage is the `new` view: show's page over an issue that does not exist
// yet (doc/design/create.md).
//
// It is the interactive `issue new`: the page holds a draft — the document
// `issue new` takes, opened on whatever the call's `doc` filled in — and
// nothing is written until Create, which commits the draft through
// host.IssueNew as the one operation the command commits. An abandoned draft
// costs nothing, and the Jira sync first sees the issue in its final form.
//
// The stops are show's, less the tabs and plus one: the header (type,
// title), the fields table, the description box, and Create, a button enter
// presses. The page opens on the title, typing, when the type is set, and on
// the type cell when it is not: a ghost's enter means "I want to add one",
// and the title is what is typed next.
type newPage struct {
	repo *cache.RepoCache

	call  *view.Call
	order []string

	// fields is the draft: the title and the type included, as `issue new`
	// takes them, a null never stored (C5: the form invents no default).
	fields  map[string]issue.Value
	aliases map[string]string
	// opened is the draft as the call gave it, so that leaving knows
	// whether anything was typed.
	opened map[string]issue.Value
	body   *commentBox

	// rows is the fields table as drawn, rebuilt on every change
	rows []tableRow

	focus  stopKind
	typing bool
	cell   int
	row    int
	warned bool

	editor *editor
	help   *help

	offset        int
	width, height int
	status        string
}

// createdMsg says a draft was committed: the page the draft was on hands it
// to the stack, which pops back to the view that opened it, or, with nothing
// underneath, puts show on the new issue in its place (C4).
type createdMsg struct {
	id    string
	shown page
}

// newNewPage is the page a `new` call describes.
func newNewPage(repo *cache.RepoCache, call *view.Call) (*newPage, error) {
	doc, err := host.IssueDraft(repo, call.Raw("doc"))
	if err != nil {
		return nil, err
	}

	p := &newPage{repo: repo, call: call, order: call.Strings("fields"), width: 80, height: 24}
	p.fields = map[string]issue.Value{}
	for key, value := range doc.Fields {
		if !issue.IsNull(value) {
			p.fields[key] = value
		}
	}
	p.aliases = doc.Aliases
	p.opened = copyFields(p.fields)

	p.body = newCommentBox(p.width)
	p.body.area.Placeholder = "the description"
	p.body.area.SetValue(doc.Body)

	p.rebuild()
	p.focus, p.cell = stopHeader, cellType
	if p.typeKey() != "" {
		p.cell = cellTitle
		if cmd := p.startEdit(schema.TitleKey, nil); cmd != nil {
			// the title's editor is a text box and never refuses
			p.editor = nil
		}
	}
	return p, nil
}

func copyFields(fields map[string]issue.Value) map[string]issue.Value {
	out := make(map[string]issue.Value, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func (p *newPage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func (p *newPage) typeKey() string {
	typeKey, _ := issue.String(p.fields[schema.TypeKey])
	return typeKey
}

func (p *newPage) title() string {
	title, _ := issue.String(p.fields[schema.TitleKey])
	return title
}

// fieldOrder is which fields are rows, and in which order: the call's, else
// the type's in schema order, as show has it; with no type there are none,
// because the fields are the type's (C3).
func (p *newPage) fieldOrder() []string {
	if len(p.order) > 0 {
		return p.order
	}
	typeKey := p.typeKey()
	if typeKey == "" {
		return nil
	}
	if s, err := p.repo.LoadSchema(); err == nil {
		if t, ok := s.Type(typeKey); ok {
			return t.FieldKeys()
		}
	}
	return sorted.Keys(p.fields)
}

// rebuild makes the table out of the draft: every field but the two the
// header has, archived, which a draft is not, and rank, which a draft has
// no place in (C6: a ghost's issue lands where the ghost stood with no
// rank written).
func (p *newPage) rebuild() {
	typeKey := p.typeKey()
	known := newKinds(p.repo)

	var rows []tableRow
	for _, key := range p.fieldOrder() {
		switch key {
		case schema.TitleKey, schema.TypeKey, issue.ArchivedKey, schema.RankKey:
			continue
		}
		value, _ := decodeValue(p.fields[key])
		if isRelation(known.of(typeKey, key)) {
			ids := linkIds(value)
			for at, id := range ids {
				rows = append(rows, tableRow{key: key, label: linkLabel(p.repo, id), first: at == 0, link: id})
			}
			if len(ids) > 0 {
				continue
			}
		}
		label := plain(p.fields[key])
		if isPerson(known.of(typeKey, key)) {
			label = personText(p.repo, value)
		}
		rows = append(rows, tableRow{key: key, label: label, first: true})
	}
	p.rows = rows
	p.row = min(max(p.row, 0), max(len(p.rows)-1, 0))
}

// dirty says the page holds something the call did not give it: a typed
// value, a body; leaving with one asks twice (C3).
func (p *newPage) dirty() bool {
	if len(p.fields) != len(p.opened) {
		return true
	}
	for key, value := range p.fields {
		if !bytes.Equal(value, p.opened[key]) {
			return true
		}
	}
	var body string
	if raw := p.call.Raw("doc"); raw != nil {
		var doc struct {
			Body string `json:"body"`
		}
		_ = json.Unmarshal(raw, &doc)
		body = doc.Body
	}
	return p.body.draft() != strings.TrimSpace(body)
}

// stops is every step of tab, top to bottom: the table only while it has
// rows, and Create last.
func (p *newPage) stops() []stopKind {
	out := []stopKind{stopHeader}
	if len(p.rows) > 0 {
		out = append(out, stopFields)
	}
	return append(out, stopBox, stopCreate)
}

func (p *newPage) current() position {
	if p.focus == stopFields && len(p.rows) == 0 {
		p.focus = stopBox
	}
	return position{stop: p.focus, typing: p.focus == stopBox && p.typing}
}

func (p *newPage) inText() bool {
	return p.current().typing
}

func (p *newPage) currentRow() *tableRow {
	if p.current().stop != stopFields || len(p.rows) == 0 {
		return nil
	}
	p.row = min(max(p.row, 0), len(p.rows)-1)
	return &p.rows[p.row]
}

// field is the field under the cursor, or "" where the cursor is on none.
func (p *newPage) field() string {
	if p.current().stop == stopHeader {
		return [...]string{schema.TypeKey, schema.TitleKey}[p.cell]
	}
	if row := p.currentRow(); row != nil {
		return row.key
	}
	return ""
}

func (p *newPage) moveFocus(by int) {
	all := p.stops()
	at := slices.Index(all, p.current().stop)
	p.focusStop(all[(at+by+len(all))%len(all)])
}

func (p *newPage) focusStop(stop stopKind) {
	if stop == stopFields && len(p.rows) == 0 {
		stop = stopBox
	}
	p.focus = stop
	p.setTyping(false)
	p.warned = false
}

func (p *newPage) setTyping(on bool) {
	p.typing = on && p.focus == stopBox
	p.body.focus(p.typing)
}

func (p *newPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.body.resize(p.width)
		return p, nil

	case tea.BackgroundColorMsg:
		p.body.restyle()
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case refreshMsg:
		// the draft is the page's own; what the store did is the pickers'
		// business, which read it when they open
		p.rebuild()
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
			p.body.area.InsertString(msg.Content)
			return p, nil
		default:
			return p, p.paste(msg.Content)
		}
	}

	if p.editor != nil {
		return p.updateEditor(msg)
	}
	if p.inText() {
		return p, p.body.Update(msg)
	}
	return p, nil
}

func (p *newPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
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

	here := p.current()
	switch {
	case keys.back.matches(press):
		return p.leave()

	case keys.next.matches(press):
		p.moveFocus(1)
	case keys.previous.matches(press):
		p.moveFocus(-1)

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
	case keys.bottom.matches(press):
		p.offset = 1 << 30

	case keys.act.matches(press):
		return p, p.act(here)
	case keys.edit.matches(press):
		return p, p.edit(here)

	case keys.copyId.matches(press):
		p.status = "no id until created"
		return p, bell()
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

// down is within a stop first, and to the next stop at its edge, so that
// the directions alone walk the whole page, Create included.
func (p *newPage) down(here position) {
	switch here.stop {
	case stopHeader:
		p.focusStop(stopFields)
	case stopFields:
		if p.row < len(p.rows)-1 {
			p.row++
			return
		}
		p.focusStop(stopBox)
	case stopBox:
		p.focusStop(stopCreate)
	}
}

func (p *newPage) up(here position) {
	switch here.stop {
	case stopFields:
		if p.row > 0 {
			p.row--
			return
		}
		p.focusStop(stopHeader)
	case stopBox:
		if len(p.rows) == 0 {
			p.focusStop(stopHeader)
			return
		}
		p.row = len(p.rows) - 1
		p.focusStop(stopFields)
	case stopCreate:
		p.focusStop(stopBox)
	}
}

// sideways walks the header's two cells; there are no tabs here.
func (p *newPage) sideways(here position, by int) {
	if here.stop == stopHeader {
		p.cell = min(max(p.cell+by, 0), cellTitle)
	}
}

// textKey is a key typed in the description's text: enter accepts the text
// into the draft and leaves it, esc leaves it too, keeping it, and the
// newline is alt+enter, which the box binds (C3).
func (p *newPage) textKey(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.act.matches(press), keys.cancel.matches(press):
		p.setTyping(false)
		return p, nil
	case keys.next.matches(press):
		p.moveFocus(1)
		return p, nil
	case keys.previous.matches(press):
		p.moveFocus(-1)
		return p, nil
	}
	return p, p.body.Update(press)
}

// leave goes back, once a draft has been warned about: back from a draft
// drops it, and there is nothing to keep.
func (p *newPage) leave() (page, tea.Cmd) {
	if p.dirty() && !p.warned {
		p.warned = true
		p.status = "esc again drops the draft"
		return p, nil
	}
	return p, func() tea.Msg { return popMsg{} }
}

// act is enter: on Create it commits the draft, on a relation's line it
// goes to the issue the line names, and anywhere else it does nothing, as
// on show (C3: nothing creates by accident).
func (p *newPage) act(here position) tea.Cmd {
	switch here.stop {
	case stopCreate:
		return p.create()
	case stopFields:
		if row := p.currentRow(); row != nil && row.link != "" {
			return p.follow(row.link)
		}
	}
	return nil
}

// edit is space: on a cell the widget its kind takes, over the draft's
// value; on the box the cursor goes into the text; on Create it rings,
// because a button is pressed with enter.
func (p *newPage) edit(here position) tea.Cmd {
	switch here.stop {
	case stopBox:
		p.setTyping(true)
		p.status = ""
		return nil
	case stopCreate:
		p.status = "enter creates"
		return bell()
	}
	return p.startEdit(p.field(), nil)
}

func (p *newPage) follow(id string) tea.Cmd {
	shown, err := newShowPage(p.repo, id, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

func (p *newPage) copyHere(here position) tea.Cmd {
	var what, value string
	switch here.stop {
	case stopHeader:
		what = p.field()
		value = plain(p.fields[what])
	case stopFields:
		row := p.currentRow()
		what, value = row.key, plain(p.fields[row.key])
		if row.link != "" {
			value = row.link
		}
	case stopBox:
		what, value = "description", p.body.draft()
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

func (p *newPage) paste(text string) tea.Cmd {
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

// startEdit opens the widget the schema says a field takes, over the
// draft's value; a bool flips in the draft at once.
func (p *newPage) startEdit(fieldKey string, pasted *string) tea.Cmd {
	if fieldKey == "" {
		return bell()
	}
	typeKey := p.typeKey()
	current, _ := decodeValue(p.fields[fieldKey])

	kind, known := fieldKind(p.repo, typeKey, fieldKey)
	if known && kind == schema.KindBool && pasted == nil {
		was, _ := current.(bool)
		p.set(fieldKey, issue.MustValue(!was))
		p.status = fieldKey + " set"
		return nil
	}

	ed, refusal, err := editable(p.repo, "", typeKey, fieldKey, current)
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

func (p *newPage) updateEditor(msg tea.Msg) (page, tea.Cmd) {
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
	if ed.key == schema.TypeKey {
		p.retype(value)
		return p, nil
	}
	p.set(ed.key, value)
	p.status = ed.key + " set"
	return p, nil
}

// set puts a value in the draft, or takes the key out for a null: a draft
// holds what was typed and nothing else.
func (p *newPage) set(key string, value issue.Value) {
	if issue.IsNull(value) || (key == schema.TitleKey && plain(value) == "") {
		delete(p.fields, key)
	} else {
		p.fields[key] = value
	}
	p.rebuild()
}

// retype changes the draft's type, keeping the values of the fields the new
// type also has and dropping the rest, with a count: the draft is the
// person's typing, and a type picked wrong is one key from right (C3).
func (p *newPage) retype(value issue.Value) {
	typeKey, _ := issue.String(value)
	dropped := 0
	if s, err := p.repo.LoadSchema(); err == nil {
		if t, ok := s.Type(typeKey); ok {
			for key := range p.fields {
				if _, builtin := schema.BuiltinKind(key); builtin {
					continue
				}
				if _, has := t.Field(key); !has {
					delete(p.fields, key)
					dropped++
				}
			}
		}
	}
	p.set(schema.TypeKey, value)
	p.status = "type set"
	if dropped > 0 {
		p.status = fmt.Sprintf("type set · %d dropped", dropped)
	}
}

// create commits the draft as `issue new` would, in one operation, and
// hands the stack the id: what the planner refuses is named in the status
// line, the cursor on the cell it names (C3, C4).
func (p *newPage) create() tea.Cmd {
	if p.typeKey() == "" {
		p.focus, p.cell = stopHeader, cellType
		p.status = "a type is required"
		return bell()
	}
	if strings.TrimSpace(p.title()) == "" {
		p.focus, p.cell = stopHeader, cellTitle
		p.status = "a title is required"
		return bell()
	}

	doc := host.IssueDocument{Fields: copyFields(p.fields), Body: p.body.draft(), Aliases: p.aliases}
	id, err := host.IssueNew(p.repo, doc)
	if err != nil {
		p.status = err.Error()
		p.pointAt(err.Error())
		return bell()
	}

	shown, err := newShowPage(p.repo, id.String(), nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return createdMsg{id: id.String(), shown: shown} }
}

// pointAt puts the cursor on the first row whose key a refusal names.
func (p *newPage) pointAt(problem string) {
	for at, row := range p.rows {
		if row.first && strings.Contains(problem, row.key) {
			p.focus, p.row = stopFields, at
			return
		}
	}
}

func (p *newPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	here := p.current()
	top := p.topLines(here)
	body, from, to := p.bodyLines(here)

	var bottom []string
	if p.editor != nil {
		bottom = p.editor.View(p.width)
	}
	bottom = append(bottom, p.statusLine())

	room := max(p.height-len(top)-len(bottom), 1)
	if from >= 0 {
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

func (p *newPage) topLines(here position) []string {
	lines := []string{callLine(p.call, "", "", p.width), ""}
	lines = append(lines, p.headerLines(here)...)
	return append(lines, "")
}

// headerLines is the type, dim, and the title, bold over a rule, as show
// draws them; an empty one says so, dim, so that the cell is there to
// stand on. There is no archived cell: a draft is not archived.
func (p *newPage) headerLines(here position) []string {
	on := here.stop == stopHeader
	focused := func(cell int) bool { return on && p.cell == cell }

	typeKey := p.typeKey()
	if typeKey == "" {
		typeKey = "(no type)"
	}
	title := p.title()
	titleStyle := styleTitle
	if title == "" {
		title, titleStyle = "(no title)", styleDim
	}
	title = truncate(title, max(p.width-ansi.StringWidth(typeKey)-16, 1))

	cells := []string{styleDim.Render(typeKey), titleStyle.Render(title)}
	if focused(cellType) {
		cells[0] = styleCell.Render(typeKey)
	}
	if focused(cellTitle) {
		cells[1] = styleCell.Bold(true).Render(title)
	}

	marker := " "
	if on {
		marker = "›"
	}
	head := marker + " " + strings.Join(cells, "  ")
	rule := "  " + styleDim.Render(strings.Repeat("━", ansi.StringWidth(typeKey)+2+max(ansi.StringWidth(title), 1)))
	return []string{fit(head, p.width), fit(rule, p.width)}
}

// bodyLines is the part that scrolls: the fields table, the description box,
// and the Create button. It says which lines the cursor is
// on, or -1 when it is on none.
func (p *newPage) bodyLines(here position) (lines []string, from, to int) {
	from, to = -1, -1

	lines, rowLine := tableLines(p.rows, p.row, here.stop == stopFields, p.width)
	if here.stop == stopFields {
		from, to = rowLine, rowLine
	}

	boxStart := len(lines)
	lines = append(lines, p.body.View(p.width, here.stop == stopBox)...)
	if here.stop == stopBox {
		from, to = boxStart, len(lines)-1
	}
	lines = append(lines, "")

	lines = append(lines, p.createLine(here))
	if here.stop == stopCreate {
		from, to = len(lines)-1, len(lines)-1
	}
	return lines, from, to
}

// createLine is the button: the one way a draft becomes an issue.
func (p *newPage) createLine(here position) string {
	label := "[ create ]"
	marker, style := " ", styleHeader
	if here.stop == stopCreate {
		marker, style = "›", styleCell.Bold(true)
	}
	return fit(marker+" "+style.Render(label), p.width)
}

func (p *newPage) statusLine() string {
	return styleStatus.Render(bottomLine(p.hintLine(), p.status, p.width))
}

func (p *newPage) hintLine() string {
	if p.editor != nil {
		return p.editor.hints()
	}

	here := p.current()
	switch here.stop {
	case stopBox:
		return hints(draftBoxHints(here.typing)...)
	case stopCreate:
		return hints(hint{"enter", "create"})
	case stopHeader:
		if edit, ok := editHint(fieldKind(p.repo, p.typeKey(), p.field())); ok {
			return hints(edit)
		}
		return hints()
	}

	row := p.currentRow()
	if row == nil {
		return hints()
	}
	var pairs []hint
	if row.link != "" {
		pairs = append(pairs, hint{"enter", "go to"})
	}
	if edit, ok := editHint(fieldKind(p.repo, p.typeKey(), row.key)); ok {
		pairs = append(pairs, edit)
	}
	return hints(pairs...)
}

// draftBoxHints are the description box's keys on new: the comment box's,
// except that enter accepts the text into the draft rather than sending it
// anywhere, because nothing is written until Create.
func draftBoxHints(typing bool) []hint {
	if typing {
		return []hint{{"enter", "done"}, {"alt+enter", "newline"}, {"esc", "leave"}}
	}
	return []hint{{"space", "type"}, {"tab", "skip"}}
}

// human is an id as the status line names it.
func human(id string) string {
	return entity.Id(id).Human()
}
