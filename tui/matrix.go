package tui

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// matrixPage is the `matrix` view: a row per value of one field, a column per
// value of another, and in each cell the sum of a number field over the issues
// that have both — or their count when no number is named.
//
// It knows nothing about allocations: it sums any number over any issue set,
// so `points by work by iteration` over `allocation` issues and
// `estimate by story by sprint` over ordinary tasks are one kind
// (doc/design/allocations.md).
//
// A cell is a sum, and a sum is not a value: nothing here is edited. Enter
// opens the issues that made the number, as an ordinary list whose query
// selects them, and space rings.
type matrixPage struct {
	repo *cache.RepoCache

	call  *view.Call
	query string
	// includeArchive brings the archived back into the input query runs over.
	includeArchive bool
	rowsKey        string
	columnsKey     string
	valueKey       string
	groupBy        string

	// the two axes, resolved on every load: their values in drawing order.
	rowAxis axis
	colAxis axis
	// groups are the blocks of rows, in the order they first appear, the
	// ungrouped last; one nameless group when group_by is not bound.
	groups []group

	// sums is the whole grid: [group][row value][column value].
	sums [][][]cellSum
	// count is how many issues the query returned, for the status line.
	count int

	// drawn is the rows as they are drawn: the data rows of each group, each
	// group's subtotal, and the grand total last. rows and cols are what the
	// filter left of each axis, by index into its values.
	drawn []drawnRow
	rows  []int
	cols  []int

	// the cursor is a cell: a row of drawn, and a column of cols, where
	// len(cols) is the totals column.
	row, col int
	// colOffset is the first data column drawn, top the first body line.
	colOffset, top int

	width, height int

	filter    string
	filtering *textinput.Model
	help      *help

	status string
}

// axis is one of the two axes, resolved for one load.
type axis struct {
	key string
	// kind is the field's kind, off the types the issues on the matrix have.
	kind schema.Kind
	// multi says the field holds a set, so one issue lands on several values.
	multi bool
	// values are the axis's values in drawing order, (none) last when it is
	// drawn at all.
	values []axisValue
}

// axisValue is one row or column: what the store holds, and how it reads.
type axisValue struct {
	// value is the stored value, "" for the issues that have none.
	value string
	label string
	// sort is what the value is ordered by where the axis has no schema
	// order of its own: the label, except for a relation, where the label
	// opens with the id it is drawn behind and the title is what sorts.
	sort string
	// none marks the (none) row or column, which "" alone cannot: a field
	// could hold an empty string.
	none bool
}

// group is one block of rows: the value that heads it, and the JSON of that
// value, which is what the drill-down query compares against.
type group struct {
	label string
	raw   json.RawMessage
	// none marks the block of the issues with no value for the field.
	none bool
}

// cellSum is one cell: how many issues are in it, and what their value field
// adds up to. The count is what a cell draws when no value field is named,
// and what tells an empty cell from one that sums to zero.
type cellSum struct {
	n   int
	sum float64
}

func (c *cellSum) add(number float64, ok bool) {
	c.n++
	if ok {
		c.sum += number
	}
}

func (c cellSum) plus(other cellSum) cellSum {
	return cellSum{n: c.n + other.n, sum: c.sum + other.sum}
}

// rowKind says what a drawn row is: the matrix's data, a group's subtotal, or
// the grand total. It is what the drill-down query leaves out.
type rowKind int

const (
	rowData rowKind = iota
	rowSubtotal
	rowGrand
)

// drawnRow is one line of the matrix body.
type drawnRow struct {
	kind rowKind
	// group indexes groups, -1 on the grand total.
	group int
	// value indexes rowAxis.values, -1 on a total row.
	value int
	label string
	// cells is one sum per drawn column, and total the row's own.
	cells []cellSum
	total cellSum
}

