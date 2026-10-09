package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// Show's side tables: `expand` on show, the list's spec or a list of it, one
// flat table per element beside the fields, under them in a window too
// narrow for both (doc/design/show-side-table.md, approved 2026-10-08, Luis).
//
// A table is the issues one relation reaches from the shown issue — a stored
// relation field of its own, or the inverse the schema declares, read as
// `expand` reads it everywhere — narrowed by the layer's query and ordered
// by (rank, id). The tables are one stop between the fields and the box,
// reached by tab and shift-tab only, because left and right stay the tab
// keys on every stop. A row is one issue and the cursor is a row: enter
// opens it, space grabs it to move it within its table, copy copies its id,
// and `/` narrows the rows. A table over an inverse ends in a ghost, which
// creates an issue already pointing at this one.

// sideCap is how many lines the side column holds at least before it
// scrolls within itself: it is the taller of the fields table and this.
const sideCap = 12

// sideTitleMin is the narrowest title a table drawn beside the fields may
// have; below it the tables go under the fields.
const sideTitleMin = 20

// sideValueMax is the widest a field's value is drawn beside a side table.
const sideValueMax = 36

// sideFieldMax is the widest a side table's field column is drawn.
const sideFieldMax = 24

// sideTable is one element of show's `expand`, resolved for the shown issue.
type sideTable struct {
	layer   *view.Layer
	program *jq.Program
	// ghost is the draft the table's ghost opens, nil where there is none:
	// a table over a stored relation of the shown issue, or an inverse whose
	// types disagree on the stored key (S6)
	ghost *host.IssueDocument
	// rows are every issue the relation reaches, the query applied, in
	// (rank, id) order; the filter narrows what is drawn, never this
	rows []sideRow
}

// sideRow is one issue in a side table.
type sideRow struct {
	id   string
	rank string
	// cells are the short id, the title, and the layer's fields, drawn
	cells []string
	// text is what the filter matches, lowered
	text string
}

// sideItem is one place the cursor stands in the side column: a row, the
// ghost, or a table's `(none)` where it has neither.
type sideItem struct {
	table int
	// row is the index in the table's rows, -1 on the ghost or `(none)`
	row   int
	ghost bool
}

// sideGrab is a row picked up to move it within its table.
type sideGrab struct {
	table int
	id    string
}

