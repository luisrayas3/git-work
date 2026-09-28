package tui

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/rank"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// listPage is the `list` view: one row per issue, one column per field.
//
// The rows are the excerpts the query returned, verbatim, so what is drawn is
// what `git work issue` prints and a jq program can be written against.
type listPage struct {
	repo *cache.RepoCache

	// the call as it was made, for the line that says what this view is, and
	// unpacked once for everything else
	call    *view.Call
	query   string
	fields  []string
	details []string
	groupBy string
	rankKey string

	rows []listRow
	// order indexes rows in the order they are drawn: filtered, grouped, and
	// sorted within a group by (rank, id) where a rank is bound.
	order []int

	cursor int
	// column is 0 on the id, which is where the cursor starts, and 1 + the
	// index into fields on a field
	column int
	top    int

	width, height int

	filter    string
	filtering *textinput.Model
	editor    *editor
	// choosing is the picker enter opens on a cell that links several issues
	choosing *picker
	help     *help

	// grabbed is the row being dragged, by index into rows, or -1.
	grabbed int
	blink   bool

	status string
}

// listRow is one issue, as the query handed it over.
type listRow struct {
	id      string
	human   string
	typeKey string
	fields  map[string]any

	// cells are the fields as drawn: a relation is the issues it names,
	// everything else its plain value; links are the ids a relation cell
	// holds, which enter follows
	cells map[string]string
	links map[string][]string

	group string
	rank  string
	// text is everything the row draws, folded, for the filter to search.
	text string
}

func newListPage(repo *cache.RepoCache, call *view.Call) (*listPage, error) {
	p := &listPage{
		repo:    repo,
		call:    call,
		query:   call.String("query"),
		fields:  call.Strings("fields"),
		details: call.Strings("details"),
		groupBy: call.String("group_by"),
		rankKey: call.String("rank"),
		width:   80,
		height:  24,
		grabbed: -1,
	}
	if len(p.fields) == 0 {
		p.fields = []string{schema.TitleKey}
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// load re-runs the query and rebuilds the rows, keeping the cursor on the
// issue it was on.
//
// It is called when the page opens, after every write it makes, and whenever
// the ref watcher says another process wrote something. The query is the
// whole of what the page knows, so there is nothing to reconcile: it is read
// again, and the cursor is put back by id.
func (p *listPage) load() error {
	was := p.currentId()

	values, err := host.IssueList(p.repo, p.query)
	if err != nil {
		return err
	}

	// A query that returned something other than issues draws nothing: the
	// list is a list of issues, and inventing rows out of whatever came back
	// would be worse than an empty one.
	items, _ := host.IssueItems(values)
	known := newKinds(p.repo)
	p.rows = make([]listRow, 0, len(items))
	for _, item := range items {
		p.rows = append(p.rows, p.newRow(item, known))
	}

	p.reorder()
	p.putCursorOn(was)
	return nil
}

func (p *listPage) newRow(item map[string]any, known *kinds) listRow {
	fields, _ := item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	row := listRow{
		id:      host.StringOr(item["id"], ""),
		human:   host.StringOr(item["human_id"], ""),
		typeKey: host.StringOr(fields[schema.TypeKey], ""),
		fields:  fields,
	}
	if row.human == "" && len(row.id) > 7 {
		row.human = row.id[:7]
	}

	row.cells = map[string]string{}
	row.links = map[string][]string{}
	for _, key := range append(append([]string{p.groupBy}, p.fields...), p.details...) {
		if key == "" {
			continue
		}
		if isRelation(known.of(row.typeKey, key)) {
			ids := linkIds(fields[key])
			row.links[key] = ids
			row.cells[key] = linkText(p.repo, ids)
			continue
		}
		row.cells[key] = plainValue(fields[key])
	}

	row.group = noGroup
	if p.groupBy != "" {
		if value := row.cells[p.groupBy]; value != "" {
			row.group = value
		}
	}
	if p.rankKey != "" {
		row.rank = plainValue(fields[p.rankKey])
	}

	var text strings.Builder
	text.WriteString(row.human)
	for _, key := range append(append([]string{}, p.fields...), p.details...) {
		text.WriteString(" ")
		text.WriteString(row.cells[key])
	}
	row.text = strings.ToLower(text.String())

	return row
}

// reorder rebuilds the drawing order: the filter, then the groups in the
// order they first appear with the ungrouped last, then the rank.
func (p *listPage) reorder() {
	matching := make([]int, 0, len(p.rows))
	needle := strings.ToLower(strings.TrimSpace(p.filter))
	for at, row := range p.rows {
		if needle == "" || strings.Contains(row.text, needle) {
			matching = append(matching, at)
		}
	}

	groups := make([]string, 0, 4)
	members := make(map[string][]int, 4)
	for _, at := range matching {
		group := p.rows[at].group
		if _, seen := members[group]; !seen {
			groups = append(groups, group)
		}
		members[group] = append(members[group], at)
	}
	// the rows with no value for the grouping field come last: they are the
	// ones nobody has filed yet, and they are what a session works through.
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[j] == noGroup && groups[i] != noGroup
	})

	p.order = p.order[:0]
	for _, group := range groups {
		rows := members[group]
		if p.rankKey != "" {
			// (rank, id), never rank alone: that tie-break is what makes two
			// concurrent drags both survive (441dcbb).
			sort.SliceStable(rows, func(i, j int) bool {
				left, right := p.rows[rows[i]], p.rows[rows[j]]
				if (left.rank == "") != (right.rank == "") {
					return right.rank == ""
				}
				if left.rank != right.rank {
					return left.rank < right.rank
				}
				return left.id < right.id
			})
		}
		p.order = append(p.order, rows...)
	}

	p.clamp()
}

