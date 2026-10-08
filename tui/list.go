package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// listPage is the `list` view: one row per issue, one column per field.
//
// The rows are the excerpts the query returned, verbatim, so what is drawn is
// what `git work issue` prints and a jq program can be written against.
// With `expand` bound they are a tree: under each opened row the issues its
// layer's relation names, drawn as a child table of their own (nest.go,
// list_view.go).
type listPage struct {
	repo *cache.RepoCache

	// the call as it was made, for the line that says what this view is, and
	// unpacked once for everything else
	call  *view.Call
	query string
	// includeArchive brings the archived back into the input query runs over.
	includeArchive bool
	// nest is `expand` resolved: the layer per level, level 0 being the
	// call's own fields, details, group_by and rank.
	nest    *nesting
	groupBy string
	rankKey string

	// items is what the query returned last, kept so that folding a row
	// rebuilds the tree without asking the store again
	items []map[string]any
	// open is the parents folded open, by id, across every rebuild: a tree
	// opens folded, so what is remembered is what was opened.
	open map[string]bool

	rows []listRow
	// nodes is the tree the rows sit in, one per row: level, parent, group,
	// rank, and the text the filter searches.
	nodes []treeRow
	// order indexes rows in the order they are drawn: filtered, grouped, and
	// sorted within a group by (rank, id) where a rank is bound.
	order []int

	cursor int
	// column is 0 on the id, which is where the cursor starts, then the tree
	// cell where anything nests, then the fields of the cursor's own layer.
	column int
	top    int

	width, height int

	filter    string
	filtering *textinput.Model
	editor    *editor
	help      *help

	// grabbed is the row being dragged, by index into rows, or -1;
	// grabGroup is the group it was picked up in, and crossed the group it
	// has been carried into, so that a drop writes the grouping field only
	// where the row really changed group.
	grabbed   int
	grabGroup string
	crossed   *crossing
	blink     bool

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
		repo:           repo,
		call:           call,
		includeArchive: call.Bool("include_archive"),
		query:          call.String("query"),
		groupBy:        call.String("group_by"),
		rankKey:        call.String("rank"),
		open:           map[string]bool{},
		width:          80,
		height:         24,
		grabbed:        -1,
	}

	root := nestLayer{
		fields:         call.Strings("fields"),
		details:        call.Strings("details"),
		groupBy:        p.groupBy,
		rankKey:        p.rankKey,
		includeArchive: p.includeArchive,
	}
	if len(root.fields) == 0 {
		root.fields = []string{schema.TitleKey}
	}
	n, err := newNesting(root, call.Expand())
	if err != nil {
		return nil, err
	}
	p.nest = n

	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// layer is the layer a row at this level draws by, the root's where the spec
// stops short, which cannot happen for a row that is in the tree.
func (p *listPage) layer(level int) *nestLayer {
	if layer := p.nest.at(level); layer != nil {
		return layer
	}
	return &p.nest.layers[0]
}

// cursorFields are the fields of the layer the cursor's row is on: the
// cells `←` and `→` walk after the id and the tree cell, which are the
// row's own table's columns.
func (p *listPage) cursorFields() []string {
	node := p.node()
	if node == nil {
		return p.nest.layers[0].fields
	}
	return p.layer(node.level).fields
}

// treeCol is the tree cell's column, -1 where nothing nests: the id is
// first and flush, the arrow the cell after it, the fields after that.
func (p *listPage) treeCol() int {
	if p.nest.expanded() {
		return 1
	}
	return -1
}

// firstFieldCol is the column of the cursor's row's first field.
func (p *listPage) firstFieldCol() int {
	if p.nest.expanded() {
		return 2
	}
	return 1
}

// lastCol is the right-most cell of the cursor's row.
func (p *listPage) lastCol() int {
	return max(p.firstFieldCol()+len(p.cursorFields())-1, 0)
}