// newShowView is the show page a call describes, `expand` included: the
// entry point from the command and from a flow, and from every Enter that
// opens an issue by a view's `show` (show_map.go).
func newShowView(repo *cache.RepoCache, call *view.Call) (*showPage, error) {
	p, err := newShowPage(repo, call.String("id"), call.Strings("fields"))
	if err != nil {
		return nil, err
	}
	// the map the pages this one opens are opened by, never this page's
	// (doc/design/show-from-a-view.md, V3)
	if call.Has("show") {
		p.call.Args["show"] = call.Raw("show")
	}
	layers := call.SideTables()
	if len(layers) == 0 {
		return p, nil
	}

	s, err := repo.LoadSchema()
	if err != nil {
		return nil, err
	}
	shownType, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	for _, layer := range layers {
		table := &sideTable{layer: layer}
		if layer.Query != "" {
			program, err := jq.Compile(layer.Query)
			if err != nil {
				return nil, fmt.Errorf("expand: %w", err)
			}
			table.program = program
		}
		table.ghost = sideGhost(s, shownType, p.id, layer.Relation)
		p.tables = append(p.tables, table)
	}
	p.call.Args["expand"] = call.Raw("expand")
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// sideGhost is the draft a table's ghost opens (S6): the stored field on the
// child that resolves the inverse, set to this issue's id, and the type when
// that field belongs to one type. A stored relation of the shown issue has
// no ghost, because the new issue would have to be written into this one, a
// second commit; nor has an inverse whose types disagree on the key, since
// nothing could be filled without the type.
func sideGhost(s *schema.Schema, shownType, id, relation string) *host.IssueDocument {
	sources := view.InverseSources(s, shownType, relation)
	if len(sources) == 0 {
		return nil
	}
	var key string
	var multi bool
	types := make([]string, 0, len(sources))
	for typeKey, keys := range sources {
		types = append(types, typeKey)
		for _, k := range keys {
			field, _ := s.Field(typeKey, k)
			switch {
			case key == "":
				key, multi = k, field.Kind.IsMulti()
			case key != k || multi != field.Kind.IsMulti():
				return nil
			}
		}
	}

	doc := host.IssueDocument{Fields: map[string]issue.Value{}}
	if multi {
		doc.Fields[key] = issue.MustValue([]string{id})
	} else {
		doc.Fields[key] = issue.StringValue(id)
	}
	if len(types) == 1 {
		doc.Fields[schema.TypeKey] = issue.StringValue(types[0])
	}
	return &doc
}

// loadSide reads every table's rows off the store: the issues its relation
// reaches from this one, its query applied, in (rank, id) order. A query
// that fails on the data is said in the status line and leaves its table
// unnarrowed, as a layer's does.
func (p *showPage) loadSide() {
	if len(p.tables) == 0 {
		return
	}
	known := newKinds(p.repo)
	type issueSet struct {
		all   map[string]map[string]any
		order []string
	}
	sets := map[bool]*issueSet{}
	for _, table := range p.tables {
		includeArchive := table.layer.IncludeArchive != nil && *table.layer.IncludeArchive
		set, ok := sets[includeArchive]
		if !ok {
			all, order := allIssues(p.repo, includeArchive)
			set = &issueSet{all: all, order: order}
			sets[includeArchive] = set
		}

		seen := map[string]bool{}
		var candidates []map[string]any
		for _, id := range childrenIndex(p.repo, set.all, set.order, table.layer.Relation)[p.id] {
			if set.all[id] != nil && !seen[id] {
				seen[id] = true
				candidates = append(candidates, set.all[id])
			}
		}
		if table.program != nil && len(candidates) > 0 {
			picked, err := runLayerQuery(table.program, candidates)
			if err != nil {
				p.status = err.Error()
			} else {
				candidates = picked
			}
		}

		rows := make([]sideRow, 0, len(candidates))
		for _, item := range candidates {
			id := host.RowId(item)
			if id == "" {
				continue
			}
			fields, _ := item["fields"].(map[string]any)
			typeKey := host.StringOr(fields[schema.TypeKey], "")
			cells := []string{host.StringOr(item["human_id"], humanOf(p.repo, id)), host.StringOr(fields[schema.TitleKey], "")}
			for _, key := range table.layer.Fields {
				value := fields[key]
				cells = append(cells, known.cellText(typeKey, key, value))
			}
			rows = append(rows, sideRow{
				id:    id,
				rank:  host.StringOr(fields[schema.RankKey], ""),
				cells: cells,
				text:  strings.ToLower(id + " " + strings.Join(cells, " ")),
			})
		}
		sort.SliceStable(rows, func(i, j int) bool {
			return lessByRank(rows[i].rank, rows[i].id, rows[j].rank, rows[j].id)
		})
		table.rows = rows
	}
}

// visible is the rows of a table the filter keeps, as indexes.
func (p *showPage) visible(table *sideTable) []int {
	out := make([]int, 0, len(table.rows))
	needle := strings.ToLower(p.filter)
	for at, row := range table.rows {
		if needle == "" || strings.Contains(row.text, needle) {
			out = append(out, at)
		}
	}
	return out
}

// sideItems is every place the cursor stands in the side column, top to
// bottom: each table's rows, then its ghost, or `(none)` where it has
// neither. The filter hides the ghosts with the rows it narrows away.
func (p *showPage) sideItems() []sideItem {
	var out []sideItem
	for t, table := range p.tables {
		rows := p.visible(table)
		for _, at := range rows {
			out = append(out, sideItem{table: t, row: at})
		}
		switch {
		case table.ghost != nil && p.filter == "":
			out = append(out, sideItem{table: t, row: -1, ghost: true})
		case len(rows) == 0:
			out = append(out, sideItem{table: t, row: -1})
		}
	}
	return out
}

// sideCurrent is the item under the side cursor, clamped, or nil where there
// are no tables.
func (p *showPage) sideCurrent() *sideItem {
	items := p.sideItems()
	if len(items) == 0 {
		return nil
	}
	p.side = min(max(p.side, 0), len(items)-1)
	return &items[p.side]
}

// sideRowAt is the row an item stands for, nil on a ghost or `(none)`.
func (p *showPage) sideRowAt(item *sideItem) *sideRow {
	if item == nil || item.row < 0 {
		return nil
	}
	return &p.tables[item.table].rows[item.row]
}

// putSideOn keeps the side cursor on an issue across a reload; where the
// issue left, the cursor keeps its place in the column.
func (p *showPage) putSideOn(id string) bool {
	for at, item := range p.sideItems() {
		if row := p.sideRowAt(&item); row != nil && row.id == id {
			p.side = at
			return true
		}
	}
	return false
}

// sideAct is enter in the side column: the row's issue opens as show, the
// ghost opens the creator, and `(none)` rings.
func (p *showPage) sideAct() tea.Cmd {
	item := p.sideCurrent()
	switch {
	case item == nil:
		return bell()
	case item.ghost:
		return openNew(p.repo, p.call.Raw("show"), cloneDoc(*p.tables[item.table].ghost))
	case item.row < 0:
		p.status = "no issue"
		return bell()
	}
	return p.follow(p.sideRowAt(item).id)
}

// cloneDoc copies a draft, so that the creator's edits stay the creator's.
func cloneDoc(doc host.IssueDocument) host.IssueDocument {
	fields := make(map[string]issue.Value, len(doc.Fields))
	for key, value := range doc.Fields {
		fields[key] = value
	}
	return host.IssueDocument{Fields: fields}
}

// sideCopy copies the row's id, whichever copy key: a row is one issue, and
// its id is what a command takes, copied as it is shown (alias-ids.md A7).
func (p *showPage) sideCopy() tea.Cmd {
	row := p.sideRowAt(p.sideCurrent())
	if row == nil {
		return bell()
	}
	p.status = "copied " + row.cells[0]
	return setClipboard(copyOf(row.id, row.cells[0]))
}

// startSideGrab is space on a row: it is picked up to move within its table.
// A filtered table is not the order the drop would write, so it rings.
func (p *showPage) startSideGrab() tea.Cmd {
	item := p.sideCurrent()
	row := p.sideRowAt(item)
	if row == nil {
		return bell()
	}
	if p.filter != "" {
		p.status = "clear the filter to move a row"
		return bell()
	}
	p.grab = &sideGrab{table: item.table, id: row.id}
	p.status = ""
	return nil
}

// sideGrabKey is a key while a row is grabbed: up and down carry it within
// its table and stop at its edges, space or enter drops it, esc puts it back,
// and left and right ring, because which relation reaches a row is not an
// order.
func (p *showPage) sideGrabKey(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		id := p.grab.id
		p.grab = nil
		if err := p.load(); err != nil {
			p.status = err.Error()
			return p, nil
		}
		p.putSideOn(id)
		p.status = "put back"
		return p, nil
	case keys.up.matches(press):
		p.carry(-1)
	case keys.down.matches(press):
		p.carry(1)
	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.sideDrop()
	case keys.left.matches(press), keys.right.matches(press):
		p.status = "stays in its table"
		return p, bell()
	}
	return p, nil
}