func (p *listPage) clamp() {
	if p.cursor >= len(p.order) {
		p.cursor = len(p.order) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.column > len(p.fields) {
		p.column = len(p.fields)
	}
	if p.column < 0 {
		p.column = 0
	}
}

func (p *listPage) currentId() string {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return ""
	}
	return p.rows[p.order[p.cursor]].id
}

func (p *listPage) current() *listRow {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return nil
	}
	return &p.rows[p.order[p.cursor]]
}

// putCursorOn keeps the cursor on the issue it was on across a refresh,
// falling back to the same position when that issue is gone.
func (p *listPage) putCursorOn(id string) {
	if id == "" {
		p.clamp()
		return
	}
	for at, index := range p.order {
		if p.rows[index].id == id {
			p.cursor = at
			return
		}
	}
	p.clamp()
}

func (p *listPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case refreshMsg:
		if err := p.load(); err != nil {
			p.status = err.Error()
		}
		return p, nil

	case blinkMsg:
		if p.grabbed < 0 {
			return p, nil
		}
		p.blink = !p.blink
		return p, blinkTick()

	case tea.KeyPressMsg:
		return p.key(msg)

	// the terminal's paste, and the answer to asking it for its clipboard,
	// are the same thing: text for the field under the cursor
	case tea.PasteMsg:
		if p.editor != nil || p.filtering != nil {
			break
		}
		return p, p.paste(msg.Content)
	case tea.ClipboardMsg:
		if p.editor != nil || p.filtering != nil {
			return p, nil
		}
		return p, p.paste(msg.Content)
	}

	// a widget that asked for a command gets the answer to it
	if p.editor != nil {
		return p.updateEditor(msg)
	}
	if p.filtering != nil {
		return p.updateFilter(msg)
	}
	return p, nil
}

func (p *listPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.help != nil:
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
	case p.editor != nil:
		return p.updateEditor(press)
	case p.choosing != nil:
		return p.updateChoice(press)
	case p.filtering != nil:
		return p.updateFilter(press)
	case p.grabbed >= 0:
		return p.updateGrab(press)
	}

	switch {
	case keys.quit.matches(press):
		return p, tea.Quit

	case keys.up.matches(press):
		p.move(-1)
	case keys.down.matches(press):
		p.move(1)
	case keys.pageUp.matches(press):
		p.move(-p.rowsPerPage())
	case keys.pageDn.matches(press):
		p.move(p.rowsPerPage())
	case keys.top.matches(press):
		p.cursor = 0
	case keys.bottom.matches(press):
		p.cursor = len(p.order) - 1

	case keys.left.matches(press):
		p.column = max(0, p.column-1)
	case keys.right.matches(press):
		p.column = min(len(p.fields), p.column+1)

	// edit before open: without the kitty protocol ctrl+enter is enter, and
	// with it the two are different keys, so the order only matters there
	case keys.edit.matches(press):
		return p, p.startEdit(nil)
	case keys.open.matches(press):
		return p.open()

	case keys.copyId.matches(press):
		return p, p.copyId()
	case keys.copy.matches(press):
		return p, p.copyCell()
	case keys.paste.matches(press):
		p.status = "reading clipboard…"
		return p, tea.ReadClipboard
	case keys.filter.matches(press):
		p.startFilter()
	case keys.grab.matches(press):
		return p.startGrab()
	case keys.help.matches(press):
		p.help = &help{}

	case keys.back.matches(press):
		// back out of the filter first; out of the view after that
		if p.filter != "" {
			p.filter = ""
			p.reorder()
			p.status = ""
			return p, nil
		}
		return p, func() tea.Msg { return popMsg{} }
	}

	p.clamp()
	return p, nil
}