// load re-runs the query and rebuilds the rows, keeping the cursor on the
// issue it was on.
//
// It is called when the page opens, after every write it makes, and whenever
// the ref watcher says another process wrote something. The query is the
// whole of what the page knows, so there is nothing to reconcile: it is read
// again, and the cursor is put back by id.
func (p *listPage) load() error {
	values, err := host.IssueList(p.repo, p.query, p.includeArchive)
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

// rebuild makes the rows out of the last query's items: the tree the layers
// describe, then a row per node, then the drawing order.
//
// A refresh that leaves the row the cursor was on hidden under a folded
// parent opens the way to it, the way a filter keeps a match's ancestors on
// the screen: the cursor is never put somewhere else because something
// closed over it.
func (p *listPage) rebuild() {
	was := p.currentId()

	known := newKinds(p.repo)
	build := func() {
		tree, err := nest(p.repo, p.items, p.nest, p.open)
		if err != nil {
			p.status = err.Error()
		}
		p.rows = make([]listRow, 0, len(tree))
		p.nodes = make([]treeRow, 0, len(tree))
		for _, n := range tree {
			row, node := p.newRow(n, known)
			p.rows = append(p.rows, row)
			p.nodes = append(p.nodes, node)
		}
	}

	build()
	if reveal(p.nodes, p.open, was) {
		build()
	}
	p.addGhosts()

	p.reorder()
	p.putCursorOn(was)
}

// addGhosts puts a ghost at the foot of each group of roots, one for the
// whole list when nothing groups it (ghost.go). It is appended after every
// row and carries no rank, so the order draws it last in its group.
func (p *listPage) addGhosts() {
	layer := p.layer(0)
	groups := []string{}
	types := map[string][]string{}
	seen := map[string]bool{}
	for index, node := range p.nodes {
		if node.level != 0 || node.ghost {
			continue
		}
		group := node.group
		if layer.groupBy == "" {
			group = noGroup
		}
		if !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
		types[group] = append(types[group], p.rows[index].typeKey)
	}
	if len(groups) == 0 {
		groups = append(groups, noGroup)
	}
	for _, group := range groups {
		row := listRow{
			id:      ghostId(group),
			human:   ghostPrefix,
			typeKey: sharedType(types[group]),
			fields:  map[string]any{},
			cells:   map[string]string{},
			links:   map[string][]string{},
		}
		if len(layer.fields) > 0 {
			row.cells[layer.fields[0]] = ghostLabel
		}
		node := treeRow{id: row.id, group: group, grouped: layer.groupBy != "", ghost: true}
		p.rows = append(p.rows, row)
		p.nodes = append(p.nodes, node)
	}
}

// ghostDoc is the draft a ghost opens: the type its group's rows share, and
// the `group_by` field as a drop into the group would write it (C6).
func (p *listPage) ghostDoc(ghost *listRow, node *treeRow) host.IssueDocument {
	doc := host.IssueDocument{Fields: map[string]issue.Value{}}
	if ghost.typeKey != "" {
		doc.Fields[schema.TypeKey] = issue.StringValue(ghost.typeKey)
	}
	for index, other := range p.nodes {
		if other.ghost || other.level != 0 || other.group != node.group {
			continue
		}
		row := &p.rows[index]
		if value, ok := groupPrefill(p.repo, row.typeKey, p.groupBy, node.group, row.fields[p.groupBy]); ok {
			doc.Fields[p.groupBy] = value
		}
		break
	}
	return doc
}

// total is how many issues the query returned: the rows, less the ghosts.
func (p *listPage) total() int {
	n := 0
	for _, node := range p.nodes {
		if !node.ghost {
			n++
		}
	}
	return n
}

// count is how many issues the list draws: the rows, less the ghosts.
func (p *listPage) count() int {
	n := 0
	for _, index := range p.order {
		if !p.nodes[index].ghost {
			n++
		}
	}
	return n
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

	layer := p.layer(n.level)
	row.cells = map[string]string{}
	row.links = map[string][]string{}
	for _, key := range append(append([]string{layer.groupBy}, layer.fields...), layer.details...) {
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
		grouped:  layer.groupBy != "",
	}
	if layer.groupBy != "" {
		if value := row.cells[layer.groupBy]; value != "" {
			node.group = value
		}
	}
	node.rank = plainValue(fields[layer.rankKey])

	var text strings.Builder
	text.WriteString(row.human)
	for _, key := range append(append([]string{}, layer.fields...), layer.details...) {
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
	p.order = treeOrder(p.nodes, p.filter)
	p.clamp()
}

func (p *listPage) clamp() {
	if p.cursor >= len(p.order) {
		p.cursor = len(p.order) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.column > p.lastCol() {
		p.column = p.lastCol()
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

	case createdMsg:
		// a draft opened from here was created: the issue appears where the
		// ghost stood, the cursor on it (doc/design/create.md, C4), or is
		// named when the query does not keep it
		if err := p.load(); err != nil {
			p.status = err.Error()
			return p, nil
		}
		p.putCursorOn(msg.id)
		p.status = "created " + human(msg.id)
		if p.currentId() != msg.id {
			p.status += ", not in this view"
		}
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
		p.column = min(p.lastCol(), p.column+1)

	case keys.next.matches(press):
		p.intoChild()
	case keys.previous.matches(press):
		p.toParent()
	case keys.foldAll.matches(press):
		p.toggleAll()

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
	case keys.edit.matches(press):
		return p.edit()
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
		p.open[node.id] = true
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

// toggleFold is space on the tree cell: a parent's children shown or
// hidden. A leaf has nothing to fold and rings.
func (p *listPage) toggleFold() tea.Cmd {
	node := p.node()
	if node == nil || node.children == 0 {
		p.status = "nothing to fold"
		return bell()
	}
	p.open[node.id] = node.folded
	p.rebuild()
	return nil
}

// toggleAll is Z: every parent folded, or, where they all are, every parent
// open. Folding wins the tie, because the summary is the thing to get back
// to from a tree that has been opened all over.
func (p *listPage) toggleAll() {
	p.open = foldAll(p.nodes, p.open)
	p.rebuild()
}

// act is enter, which opens and never edits: on a relation cell the issue it
// names, the first of several, and on any other cell the row's own issue
// (doc/design/terminal-renderer.md, 2026-10-02).
func (p *listPage) act() (page, tea.Cmd) {
	row := p.current()
	if row == nil {
		return p, nil
	}
	if node := p.node(); node.ghost {
		return p, openNew(p.repo, p.ghostDoc(row, node))
	}
	if links := row.links[p.fieldKey()]; len(links) > 0 {
		return p, p.push(links[0])
	}
	return p, p.push(row.id)
}

// edit is space: on a field it edits it — the widget its kind takes, a
// relation's picker on the current value — on the tree cell it folds, and on
// the id, which is not editable, it grabs the row to move it. A column that
// is not a field of the row's type is drawn, never written, and rings the
// bell (startEdit).
func (p *listPage) edit() (page, tea.Cmd) {
	if node := p.node(); node != nil && node.ghost {
		p.status = "enter adds an issue here"
		return p, bell()
	}
	switch p.column {
	case 0:
		return p.startGrab()
	case p.treeCol():
		return p, p.toggleFold()
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

// fieldKey is the field under the column cursor, or "" on the id and on the
// tree cell, neither of which is a field.
func (p *listPage) fieldKey() string {
	fields := p.cursorFields()
	at := p.column - p.firstFieldCol()
	if at < 0 || at >= len(fields) {
		return ""
	}
	return fields[at]
}

// copyCell puts the cell under the cursor on the clipboard (setClipboard).
//
// On the id column that is the id, which is where the cursor starts: the
// chat pin (ca81145) is the first key anybody presses.
func (p *listPage) copyCell() tea.Cmd {
	row := p.current()
	if row == nil || isGhost(row.id) {
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
//
// The clipboard gets the whole id; the message names the short one, which is
// the id the screen shows and the only part that fits the line.
func (p *listPage) copyId() tea.Cmd {
	row := p.current()
	if row == nil || isGhost(row.id) {
		return bell()
	}
	p.status = "copied " + row.human
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
	if row == nil || isGhost(row.id) {
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
	p.writeFields(id, map[string]issue.Value{key: value}, key+" set")
}

// writeFields is a write of several keys at once, which is still one
// operation per key in one commit: what a drop that both reorders a row and
// moves it into another group makes.
func (p *listPage) writeFields(id string, fields map[string]issue.Value, said string) {
	if _, err := host.IssueSet(p.repo, id, fields, false); err != nil {
		p.status = err.Error()
		return
	}
	p.status = said
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
}

// startGrab picks the row under the cursor up, to drop it somewhere else.
//
// It needs no binding: `rank` is a field of every type and the argument's
// own default (D8), so a move always has somewhere to be written.
func (p *listPage) startGrab() (page, tea.Cmd) {
	if p.current() == nil {
		return p, nil
	}
	p.grabbed = p.order[p.cursor]
	p.grabGroup = p.nodes[p.grabbed].group
	p.crossed = nil
	p.blink = true
	return p, blinkTick()
}

func (p *listPage) updateGrab(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		// the row goes back where it was, its group included: the order is
		// rebuilt from the store, which never changed.
		p.nodes[p.grabbed].group = p.grabGroup
		p.grabbed = -1
		p.crossed = nil
		p.reorder()
		p.putCursorOn(p.currentId())
		return p, nil

	case keys.up.matches(press):
		return p, p.dragBy(-1)
	case keys.down.matches(press):
		return p, p.dragBy(1)

	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragBy moves the grabbed row among its siblings — within its group and
// its level, its own subtree with it — on the screen only: the key is
// computed once, when it is dropped, so a drag of six rows is one write.
//
// Past the last sibling, a root enters the neighbouring group (group.go):
// what changes there is which header the row is drawn under, which is the
// row's own group, so nothing in the order moves.
func (p *listPage) dragBy(by int) tea.Cmd {
	if to := moveBlock(p.nodes, p.order, p.cursor, by); to != p.cursor {
		p.cursor = to
		return nil
	}
	if p.groupBy == "" {
		return nil
	}
	neighbour, ok := crossGroup(p.nodes, p.order, p.cursor, by)
	if !ok {
		return nil
	}

	row := &p.rows[p.grabbed]
	if refusal := crossRefusal(p.repo, row.typeKey, p.groupBy); refusal != "" {
		p.status = refusal
		return bell()
	}

	group := p.nodes[neighbour].group
	p.nodes[p.grabbed].group = group
	p.crossed = &crossing{group: group, value: groupValue(p.rows[neighbour].fields[p.groupBy], group)}
	p.cursor = crossGhost(p.nodes, p.order, p.cursor, by)
	p.status = ""
	return nil
}

// drop writes where the grabbed row landed: the rank, one key strictly
// between its new neighbours', and the `group_by` field when the row
// crossed into another group — both keys in one call, which is one commit.
//
// The siblings drawn above it that have no rank are given one first, each
// its own commit, so that the drop reads as it was drawn (rank.go).
func (p *listPage) drop() tea.Cmd {
	row := &p.rows[p.grabbed]
	group := p.nodes[p.grabbed].group
	p.grabbed = -1

	above, key, err := scopeFills(p.nodes, p.order, p.cursor)
	if err != nil {
		p.status = err.Error()
		p.reorder()
		return nil
	}

	rankKey := p.layer(p.nodes[p.order[p.cursor]].level).rankKey
	fields := map[string]issue.Value{rankKey: issue.StringValue(key)}
	said := rankKey + " set"
	if p.crossed != nil && group != p.grabGroup {
		fields[p.groupBy] = p.crossed.value
		said = "moved to " + p.crossed.group
	}
	p.crossed = nil

	if err := writeFills(p.repo, above, rankKey); err != nil {
		p.status = err.Error()
		p.reorder()
		return bell()
	}
	p.writeFields(row.id, fields, saidAnd(said, above))
	p.putCursorOn(row.id)
	return nil
}

func blinkTick() tea.Cmd {
	return tea.Tick(blinkInterval, func(_ time.Time) tea.Msg { return blinkMsg{} })
}

const blinkInterval = 400 * time.Millisecond

// lessByRank is the one order rule a rank imposes, everywhere one orders
// anything: (rank, id), the issues without a rank last.
//
// Never rank alone: the tie-break by id is what makes two concurrent drags
// into the same gap both survive (441dcbb). Two issues with no rank at all
// are equal here rather than ordered by id, so that they keep the query's
// own order — which is the whole order of a store nobody has dragged
// anything in yet, `rank` being null until a drop writes one (D8).
func lessByRank(rankI, idI, rankJ, idJ string) bool {
	if (rankI == "") != (rankJ == "") {
		return rankJ == ""
	}
	if rankI == "" {
		return false
	}
	if rankI != rankJ {
		return rankI < rankJ
	}
	return idI < idJ
}