// carry moves the grabbed row one place in its table, on the screen only:
// the rank is computed once, at the drop.
func (p *showPage) carry(by int) {
	table := p.tables[p.grab.table]
	at := slices.IndexFunc(table.rows, func(row sideRow) bool { return row.id == p.grab.id })
	to := at + by
	if at < 0 || to < 0 || to >= len(table.rows) {
		return
	}
	table.rows[at], table.rows[to] = table.rows[to], table.rows[at]
	p.putSideOn(p.grab.id)
}

// sideDrop writes where the grabbed row landed: the unranked rows drawn
// above it in its table are ranked first, one commit each, and then the row
// itself, between its neighbours (rank.go).
func (p *showPage) sideDrop() tea.Cmd {
	grab := p.grab
	p.grab = nil
	table := p.tables[grab.table]
	ids := make([]string, len(table.rows))
	ranks := make([]string, len(table.rows))
	at := -1
	for i, row := range table.rows {
		ids[i], ranks[i] = row.id, row.rank
		if row.id == grab.id {
			at = i
		}
	}
	above, key, err := fillRanks(ids, ranks, at)
	if err == nil {
		err = writeFills(p.repo, above)
	}
	if err == nil {
		_, err = host.IssueSet(p.repo, grab.id, map[string]issue.Value{schema.RankKey: issue.StringValue(key)}, false)
	}
	if err != nil {
		p.status = err.Error()
	} else {
		p.status = saidAnd("rank set", above)
	}
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	p.putSideOn(grab.id)
	return nil
}

