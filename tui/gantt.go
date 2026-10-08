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

	call  *view.Call
	query string
	// includeArchive brings the archived back into the input query runs over.
	includeArchive bool
	startKey       string
	stopKey        string
	labelKey       string
	scale          string
	from, to       string
	progressKey    string
	groupBy        string
	// nest is `expand` resolved: the layer per level, level 0 being the
	// call's own group_by (nest.go).
	nest *nesting

	items []map[string]any
	// open is the parents folded open, by key: a tree opens folded, so what
	// is remembered is what was opened.
	open map[string]bool
	// opening is the call's `open`, levels to unfold the first tree to, -1
	// for every one; it is spent on the first build (R4).
	opening int
	// kept is the order the person dragged keyed rows into, by key, which
	// this view holds until it is quit and never writes (keep.go, R5).
	kept map[string]int
	// refused is what checkLevels said of the last tree built, which load
	// returns: a tree the view cannot draw is refused, as a repeated key is.
	refused error

	bars  []bar
	nodes []treeRow
	order []int

	// periods is the chart, the first day of each period in order; the
	// cursor is the row at order[cursor] and the period at col, or the tree
	// cell at -1, which is the arrow `←` reaches from the first period.
	periods []time.Time
	cursor  int
	col     int
	// colOffset is the first period drawn, top the first body line drawn.
	colOffset, top int
	// placed says the cursor has been put on a cell, so that a rebuild
	// keeps where it is rather than opening the chart again.
	placed bool

	width, height int

	filter    string
	filtering *textinput.Model
	help      *help

	// grabbed is the bar being moved, by index into bars, or -1; grabFrom
	// is where it was picked up, so that a drop back there writes nothing,
	// and crossed the group it has been carried into since (group.go).
	grabbed   int
	grabFrom  struct{ cursor, col int }
	grabGroup string
	crossed   *crossing
	blink     bool

	status string
}

// bar is one row on the chart: key is its identity on the screen, id the
// issue it acts on, "" where it stands for none (doc/design/query-rows.md).
type bar struct {
	key     string
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
		repo:           repo,
		call:           call,
		includeArchive: call.Bool("include_archive"),
		query:          call.String("query"),
		startKey:       call.String("start"),
		stopKey:        call.String("stop"),
		labelKey:       call.String("label"),
		scale:          call.String("scale"),
		from:           call.String("from"),
		to:             call.String("to"),
		progressKey:    call.String("progress"),
		groupBy:        call.String("group_by"),
		open:           map[string]bool{},
		opening:        call.OpenLevels(),
		kept:           map[string]int{},
		width:          80,
		height:         24,
		col:            -1,
		grabbed:        -1,
	}
	n, err := newNesting(nestLayer{groupBy: p.groupBy, includeArchive: p.includeArchive}, call.Expand())
	if err != nil {
		return nil, err
	}
	p.nest = n
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
	values, err := host.IssueList(p.repo, p.query, p.includeArchive)
	if err != nil {
		return err
	}
	p.items, _ = host.ViewRows(values)
	if err := host.CheckRowKeys(p.items); err != nil {
		p.items = nil
		return err
	}
	p.rebuild()
	if err := p.refused; err != nil {
		p.items = nil
		p.rebuild()
		return err
	}
	return nil
}