func (p *listPage) move(by int) {
	p.cursor = min(max(p.cursor+by, 0), max(len(p.order)-1, 0))
}

// open opens the issue under the cursor, or, on a cell that links other
// issues, the one it links: a link is followed where it is drawn.
func (p *listPage) open() (page, tea.Cmd) {
	row := p.current()
	if row == nil {
		return p, nil
	}
	switch links := row.links[p.fieldKey()]; len(links) {
	case 0:
		return p, p.push(row.id)
	case 1:
		return p, p.push(links[0])
	default:
		items := make([]choice, 0, len(links))
		for _, id := range links {
			items = append(items, choice{label: linkLabel(p.repo, id), value: id})
		}
		p.choosing = newPicker(items, "")
		return p, nil
	}
}

func (p *listPage) updateChoice(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		p.choosing = nil
	case keys.up.matches(press):
		p.choosing.cursor = max(0, p.choosing.cursor-1)
	case keys.down.matches(press):
		p.choosing.cursor = min(len(p.choosing.items)-1, p.choosing.cursor+1)
	case keys.open.matches(press):
		id := p.choosing.items[p.choosing.cursor].value
		p.choosing = nil
		return p, p.push(id)
	}
	return p, nil
}

// push opens an issue over the list.
func (p *listPage) push(id string) tea.Cmd {
	shown, err := newShowPage(p.repo, id, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

// fieldKey is the field under the column cursor, or "" on the id.
func (p *listPage) fieldKey() string {
	if p.column == 0 || p.column > len(p.fields) {
		return ""
	}
	return p.fields[p.column-1]
}

// copyCell puts the cell under the cursor on the clipboard with OSC 52, which
// is the one way that works over ssh and in a multiplexer, because it is the
// terminal that copies and not the machine the program runs on.
//
// On the id column that is the id, which is where the cursor starts: the
// chat pin (ca81145) is the first key anybody presses.
func (p *listPage) copyCell() tea.Cmd {
	row := p.current()
	if row == nil {
		return bell()
	}
	fieldKey := p.fieldKey()
	if fieldKey == "" {
		return p.copyId()
	}
	// a link copies the ids it holds, which is what another command takes
	value := plainValue(row.fields[fieldKey])
	if value == "" {
		p.status = fieldKey + " empty"
		return bell()
	}
	p.status = "copied " + fieldKey
	return tea.SetClipboard(value)
}

// copyId copies the issue's whole id, whatever column the cursor is on.
func (p *listPage) copyId() tea.Cmd {
	row := p.current()
	if row == nil {
		return bell()
	}
	p.status = "copied " + row.id
	return tea.SetClipboard(row.id)
}

// paste opens the editor on the field under the cursor with the text in it.
// Enter writes it; a paste alone never does.
func (p *listPage) paste(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		p.status = "clipboard empty"
		return bell()
	}
	return p.startEdit(&text)
}

func (p *listPage) startFilter() {
	input := textinput.New()
	input.SetValue(p.filter)
	input.SetWidth(p.width - 10)
	input.Focus()
	p.filtering = &input
}

func (p *listPage) updateFilter(msg tea.Msg) (page, tea.Cmd) {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			p.filtering = nil
			p.filter = ""
			p.reorder()
			return p, nil
		case keys.open.matches(press):
			p.filtering = nil
			p.reorder()
			return p, nil
		}
	}

	updated, cmd := p.filtering.Update(msg)
	p.filtering = &updated
	p.filter = updated.Value()
	p.reorder()
	return p, cmd
}