// startFilter opens `/` over the side tables.
func (p *showPage) startFilter() {
	input := textinput.New()
	input.SetValue(p.filter)
	input.SetWidth(max(p.width-10, 1))
	input.Focus()
	p.filtering = &input
}

// filterKey is a key while `/` is open: enter keeps the narrowing, esc
// clears it, and anything else is typed into it.
func (p *showPage) filterKey(msg tea.Msg) (page, tea.Cmd) {
	was := p.sideRowAt(p.sideCurrent())
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			p.filtering = nil
			p.filter = ""
			p.keepSide(was)
			return p, nil
		case keys.act.matches(press):
			p.filtering = nil
			return p, nil
		}
	}
	updated, cmd := p.filtering.Update(msg)
	p.filtering = &updated
	p.filter = updated.Value()
	p.keepSide(was)
	return p, cmd
}

// keepSide puts the cursor back on the row it was on, where the filter
// still draws it, and on the first item where it does not.
func (p *showPage) keepSide(was *sideRow) {
	if was != nil && p.putSideOn(was.id) {
		return
	}
	p.side = 0
}

// sideLine is one line of the side column, unwindowed.
type sideLine struct {
	text string
	// item is the cursor stop the line draws, -1 where it is none
	item int
	// table is the table the line belongs to, and heading says it is that
	// table's heading; its header is the line after
	table   int
	heading bool
}

// sideWidths is a table's columns, measured over every row, so a refresh or
// a filter moves nothing that did not change: the id, the title at its
// natural width, and each field's, capped.
func sideWidths(table *sideTable) []int {
	widths := []int{idWidth, ansi.StringWidth("title")}
	for _, key := range table.layer.Fields {
		widths = append(widths, min(ansi.StringWidth(key), sideFieldMax))
	}
	for _, row := range table.rows {
		for c, cell := range row.cells {
			limit := sideFieldMax
			// neither the title nor the id is capped: a cut key is another
			// issue's (alias-ids.md A3)
			if c <= 1 {
				limit = 1 << 20
			}
			widths[c] = max(widths[c], min(ansi.StringWidth(cell), limit))
		}
	}
	return widths
}

// sideNeeds is the narrowest the side column can be with every table's title
// at least sideTitleMin wide, or its natural width where that is narrower.
func (p *showPage) sideNeeds() int {
	need := 0
	for _, table := range p.tables {
		widths := sideWidths(table)
		width := 1 + widths[0] + 2 + min(widths[1], sideTitleMin)
		for _, w := range widths[2:] {
			width += 2 + w
		}
		need = max(need, width)
	}
	return need
}

// sideLines is the whole side column at a width, unwindowed, and the line
// of each item.
func (p *showPage) sideLines(width int, here position) []sideLine {
	items := p.sideItems()
	itemAt := map[[2]int]int{}
	ghostAt := map[int]int{}
	noneAt := map[int]int{}
	for at, item := range items {
		switch {
		case item.ghost:
			ghostAt[item.table] = at
		case item.row < 0:
			noneAt[item.table] = at
		default:
			itemAt[[2]int{item.table, item.row}] = at
		}
	}
	on := here.stop == stopSide

	var out []sideLine
	for t, table := range p.tables {
		if t > 0 {
			out = append(out, sideLine{item: -1, table: t})
		}
		widths := sideWidths(table)
		fixed := 1 + widths[0]
		for _, w := range widths[2:] {
			fixed += 2 + w
		}
		widths[1] = max(min(widths[1], width-fixed-2), 1)

		join := func(cells []string) string {
			parts := make([]string, len(widths))
			for c := range widths {
				cell := ""
				if c < len(cells) {
					cell = cells[c]
				}
				parts[c] = pad(cell, widths[c])
			}
			return strings.TrimRight(strings.Join(parts, "  "), " ")
		}

		out = append(out, sideLine{text: fit(" "+styleHeader.Render(table.layer.Relation), width), item: -1, table: t, heading: true})
		header := append([]string{"id", "title"}, table.layer.Fields...)
		out = append(out, sideLine{text: fit(" "+styleDim.Render(join(header)), width), item: -1, table: t})

		rows := p.visible(table)
		for _, at := range rows {
			row := table.rows[at]
			item := itemAt[[2]int{t, at}]
			text := join(row.cells)
			marker, style := " ", styleDim.Faint(false)
			drawn := ""
			switch {
			case p.grab != nil && p.grab.id == row.id && p.grab.table == t:
				marker, style = "≡", styleGrab
			case on && item == p.side:
				marker, style = "›", styleCell
			case len(row.cells) > 0 && p.repo.DisplayNamespace() != "" && !isAlias(row.id, row.cells[0]):
				// a hash an issue fell back to is dim (alias-ids.md A3)
				id := pad(row.cells[0], widths[0])
				drawn = styleDim.Render(id) + style.Render(strings.TrimPrefix(text, id))
			}
			if drawn == "" {
				drawn = style.Render(text)
			}
			out = append(out, sideLine{text: fit(marker+drawn, width), item: item, table: t})
		}
		if len(rows) == 0 {
			item, stop := noneAt[t]
			if !stop {
				item = -1
			}
			marker, style := " ", styleDim
			if stop && on && item == p.side {
				marker, style = "›", styleCell
			}
			out = append(out, sideLine{text: fit(marker+style.Render(noGroup), width), item: item, table: t})
		}
		if item, ok := ghostAt[t]; ok {
			marker, style := " ", styleDim
			if on && item == p.side {
				marker, style = "›", styleCell
			}
			text := pad(ghostPrefix, widths[0]) + "  " + ghostLabel
			out = append(out, sideLine{text: fit(marker+style.Render(text), width), item: item, table: t})
		}
	}
	return out
}

