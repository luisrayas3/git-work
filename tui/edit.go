package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// editor is the widget one field is edited with.
//
// Which widget it is comes from the schema, not from the value: the field's
// kind is what says whether a list of choices or a line of text is the honest
// thing to show, and the schema is the only place that knows (bb9e89e).
type editor struct {
	// key is the field being edited, and issueId the issue it is on.
	issueId string
	key     string
	kind    schema.Kind

	// one of these two is live, by kind
	input  textinput.Model
	picker *picker

	// cleared says the field is to be set to null, which is what an empty
	// text box means for everything but the title.
	clearable bool
}

// picker is a list of choices: an enum's values, the repository's people, or
// the issues a relation may name.
//
// `/` narrows it the way `/` narrows a list: typed text keeps the choices it
// is in, enter keeps the narrowing and goes back to choosing, esc drops it.
// A relation's choices are every issue of its target types, which is a list
// too long to walk, so this is what makes that picker usable.
type picker struct {
	items  []choice
	cursor int

	query     string
	narrowing *textinput.Model
}

// choice is one row of a picker: what it is, and what it writes.
type choice struct {
	label string
	dim   string
	value string

	// current marks the value the field holds now.
	current bool
}

// editable decides what editing a field means here, given the live schema.
//
// It returns a refusal rather than an error for the kinds this renderer does
// not edit yet: a set-valued field is `git work issue add`/`remove`, and
// saying so is more use than a widget that can only replace the whole set.
func editable(repo *cache.RepoCache, issueId, typeKey, fieldKey string, current any) (*editor, string, error) {
	kind, ok := fieldKind(repo, typeKey, fieldKey)
	if !ok {
		return nil, fmt.Sprintf("%s: not a field of %s", fieldKey, typeKey), nil
	}

	switch kind {
	case schema.KindEnum, schema.KindOrdinalEnum:
		values, err := enumChoices(repo, typeKey, fieldKey)
		if err != nil {
			return nil, "", err
		}
		return &editor{issueId: issueId, key: fieldKey, kind: kind, picker: newPicker(values, plainValue(current))}, "", nil

	case schema.KindIdentity:
		people, err := identityChoices(repo)
		if err != nil {
			return nil, "", err
		}
		return &editor{issueId: issueId, key: fieldKey, kind: kind, picker: newPicker(people, plainValue(current))}, "", nil

	case schema.KindRelation:
		targets, err := relationChoices(repo, issueId, typeKey, fieldKey, plainValue(current))
		if err != nil {
			return nil, "", err
		}
		// the picker opens on the current value: going to it is enter on
		// the cell, and never a choice here (2026-10-02)
		return &editor{issueId: issueId, key: fieldKey, kind: kind, picker: newPicker(targets, plainValue(current))}, "", nil

	case schema.KindMultiRelation:
		// changing the set is add and remove, two host calls, so two
		// commits, which one edit must not be (doc/design/terminal-renderer.md,
		// 2026-09-29); enter on a line still goes to its issue
		return nil, fmt.Sprintf("%s: use git work issue add/remove", fieldKey), nil

	case schema.KindText, schema.KindNumber, schema.KindDate:
		input := textinput.New()
		input.SetValue(plainValue(current))
		input.SetWidth(40)
		input.Focus()
		// the title is the one field that can not be cleared: no surface can
		// show an issue without one (entities/issue).
		return &editor{issueId: issueId, key: fieldKey, kind: kind, input: input, clearable: fieldKey != schema.TitleKey}, "", nil

	default:
		return nil, fmt.Sprintf("%s: use git work issue add/remove", fieldKey), nil
	}
}