func (p *matrixPage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func newMatrixPage(repo *cache.RepoCache, call *view.Call) (*matrixPage, error) {
	p := &matrixPage{
		repo:           repo,
		call:           call,
		includeArchive: call.Bool("include_archive"),
		query:          call.String("query"),
		rowsKey:        call.String("rows"),
		columnsKey:     call.String("columns"),
		valueKey:       call.String("value"),
		groupBy:        call.String("group_by"),
		width:          80,
		height:         24,
	}
	p.rowAxis = axis{key: p.rowsKey}
	p.colAxis = axis{key: p.columnsKey}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// load re-runs the query and rebuilds the whole grid, keeping the cursor on
// the row and the column it was on — by their values, not their places, so a
// sprint somebody opened in another process does not move it (A11).
func (p *matrixPage) load() error {
	wasRow, wasCol := p.cursorKeys()

	values, err := host.IssueList(p.repo, p.query, p.includeArchive)
	if err != nil {
		return err
	}
	items := host.IssueRows(values)
	p.count = len(items)

	p.build(items)
	p.arrange()
	p.putCursorOn(wasRow, wasCol)
	return nil
}

// build resolves both axes and the groups, then fills the grid.
func (p *matrixPage) build(items []map[string]any) {
	onMatrix := map[string]bool{}
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		onMatrix[host.StringOr(fields[schema.TypeKey], "")] = true
	}

	p.rowAxis = p.resolveAxis(p.rowsKey, p.call.Strings("row_values"), items, onMatrix)
	p.colAxis = p.resolveAxis(p.columnsKey, p.call.Strings("column_values"), items, onMatrix)
	p.groups = p.resolveGroups(items)

	rowAt := placesOf(p.rowAxis)
	colAt := placesOf(p.colAxis)
	groupAt := make(map[string]int, len(p.groups))
	for at, g := range p.groups {
		groupAt[g.label] = at
	}

	p.sums = make([][][]cellSum, len(p.groups))
	for g := range p.sums {
		p.sums[g] = make([][]cellSum, len(p.rowAxis.values))
		for r := range p.sums[g] {
			p.sums[g][r] = make([]cellSum, len(p.colAxis.values))
		}
	}

	known := newKinds(p.repo)
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		if fields == nil {
			fields = map[string]any{}
		}
		typeKey := host.StringOr(fields[schema.TypeKey], "")

		number, hasNumber := asNumber(fields[p.valueKey])
		g := groupAt[p.groupLabel(known, typeKey, fields)]
		// An issue with several values on an axis lands on each of them, so
		// a multi-valued axis double-counts; dividing the number between
		// them would invent data (A5).
		for _, rowValue := range axisValuesOf(fields[p.rowsKey]) {
			r, ok := rowAt[rowValue]
			if !ok {
				continue
			}
			for _, colValue := range axisValuesOf(fields[p.columnsKey]) {
				c, ok := colAt[colValue]
				if !ok {
					continue
				}
				p.sums[g][r][c].add(number, hasNumber)
			}
		}
	}
}

// placesOf indexes an axis by its stored value, so the grid can be filled by
// lookup; (none) is the empty string, which nothing else can be because an
// absent value never reaches here as one.
func placesOf(a axis) map[string]int {
	at := make(map[string]int, len(a.values))
	for index, value := range a.values {
		at[value.value] = index
	}
	return at
}

// axisValuesOf is the values one issue has on an axis: one for a scalar
// field, several for a set, and the empty string — (none) — for nothing.
func axisValuesOf(value any) []string {
	switch v := value.(type) {
	case nil:
		return []string{""}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text := plainValue(item); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return []string{""}
		}
		return out
	default:
		return []string{plainValue(v)}
	}
}

// asNumber reads a value field, which the store holds as JSON: a number is a
// number, and anything else contributes nothing rather than failing the view.
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	}
	return 0, false
}