// startEdit opens the widget the schema says this field takes, with pasted
// text in it when there is some.
//
// A bool has no widget: there is one other value, so asking which one would
// be a question with one answer. A cell that cannot be edited rings the bell.
func (p *listPage) startEdit(pasted *string) tea.Cmd {
	row := p.current()
	if row == nil {
		return bell()
	}
	fieldKey := p.fieldKey()
	if fieldKey == "" {
		p.status = "id not editable"
		return bell()
	}

	kind, known := fieldKind(p.repo, row.typeKey, fieldKey)
	if known && kind == schema.KindBool && pasted == nil {
		was, _ := row.fields[fieldKey].(bool)
		p.write(row.id, fieldKey, issue.MustValue(!was))
		return nil
	}

	ed, refusal, err := editable(p.repo, row.typeKey, fieldKey, row.fields[fieldKey])
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
	ed.issueId = row.id
	p.editor = ed
	p.status = ""
	return nil
}

func (p *listPage) updateEditor(msg tea.Msg) (page, tea.Cmd) {
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
	p.write(ed.issueId, ed.key, value)
	return p, nil
}

// write is every edit's one exit: through host, which is through the cache,
// which is the only thing that holds the write lock (AGENTS.md).
//
// A refusal — the schema's, or the entity's — is a status line and nothing
// else changes, because the store did not change either.
func (p *listPage) write(id, key string, value issue.Value) {
	if _, err := host.IssueSet(p.repo, id, map[string]issue.Value{key: value}, false); err != nil {
		p.status = err.Error()
		return
	}
	p.status = key + " set"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
}

// startGrab picks the row under the cursor up, to drop it somewhere else.
//
// It needs a rank field: without one the order is the query's, and moving a
// row would be a change with nowhere to be written.
func (p *listPage) startGrab() (page, tea.Cmd) {
	if p.rankKey == "" {
		p.status = "no rank: cannot reorder"
		return p, nil
	}
	if p.current() == nil {
		return p, nil
	}
	p.grabbed = p.order[p.cursor]
	p.blink = true
	return p, blinkTick()
}

func (p *listPage) updateGrab(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		// the row goes back where it was: the order is rebuilt from the
		// store, which never changed.
		p.grabbed = -1
		p.reorder()
		p.putCursorOn(p.currentId())
		return p, nil

	case keys.up.matches(press):
		p.dragBy(-1)
	case keys.down.matches(press):
		p.dragBy(1)

	case keys.grab.matches(press), keys.open.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragBy moves the grabbed row within its group, on the screen only: the key
// is computed once, when it is dropped, so a drag of six rows is one write.
func (p *listPage) dragBy(by int) {
	to := p.cursor + by
	if to < 0 || to >= len(p.order) {
		return
	}
	if p.rows[p.order[to]].group != p.rows[p.grabbed].group {
		return
	}
	p.order[p.cursor], p.order[to] = p.order[to], p.order[p.cursor]
	p.cursor = to
}

// drop writes the rank of the grabbed row: one key strictly between its new
// neighbours', which is one operation on one issue.
func (p *listPage) drop() tea.Cmd {
	row := &p.rows[p.grabbed]
	p.grabbed = -1

	lo, hi := p.neighbourRanks()
	key, err := rank.Between(lo, hi)
	if err != nil {
		p.status = err.Error()
		p.reorder()
		return nil
	}

	p.write(row.id, p.rankKey, issue.StringValue(key))
	p.putCursorOn(row.id)
	return nil
}

// neighbourRanks reads the ranks the dropped row has to land between.
//
// An empty string on either side is the end of the list, which is what
// rank.Between takes for "before everything" and "after everything".
func (p *listPage) neighbourRanks() (string, string) {
	group := p.rows[p.order[p.cursor]].group

	var lo, hi string
	for at := p.cursor - 1; at >= 0; at-- {
		if p.rows[p.order[at]].group != group {
			break
		}
		if r := p.rows[p.order[at]].rank; r != "" {
			lo = r
			break
		}
	}
	for at := p.cursor + 1; at < len(p.order); at++ {
		if p.rows[p.order[at]].group != group {
			break
		}
		if r := p.rows[p.order[at]].rank; r != "" {
			hi = r
			break
		}
	}
	return lo, hi
}

func blinkTick() tea.Cmd {
	return tea.Tick(blinkInterval, func(_ time.Time) tea.Msg { return blinkMsg{} })
}

const blinkInterval = 400 * time.Millisecond