// rebuild makes the rows out of the last query's items: the tree along
// `expand`, a bar per node, the drawing order, and the chart's extent.
func (p *ganttPage) rebuild() {
	was := p.currentKey()
	at, hadCol := p.colDate()

	known := newKinds(p.repo)
	build := func() {
		tree, err := nest(p.repo, p.items, p.nest, p.open)
		if err != nil {
			p.status = err.Error()
		}
		p.refused = checkLevels(p.repo, tree)
		p.bars = make([]bar, 0, len(tree))
		p.nodes = make([]treeRow, 0, len(tree))
		for _, n := range tree {
			b, node := p.newBar(n, known)
			p.bars = append(p.bars, b)
			p.nodes = append(p.nodes, node)
		}
	}
	build()
	if p.opening != 0 {
		// the call's `open`, on the first tree only (R4)
		openTo(p.nodes, p.opening, p.open)
		p.opening = 0
		build()
	}
	if reveal(p.nodes, p.open, was) {
		build()
	}
	p.addGhosts()

	p.reorder()
	p.putCursorOn(was)
	p.layoutPeriods()
	switch {
	case p.placed && p.col < 0:
		// the cursor is on the arrow cell, which is no date: a fold leaves
		// it where it is
	case hadCol:
		p.col = p.index(at)
	case p.from == "" && p.to == "" && p.index(today()) >= 0 && p.index(today()) < len(p.periods):
		// with neither `from` nor `to` the chart opens on today, its period
		// the left-most
		p.col = p.index(today())
		p.colOffset = p.col
	case p.cursor < len(p.order):
		// the cursor opens on the first row's first cell: where its bar
		// starts, which is where a grab would move its start
		if first, _, _, ok := p.span(p.order[p.cursor]); ok {
			p.col = first
		}
	}
	p.clampCol()
	p.placed = true
}