// resolveAxis is one axis in drawing order (A5): the values the call listed,
// else an enum's schema order, then every value the data has that neither
// placed, by the label it draws, then (none).
func (p *matrixPage) resolveAxis(key string, listed []string, items []map[string]any, onMatrix map[string]bool) axis {
	a := axis{key: key}

	// what the schema says about the field, off the types the issues have:
	// every type owns its own field (e7e58f2), and the first type's order
	// wins, later ones only adding what it did not have.
	names := map[string]string{}
	var order []string
	if s, err := p.repo.LoadSchema(); err == nil {
		for _, typeKey := range s.TypeKeys() {
			if len(onMatrix) > 0 && !onMatrix[typeKey] {
				continue
			}
			field, ok := s.Field(typeKey, key)
			if !ok {
				continue
			}
			if a.kind == "" {
				a.kind = field.Kind
			}
			for _, value := range field.Values {
				if _, named := names[value.Id]; !named {
					names[value.Id] = value.Name
					order = append(order, value.Id)
				}
			}
		}
	}
	if a.kind == "" {
		a.kind, _ = schema.BuiltinKind(key)
	}
	a.multi = isMultiKind(a.kind)

	// what the data has
	data := map[string]bool{}
	none := false
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		for _, value := range axisValuesOf(fields[key]) {
			if value == "" {
				none = true
				continue
			}
			data[value] = true
		}
	}

	placed := map[string]bool{}
	add := func(value string) {
		if value == "" || placed[value] {
			return
		}
		placed[value] = true
		label, key := p.axisNames(a.kind, names, value)
		a.values = append(a.values, axisValue{value: value, label: label, sort: key})
	}

	if listed != nil {
		for _, value := range listed {
			add(value)
		}
	} else if a.kind == schema.KindEnum || a.kind == schema.KindOrdinalEnum || a.kind == schema.KindMultiEnum {
		for _, value := range order {
			add(value)
		}
	}

	rest := make([]axisValue, 0, len(data))
	for value := range data {
		if placed[value] {
			continue
		}
		label, key := p.axisNames(a.kind, names, value)
		rest = append(rest, axisValue{value: value, label: label, sort: key})
	}
	sort.Slice(rest, func(i, j int) bool { return lessByLabel(rest[i], rest[j]) })
	a.values = append(a.values, rest...)

	if none {
		a.values = append(a.values, axisValue{label: noGroup, none: true})
	}
	return a
}

// axisNames is one axis value as it reads, and what it sorts by: a relation
// is the issue it names and sorts by that issue's title, an identity is the
// person, an enum its name, and anything else itself.
func (p *matrixPage) axisNames(kind schema.Kind, names map[string]string, value string) (label, sortKey string) {
	switch {
	case isRelation(kind):
		// the drawn label opens with the id, which is a hash: a relation
		// axis reads in title order, not in hash order
		return linkLabel(p.repo, value), issueTitle(p.repo, value)
	case isPerson(kind):
		label = host.UserName(p.repo, value)
		return label, label
	}
	if name := names[value]; name != "" {
		return name, name
	}
	return value, value
}

// issueTitle is the title of the issue an axis value names, or the value
// itself for an issue this clone has not pulled.
func issueTitle(repo *cache.RepoCache, id string) string {
	excerpt, err := repo.Issues().ResolveExcerpt(entity.Id(id))
	if err != nil {
		return id
	}
	return excerpt.Title()
}

// lessByLabel orders the values an axis takes from the data: by what they
// read as, which is the order a person sees on the screen, with the stored
// value as the tie-break. Two keys that are both numbers compare as numbers,
// so a number axis does not put 10 before 2.
func lessByLabel(left, right axisValue) bool {
	if l, lok := strconv.ParseFloat(left.sort, 64); lok == nil {
		if r, rok := strconv.ParseFloat(right.sort, 64); rok == nil && l != r {
			return l < r
		}
	}
	if left.sort != right.sort {
		return left.sort < right.sort
	}
	return left.value < right.value
}

func isMultiKind(kind schema.Kind) bool {
	switch kind {
	case schema.KindMultiEnum, schema.KindMultiIdentity, schema.KindMultiRelation:
		return true
	}
	return false
}