// fieldKind reads a field's kind off the live schema.
//
// The four built-ins answer even where no type is defined, so a title is
// editable in a store that has no schema yet, which is the bootstrap state.
func fieldKind(repo *cache.RepoCache, typeKey, fieldKey string) (schema.Kind, bool) {
	s, err := repo.LoadSchema()
	if err == nil {
		if t, ok := s.Type(typeKey); ok {
			if field, ok := t.Field(fieldKey); ok {
				return field.Kind, true
			}
		}
	}
	return schema.BuiltinKind(fieldKey)
}

func enumChoices(repo *cache.RepoCache, typeKey, fieldKey string) ([]choice, error) {
	s, err := repo.LoadSchema()
	if err != nil {
		return nil, err
	}
	if fieldKey == schema.TypeKey {
		// the type's values are the types, and there is no clearing it
		out := make([]choice, 0, len(s.Types))
		for _, key := range s.TypeKeys() {
			label := s.Types[key].Name
			if label == "" {
				label = key
			}
			out = append(out, choice{label: label, dim: key, value: key})
		}
		return out, nil
	}
	field, ok := s.Field(typeKey, fieldKey)
	if !ok {
		return nil, fmt.Errorf("no field %s on %s", fieldKey, typeKey)
	}

	out := make([]choice, 0, len(field.Values)+1)
	for _, value := range field.Values {
		label := value.Name
		if label == "" {
			label = value.Id
		}
		out = append(out, choice{label: label, dim: value.Id, value: value.Id})
	}
	// Clearing is a choice like any other, because `set` with a null clears
	// a field, and a picker that could not do it would send the user to the
	// shell for the one edit a picker is best at.
	out = append(out, choice{label: "(none)", dim: "clear", value: ""})
	return out, nil
}

func identityChoices(repo *cache.RepoCache) ([]choice, error) {
	ids := repo.Identities().AllIds()

	out := make([]choice, 0, len(ids)+1)
	for _, id := range ids {
		identity, err := repo.Identities().Resolve(id)
		if err != nil {
			return nil, err
		}
		// the label is the name a cell draws the value as, so the picker
		// opens on the one the cell showed
		out = append(out, choice{label: host.UserName(repo, id.String()), dim: identity.Email(), value: id.String()})
	}
	out = append(out, choice{label: "(nobody)", dim: "clear", value: ""})
	return out, nil
}

// relationChoices are the issues a relation may name: those of its
// target_types, or every issue where it names none, last edited first as a
// list is, each drawn as a link is — short id and title — and then (none),
// which clears it. The issue itself is left out, and the candidates are the
// unarchived, read from the same helper the nesting and show's children read
// (doc/design/include-archive.md, I5); an archived issue is listed only when
// it is the value already there: the picker opens on what the cell showed,
// whatever it is.
func relationChoices(repo *cache.RepoCache, issueId, typeKey, fieldKey, current string) ([]choice, error) {
	s, err := repo.LoadSchema()
	if err != nil {
		return nil, err
	}
	field, ok := s.Field(typeKey, fieldKey)
	if !ok {
		return nil, fmt.Errorf("no field %s on %s", fieldKey, typeKey)
	}
	allowed := map[string]bool{}
	for _, target := range field.TargetTypes {
		allowed[target] = true
	}

	type candidate struct {
		id, typeKey string
		edited      int64
	}
	var found []candidate
	all, order := allIssues(repo, false)
	for _, id := range order {
		if id == issueId {
			continue
		}
		fields, _ := all[id]["fields"].(map[string]any)
		targetType := host.StringOr(fields[schema.TypeKey], "")
		if id != current && len(allowed) > 0 && !allowed[targetType] {
			continue
		}
		edit, _ := all[id]["edit_time"].(map[string]any)
		edited, _ := edit["timestamp"].(float64)
		found = append(found, candidate{id: id, typeKey: targetType, edited: int64(edited)})
	}
	// the value already there is listed even when the input leaves it out
	if current != "" && current != issueId && all[current] == nil {
		if excerpt, err := repo.Issues().ResolveExcerpt(entity.Id(current)); err == nil {
			targetType, _ := issue.String(excerpt.Fields[schema.TypeKey])
			found = append(found, candidate{id: current, typeKey: targetType, edited: excerpt.EditUnixTime})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].edited != found[j].edited {
			return found[i].edited > found[j].edited
		}
		return found[i].id < found[j].id
	})

	var out []choice
	for _, c := range found {
		out = append(out, choice{label: linkLabel(repo, c.id), dim: c.typeKey, value: c.id, current: c.id == current})
	}
	out = append(out, choice{label: "(none)", dim: "clear", value: ""})
	return out, nil
}

