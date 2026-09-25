package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
)

// showPage is the `show` view: one issue, its fields, and what was said about
// it.
//
// It is both a view of its own — `git work view show '{"id":"…"}'` — and what
// enter opens from the list, because they are the same page and there is no
// reason for two.
type showPage struct {
	repo *cache.RepoCache

	id    string
	order []string

	snapshot *issue.Snapshot
	log      []cmdjson.IssueOperation

	// history says the lower pane is the op log rather than the comments.
	history bool

	// choosing is the picker `e` opens over the fields, before the editor.
	choosing *picker
	editor   *editor
	comment  *commentBox
	helping  bool

	offset        int
	width, height int
	status        string
}

func newShowPage(repo *cache.RepoCache, id string, fields []string) (*showPage, error) {
	p := &showPage{repo: repo, id: id, order: fields, width: 80, height: 24}
	if err := p.load(); err != nil {
		return nil, err
	}
	// the id is resolved once, to the whole one, so that a refresh after a
	// prefix was typed still finds the same issue
	p.id = p.snapshot.Id().String()
	return p, nil
}

func (p *showPage) load() error {
	snapshot, err := host.IssueSnapshot(p.repo, p.id)
	if err != nil {
		return err
	}
	p.snapshot = snapshot

	if p.history {
		entries, err := host.IssueLog(p.repo, p.id)
		if err != nil {
			return err
		}
		p.log = entries
	}
	return nil
}

// fieldOrder is which fields are shown, and in which order.
//
// The call may name them. Where it does not, the schema does: the type's
// fields in schema order, which is the order they were authored in. Where
// even that is unknown — a store with no schema, an issue whose type is gone —
// the keys sort, because an arbitrary stable order beats a random one.
func (p *showPage) fieldOrder() []string {
	if len(p.order) > 0 {
		return p.order
	}

	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	if s, err := p.repo.LoadSchema(); err == nil {
		if t, ok := s.Type(typeKey); ok {
			keys := make([]string, 0, len(t.Fields))
			for _, key := range t.FieldKeys() {
				keys = append(keys, key)
			}
			return keys
		}
	}

	return sorted.Keys(p.snapshot.Fields)
}

func (p *showPage) Update(msg tea.Msg) (page, tea.Cmd) {
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

	if p.editor != nil {
		return p.updateEditor(msg)
	}
	if p.comment != nil {
		return p.updateComment(msg)
	}
	return p, nil
}

func (p *showPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case p.helping:
		p.helping = false
		return p, nil
	case p.editor != nil:
		return p.updateEditor(press)
	case p.comment != nil:
		return p.updateComment(press)
	case p.choosing != nil:
		return p.updateChoice(press)
	}

	switch {
	case key.Matches(press, keys.quit), key.Matches(press, keys.back):
		// the list is underneath, where it was; with nothing underneath the
		// stack quits, which is what `git work view show` should do.
		return p, func() tea.Msg { return popMsg{} }

	case key.Matches(press, keys.up):
		p.offset = max(0, p.offset-1)
	case key.Matches(press, keys.down):
		p.offset++
	case key.Matches(press, keys.pageUp):
		p.offset = max(0, p.offset-max(p.height-4, 1))
	case key.Matches(press, keys.pageDn):
		p.offset += max(p.height-4, 1)
	case key.Matches(press, keys.top):
		p.offset = 0

	case key.Matches(press, keys.toggle):
		p.history = !p.history
		if err := p.load(); err != nil {
			p.status = err.Error()
		}
		p.offset = 0

	case key.Matches(press, keys.edit):
		p.startEdit()
	case key.Matches(press, keys.comment):
		p.comment = newCommentBox(p.id, p.width, p.height/3)
	case key.Matches(press, keys.help):
		p.helping = true
	}

	return p, nil
}

// startEdit asks which field first: a page shows many and the cursor is a
// scroll position, not a selection.
func (p *showPage) startEdit() {
	keys := p.fieldOrder()
	items := make([]choice, 0, len(keys))
	for _, key := range keys {
		items = append(items, choice{label: key, dim: plain(p.snapshot.Fields[key]), value: key})
	}
	p.choosing = newPicker(items, "")
}

func (p *showPage) updateChoice(press tea.KeyPressMsg) (page, tea.Cmd) {
	switch {
	case key.Matches(press, keys.cancel):
		p.choosing = nil
	case key.Matches(press, keys.up):
		p.choosing.cursor = max(0, p.choosing.cursor-1)
	case key.Matches(press, keys.down):
		p.choosing.cursor = min(len(p.choosing.items)-1, p.choosing.cursor+1)
	case key.Matches(press, keys.open):
		fieldKey := p.choosing.items[p.choosing.cursor].value
		p.choosing = nil
		p.openEditor(fieldKey)
	}
	return p, nil
}