// resolveGroups is the blocks of rows, in the order they first appear, the
// ungrouped last, as the list and the board order their groups.
func (p *matrixPage) resolveGroups(items []map[string]any) []group {
	if p.groupBy == "" {
		return []group{{}}
	}

	known := newKinds(p.repo)
	var groups []group
	at := map[string]int{}
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		if fields == nil {
			fields = map[string]any{}
		}
		label := p.groupLabel(known, host.StringOr(fields[schema.TypeKey], ""), fields)
		if _, seen := at[label]; seen {
			continue
		}
		at[label] = len(groups)
		raw, err := json.Marshal(fields[p.groupBy])
		if err != nil {
			raw = json.RawMessage("null")
		}
		groups = append(groups, group{label: label, raw: raw, none: label == noGroup})
	}
	if len(groups) == 0 {
		groups = []group{{label: noGroup, raw: json.RawMessage("null"), none: true}}
	}

	// the issues nobody has filed under the grouping field come last
	if index, ok := at[noGroup]; ok && index != len(groups)-1 {
		last := groups[index]
		groups = append(groups[:index], groups[index+1:]...)
		groups = append(groups, last)
	}
	return groups
}

// groupLabel is the block an issue falls in: the drawn value of the grouping
// field, or (none), which is the board's rule.
func (p *matrixPage) groupLabel(known *kinds, typeKey string, fields map[string]any) string {
	if p.groupBy == "" {
		return ""
	}
	if value := known.cellText(typeKey, p.groupBy, fields[p.groupBy]); value != "" {
		return value
	}
	return noGroup
}

// arrange rebuilds what is drawn out of the grid: the filter, the data rows
// of each block, each block's subtotal, and the grand total last.
//
// The filter narrows the axes, not the issues: hiding issues would change
// every sum on the screen while the totals still read as totals (A9). An axis
// in which nothing matches is left whole, so narrowing the rows does not
// empty the columns.
func (p *matrixPage) arrange() {
	needle := strings.ToLower(strings.TrimSpace(p.filter))

	p.cols = keptValues(p.colAxis.values, needle)
	p.rows = keptValues(p.rowAxis.values, needle)

	p.drawn = p.drawn[:0]
	grand := make([]cellSum, len(p.cols))
	for g := range p.groups {
		block := make([]cellSum, len(p.cols))
		for _, r := range p.rows {
			row := drawnRow{kind: rowData, group: g, value: r,
				label: p.rowAxis.values[r].label, cells: make([]cellSum, len(p.cols))}
			for at, c := range p.cols {
				row.cells[at] = p.sums[g][r][c]
				row.total = row.total.plus(row.cells[at])
				block[at] = block[at].plus(row.cells[at])
			}
			p.drawn = append(p.drawn, row)
		}
		if p.groupBy != "" {
			p.drawn = append(p.drawn, p.totalRow(rowSubtotal, g, "total", block))
		}
		for at := range grand {
			grand[at] = grand[at].plus(block[at])
		}
	}
	p.drawn = append(p.drawn, p.totalRow(rowGrand, -1, "total", grand))

	p.clamp()
}

func (p *matrixPage) totalRow(kind rowKind, group int, label string, cells []cellSum) drawnRow {
	row := drawnRow{kind: kind, group: group, value: -1, label: label,
		cells: append([]cellSum(nil), cells...)}
	for _, cell := range cells {
		row.total = row.total.plus(cell)
	}
	return row
}

// keptValues is the values of one axis the filter leaves, by index; an axis
// nothing matches is left whole.
func keptValues(values []axisValue, needle string) []int {
	kept := make([]int, 0, len(values))
	for at, value := range values {
		if needle == "" || strings.Contains(strings.ToLower(value.label), needle) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		kept = kept[:0]
		for at := range values {
			kept = append(kept, at)
		}
	}
	return kept
}

func (p *matrixPage) clamp() {
	p.row = min(max(p.row, 0), max(len(p.drawn)-1, 0))
	// the last column is the totals column, which is a cell like any other
	p.col = min(max(p.col, 0), len(p.cols))
}

// cursorKeys names the cell the cursor is on in terms that survive a reload:
// the row's group and value, and the column's value.
func (p *matrixPage) cursorKeys() (row, col string) {
	if p.row < len(p.drawn) {
		r := p.drawn[p.row]
		switch r.kind {
		case rowGrand:
			row = "grand"
		case rowSubtotal:
			row = "subtotal\x00" + p.groupKey(r.group)
		default:
			row = "row\x00" + p.groupKey(r.group) + "\x00" + p.rowAxis.values[r.value].value
		}
	}
	col = "total"
	if p.col < len(p.cols) {
		col = "col\x00" + p.colAxis.values[p.cols[p.col]].value
	}
	return row, col
}

