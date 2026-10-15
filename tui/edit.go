package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
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

	// link is the link picker's state, on the editor space on the links row
	// opens (links.go); nil on every other
	link *linkEdit
}

// picker is a list of choices: an enum's values, the repository's people, or
// the issues a relation may name (doc/design/app.md, A5).
//
// It opens with its input focused: typing narrows at once, the arrows move,
// enter chooses, the first esc clears the text and the second cancels. A
// static picker narrows its own choices (choice.matches); the finder, a
// picker over the issues, asks the search again as the text changes
// (finder.go), since a relation's choices are every issue of its target
// types, a list too long to hold drawn.
type picker struct {
	input textinput.Model

	// fixed are a static picker's choices; on the finder, the ones after the
	// search's, such as (none), narrowed as a static picker's are
	fixed []choice
	// search is the finder's: the choices the text finds, and how many more
	// it found than it hands back
	search func(text string) ([]choice, int)

	// items are the choices the text keeps, in order, and more the ones the
	// finder found and does not hand back
	items  []choice
	more   int
	cursor int

	// current is the value the field holds now, where the cursor rests while
	// nothing is typed
	current string

	// mark is what a current choice is marked with: "● current" unless a
	// picker says otherwise, as a membership picker's "● linked"
	mark string
}

// choice is one row of a picker: what it is, and what it writes.
type choice struct {
	label string
	dim   string
	value string

	// current marks the value the field holds now.
	current bool
	// also is text it matches on that it does not draw: a person's login
	also string
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
		finder, err := relationFinder(repo, issueId, typeKey, fieldKey, plainValue(current))
		if err != nil {
			return nil, "", err
		}
		// the picker opens on the current value: going to it is enter on
		// the cell, and never a choice here (2026-10-02)
		return &editor{issueId: issueId, key: fieldKey, kind: kind, picker: finder}, "", nil

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
		// matched on name, login and email (doc/design/app.md, A5)
		out = append(out, choice{label: host.UserName(repo, id.String()), dim: identity.Email(), also: identity.Login(), value: id.String()})
	}
	out = append(out, choice{label: "(nobody)", dim: "clear", value: ""})
	return out, nil
}

// relationFinder is a relation's picker: the finder narrowed to the field's
// target_types, or every type where it names none, without the issue itself,
// and then (none), which clears it. The current value is listed even where
// the narrowing would leave it out — archived, or of a type no longer a
// target — since the picker opens on what the cell showed, whatever it is
// (doc/design/include-archive.md, I5).
func relationFinder(repo *cache.RepoCache, issueId, typeKey, fieldKey, current string) (*picker, error) {
	s, err := repo.LoadSchema()
	if err != nil {
		return nil, err
	}
	field, ok := s.Field(typeKey, fieldKey)
	if !ok {
		return nil, fmt.Errorf("no field %s on %s", fieldKey, typeKey)
	}
	scope := finderScope{types: field.TargetTypes, exclude: issueId, current: current}
	none := []choice{{label: "(none)", dim: "clear", value: ""}}
	return newFinder(repo, scope, none, current), nil
}

// newPicker is a static picker over its choices, on the current value.
func newPicker(items []choice, current string) *picker {
	p := &picker{input: pickerInput(), fixed: items, current: current}
	p.refresh()
	return p
}

// pickerInput is a picker's line, focused from the start.
func pickerInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "type to narrow"
	input.SetWidth(40)
	input.Focus()
	return input
}

// paste puts pasted text in the widget: in the line for a text, and for a
// list of choices in its input, which narrows to what it names — an enum's
// value or label, a person, an issue's id whole or short. Nothing is written
// until enter, so a paste that landed on the wrong field is an esc away from
// never having happened.
func (e *editor) paste(text string) string {
	text = strings.TrimSpace(text)
	if e.picker == nil {
		e.input.SetValue(text)
		e.input.CursorEnd()
		return ""
	}
	// a paste that names a choice outright is that choice, and the text
	// stays empty, so esc still cancels
	if e.picker.text() == "" {
		for at, item := range e.picker.items {
			if item.value != "" && (strings.EqualFold(item.value, text) || strings.EqualFold(item.label, text)) {
				e.picker.cursor = at
				return ""
			}
		}
	}
	e.picker.setText(text)
	if len(e.picker.items) == 0 {
		e.picker.setText("")
		return fmt.Sprintf("%s: no value %q", e.key, text)
	}
	return ""
}

// Update runs the widget, and says when the user is done with it.
func (e *editor) Update(msg tea.Msg) (done bool, cancelled bool, cmd tea.Cmd) {
	if e.picker != nil {
		return e.picker.Update(msg)
	}
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			return true, true, nil
		case keys.act.matches(press):
			return true, false, nil
		}
	}
	var updated textinput.Model
	updated, cmd = e.input.Update(msg)
	e.input = updated
	return false, false, cmd
}