// sideWindow is the side column as drawn: every line where they fit in the
// cap, and otherwise a window of the cap that follows the cursor, the
// heading and header of the cursor's table kept on its first lines, and a
// dim last line saying how many rows are out of sight. It says which of its
// lines the cursor is on, or -1.
func (p *showPage) sideWindow(width, limit int, here position) ([]string, int) {
	all := p.sideLines(width, here)
	cursor := -1
	for at, line := range all {
		if line.item >= 0 && line.item == p.side {
			cursor = at
		}
	}

	if len(all) <= limit {
		out := make([]string, len(all))
		for at, line := range all {
			out[at] = line.text
		}
		if here.stop != stopSide {
			cursor = -1
		}
		return out, cursor
	}

	room := max(limit-1, 3)
	top := min(p.sideTop, len(all)-room)
	if cursor >= 0 {
		if cursor < top {
			top = cursor
		}
		if cursor > top+room-1 {
			top = cursor - room + 1
		}
	}
	top = min(max(top, 0), len(all)-room)
	heading := -1
	if cursor >= 0 {
		for at := cursor; at >= 0; at-- {
			if all[at].heading {
				heading = at
				break
			}
		}
		// the heading scrolled off: it and its header take the first two
		// lines, so the cursor has to stay under them
		if heading >= 0 && top > heading && cursor < top+2 {
			top = max(cursor-2, heading)
		}
	}
	p.sideTop = top

	out := make([]string, 0, limit)
	for at := top; at < top+room; at++ {
		out = append(out, all[at].text)
	}
	pinned := heading >= 0 && top > heading
	if pinned {
		out[0], out[1] = all[heading].text, all[heading+1].text
	}

	countRows := func(lines []sideLine) int {
		n := 0
		for _, line := range lines {
			if line.item >= 0 {
				n++
			}
		}
		return n
	}
	hiddenAbove := countRows(all[:top])
	if pinned {
		hiddenAbove += countRows(all[top : top+2])
	}
	below := countRows(all[top+room:])
	var said []string
	if hiddenAbove > 0 {
		said = append(said, fmt.Sprintf("↑ %d", hiddenAbove))
	}
	if below > 0 {
		said = append(said, fmt.Sprintf("↓ %d", below))
	}
	out = append(out, fit(" "+styleDim.Render(strings.Join(said, " · ")+" more"), width))

	if here.stop != stopSide || cursor < 0 {
		return out, -1
	}
	return out, cursor - top
}

// sideHints is the line on the side column.
func (p *showPage) sideHints() string {
	item := p.sideCurrent()
	switch {
	case item == nil:
		return hints()
	case item.ghost:
		return hints(hint{"enter", "new issue"})
	case item.row < 0:
		return hints()
	}
	return hints(hint{"enter", "open"}, hint{"space", "grab"})
}