func (p *matrixPage) groupKey(index int) string {
	if index < 0 || index >= len(p.groups) {
		return ""
	}
	return p.groups[index].label
}

// putCursorOn puts the cursor back on the cell it named, falling back to
// where it was when that row or column is gone.
func (p *matrixPage) putCursorOn(row, col string) {
	if row != "" {
		for at := range p.drawn {
			was := p.row
			p.row = at
			found, _ := p.cursorKeys()
			p.row = was
			if found == row {
				p.row = at
				break
			}
		}
	}
	if col != "" {
		for at := 0; at <= len(p.cols); at++ {
			was := p.col
			p.col = at
			_, found := p.cursorKeys()
			p.col = was
			if found == col {
				p.col = at
				break
			}
		}
	}
	p.clamp()
}

// cell is the sum under the cursor.
func (p *matrixPage) cell() cellSum {
	if p.row >= len(p.drawn) {
		return cellSum{}
	}
	row := p.drawn[p.row]
	if p.col >= len(p.cols) {
		return row.total
	}
	return row.cells[p.col]
}

// cellText is a cell as it draws: blank when no issue is in it, the count
// when no value field is named, and the sum otherwise. A cell with issues
// whose numbers add to zero draws 0, which is the difference a planner is
// looking for (A6).
func (p *matrixPage) cellText(cell cellSum) string {
	if cell.n == 0 {
		return ""
	}
	if p.valueKey == "" {
		return strconv.Itoa(cell.n)
	}
	return strconv.FormatFloat(cell.sum, 'f', -1, 64)
}

func (p *matrixPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case createdMsg:
		if err := p.load(); err != nil {
			p.status = err.Error()
			return p, nil
		}
		p.status = "created " + human(msg.id)
		return p, nil

	case refreshMsg:
		if err := p.load(); err != nil {
			p.status = err.Error()
		}
		return p, nil

	case tea.KeyPressMsg:
		return p.key(msg)

	// a sum has nothing to paste into: a field is edited on show
	case tea.PasteMsg, tea.ClipboardMsg:
		if p.filtering != nil {
			break
		}
		p.status = "nothing to paste into"
		return p, bell()
	}

	if p.filtering != nil {
		return p.updateFilter(msg)
	}
	return p, nil
}

func (p *matrixPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.help != nil:
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
	case p.filtering != nil:
		return p.updateFilter(press)
	}

	switch {
	case keys.quit.matches(press):
		return p, tea.Quit

	case keys.up.matches(press):
		p.row--
	case keys.down.matches(press):
		p.row++
	case keys.pageUp.matches(press):
		p.row -= p.rowsPerPage()
	case keys.pageDn.matches(press):
		p.row += p.rowsPerPage()
	case keys.top.matches(press):
		p.col = 0
	case keys.bottom.matches(press):
		p.col = len(p.cols)

	case keys.left.matches(press):
		p.col--
	case keys.right.matches(press):
		p.col++

	case keys.act.matches(press):
		return p, p.open()

	// a cell is a sum, and a sum is not a value: it is opened, never edited
	case keys.edit.matches(press):
		p.status = "a sum is not editable: enter opens its issues"
		return p, bell()
	case keys.copy.matches(press):
		return p, p.copyCell()
	case keys.copyId.matches(press):
		return p, p.copyCommand()
	case keys.paste.matches(press):
		p.status = "nothing to paste into"
		return p, bell()
	case keys.filter.matches(press):
		p.startFilter()
	case keys.help.matches(press):
		p.help = &help{}

	case keys.back.matches(press):
		if p.filter != "" {
			p.filter = ""
			p.arrange()
			p.status = ""
			return p, nil
		}
		return p, func() tea.Msg { return popMsg{} }
	}

	p.clamp()
	return p, nil
}

// rowsPerPage is what a page key moves by: the rows that fit, at least one.
func (p *matrixPage) rowsPerPage() int {
	return max(p.height-5, 1)
}

