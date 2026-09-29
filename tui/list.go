package tui

import (
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
// With `expand` bound they are a tree: a row's children along that relation
// under it, to `depth` levels (nest.go).
type listPage struct {
	repo *cache.RepoCache

	// the call as it was made, for the line that says what this view is, and
	// unpacked once for everything else
	call      *view.Call
	query     string
	fields    []string
	details   []string
	groupBy   string
	expandKey string
	depth     int
	rankKey   string

	// items is what the query returned last, kept so that folding a row
	// rebuilds the tree without asking the store again
	items []map[string]any
	// folded is the rows folded shut, by id, across every rebuild.
	folded map[string]bool

	rows []listRow
	// nodes is the tree the rows sit in, one per row: level, parent, group,
	// rank, and the text the filter searches.
	nodes []treeRow
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
	help      *help

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
}

func (p *listPage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func newListPage(repo *cache.RepoCache, call *view.Call) (*listPage, error) {
	p := &listPage{
		repo:      repo,
		call:      call,
		query:     call.String("query"),
		fields:    call.Strings("fields"),
		details:   call.Strings("details"),
		groupBy:   call.String("group_by"),
		expandKey: call.String("expand"),
		depth:     nestDepth(call),
		rankKey:   call.String("rank"),
		folded:    map[string]bool{},
		width:     80,
		height:    24,
		grabbed:   -1,
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
	values, err := host.IssueList(p.repo, p.query)
	if err != nil {
		return err
	}

	// A query that returned something other than issues draws nothing: the
	// list is a list of issues, and inventing rows out of whatever came back
	// would be worse than an empty one.
	p.items, _ = host.IssueItems(values)
	p.rebuild()
	return nil
}

// rebuild makes the rows out of the last query's items: the tree along
// `expand`, then a row per node, then the drawing order.
func (p *listPage) rebuild() {
	was := p.currentId()

	known := newKinds(p.repo)
	tree := nest(p.repo, p.items, p.expandKey, p.depth, p.folded)
	p.rows = make([]listRow, 0, len(tree))
	p.nodes = make([]treeRow, 0, len(tree))
	for _, n := range tree {
		row, node := p.newRow(n, known)
		p.rows = append(p.rows, row)
		p.nodes = append(p.nodes, node)
	}

	p.reorder()
	p.putCursorOn(was)
}

func (p *listPage) newRow(n nested, known *kinds) (listRow, treeRow) {
	fields, _ := n.item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	row := listRow{
		id:      n.id,
		human:   host.StringOr(n.item["human_id"], ""),
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
		row.cells[key] = known.cellText(row.typeKey, key, fields[key])
	}

	node := treeRow{
		id:       n.id,
		parent:   n.parent,
		level:    n.level,
		children: n.children,
		folded:   n.folded,
		hidden:   n.hidden,
		group:    noGroup,
	}
	if p.groupBy != "" {
		if value := row.cells[p.groupBy]; value != "" {
			node.group = value
		}
	}
	if p.rankKey != "" {
		node.rank = plainValue(fields[p.rankKey])
	}

	var text strings.Builder
	text.WriteString(row.human)
	for _, key := range append(append([]string{}, p.fields...), p.details...) {
		text.WriteString(" ")
		text.WriteString(row.cells[key])
	}
	node.text = strings.ToLower(text.String())

	return row, node
}

// reorder rebuilds the drawing order: the filter, then the groups in the
// order they first appear with the ungrouped last, then the rank, each
// subtree under its root (treeOrder).
func (p *listPage) reorder() {
	p.order = treeOrder(p.nodes, p.filter, p.rankKey != "")
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

// node is the tree row under the cursor.
func (p *listPage) node() *treeRow {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return nil
	}
	return &p.nodes[p.order[p.cursor]]
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
	case p.filtering != nil:
		return p.updateFilter(press)
	case p.grabbed >= 0:
		return p.updateGrab(press)
	}

	switch {
	case keys.quit.matches(press):
		return p, tea.Quit

	case keys.up.matches(press):
		p.moveAtLevel(-1)
	case keys.down.matches(press):
		p.moveAtLevel(1)
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

	case keys.next.matches(press):
		p.intoChild()
	case keys.previous.matches(press):
		p.toParent()
	case keys.fold.matches(press):
		p.toggleFold()

	case keys.act.matches(press):
		return p.act()

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

// moveAtLevel is up and down: between the rows at the cursor's own nesting
// depth, so that a level reads as the list it is, and tab is the way down.
// Without `expand` every row is at the root, and this is the next row.
func (p *listPage) moveAtLevel(by int) {
	node := p.node()
	if node == nil {
		return
	}
	for at := p.cursor + by; at >= 0 && at < len(p.order); at += by {
		if p.nodes[p.order[at]].level == node.level {
			p.cursor = at
			return
		}
	}
}

// intoChild is tab: onto the row's first child, unfolding it on the way.
func (p *listPage) intoChild() {
	node := p.node()
	if node == nil || node.children == 0 {
		return
	}
	if node.folded {
		p.folded[node.id] = false
		p.rebuild()
		node = p.node()
	}
	if at := p.cursor + 1; at < len(p.order) && p.nodes[p.order[at]].level == node.level+1 {
		p.cursor = at
	}
}

// toParent is shift-tab: onto the row this one is under.
func (p *listPage) toParent() {
	node := p.node()
	if node == nil || node.level == 0 {
		return
	}
	for at := p.cursor - 1; at >= 0; at-- {
		if p.nodes[p.order[at]].level < node.level {
			p.cursor = at
			return
		}
	}
}

// toggleFold is z: a parent's children shown or hidden.
func (p *listPage) toggleFold() {
	node := p.node()
	if node == nil || node.children == 0 {
		return
	}
	p.folded[node.id] = !node.folded
	p.rebuild()
}

// act is enter, the one action key: on the id it opens the issue, and on
// any other cell it edits it — a relation's picker opening on "go to" the
// issue it names, so enter, enter follows the link
// (doc/design/terminal-renderer.md, 2026-09-29).
func (p *listPage) act() (page, tea.Cmd) {
	row := p.current()
	if row == nil {
		return p, nil
	}
	if p.column == 0 {
		return p, p.push(row.id)
	}
	return p, p.startEdit(nil)
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

// copyCell puts the cell under the cursor on the clipboard (setClipboard).
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
	return setClipboard(value)
}

// copyId copies the issue's whole id, whatever column the cursor is on.
func (p *listPage) copyId() tea.Cmd {
	row := p.current()
	if row == nil {
		return bell()
	}
	p.status = "copied " + row.id
	return setClipboard(row.id)
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
		case keys.act.matches(press):
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

	ed, refusal, err := editable(p.repo, row.id, row.typeKey, fieldKey, row.fields[fieldKey])
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

	if id := ed.goTo(); id != "" {
		return p, p.push(id)
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

	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragBy moves the grabbed row among its siblings — within its group and
// its level, its own subtree with it — on the screen only: the key is
// computed once, when it is dropped, so a drag of six rows is one write.
func (p *listPage) dragBy(by int) {
	p.cursor = moveBlock(p.nodes, p.order, p.cursor, by)
}

// drop writes the rank of the grabbed row: one key strictly between its new
// neighbours', which is one operation on one issue.
func (p *listPage) drop() tea.Cmd {
	row := &p.rows[p.grabbed]
	p.grabbed = -1

	lo, hi := siblingRanks(p.nodes, p.order, p.cursor)
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

func blinkTick() tea.Cmd {
	return tea.Tick(blinkInterval, func(_ time.Time) tea.Msg { return blinkMsg{} })
}

const blinkInterval = 400 * time.Millisecond

// lessByRank is the order rule a bound rank imposes: (rank, id), the issues
// without a rank last. Never rank alone: the tie-break by id is what makes
// two concurrent drags into the same gap both survive (441dcbb).
func lessByRank(rankI, idI, rankJ, idJ string) bool {
	if (rankI == "") != (rankJ == "") {
		return rankJ == ""
	}
	if rankI != rankJ {
		return rankI < rankJ
	}
	return idI < idJ
}