func newPicker(items []choice, current string) *picker {
	p := &picker{items: items}
	for at, item := range items {
		if item.value == current {
			p.cursor = at
		}
	}
	return p
}

// paste puts pasted text in the widget: in the line for a text, on the value
// it names for a list of choices. Nothing is written until enter, so a paste
// that landed on the wrong field is an esc away from never having happened.
func (e *editor) paste(text string) string {
	text = strings.TrimSpace(text)
	if e.picker == nil {
		e.input.SetValue(text)
		e.input.CursorEnd()
		return ""
	}
	for at, item := range e.picker.items {
		if item.value != "" && (strings.EqualFold(item.value, text) || strings.EqualFold(item.label, text)) {
			e.picker.cursor = at
			return ""
		}
	}
	// an issue is pasted as its id, whole or short, as every command takes it
	if e.kind == schema.KindRelation && len(text) >= 4 {
		for at, item := range e.picker.items {
			if item.value != "" && strings.HasPrefix(item.value, strings.ToLower(text)) {
				e.picker.cursor = at
				return ""
			}
		}
	}
	return fmt.Sprintf("%s: no value %q", e.key, text)
}

// Update runs the widget, and says when the user is done with it.
func (e *editor) Update(msg tea.Msg) (done bool, cancelled bool, cmd tea.Cmd) {
	if e.picker != nil && e.picker.narrowing != nil {
		return false, false, e.picker.narrow(msg)
	}
	press, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch {
		case keys.cancel.matches(press) && e.picker != nil && e.picker.query != "":
			// out of the narrowing first, out of the picker after that,
			// as back is on a list
			e.picker.setQuery("")
			return false, false, nil
		case keys.cancel.matches(press):
			return true, true, nil
		case keys.act.matches(press):
			if e.picker != nil && !e.picker.matches(e.picker.cursor) {
				// nothing is under the cursor: the narrowing hides it all
				return false, false, bell()
			}
			return true, false, nil
		}
	}

	if e.picker != nil {
		if ok {
			switch {
			case keys.up.matches(press):
				e.picker.move(-1)
			case keys.down.matches(press):
				e.picker.move(1)
			case keys.filter.matches(press):
				e.picker.startNarrowing()
			}
		}
		return false, false, nil
	}

	var updated textinput.Model
	updated, cmd = e.input.Update(msg)
	e.input = updated
	return false, false, cmd
}

// Value is what the editor writes, as the store holds it.
func (e *editor) Value() (issue.Value, error) {
	if e.picker != nil {
		chosen := e.picker.items[e.picker.cursor].value
		if chosen == "" {
			return issue.MustValue(nil), nil
		}
		return issue.StringValue(chosen), nil
	}

	text := strings.TrimSpace(e.input.Value())
	if text == "" && e.clearable {
		return issue.MustValue(nil), nil
	}

	if e.kind == schema.KindNumber {
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%s is a number: %s is not one", e.key, text)
		}
		return issue.MustValue(number), nil
	}

	return issue.StringValue(text), nil
}