// open is enter: the issues summed into the cell, as an ordinary list whose
// query selects them (A9). On a totals row or the totals column that is the
// whole row, the whole column or the lot, because a total is the cell with
// one clause of its query dropped.
func (p *matrixPage) open() tea.Cmd {
	call, err := p.cellCall()
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	shown, err := newListPage(p.repo, call)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

// cellCall is the list call the cell drills down into: this matrix's own
// query, piped into a select per bound axis.
//
// The predicate, never the ids that happen to be in the cell today: the list
// it opens is a live view like every other, and it has to re-run to the same
// cell after somebody writes.
func (p *matrixPage) cellCall() (*view.Call, error) {
	if p.row >= len(p.drawn) {
		return nil, errNothingHere
	}
	row := p.drawn[p.row]

	query := p.query
	if row.kind == rowData {
		query += "\n| " + axisClause(p.rowAxis, p.rowAxis.values[row.value])
	}
	if p.col < len(p.cols) {
		query += "\n| " + axisClause(p.colAxis, p.colAxis.values[p.cols[p.col]])
	}
	if p.groupBy != "" && row.group >= 0 && row.kind != rowGrand {
		query += "\n| map(select(" + fieldPath(p.groupBy) + " == " + string(p.groups[row.group].raw) + "))"
	}

	fields := make([]string, 0, 4)
	for _, key := range []string{schema.TitleKey, p.rowsKey, p.columnsKey, p.valueKey} {
		if key != "" && !contains(fields, key) {
			fields = append(fields, key)
		}
	}

	kwargs := map[string]json.RawMessage{
		"query":  mustJSON(query),
		"fields": mustJSON(fields),
	}
	// the same input, or the list would not be the issues the cell summed
	if p.includeArchive {
		kwargs["include_archive"] = mustJSON(true)
	}
	return view.Parse(view.KindList, kwargs)
}

// axisClause selects the issues on one row or column: an equality for a
// scalar field, a membership for a set, and emptiness for (none).
func axisClause(a axis, value axisValue) string {
	path := fieldPath(a.key)
	switch {
	case value.none && a.multi:
		return "map(select((" + path + " // []) | length == 0))"
	case value.none:
		return "map(select(" + path + " == null))"
	case a.multi:
		return "map(select((" + path + " // []) | index(" + string(mustJSON(value.value)) + ")))"
	default:
		return "map(select(" + path + " == " + string(mustJSON(value.value)) + "))"
	}
}

// fieldPath names a field in a jq program by index, so that a key with a
// hyphen or a dot in it reads the field it names and not a syntax error.
func fieldPath(key string) string {
	return ".fields[" + string(mustJSON(key)) + "]"
}

func contains(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

// errNothingHere is what a key that acts on a cell answers on an empty
// matrix, where there is no cell at all.
var errNothingHere = errors.New("nothing here")

// copyCell puts the number under the cursor on the clipboard.
func (p *matrixPage) copyCell() tea.Cmd {
	text := p.cellText(p.cell())
	if text == "" {
		p.status = "cell empty"
		return bell()
	}
	p.status = "copied " + text
	return setClipboard(text)
}

// copyCommand is copy-id on a matrix: a cell has no id, and what identifies
// it is the command that lists exactly its issues (A9).
func (p *matrixPage) copyCommand() tea.Cmd {
	call, err := p.cellCall()
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	p.status = "copied the cell's command"
	return setClipboard(command(call))
}

func (p *matrixPage) startFilter() {
	input := textinput.New()
	input.SetValue(p.filter)
	input.SetWidth(p.width - 10)
	input.Focus()
	p.filtering = &input
}

func (p *matrixPage) updateFilter(msg tea.Msg) (page, tea.Cmd) {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			p.filtering = nil
			p.filter = ""
			p.arrange()
			return p, nil
		case keys.act.matches(press):
			p.filtering = nil
			p.arrange()
			return p, nil
		}
	}

	updated, cmd := p.filtering.Update(msg)
	p.filtering = &updated
	p.filter = updated.Value()
	p.arrange()
	return p, cmd
}
