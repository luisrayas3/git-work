package tui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// listPage is the `list` view: one row per issue, one column per field.
//
// The rows are the excerpts the query returned, verbatim, so what is drawn is
// what `git work issue` prints and a jq program can be written against.
type listPage struct {
	repo *cache.RepoCache

	// the call, unpacked once
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
	column int
	top    int

	width, height int

	filter    string
	filtering *textinput.Model
	helping   bool

	status string
}

// listRow is one issue, as the query handed it over.
type listRow struct {
	id      string
	human   string
	typeKey string
	fields  map[string]any

	group string
	rank  string
	// text is everything the row draws, folded, for the filter to search.
	text string
}

func newListPage(repo *cache.RepoCache, call *view.Call) (*listPage, error) {
	p := &listPage{
		repo:    repo,
		query:   call.String("query"),
		fields:  call.Strings("fields"),
		details: call.Strings("details"),
		groupBy: call.String("group_by"),
		rankKey: call.String("rank"),
		width:   80,
		height:  24,
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

	items := issueItems(values)
	p.rows = make([]listRow, 0, len(items))
	for _, item := range items {
		p.rows = append(p.rows, p.newRow(item))
	}

	p.reorder()
	p.putCursorOn(was)
	return nil
}

func (p *listPage) newRow(item map[string]any) listRow {
	fields, _ := item["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	row := listRow{
		id:      stringOf(item["id"]),
		human:   stringOf(item["human_id"]),
		typeKey: stringOf(fields[schema.TypeKey]),
		fields:  fields,
	}
	if row.human == "" && len(row.id) > 7 {
		row.human = row.id[:7]
	}

	row.group = noGroup
	if p.groupBy != "" {
		if value := plainValue(fields[p.groupBy]); value != "" {
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
		text.WriteString(plainValue(fields[key]))
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
	if p.column >= len(p.fields) {
		p.column = len(p.fields) - 1
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

	case refreshMsg:
		if err := p.load(); err != nil {
			p.status = err.Error()
		}
		return p, nil

	case tea.KeyPressMsg:
		return p.key(msg)
	}

	return p, nil
}

func (p *listPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.helping:
		p.helping = false
		return p, nil
	case p.filtering != nil:
		return p.updateFilter(press)
	}

	switch {
	case key.Matches(press, keys.quit):
		return p, tea.Quit

	case key.Matches(press, keys.up):
		p.move(-1)
	case key.Matches(press, keys.down):
		p.move(1)
	case key.Matches(press, keys.pageUp):
		p.move(-p.rowsPerPage())
	case key.Matches(press, keys.pageDn):
		p.move(p.rowsPerPage())
	case key.Matches(press, keys.top):
		p.cursor = 0
	case key.Matches(press, keys.bottom):
		p.cursor = len(p.order) - 1

	case key.Matches(press, keys.left):
		p.column = max(0, p.column-1)
	case key.Matches(press, keys.right):
		p.column = min(len(p.fields)-1, p.column+1)

	case key.Matches(press, keys.yank):
		return p.yank()
	case key.Matches(press, keys.filter):
		p.startFilter()
	case key.Matches(press, keys.help):
		p.helping = true

	case key.Matches(press, keys.back):
		if p.filter != "" {
			p.filter = ""
			p.reorder()
			p.status = ""
		}
	}

	p.clamp()
	return p, nil
}

func (p *listPage) move(by int) {
	p.cursor = min(max(p.cursor+by, 0), max(len(p.order)-1, 0))
}

// yank puts the issue's id on the clipboard with OSC 52, which is the one way
// that works over ssh and in a multiplexer, because it is the terminal that
// copies and not the machine the program runs on.
func (p *listPage) yank() (page, tea.Cmd) {
	row := p.current()
	if row == nil {
		return p, nil
	}
	p.status = "yanked " + row.id
	return p, tea.SetClipboard(row.id)
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
		case key.Matches(press, keys.cancel):
			p.filtering = nil
			p.filter = ""
			p.reorder()
			return p, nil
		case key.Matches(press, keys.open):
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

// issueItems flattens whatever the jq program emitted into issues.
//
// A program usually returns one value, the array; one that emitted issues one
// at a time meant the same thing, so both are read the same way.
func issueItems(values []any) []map[string]any {
	var out []map[string]any

	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if _, ok := typed["id"].(string); ok {
				out = append(out, typed)
			}
		}
	}

	for _, value := range values {
		walk(value)
	}
	return out
}

func stringOf(value any) string {
	s, _ := value.(string)
	return s
}