func (p *ganttPage) newBar(n nested, known *kinds) (bar, treeRow) {
	fields, _ := n.item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	b := bar{
		key:     n.key,
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

	layer := p.layer(n.level)
	node := treeRow{
		key:      n.key,
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
		value := known.cellText(b.typeKey, layer.groupBy, fields[layer.groupBy])
		if isRelation(known.of(b.typeKey, layer.groupBy)) {
			value = linkText(p.repo, linkIds(fields[layer.groupBy]))
		}
		if value != "" {
			node.group = value
		}
	}
	node.rank = plainValue(fields[schema.RankKey])
	node.text = strings.ToLower(b.human + " " + b.label)

	return b, node
}

func (p *ganttPage) reorder() {
	applyKept(p.nodes, p.kept)
	p.order = treeOrder(p.nodes, p.filter)
	p.clamp()
}

func (p *ganttPage) clamp() {
	p.cursor = min(max(p.cursor, 0), max(len(p.order)-1, 0))
}

// clampCol keeps the cursor on a cell: a period, or, where the rows are a
// tree, the arrow cell at -1 that `←` reaches from the first period.
func (p *ganttPage) clampCol() {
	p.col = min(max(p.col, p.minCol()), max(len(p.periods)-1, 0))
}

// minCol is the left-most cell: the tree's arrow where anything nests, and
// the first period where nothing does.
func (p *ganttPage) minCol() int {
	if p.nest.expanded() {
		return -1
	}
	return 0
}

// layer is the layer a row at this level draws by (nest.go).
func (p *ganttPage) layer(level int) *nestLayer {
	if layer := p.nest.at(level); layer != nil {
		return layer
	}
	return &p.nest.layers[0]
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

// currentKey is the key of the row under the cursor, which is what a
// refresh puts the cursor back on.
func (p *ganttPage) currentKey() string {
	if b := p.current(); b != nil {
		return b.key
	}
	return ""
}

// currentId is the issue the row under the cursor acts on.
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

// putCursorOn keeps the cursor on the row it was on across a refresh, by
// key, falling back to the same position when that row is gone.
func (p *ganttPage) putCursorOn(key string) {
	if key != "" {
		for at, index := range p.order {
			if p.bars[index].key == key {
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
	if scale == "day" || scale == "week" {
		return 3
	}
	return 4
}

// periodLabel is a period's own label, the fine line of the header — the
// day of the month, the ISO week number, the month, the quarter; coarse is
// the label over it, said where it changes — the month over days and
// weeks (a week's being its Monday's), the year over months and quarters —
// and short is the coarse label without the year, for a change within one.
func periodLabel(t time.Time, scale string) (fine, coarse, short string) {
	switch scale {
	case "day":
		return fmt.Sprintf("%d", t.Day()), t.Format("Jan 2006"), t.Format("Jan")
	case "week":
		_, week := t.ISOWeek()
		return fmt.Sprintf("%d", week), t.Format("Jan 2006"), t.Format("Jan")
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
//
// **A missing start is today** (2026-10-02, `04248c5`): a row with a stop
// and no start has not started and could start at any point from now on, so
// what it covers is today to its stop — or, for a stop already past, the
// stop to today — and the chart is sized to that rather than to the point
// its stop is.
func (p *ganttPage) extent(index int) (first, last time.Time, own, ok bool) {
	b := &p.bars[index]
	if b.hasStart || b.hasStop {
		start, stop := p.dates(b)
		switch {
		case !b.hasStop:
			return start, start, true, true
		case !b.hasStart:
			now := today()
			return minTime(now, stop), maxTime(now, stop), true, true
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
// else over the extent of the dates on the chart; with neither, today's
// period is in the extent too, and the chart runs on to fill the window
// from the first period drawn, so that today can be the left-most whatever
// the data's end.
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
	if now := today(); !found {
		first, last = now, now
	} else if p.from == "" && p.to == "" {
		first, last = minTime(first, now), maxTime(last, now)
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
	if p.from == "" && p.to == "" {
		for len(p.periods) < p.colOffset+p.capacity() && len(p.periods) <= 10000 {
			p.periods = append(p.periods, shift(p.periods[len(p.periods)-1], p.scale, 1))
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
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

// nowCol is today's period on the chart: -1 before its first period,
// len(periods) after its last.
//
// index() answers a cursor, so it clamps a date past the chart to the last
// period; a band that begins at today needs to know that today is off the
// chart, and would otherwise be drawn on its last period rather than not at
// all.
func (p *ganttPage) nowCol() int {
	if len(p.periods) == 0 {
		return 0
	}
	now := periodStart(today(), p.scale)
	switch {
	case now.Before(p.periods[0]):
		return -1
	case now.After(p.periods[len(p.periods)-1]):
		return len(p.periods)
	}
	return p.index(now)
}

// milestoneCol is the period a one-date row's own date sits in, which is
// not its extent's first: a row with only a stop covers today to that stop,
// and the trail is drawn around the stop.
func (p *ganttPage) milestoneCol(b *bar) int {
	start, stop := p.dates(b)
	if b.hasStart {
		return p.index(start)
	}
	return p.index(stop)
}

// total is how many issues the query returned: the rows, less the ghosts.
func (p *ganttPage) total() int {
	n := 0
	for _, node := range p.nodes {
		if !node.ghost {
			n++
		}
	}
	return n
}

// count is how many issues the chart draws: the rows, less the ghosts.
func (p *ganttPage) count() int {
	n := 0
	for _, index := range p.order {
		if !p.nodes[index].ghost {
			n++
		}
	}
	return n
}

// addGhosts puts a ghost at the foot of each group of roots, one for the
// whole chart when nothing groups it (ghost.go), as the list does.
func (p *ganttPage) addGhosts() {
	groups := []string{}
	types := map[string][]string{}
	seen := map[string]bool{}
	for index, node := range p.nodes {
		if node.level != 0 || node.ghost {
			continue
		}
		group := node.group
		if p.groupBy == "" {
			group = noGroup
		}
		if !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
		types[group] = append(types[group], p.bars[index].typeKey)
	}
	if len(groups) == 0 {
		groups = append(groups, noGroup)
	}
	for _, group := range groups {
		b := bar{key: ghostId(group), human: ghostPrefix, typeKey: sharedType(types[group]), fields: map[string]any{}, label: ghostLabel}
		node := treeRow{key: b.key, group: group, grouped: p.groupBy != "", ghost: true}
		p.bars = append(p.bars, b)
		p.nodes = append(p.nodes, node)
	}
}

// ghostDoc is the draft a ghost opens: the type its group's rows share, and
// the `group_by` field as a drop into the group would write it (C6).
func (p *ganttPage) ghostDoc(ghost *bar, node *treeRow) host.IssueDocument {
	doc := host.IssueDocument{Fields: map[string]issue.Value{}}
	if ghost.typeKey != "" {
		doc.Fields[schema.TypeKey] = issue.StringValue(ghost.typeKey)
	}
	for index, other := range p.nodes {
		if other.ghost || other.level != 0 || other.group != node.group {
			continue
		}
		b := &p.bars[index]
		if value, ok := groupPrefill(p.repo, b.typeKey, p.groupBy, node.group, b.fields[p.groupBy]); ok {
			doc.Fields[p.groupBy] = value
		}
		break
	}
	return doc
}

func (p *ganttPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// the chart fills the window, so it is laid out again
		p.width, p.height = msg.Width, msg.Height
		at, hadCol := p.colDate()
		p.layoutPeriods()
		if hadCol {
			p.col = p.index(at)
		}
		p.clampCol()
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
	case keys.foldAll.matches(press):
		p.toggleAll()

	case keys.act.matches(press):
		if b := p.current(); b != nil {
			if node := p.node(); node.ghost {
				return p, openNew(p.repo, p.ghostDoc(b, node))
			}
			if b.id == "" {
				// a row that stands for no issue has nothing to open (R2)
				return p, bell()
			}
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
		if node := p.node(); node != nil && node.ghost {
			p.status = "enter adds an issue here"
			return p, bell()
		}
		// on the arrow cell space folds, as it does on a list; a bar is
		// grabbed from the chart
		if p.col < 0 {
			return p, p.toggleFold()
		}
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
		p.open[node.key] = true
		p.rebuild()
		node = p.node()
	}
	if at := p.cursor + 1; at < len(p.order) && p.nodes[p.order[at]].level == node.level+1 {
		p.cursor = at
	}
}

// toParent is shift-tab: onto the row this one is under, and onto its arrow
// cell, where space folds back what tab opened.
func (p *ganttPage) toParent() {
	node := p.node()
	if node == nil || node.level == 0 {
		return
	}
	for at := p.cursor - 1; at >= 0; at-- {
		if p.nodes[p.order[at]].level < node.level {
			p.cursor = at
			p.col = p.minCol()
			return
		}
	}
}

// toggleFold is space on the arrow cell: a parent's children shown or
// hidden. A leaf has nothing to fold and rings.
func (p *ganttPage) toggleFold() tea.Cmd {
	node := p.node()
	if node == nil || node.children == 0 {
		p.status = "nothing to fold"
		return bell()
	}
	p.open[node.key] = node.folded
	p.rebuild()
	return nil
}

// toggleAll is Z: every parent folded, or, where none is open, every one of
// them opened (foldAll).
func (p *ganttPage) toggleAll() {
	p.open = foldAll(p.nodes, p.open)
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
	if b == nil || b.id == "" {
		return bell()
	}
	// the clipboard gets the whole id, the message the short one (copyId)
	p.status = "copied " + b.human
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

// startGrab picks the bar under the cursor up. It needs no binding at all:
// moving a bar along the chart is the gantt's reason to exist, and the order
// up and down writes is the built-in rank every type has (D8).
func (p *ganttPage) startGrab() (page, tea.Cmd) {
	if p.current() == nil {
		return p, nil
	}
	p.grabbed = p.order[p.cursor]
	p.grabFrom.cursor, p.grabFrom.col = p.cursor, p.col
	p.grabGroup = p.nodes[p.grabbed].group
	p.crossed = nil
	p.blink = true
	return p, blinkTick()
}

func (p *ganttPage) updateGrab(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		// the bar goes back where it was: its dates are the stored ones
		// again, its group the stored one, and the order is rebuilt from the
		// store, which never changed
		b := &p.bars[p.grabbed]
		p.nodes[p.grabbed].group = p.grabGroup
		p.crossed = nil
		p.grabbed = -1
		p.putBack(b)
		return p, nil

	case keys.left.matches(press):
		return p, p.dragAlong(-1)
	case keys.right.matches(press):
		return p, p.dragAlong(1)

	case keys.up.matches(press):
		return p, p.dragBy(-1)
	case keys.down.matches(press):
		return p, p.dragBy(1)

	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragBy moves the grabbed row among its siblings, its subtree with it, and
// past the last of them carries a root into the neighbouring group, which
// is the same move a list makes (group.go).
func (p *ganttPage) dragBy(by int) tea.Cmd {
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

	b := &p.bars[p.grabbed]
	if p.nodes[p.grabbed].keyed() {
		// a share of an issue moved to another group would write the
		// issue's field, which is not what moving a share means (R5)
		p.status = "stays in its group"
		return bell()
	}
	if refusal := crossRefusal(p.repo, b.typeKey, p.groupBy); refusal != "" {
		p.status = refusal
		return bell()
	}

	group := p.nodes[neighbour].group
	p.nodes[p.grabbed].group = group
	p.crossed = &crossing{group: group, value: groupValue(p.bars[neighbour].fields[p.groupBy], group)}
	p.cursor = crossGhost(p.nodes, p.order, p.cursor, by)
	p.status = ""
	return nil
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
	if b.id == "" {
		// its dates are the query's, and no issue's to write (R2)
		p.status = "no issue: dates not movable"
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

// drop writes where the bar landed: each date that moved, the rank when the
// row moved, and the `group_by` field when it crossed into another group,
// in one call, which is one commit. A bar dropped where it was picked up
// writes nothing.
//
// The siblings drawn above it that have no rank are given one first, each
// its own commit, so that the drop reads as it was drawn (rank.go).
func (p *ganttPage) drop() tea.Cmd {
	b := &p.bars[p.grabbed]
	keyed := p.nodes[p.grabbed].keyed()
	into := p.crossed
	if into != nil && p.nodes[p.grabbed].group == p.grabGroup {
		into = nil // carried out of its group and back into it
	}
	p.crossed = nil
	p.grabbed = -1

	fields := map[string]issue.Value{}
	if b.dStart != 0 {
		fields[p.startKey] = issue.StringValue(shiftText(b.startText, p.scale, b.dStart))
	}
	if b.dStop != 0 {
		fields[p.stopKey] = issue.StringValue(shiftText(b.stopText, p.scale, b.dStop))
	}
	said := "moved"
	var above []rankFill
	// a row in another group has new neighbours, so a crossing is a reorder
	// even where the cursor did not move
	reordered := p.cursor != p.grabFrom.cursor || into != nil
	if reordered && keyed {
		// a keyed row's place is the view's to hold, never the issue's
		// rank to write (R5)
		keepOrder(p.nodes, p.order, p.cursor, p.kept)
		if len(fields) == 0 {
			said = keptSaid
		}
	}
	if reordered && !keyed {
		fills, key, err := scopeFills(p.nodes, p.order, p.cursor)
		if err != nil {
			p.status = err.Error()
			p.putBack(b)
			return bell()
		}
		above = fills
		fields[schema.RankKey] = issue.StringValue(key)
		if len(fields) == 1 {
			said = "rank set"
		}
	}
	if into != nil {
		fields[p.groupBy] = into.value
		said = "moved to " + into.group
	}
	if len(fields) == 0 {
		p.putBack(b)
		p.status = ""
		if keyed && reordered {
			p.status = keptSaid
		}
		return nil
	}

	id := b.id
	if err := writeFills(p.repo, above); err != nil {
		p.status = err.Error()
		p.putBack(b)
		return bell()
	}
	if _, err := host.IssueSet(p.repo, id, fields, false); err != nil {
		p.status = err.Error()
		p.putBack(b)
		return bell()
	}
	p.status = saidAnd(said, above)
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	p.putCursorOn(b.key)
	return nil
}

// putBack undoes a grab on the screen: the stored dates, the stored order.
func (p *ganttPage) putBack(b *bar) {
	b.dStart, b.dStop = 0, 0
	at, _ := p.colDate()
	p.reorder()
	p.putCursorOn(b.key)
	p.layoutPeriods()
	p.col = p.index(at)
	p.clampCol()
}