// Value is what the editor writes, as the store holds it.
func (e *editor) Value() (issue.Value, error) {
	if e.picker != nil {
		chosen, ok := e.picker.chosen()
		if !ok || chosen.value == "" {
			return issue.MustValue(nil), nil
		}
		return issue.StringValue(chosen.value), nil
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
	lines := append([]string{title}, e.picker.lines(width)...)
	if e.picker.text() != "" {
		return append(lines, styleDim.Render("enter saves · esc clears"))
	}
	return append(lines, styleDim.Render("enter saves · esc cancels"))
}

// pickerRows is as many choices as a picker draws at once.
const pickerRows = 10

// Update is a key in the picker: the arrows and the page keys move, enter
// chooses what is under the cursor, esc clears the text and then cancels,
// and anything else is the input's, the choices narrowed again when its text
// changes.
func (p *picker) Update(msg tea.Msg) (chose bool, cancelled bool, cmd tea.Cmd) {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case keys.cancel.matches(press):
			if p.text() != "" {
				p.setText("")
				return false, false, nil
			}
			return true, true, nil
		case keys.act.matches(press):
			if _, ok := p.chosen(); !ok {
				// nothing is under the cursor: the text kept nothing
				return false, false, bell()
			}
			return true, false, nil
		case keys.up.matches(press):
			p.move(-1)
			return false, false, nil
		case keys.down.matches(press):
			p.move(1)
			return false, false, nil
		case press.String() == "pgup":
			p.move(-pickerRows)
			return false, false, nil
		case press.String() == "pgdown":
			p.move(pickerRows)
			return false, false, nil
		}
	}
	was := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != was {
		p.refresh()
	}
	return false, false, cmd
}

// text is what has been typed.
func (p *picker) text() string {
	return p.input.Value()
}

// setText puts text in the input in place of what is there, as a paste or a
// clear does.
func (p *picker) setText(text string) {
	p.input.SetValue(text)
	p.input.CursorEnd()
	p.refresh()
}

// chosen is the choice under the cursor, if the text kept any.
func (p *picker) chosen() (choice, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return choice{}, false
	}
	return p.items[p.cursor], true
}

// refresh narrows the choices to the text: the finder's search first, then
// the fixed choices that match. The cursor goes to the best match, which is
// the first, or with nothing typed back to the current value.
func (p *picker) refresh() {
	text := p.text()
	p.items, p.more = nil, 0
	if p.search != nil {
		p.items, p.more = p.search(text)
	}
	for _, item := range p.fixed {
		if item.matches(text) {
			p.items = append(p.items, item)
		}
	}
	p.cursor = 0
	if text == "" {
		for at, item := range p.items {
			if item.value == p.current {
				p.cursor = at
				break
			}
		}
	}
}

// matches says whether the text narrows to a choice: its label, what is dim
// beside it, what else it is known by, or the start of what it writes, which
// for an issue is its id. Case is ignored.
func (c choice) matches(text string) bool {
	if text == "" {
		return true
	}
	text = strings.ToLower(text)
	return strings.Contains(strings.ToLower(c.label), text) ||
		strings.Contains(strings.ToLower(c.dim), text) ||
		strings.Contains(strings.ToLower(c.also), text) ||
		strings.HasPrefix(strings.ToLower(c.value), text)
}

// move is up and down among the choices the text keeps, stopping at either
// end.
func (p *picker) move(by int) {
	if len(p.items) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+by, 0), len(p.items)-1)
}

// lines are the input, then a window over the choices that follows the
// cursor, so that the page above keeps its rows.
func (p *picker) lines(width int) []string {
	lines := []string{fit("/"+p.input.View(), width)}
	if len(p.items) == 0 {
		return append(lines, styleDim.Render("  (no match)"))
	}
	top := 0
	scroll(&top, p.cursor, pickerRows, len(p.items))
	labelWidth := 24
	for _, item := range p.items {
		labelWidth = max(labelWidth, min(ansi.StringWidth(item.label)+2, width/2))
	}
	for at := top; at < min(top+pickerRows, len(p.items)); at++ {
		item := p.items[at]
		marker := "  "
		// padded before it is styled: a style is escape codes, and they are
		// not columns.
		text := truncate(item.label, labelWidth-2)
		label := pad(text, labelWidth)
		if at == p.cursor {
			marker = "> "
			label = styleCursor.Render(text) + pad("", labelWidth-ansi.StringWidth(text))
		}
		dim := item.dim
		if item.current {
			mark := p.mark
			if mark == "" {
				mark = "● current"
			}
			dim = mark + "  " + dim
		}
		lines = append(lines, fit(marker+label+styleDim.Render(dim), width))
	}
	if p.more > 0 {
		lines = append(lines, styleDim.Render(fit(fmt.Sprintf("  … %d more: type to narrow", p.more), width)))
	}
	return lines
}
