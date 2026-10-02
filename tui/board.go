package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// boardPage is the `board` view: a column per value of one field, a card per
// issue, and a swimlane per value of another field when one is bound.
//
// A board is a list with a second axis, and the edits it makes are moves:
// grabbing a card and dropping it in another column sets the `columns` field,
// dropping it higher or lower in its column sets the rank. Nothing on a card
// is edited in place — enter opens the issue, and show is where a field is
// edited — because a card is a summary, and the board is for moving them
// (decided 2026-09-28, doc/design/terminal-renderer.md).
type boardPage struct {
	repo *cache.RepoCache

	call       *view.Call
	query      string
	columnsKey string
	values     []string
	cardKeys   []string
	groupBy    string
	rankKey    string
	// colWidth is the narrowest a column goes before the board scrolls
	// sideways instead (`column_width`, view/kinds.go, which is the
	// authority on its default).
	colWidth int

	// cards is every issue the query returned, in the query's order.
	cards []card
	// columns is the drawing order of the columns, resolved on every load.
	columns []column
	// lanes is the board as drawn: a stack of card indexes per column, per
	// lane; one lane, with no group, when group_by is not bound.
	lanes []lane

	// the cursor is a card: its lane, its column and its place in the stack
	lane, col, row int
	// colOffset is the first column drawn, top the first body line drawn.
	colOffset, top int

	width, height int

	filter    string
	filtering *textinput.Model
	help      *help

	// grabbed is the card being moved, by index into cards, or -1; grabFrom
	// is where it was picked up, so that a drop back there writes nothing,
	// grabGroup the swimlane it came from and crossed the one it has been
	// carried into since (group.go).
	grabbed   int
	grabFrom  [3]int
	grabGroup string
	crossed   *crossing
	blink     bool

	status string
}

// card is one issue on the board.
type card struct {
	id      string
	human   string
	typeKey string
	fields  map[string]any

	// cells is what the card draws, by card key; a relation is the issues it
	// names, and links says which cells are relations
	cells map[string]string
	links map[string]bool

	// value is the columns field, "" for none; group the swimlane's.
	value string
	group string
	rank  string
	// text is everything the card draws, folded, for the filter to search.
	text string
}

// column is one column: the value it holds, and how its header reads.
type column struct {
	value string
	label string
}

// lane is one swimlane: its group, and a stack of card indexes per column.
type lane struct {
	group  string
	stacks [][]int
}

