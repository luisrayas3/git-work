package tui

import (
	"fmt"
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

// ganttPage is the `gantt` view: a row per issue, a bar from its `start` to
// its `stop` on a chart of periods, one column per day, week, month or
// quarter (`scale`).
//
// The cursor is a cell: a row and a period. Enter opens the row's issue in
// show, as on a board, and the only edits a gantt makes are moves: grabbing
// a bar and moving it sideways shifts its dates by one period a step — on
// the bar's first cell only the start, on its last only the stop, anywhere
// between them both — and moving it up or down changes its rank. With
// `expand` bound the rows are a tree, rows under rows, and a parent with
// no dates of its own draws the envelope of its children's
// (doc/design/terminal-renderer.md, Gantt and Nesting).
type ganttPage struct {
	repo *cache.RepoCache

	call        *view.Call
	query       string
	startKey    string
	stopKey     string
	labelKey    string
	scale       string
	from, to    string
	progressKey string
	groupBy     string
	expandKey   string
	depth       int
	rankKey     string

	items  []map[string]any
	folded map[string]bool

	bars  []bar
	nodes []treeRow
	order []int

	// periods is the chart, the first day of each period in order; the
	// cursor is the row at order[cursor] and the period at col.
	periods []time.Time
	cursor  int
	col     int
	// colOffset is the first period drawn, top the first body line drawn.
	colOffset, top int

	width, height int

	filter    string
	filtering *textinput.Model
	help      *help

	// grabbed is the bar being moved, by index into bars, or -1; grabFrom
	// is where it was picked up, so that a drop back there writes nothing.
	grabbed  int
	grabFrom struct{ cursor, col int }
	blink    bool

	status string
}

// bar is one issue on the chart.
type bar struct {
	id      string
	human   string
	typeKey string
	fields  map[string]any

	label string
	// start and stop are the dates as stored, at midnight of their own
	// day, and the text they were stored as, which is what a shifted date
	// is written back in the shape of; a bar missing one is a milestone.
	start, stop         time.Time
	hasStart, hasStop   bool
	startText, stopText string
	// dStart and dStop are the periods a grabbed bar's dates have been
	// moved by, on the screen only, until it is dropped.
	dStart, dStop int

	progress    float64
	hasProgress bool
}

// today is the day the chart marks as now, and the chart's extent when
// nothing on it has a date; a variable so that a test can fix it.
var today = func() time.Time {
	return time.Now()
}

func (p *ganttPage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func newGanttPage(repo *cache.RepoCache, call *view.Call) (*ganttPage, error) {
	p := &ganttPage{
		repo:        repo,
		call:        call,
		query:       call.String("query"),
		startKey:    call.String("start"),
		stopKey:     call.String("stop"),
		labelKey:    call.String("label"),
		scale:       call.String("scale"),
		from:        call.String("from"),
		to:          call.String("to"),
		progressKey: call.String("progress"),
		groupBy:     call.String("group_by"),
		expandKey:   call.String("expand"),
		depth:       nestDepth(call),
		rankKey:     call.String("rank"),
		folded:      map[string]bool{},
		width:       80,
		height:      24,
		col:         -1,
		grabbed:     -1,
	}
	if p.labelKey == "" {
		p.labelKey = schema.TitleKey
	}
	if p.scale == "" {
		p.scale = "week"
	}
	for _, edge := range []struct{ name, text string }{{"from", p.from}, {"to", p.to}} {
		if _, ok := parseDate(edge.text); edge.text != "" && !ok {
			return nil, fmt.Errorf("%s: %q is not a date; write 2026-09-23 or an RFC 3339 time", edge.name, edge.text)
		}
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// load re-runs the query and rebuilds the chart, keeping the cursor on the
// issue it was on and on the same period, wherever both are now.
func (p *ganttPage) load() error {
	values, err := host.IssueList(p.repo, p.query)
	if err != nil {
		return err
	}
	p.items, _ = host.IssueItems(values)
	p.rebuild()
	return nil
}

// rebuild makes the rows out of the last query's items: the tree along
// `expand`, a bar per node, the drawing order, and the chart's extent.
func (p *ganttPage) rebuild() {
	was := p.currentId()
	at, hadCol := p.colDate()

	known := newKinds(p.repo)
	tree := nest(p.repo, p.items, p.expandKey, p.depth, p.folded)
	p.bars = make([]bar, 0, len(tree))
	p.nodes = make([]treeRow, 0, len(tree))
	for _, n := range tree {
		b, node := p.newBar(n, known)
		p.bars = append(p.bars, b)
		p.nodes = append(p.nodes, node)
	}

	p.reorder()
	p.putCursorOn(was)
	p.layoutPeriods()
	switch {
	case hadCol:
		p.col = p.index(at)
	case p.cursor < len(p.order):
		// the cursor opens on the first row's first cell: where its bar
		// starts, which is where a grab would move its start
		if first, _, _, ok := p.span(p.order[p.cursor]); ok {
			p.col = first
		}
	}
	p.clampCol()
}

func (p *ganttPage) newBar(n nested, known *kinds) (bar, treeRow) {
	fields, _ := n.item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	b := bar{
		id:      n.id,
		human:   host.StringOr(n.item["human_id"], ""),
		typeKey: host.StringOr(fields[schema.TypeKey], ""),
		fields:  fields,
	}
	if b.human == "" && len(b.id) > idWidth {
		b.human = b.id[:idWidth]
	}
	if isRelation(known.of(b.typeKey, p.labelKey)) {
		b.label = linkText(p.repo, linkIds(fields[p.labelKey]))
	} else {
		b.label = known.cellText(b.typeKey, p.labelKey, fields[p.labelKey])
	}
	b.startText = plainValue(fields[p.startKey])
	b.start, b.hasStart = parseDate(b.startText)
	b.stopText = plainValue(fields[p.stopKey])
	b.stop, b.hasStop = parseDate(b.stopText)
	if p.progressKey != "" {
		b.progress, b.hasProgress = fields[p.progressKey].(float64)
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
		value := known.cellText(b.typeKey, p.groupBy, fields[p.groupBy])
		if isRelation(known.of(b.typeKey, p.groupBy)) {
			value = linkText(p.repo, linkIds(fields[p.groupBy]))
		}
		if value != "" {
			node.group = value
		}
	}
	if p.rankKey != "" {
		node.rank = plainValue(fields[p.rankKey])
	}
	node.text = strings.ToLower(b.human + " " + b.label)

	return b, node
}

func (p *ganttPage) reorder() {
	p.order = treeOrder(p.nodes, p.filter, p.rankKey != "")
	p.clamp()
}

func (p *ganttPage) clamp() {
	p.cursor = min(max(p.cursor, 0), max(len(p.order)-1, 0))
}

func (p *ganttPage) clampCol() {
	p.col = min(max(p.col, 0), max(len(p.periods)-1, 0))
}

func (p *ganttPage) current() *bar {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return nil
	}
	return &p.bars[p.order[p.cursor]]
}

func (p *ganttPage) node() *treeRow {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return nil
	}
	return &p.nodes[p.order[p.cursor]]
}

func (p *ganttPage) currentId() string {
	if b := p.current(); b != nil {
		return b.id
	}
	return ""
}

// colDate is the day the cursor's period starts on, when there is one.
func (p *ganttPage) colDate() (time.Time, bool) {
	if p.col < 0 || p.col >= len(p.periods) {
		return time.Time{}, false
	}
	return p.periods[p.col], true
}

// putCursorOn keeps the cursor on the issue it was on across a refresh,
// falling back to the same position when that issue is gone.
func (p *ganttPage) putCursorOn(id string) {
	if id != "" {
		for at, index := range p.order {
			if p.bars[index].id == id {
				p.cursor = at
				return
			}
		}
	}
	p.clamp()
}

// The chart's periods.

// parseDate reads a stored date, as the schema allows it: a day, or an RFC
// 3339 time, of which the day is what the chart draws.
func parseDate(text string) (time.Time, bool) {
	if text == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.DateOnly, text); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

// shiftText moves a stored date by n periods and writes it back in the
// shape it was stored in: a day stays a day, a time keeps its clock.
func shiftText(text, scale string, n int) string {
	if t, err := time.Parse(time.DateOnly, text); err == nil {
		return shift(t, scale, n).Format(time.DateOnly)
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return shift(t, scale, n).Format(time.RFC3339)
	}
	return text
}

// shift is a date n periods on, from the date itself and not from the
// period it is in, so that a bar dragged and dragged back lands where it was.
func shift(t time.Time, scale string, n int) time.Time {
	switch scale {
	case "day":
		return t.AddDate(0, 0, n)
	case "month":
		return t.AddDate(0, n, 0)
	case "quarter":
		return t.AddDate(0, 3*n, 0)
	default:
		return t.AddDate(0, 0, 7*n)
	}
}

// periodStart is the first day of the period a date is in: the day, the
// Monday, the first of the month, the first of the quarter.
func periodStart(t time.Time, scale string) time.Time {
	y, m, d := t.Date()
	switch scale {
	case "day":
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	case "quarter":
		return time.Date(y, (m-1)/3*3+1, 1, 0, 0, 0, 0, time.UTC)
	default:
		day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	}
}

// periodWidth is how many cells one period is: room for a day of the
// month, or a month's or a quarter's name.
func periodWidth(scale string) int {
	if scale == "day" {
		return 3
	}
	return 4
}

// periodLabel is a period's own label, the fine line of the header; coarse
// is the label over it, said where it changes — the month over days and
// weeks, the year over months and quarters — and short is the coarse label
// without the year, for a change within one.
func periodLabel(t time.Time, scale string) (fine, coarse, short string) {
	switch scale {
	case "day", "week":
		return fmt.Sprintf("%d", t.Day()), t.Format("Jan 2006"), t.Format("Jan")
	case "month":
		return t.Format("Jan"), t.Format("2006"), t.Format("2006")
	default:
		return fmt.Sprintf("Q%d", (int(t.Month())-1)/3+1), t.Format("2006"), t.Format("2006")
	}
}

// dates is a bar's dates as they are on the screen: the stored ones, moved
// by however many periods a grab has dragged them.
func (p *ganttPage) dates(b *bar) (start, stop time.Time) {
	return shift(b.start, p.scale, b.dStart), shift(b.stop, p.scale, b.dStop)
}

// extent is the first and last day a bar covers: its own dates, or, for a
// parent with none of its own, the envelope of everything under it; and
// whether it has any.
func (p *ganttPage) extent(index int) (first, last time.Time, own, ok bool) {
	b := &p.bars[index]
	if b.hasStart || b.hasStop {
		start, stop := p.dates(b)
		switch {
		case !b.hasStop:
			return start, start, true, true
		case !b.hasStart:
			return stop, stop, true, true
		}
		return start, stop, true, true
	}

	level := p.nodes[index].level
	for at := index + 1; at < len(p.bars) && p.nodes[at].level > level; at++ {
		if p.bars[at].hasStart || p.bars[at].hasStop {
			s, e, _, _ := p.extent(at)
			if !ok || s.Before(first) {
				first = s
			}
			if !ok || e.After(last) {
				last = e
			}
			ok = true
		}
	}
	return first, last, false, ok
}

// layoutPeriods sets the chart's periods: from `from` to `to` when given,
// else over the extent of the dates on the chart, and today's period when
// nothing on it has a date.
func (p *ganttPage) layoutPeriods() {
	var first, last time.Time
	found := false
	for index := range p.bars {
		s, e, _, ok := p.extent(index)
		if !ok {
			continue
		}
		if !found || s.Before(first) {
			first = s
		}
		if !found || e.After(last) {
			last = e
		}
		found = true
	}
	if !found {
		first, last = today(), today()
	}
	if t, ok := parseDate(p.from); ok {
		first = t
	}
	if t, ok := parseDate(p.to); ok {
		last = t
	}

	first, last = periodStart(first, p.scale), periodStart(last, p.scale)
	p.periods = p.periods[:0]
	for t := first; !t.After(last); t = shift(t, p.scale, 1) {
		p.periods = append(p.periods, t)
		if len(p.periods) > 10000 {
			break
		}
	}
	if len(p.periods) == 0 {
		p.periods = append(p.periods, first)
	}
}

// index is the period a day is in: -1 before the chart, len(periods) after.
func (p *ganttPage) index(t time.Time) int {
	t = periodStart(t, p.scale)
	if len(p.periods) == 0 || t.Before(p.periods[0]) {
		return -1
	}
	return sort.Search(len(p.periods), func(i int) bool { return p.periods[i].After(t) }) - 1
}

// span is the periods a bar covers, first to last inclusive, or ok false.
func (p *ganttPage) span(index int) (first, last int, own, ok bool) {
	s, e, own, ok := p.extent(index)
	if !ok {
		return 0, 0, own, false
	}
	first, last = p.index(s), p.index(e)
	if first > last {
		first, last = last, first
	}
	return first, last, own, true
}

func (p *ganttPage) count() int {
	return len(p.order)
}

func (p *ganttPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case refreshMsg:
		// a grab holds dates the store does not have; the store just
		// changed under it, so it is let go rather than dropped on a chart
		// that is not the one it was picked up from
		if p.grabbed >= 0 {
			p.grabbed = -1
			p.status = "store changed: grab released"
		}
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

	// a bar has no cell to paste into: a field is edited on show
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

func (p *ganttPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.help != nil:
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
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
		p.col--
	case keys.right.matches(press):
		p.col++

	case keys.next.matches(press):
		p.intoChild()
	case keys.previous.matches(press):
		p.toParent()
	case keys.fold.matches(press):
		p.toggleFold()

	case keys.act.matches(press):
		if b := p.current(); b != nil {
			return p, p.push(b.id)
		}

	// a bar is the issue, and has no cell: copy is the id either way
	case keys.copy.matches(press), keys.copyId.matches(press):
		return p, p.copyId()
	case keys.paste.matches(press):
		p.status = "nothing to paste into"
		return p, bell()
	case keys.filter.matches(press):
		p.startFilter()
	case keys.grab.matches(press):
		return p.startGrab()
	case keys.help.matches(press):
		p.help = &help{}

	case keys.back.matches(press):
		if p.filter != "" {
			p.filter = ""
			p.reorder()
			p.status = ""
			return p, nil
		}
		return p, func() tea.Msg { return popMsg{} }
	}

	p.clamp()
	p.clampCol()
	return p, nil
}

// move is up and down: between rows, every level, the column kept, because
// the cursor is a cell and a column is a date.
func (p *ganttPage) move(by int) {
	p.cursor = min(max(p.cursor+by, 0), max(len(p.order)-1, 0))
}

// rowsPerPage is what a page key moves by: the rows that fit, at least one.
func (p *ganttPage) rowsPerPage() int {
	return max(p.height-6, 1)
}

// intoChild is tab: onto the row's first child, unfolding it on the way.
func (p *ganttPage) intoChild() {
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
func (p *ganttPage) toParent() {
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
func (p *ganttPage) toggleFold() {
	node := p.node()
	if node == nil || node.children == 0 {
		return
	}
	p.folded[node.id] = !node.folded
	p.rebuild()
}

// push opens an issue over the chart.
func (p *ganttPage) push(id string) tea.Cmd {
	shown, err := newShowPage(p.repo, id, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

func (p *ganttPage) copyId() tea.Cmd {
	b := p.current()
	if b == nil {
		return bell()
	}
	p.status = "copied " + b.id
	return setClipboard(b.id)
}

func (p *ganttPage) startFilter() {
	input := textinput.New()
	input.SetValue(p.filter)
	input.SetWidth(p.width - 10)
	input.Focus()
	p.filtering = &input
}

func (p *ganttPage) updateFilter(msg tea.Msg) (page, tea.Cmd) {
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

// startGrab picks the bar under the cursor up. It needs no rank: moving a
// bar along the chart is the gantt's reason to exist, and only moving it
// up or down needs an order to write.
func (p *ganttPage) startGrab() (page, tea.Cmd) {
	if p.current() == nil {
		return p, nil
	}
	p.grabbed = p.order[p.cursor]
	p.grabFrom.cursor, p.grabFrom.col = p.cursor, p.col
	p.blink = true
	return p, blinkTick()
}

func (p *ganttPage) updateGrab(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		// the bar goes back where it was: its dates are the stored ones
		// again, and the order is rebuilt from the store, which never changed
		b := &p.bars[p.grabbed]
		p.grabbed = -1
		p.putBack(b)
		return p, nil

	case keys.left.matches(press):
		return p, p.dragAlong(-1)
	case keys.right.matches(press):
		return p, p.dragAlong(1)

	case keys.up.matches(press), keys.down.matches(press):
		if p.rankKey == "" {
			p.status = "no rank: cannot reorder"
			return p, bell()
		}
		if keys.up.matches(press) {
			p.cursor = moveBlock(p.nodes, p.order, p.cursor, -1)
		} else {
			p.cursor = moveBlock(p.nodes, p.order, p.cursor, 1)
		}

	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragAlong shifts the grabbed bar by one period, on the screen only: on
// its first cell the start, on its last the stop, anywhere else both, so
// that the bar keeps its length. A bar of one cell grows: left moves its
// start, right its stop. A start never passes its stop.
func (p *ganttPage) dragAlong(by int) tea.Cmd {
	b := &p.bars[p.grabbed]
	if !b.hasStart && !b.hasStop {
		p.status = "no dates: nothing to move"
		return bell()
	}

	first, last, _, _ := p.span(p.grabbed)
	moveStart, moveStop := true, true
	switch {
	case !b.hasStop:
		moveStop = false
	case !b.hasStart:
		moveStart = false
	case first == last && p.col == first:
		moveStart, moveStop = by < 0, by > 0
	case p.col == first:
		moveStop = false
	case p.col == last:
		moveStart = false
	}

	if moveStart != moveStop && b.hasStart && b.hasStop {
		start, stop := p.dates(b)
		if moveStart {
			start = shift(b.start, p.scale, b.dStart+by)
		} else {
			stop = shift(b.stop, p.scale, b.dStop+by)
		}
		if start.After(stop) {
			p.status = "start would pass stop"
			return bell()
		}
	}

	if moveStart {
		b.dStart += by
	}
	if moveStop {
		b.dStop += by
	}

	// the chart follows the bar when its extent is the data's
	at, _ := p.colDate()
	at = shift(at, p.scale, by)
	p.layoutPeriods()
	p.col = p.index(at)
	p.clampCol()
	p.status = ""
	return nil
}

// drop writes where the bar landed: each date that moved, and the rank
// when one is bound and the row moved, in one call, which is one commit. A
// bar dropped where it was picked up writes nothing.
func (p *ganttPage) drop() tea.Cmd {
	b := &p.bars[p.grabbed]
	p.grabbed = -1

	fields := map[string]issue.Value{}
	if b.dStart != 0 {
		fields[p.startKey] = issue.StringValue(shiftText(b.startText, p.scale, b.dStart))
	}
	if b.dStop != 0 {
		fields[p.stopKey] = issue.StringValue(shiftText(b.stopText, p.scale, b.dStop))
	}
	said := "moved"
	if p.rankKey != "" && p.cursor != p.grabFrom.cursor {
		lo, hi := siblingRanks(p.nodes, p.order, p.cursor)
		key, err := rank.Between(lo, hi)
		if err != nil {
			p.status = err.Error()
			p.putBack(b)
			return bell()
		}
		fields[p.rankKey] = issue.StringValue(key)
		if len(fields) == 1 {
			said = "rank set"
		}
	}
	if len(fields) == 0 {
		p.putBack(b)
		p.status = ""
		return nil
	}

	id := b.id
	if _, err := host.IssueSet(p.repo, id, fields, false); err != nil {
		p.status = err.Error()
		p.putBack(b)
		return bell()
	}
	p.status = said
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	p.putCursorOn(id)
	return nil
}

// putBack undoes a grab on the screen: the stored dates, the stored order.
func (p *ganttPage) putBack(b *bar) {
	b.dStart, b.dStop = 0, 0
	at, _ := p.colDate()
	p.reorder()
	p.putCursorOn(b.id)
	p.layoutPeriods()
	p.col = p.index(at)
	p.clampCol()
}