func (p *showPage) openEditor(fieldKey string) {
	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	current, _ := decodeValue(p.snapshot.Fields[fieldKey])

	kind, known := fieldKind(p.repo, typeKey, fieldKey)
	if known && kind == schema.KindBool {
		was, _ := current.(bool)
		p.write(fieldKey, issue.MustValue(!was))
		return
	}

	ed, refusal, err := editable(p.repo, typeKey, fieldKey, current)
	switch {
	case err != nil:
		p.status = err.Error()
	case refusal != "":
		p.status = refusal
	default:
		ed.issueId = p.id
		p.editor = ed
		p.status = ""
	}
}

func (p *showPage) updateEditor(msg tea.Msg) (page, tea.Cmd) {
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
	p.write(ed.key, value)
	return p, nil
}

func (p *showPage) write(fieldKey string, value issue.Value) {
	if _, err := host.IssueSet(p.repo, p.id, map[string]issue.Value{fieldKey: value}, false); err != nil {
		p.status = err.Error()
		return
	}
	p.status = fieldKey + " set"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
}

func (p *showPage) updateComment(msg tea.Msg) (page, tea.Cmd) {
	done, body, cmd := p.comment.Update(msg)
	if !done {
		return p, cmd
	}

	p.comment = nil
	if body == "" {
		return p, nil
	}

	if _, err := host.IssueCommentNew(p.repo, p.id, body); err != nil {
		p.status = err.Error()
		return p, nil
	}
	p.status = "commented"
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	return p, nil
}

func (p *showPage) View() string {
	if p.helping {
		return strings.Join(helpLines(), "\n")
	}

	body := p.lines()

	var bottom []string
	switch {
	case p.editor != nil:
		bottom = p.editor.View(p.width)
	case p.comment != nil:
		bottom = p.comment.View(p.width)
	case p.choosing != nil:
		bottom = append([]string{styleHeader.Render("which field?")}, pickerLines(p.choosing, p.width)...)
	}
	bottom = append(bottom, p.statusLine())

	room := max(p.height-len(bottom), 1)
	if p.offset > len(body)-room {
		p.offset = max(0, len(body)-room)
	}

	lines := make([]string, 0, p.height)
	for at := p.offset; at < min(p.offset+room, len(body)); at++ {
		lines = append(lines, body[at])
	}
	for len(lines) < room {
		lines = append(lines, "")
	}

	return strings.Join(append(lines, bottom...), "\n")
}

// lines is the whole page, unwindowed: the header, the fields, then either
// what was said about the issue or what was done to it.
func (p *showPage) lines() []string {
	snap := p.snapshot
	typeKey, _ := issue.String(snap.Fields[schema.TypeKey])

	head := snap.Id().Human() + "  " + snap.Title()
	if typeKey != "" {
		head += "  " + styleDim.Render("("+typeKey+")")
	}

	lines := []string{styleHeader.Render(fit(head, p.width)), ""}

	for _, key := range p.fieldOrder() {
		if key == schema.TitleKey {
			continue
		}
		lines = append(lines, fit(fmt.Sprintf("%s: %s", pad(key, 14), plain(snap.Fields[key])), p.width))
	}
	lines = append(lines, "")

	if p.history {
		lines = append(lines, styleHeader.Render("history"))
		for _, entry := range p.log {
			when := time.Unix(entry.UnixTime, 0).Format("2006-01-02 15:04")
			lines = append(lines, fit(fmt.Sprintf("%s  %s  %-14s %s",
				entry.HumanId, when, entry.Type, opSummary(entry)), p.width))
		}
		return lines
	}

	for at, comment := range snap.Comments {
		header := fmt.Sprintf("%s  %s", comment.Author.DisplayName(), comment.FormatTimeRel())
		if at == 0 {
			// the first comment is the issue's body, the one it always has
			header = styleDim.Render(header)
		} else {
			lines = append(lines, "")
			header = styleHeader.Render(header)
		}
		lines = append(lines, header)
		for _, line := range strings.Split(comment.Message, "\n") {
			lines = append(lines, fit(line, p.width))
		}
	}

	return lines
}

func (p *showPage) statusLine() string {
	pane := "comments"
	if p.history {
		pane = "history"
	}
	left := p.status
	if left == "" {
		left = "? for keys"
	}
	return styleStatus.Render(fit(fmt.Sprintf("%s · %s · %s", left, p.id[:7], pane), p.width))
}

// opSummary is one operation in one line: what it touched, not its whole JSON.
func opSummary(entry cmdjson.IssueOperation) string {
	var op struct {
		Key     string `json:"key"`
		Message string `json:"message"`
		Title   string `json:"title"`
	}
	if err := decodeInto(entry.Op, &op); err != nil {
		return ""
	}

	switch {
	case op.Key != "":
		return op.Key
	case op.Title != "":
		return op.Title
	case op.Message != "":
		return truncate(strings.SplitN(op.Message, "\n", 2)[0], 60)
	default:
		return ""
	}
}

// pickerLines draws a picker where a page owns one directly.
func pickerLines(p *picker, width int) []string {
	lines := make([]string, 0, len(p.items))
	for at, item := range p.items {
		marker := "  "
		label := pad(item.label, 16)
		if at == p.cursor {
			marker = "> "
			label = styleCursor.Render(label)
		}
		lines = append(lines, fit(marker+label+styleDim.Render(truncate(item.dim, 40)), width))
	}
	return lines
}