func (p *boardPage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func newBoardPage(repo *cache.RepoCache, call *view.Call) (*boardPage, error) {
	p := &boardPage{
		repo:       repo,
		call:       call,
		query:      call.String("query"),
		columnsKey: call.String("columns"),
		values:     call.Strings("values"),
		cardKeys:   call.Strings("card"),
		groupBy:    call.String("group_by"),
		rankKey:    call.String("rank"),
		colWidth:   call.Int("column_width"),
		width:      80,
		height:     24,
		grabbed:    -1,
	}
	if len(p.cardKeys) == 0 {
		p.cardKeys = []string{schema.TitleKey}
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// load re-runs the query and rebuilds the board, keeping the cursor on the
// card it was on — wherever that card is now, because another process may
// have moved it to another column.
func (p *boardPage) load() error {
	was := p.currentId()

	values, err := host.IssueList(p.repo, p.query)
	if err != nil {
		return err
	}

	items, _ := host.IssueItems(values)
	known := newKinds(p.repo)
	p.cards = make([]card, 0, len(items))
	for _, item := range items {
		p.cards = append(p.cards, p.newCard(item, known))
	}

	p.columns = p.resolveColumns()
	p.arrange()
	p.putCursorOn(was)
	return nil
}

func (p *boardPage) newCard(item map[string]any, known *kinds) card {
	fields, _ := item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	c := card{
		id:      host.StringOr(item["id"], ""),
		human:   host.StringOr(item["human_id"], ""),
		typeKey: host.StringOr(fields[schema.TypeKey], ""),
		fields:  fields,
		cells:   map[string]string{},
		links:   map[string]bool{},
	}
	if c.human == "" && len(c.id) > idWidth {
		c.human = c.id[:idWidth]
	}

	for _, key := range p.cardKeys {
		if isRelation(known.of(c.typeKey, key)) {
			c.links[key] = true
			c.cells[key] = linkText(p.repo, linkIds(fields[key]))
			continue
		}
		c.cells[key] = known.cellText(c.typeKey, key, fields[key])
	}

	c.value = plainValue(fields[p.columnsKey])
	c.group = noGroup
	if p.groupBy != "" {
		if value := known.cellText(c.typeKey, p.groupBy, fields[p.groupBy]); value != "" {
			c.group = value
		}
	}
	c.rank = plainValue(fields[p.rankKey])

	var text strings.Builder
	text.WriteString(c.human)
	for _, key := range p.cardKeys {
		text.WriteString(" ")
		text.WriteString(c.cells[key])
	}
	c.text = strings.ToLower(text.String())

	return c
}

// resolveColumns is the columns in drawing order: `values` when given, else
// the field's schema order; then, trailing, every value the data has that
// is not listed, so that nothing vanishes; then (none) for the cards with no
// value at all. A listed column is drawn empty; the trailing ones only exist
// while a card is in them.
//
// The schema order is read off the types the board's cards have, because
// every type owns its own field (e7e58f2) and an iteration's statuses are
// not columns of a board of tasks; with no cards at all, off every type.
func (p *boardPage) resolveColumns() []column {
	var columns []column
	seen := map[string]bool{}
	// a people field's columns are its people, headed by name
	person := false
	add := func(value, label string) {
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		if label == "" && person {
			label = host.UserName(p.repo, value)
		}
		if label == "" {
			label = value
		}
		columns = append(columns, column{value: value, label: label})
	}

	onBoard := map[string]bool{}
	for _, c := range p.cards {
		onBoard[c.typeKey] = true
	}

	names := map[string]string{}
	if s, err := p.repo.LoadSchema(); err == nil {
		// the first type's order wins and later types only add what it
		// did not have
		for _, typeKey := range s.TypeKeys() {
			if len(onBoard) > 0 && !onBoard[typeKey] {
				continue
			}
			field, ok := s.Field(typeKey, p.columnsKey)
			if !ok {
				continue
			}
			if isPerson(field.Kind) {
				person = true
			}
			for _, value := range field.Values {
				if _, named := names[value.Id]; !named {
					names[value.Id] = value.Name
				}
				if p.values == nil {
					add(value.Id, value.Name)
				}
			}
		}
	}
	for _, value := range p.values {
		add(value, names[value])
	}

	none := false
	for _, c := range p.cards {
		if c.value == "" {
			none = true
			continue
		}
		add(c.value, names[c.value])
	}
	if none {
		columns = append(columns, column{value: "", label: noGroup})
	}
	return columns
}

// arrange rebuilds the lanes: the filter, then the groups in the order they
// first appear with the ungrouped last, then each card into its column, in
// the query's order or by (rank, id) where a rank is bound.
func (p *boardPage) arrange() {
	at := make(map[string]int, len(p.columns))
	for index, column := range p.columns {
		at[column.value] = index
	}

	needle := strings.ToLower(strings.TrimSpace(p.filter))
	p.lanes = p.lanes[:0]
	laneOf := map[string]int{}
	for index, c := range p.cards {
		if needle != "" && !strings.Contains(c.text, needle) {
			continue
		}
		group := c.group
		if p.groupBy == "" {
			group = ""
		}
		which, ok := laneOf[group]
		if !ok {
			which = len(p.lanes)
			laneOf[group] = which
			p.lanes = append(p.lanes, lane{group: group, stacks: make([][]int, len(p.columns))})
		}
		col := at[c.value]
		p.lanes[which].stacks[col] = append(p.lanes[which].stacks[col], index)
	}
	if len(p.lanes) == 0 {
		p.lanes = append(p.lanes, lane{stacks: make([][]int, len(p.columns))})
	}

	// the cards nobody has filed under the grouping field come last
	if none, ok := laneOf[noGroup]; ok && none != len(p.lanes)-1 {
		last := p.lanes[none]
		p.lanes = append(p.lanes[:none], p.lanes[none+1:]...)
		p.lanes = append(p.lanes, last)
	}

	for _, l := range p.lanes {
		for _, stack := range l.stacks {
			sortStack(p.cards, stack)
		}
	}

	p.clamp()
}

// sortStack orders one stack by (rank, id), stably, so that cards without a
// rank keep the query's order among themselves, at the end.
func sortStack(cards []card, stack []int) {
	for i := 1; i < len(stack); i++ {
		for j := i; j > 0; j-- {
			left, right := cards[stack[j-1]], cards[stack[j]]
			if !lessByRank(right.rank, right.id, left.rank, left.id) {
				break
			}
			stack[j-1], stack[j] = stack[j], stack[j-1]
		}
	}
}

// clamp keeps the cursor on a card where there is one: the cursor's own
// place when it still holds a card, else the first card on the board.
func (p *boardPage) clamp() {
	p.lane = min(max(p.lane, 0), max(len(p.lanes)-1, 0))
	p.col = min(max(p.col, 0), max(len(p.columns)-1, 0))
	if stack := p.stack(); len(stack) > 0 {
		p.row = min(max(p.row, 0), len(stack)-1)
		return
	}
	p.row = 0
	for l, la := range p.lanes {
		for c, stack := range la.stacks {
			if len(stack) > 0 {
				p.lane, p.col = l, c
				return
			}
		}
	}
}

// stack is the column the cursor is in, within its lane.
func (p *boardPage) stack() []int {
	if p.lane >= len(p.lanes) || p.col >= len(p.columns) {
		return nil
	}
	return p.lanes[p.lane].stacks[p.col]
}

func (p *boardPage) current() *card {
	stack := p.stack()
	if p.row < 0 || p.row >= len(stack) {
		return nil
	}
	return &p.cards[stack[p.row]]
}

func (p *boardPage) currentId() string {
	if c := p.current(); c != nil {
		return c.id
	}
	return ""
}

// putCursorOn keeps the cursor on the card it was on across a refresh,
// wherever it is now, falling back to the same place when it is gone.
func (p *boardPage) putCursorOn(id string) {
	if id != "" {
		for l, la := range p.lanes {
			for c, stack := range la.stacks {
				for r, index := range stack {
					if p.cards[index].id == id {
						p.lane, p.col, p.row = l, c, r
						return
					}
				}
			}
		}
	}
	p.clamp()
}

// count is how many cards are on the board, after the filter.
func (p *boardPage) count() int {
	n := 0
	for _, la := range p.lanes {
		for _, stack := range la.stacks {
			n += len(stack)
		}
	}
	return n
}

func (p *boardPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil

	case statusMsg:
		p.status = string(msg)
		return p, nil

	case refreshMsg:
		// a grab holds a placement the store does not have; the store just
		// changed under it, so it is let go rather than dropped on a board
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

	// a card has no cell to paste into: a field is edited on show
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

func (p *boardPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
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
		p.moveRow(-1)
	case keys.down.matches(press):
		p.moveRow(1)
	case keys.pageUp.matches(press):
		p.moveWithin(-p.cardsPerPage())
	case keys.pageDn.matches(press):
		p.moveWithin(p.cardsPerPage())
	case keys.top.matches(press):
		p.row = 0
	case keys.bottom.matches(press):
		p.row = max(len(p.stack())-1, 0)

	case keys.left.matches(press):
		p.moveCol(-1)
	case keys.right.matches(press):
		p.moveCol(1)

	case keys.act.matches(press):
		if c := p.current(); c != nil {
			return p, p.push(c.id)
		}

	// a card is the issue, and has no cell: copy is the id either way
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
			p.arrange()
			p.status = ""
			return p, nil
		}
		return p, func() tea.Msg { return popMsg{} }
	}

	p.clamp()
	return p, nil
}

// moveRow moves up or down the column, and past its end into the same
// column of the next lane that has a card there.
func (p *boardPage) moveRow(by int) {
	to := p.row + by
	if to >= 0 && to < len(p.stack()) {
		p.row = to
		return
	}
	for l := p.lane + by; l >= 0 && l < len(p.lanes); l += by {
		if stack := p.lanes[l].stacks[p.col]; len(stack) > 0 {
			p.lane = l
			if by < 0 {
				p.row = len(stack) - 1
			} else {
				p.row = 0
			}
			return
		}
	}
}

// moveWithin moves within the column only, which is what a page key does.
func (p *boardPage) moveWithin(by int) {
	p.row = min(max(p.row+by, 0), max(len(p.stack())-1, 0))
}

// moveCol moves to the nearest column in that direction with a card in this
// lane; a column with nothing in it is nothing to stand on.
func (p *boardPage) moveCol(by int) {
	for c := p.col + by; c >= 0 && c < len(p.columns); c += by {
		if stack := p.lanes[p.lane].stacks[c]; len(stack) > 0 {
			p.col = c
			p.row = min(p.row, len(stack)-1)
			return
		}
	}
}

// cardsPerPage is what a page key moves by: a card is a few lines tall.
func (p *boardPage) cardsPerPage() int {
	return max(p.height/4, 1)
}

// push opens an issue over the board.
func (p *boardPage) push(id string) tea.Cmd {
	shown, err := newShowPage(p.repo, id, nil)
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}

func (p *boardPage) copyId() tea.Cmd {
	c := p.current()
	if c == nil {
		return bell()
	}
	// the clipboard gets the whole id, the message the short one (copyId)
	p.status = "copied " + c.human
	return setClipboard(c.id)
}

func (p *boardPage) startFilter() {
	input := textinput.New()
	input.SetValue(p.filter)
	input.SetWidth(p.width - 10)
	input.Focus()
	p.filtering = &input
}

func (p *boardPage) updateFilter(msg tea.Msg) (page, tea.Cmd) {
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

// startGrab picks the card under the cursor up. It needs no binding at all:
// moving a card to another column is the board's reason to exist, and the
// order up and down writes is the built-in rank every type has (D8).
func (p *boardPage) startGrab() (page, tea.Cmd) {
	c := p.current()
	if c == nil {
		return p, nil
	}
	p.grabbed = p.stack()[p.row]
	p.grabFrom = [3]int{p.lane, p.col, p.row}
	p.grabGroup = p.lanes[p.lane].group
	p.crossed = nil
	p.blink = true
	return p, blinkTick()
}

func (p *boardPage) updateGrab(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		// the card goes back where it was, its lane included: the board is
		// rebuilt from the store, which never changed
		id := p.cards[p.grabbed].id
		p.grabbed = -1
		p.crossed = nil
		p.arrange()
		p.putCursorOn(id)
		return p, nil

	case keys.left.matches(press):
		p.dragAcross(-1)
	case keys.right.matches(press):
		p.dragAcross(1)

	case keys.up.matches(press):
		return p, p.dragBy(-1)
	case keys.down.matches(press):
		return p, p.dragBy(1)

	case keys.grab.matches(press), keys.act.matches(press):
		return p, p.drop()
	}
	return p, nil
}

// dragAcross moves the grabbed card into the neighbouring column, empty or
// not, on the screen only: the field is written once, when it is dropped.
func (p *boardPage) dragAcross(by int) {
	to := p.col + by
	if to < 0 || to >= len(p.columns) {
		return
	}
	stacks := p.lanes[p.lane].stacks
	stacks[p.col] = append(stacks[p.col][:p.row], stacks[p.col][p.row+1:]...)

	row := min(p.row, len(stacks[to]))
	stacks[to] = append(stacks[to], 0)
	copy(stacks[to][row+1:], stacks[to][row:])
	stacks[to][row] = p.grabbed
	p.col, p.row = to, row
}

// dragBy moves the grabbed card within its column, on the screen only; past
// the end of the stack it carries the card into the neighbouring swimlane,
// at that lane's end going up and its start going down, and the drop writes
// the `group_by` field the way a column drop writes `columns` (group.go).
func (p *boardPage) dragBy(by int) tea.Cmd {
	stack := p.stack()
	if to := p.row + by; to >= 0 && to < len(stack) {
		stack[p.row], stack[to] = stack[to], stack[p.row]
		p.row = to
		return nil
	}
	return p.crossLane(by)
}

// crossLane carries the grabbed card into the lane above or below, in the
// column it is in.
func (p *boardPage) crossLane(by int) tea.Cmd {
	lane := p.lane + by
	if p.groupBy == "" || lane < 0 || lane >= len(p.lanes) {
		return nil
	}
	c := &p.cards[p.grabbed]
	if refusal := crossRefusal(p.repo, c.typeKey, p.groupBy); refusal != "" {
		p.status = refusal
		return bell()
	}
	into, ok := p.laneValue(lane)
	if !ok {
		return nil
	}

	from := p.lanes[p.lane].stacks
	from[p.col] = append(from[p.col][:p.row], from[p.col][p.row+1:]...)

	stacks := p.lanes[lane].stacks
	row := 0
	if by < 0 {
		row = len(stacks[p.col])
	}
	stacks[p.col] = append(stacks[p.col], 0)
	copy(stacks[p.col][row+1:], stacks[p.col][row:])
	stacks[p.col][row] = p.grabbed

	p.lane, p.row = lane, row
	p.crossed = &into
	p.status = ""
	return nil
}

// laneValue is what a drop into a lane writes: the value another card in it
// holds, whatever column that card is in. A lane the grabbed card emptied on
// its way out is its own, so the card's stored value is the answer there.
func (p *boardPage) laneValue(lane int) (crossing, bool) {
	group := p.lanes[lane].group
	for _, stack := range p.lanes[lane].stacks {
		for _, index := range stack {
			if index != p.grabbed {
				return crossing{group: group, value: groupValue(p.cards[index].fields[p.groupBy], group)}, true
			}
		}
	}
	if c := &p.cards[p.grabbed]; group == c.group {
		return crossing{group: group, value: groupValue(c.fields[p.groupBy], group)}, true
	}
	return crossing{}, false
}

// drop writes where the card landed: the columns field when the column
// changed, the `group_by` field when the card crossed into another swimlane,
// and the rank whenever it moved at all — a card in a new column or a new
// lane has new neighbours — every key in one call, which is one commit. A
// card dropped where it was picked up writes nothing.
//
// The cards drawn above it in the stack it landed in that have no rank are
// given one first, each its own commit, so that the drop reads as it was
// drawn (rank.go).
func (p *boardPage) drop() tea.Cmd {
	c := &p.cards[p.grabbed]
	into := p.crossed
	if into != nil && p.lanes[p.lane].group == p.grabGroup {
		into = nil // carried out of its lane and back into it
	}
	p.crossed = nil
	p.grabbed = -1

	if [3]int{p.lane, p.col, p.row} == p.grabFrom {
		p.status = ""
		return nil
	}

	fields := map[string]issue.Value{}
	said := "rank set"
	if target := p.columns[p.col]; target.value != c.value {
		if target.value == "" {
			fields[p.columnsKey] = issue.MustValue(nil)
		} else {
			fields[p.columnsKey] = issue.StringValue(target.value)
		}
		said = "moved to " + target.label
	}
	if into != nil {
		fields[p.groupBy] = into.value
		said = "moved to " + into.group
	}
	above, key, err := p.stackFills()
	if err != nil {
		p.status = err.Error()
		p.arrange()
		p.putCursorOn(c.id)
		return bell()
	}
	fields[p.rankKey] = issue.StringValue(key)

	id := c.id
	if err := writeFills(p.repo, above, p.rankKey); err != nil {
		p.status = err.Error()
		p.arrange()
		p.putCursorOn(id)
		return bell()
	}
	if _, err := host.IssueSet(p.repo, id, fields, false); err != nil {
		p.status = err.Error()
		p.arrange()
		p.putCursorOn(id)
		return bell()
	}
	p.status = saidAnd(said, above)
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	p.putCursorOn(id)
	return nil
}

// stackFills is fillRanks over the stack the dropped card landed in, which
// is one lane's one column, and is the card's whole ordering scope.
func (p *boardPage) stackFills() (above []rankFill, key string, err error) {
	stack := p.stack()
	ids := make([]string, len(stack))
	ranks := make([]string, len(stack))
	for at, index := range stack {
		ids[at] = p.cards[index].id
		ranks[at] = p.cards[index].rank
	}
	return fillRanks(ids, ranks, p.row)
}