// View draws the widget over the bottom of the page.
func (e *editor) View(width int) []string {
	title := styleHeader.Render(e.key)

	if e.picker == nil {
		return []string{title, fit(e.input.View(), width), styleDim.Render("enter saves · esc cancels")}
	}

	lines := []string{title}
	switch {
	case e.picker.narrowing != nil:
		lines = append(lines, fit("/"+e.picker.narrowing.View(), width))
	case e.picker.query != "":
		lines = append(lines, styleDim.Render(fit("/"+e.picker.query, width)))
	}

	visible := e.picker.visible()
	if len(visible) == 0 {
		lines = append(lines, styleDim.Render("  (no match)"))
	}
	// a long list is a window over it that follows the cursor, so that the
	// page above keeps its rows
	at := 0
	for i, index := range visible {
		if index == e.picker.cursor {
			at = i
		}
	}
	top := 0
	scroll(&top, at, pickerRows, len(visible))
	labelWidth := 24
	for _, index := range visible {
		labelWidth = max(labelWidth, min(ansi.StringWidth(e.picker.items[index].label)+2, width/2))
	}
	for _, index := range visible[top:min(top+pickerRows, len(visible))] {
		item := e.picker.items[index]
		marker := "  "
		// padded before it is styled: a style is escape codes, and they are
		// not columns.
		text := truncate(item.label, labelWidth-2)
		label := pad(text, labelWidth)
		switch {
		case index == e.picker.cursor:
			marker = "> "
			label = styleCursor.Render(text) + pad("", labelWidth-ansi.StringWidth(text))
		}
		dim := item.dim
		if item.current {
			dim = "● current  " + dim
		}
		lines = append(lines, fit(marker+label+styleDim.Render(dim), width))
	}
	if e.picker.narrowing != nil {
		return append(lines, styleDim.Render("enter keeps · esc clears"))
	}
	return append(lines, styleDim.Render("enter saves · esc cancels"))
}

// pickerRows is as many choices as a picker draws at once.
const pickerRows = 10

// matches says whether the choice at an index is one the narrowing keeps: its
// label, what is dim beside it, or the start of what it writes, which for an
// issue is its id.
func (p *picker) matches(at int) bool {
	if at < 0 || at >= len(p.items) {
		return false
	}
	item := p.items[at]
	if p.query == "" {
		return true
	}
	query := strings.ToLower(p.query)
	return strings.Contains(strings.ToLower(item.label), query) ||
		strings.Contains(strings.ToLower(item.dim), query) ||
		strings.HasPrefix(strings.ToLower(item.value), query)
}

// visible are the indexes of the choices the narrowing keeps, in order.
func (p *picker) visible() []int {
	out := make([]int, 0, len(p.items))
	for at := range p.items {
		if p.matches(at) {
			out = append(out, at)
		}
	}
	return out
}

// move is up and down among the choices the narrowing keeps.
func (p *picker) move(by int) {
	for at := p.cursor + by; at >= 0 && at < len(p.items); at += by {
		if p.matches(at) {
			p.cursor = at
			return
		}
	}
}

// setQuery narrows, and keeps the cursor on a choice that is still there.
func (p *picker) setQuery(query string) {
	p.query = query
	if p.matches(p.cursor) {
		return
	}
	if visible := p.visible(); len(visible) > 0 {
		p.cursor = visible[0]
	}
}

func (p *picker) startNarrowing() {
	input := textinput.New()
	input.SetValue(p.query)
	input.SetWidth(40)
	input.CursorEnd()
	input.Focus()
	p.narrowing = &input
}

// narrow is a key typed while narrowing: text is the query, up and down
// still move, enter keeps the narrowing and esc drops it.
func (p *picker) narrow(msg tea.Msg) tea.Cmd {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			p.narrowing = nil
			p.setQuery("")
			return nil
		case keys.act.matches(press):
			p.narrowing = nil
			return nil
		case press.String() == "up" || press.String() == "ctrl+p":
			p.move(-1)
			return nil
		case press.String() == "down" || press.String() == "ctrl+n":
			p.move(1)
			return nil
		}
	}
	updated, cmd := p.narrowing.Update(msg)
	p.narrowing = &updated
	p.setQuery(updated.Value())
	return cmd
}
