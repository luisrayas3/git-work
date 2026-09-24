package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
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

// picker is a list of choices: an enum's values, or the repository's people.
type picker struct {
	items  []choice
	cursor int
}

// choice is one row of a picker: what it is, and what it writes.
type choice struct {
	label string
	dim   string
	value string
}

// editable decides what editing a field means here, given the live schema.
//
// It returns a refusal rather than an error for the kinds this renderer does
// not edit yet: a set-valued field is `git work issue add`/`remove`, and
// saying so is more use than a widget that can only replace the whole set.
func editable(repo *cache.RepoCache, typeKey, fieldKey string, current any) (*editor, string, error) {
	kind, ok := fieldKind(repo, typeKey, fieldKey)
	if !ok {
		return nil, fmt.Sprintf("no schema for %s: %s is not a field this renderer knows", typeKey, fieldKey), nil
	}

	switch kind {
	case schema.KindEnum, schema.KindOrdinalEnum:
		values, err := enumChoices(repo, typeKey, fieldKey)
		if err != nil {
			return nil, "", err
		}
		return &editor{key: fieldKey, kind: kind, picker: newPicker(values, plainValue(current))}, "", nil

	case schema.KindIdentity:
		people, err := identityChoices(repo)
		if err != nil {
			return nil, "", err
		}
		return &editor{key: fieldKey, kind: kind, picker: newPicker(people, plainValue(current))}, "", nil

	case schema.KindText, schema.KindNumber, schema.KindDate:
		input := textinput.New()
		input.SetValue(plainValue(current))
		input.SetWidth(40)
		input.Focus()
		// the title is the one field that can not be cleared: no surface can
		// show an issue without one (entities/issue).
		return &editor{key: fieldKey, kind: kind, input: input, clearable: fieldKey != schema.TitleKey}, "", nil

	default:
		return nil, fmt.Sprintf("edit %s with git work issue add/remove for now", fieldKey), nil
	}
}

// fieldKind reads a field's kind off the live schema.
//
// The three built-ins answer even where no type is defined, so a title is
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
	out = append(out, choice{label: "(none)", dim: "clears the field", value: ""})
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
		out = append(out, choice{label: identity.Name(), dim: identity.Email(), value: id.String()})
	}
	out = append(out, choice{label: "(nobody)", dim: "clears the field", value: ""})
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

// Update runs the widget, and says when the user is done with it.
func (e *editor) Update(msg tea.Msg) (done bool, cancelled bool, cmd tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch {
		case key.Matches(press, keys.cancel):
			return true, true, nil
		case key.Matches(press, keys.open):
			return true, false, nil
		}
	}

	if e.picker != nil {
		if ok {
			switch {
			case key.Matches(press, keys.up):
				e.picker.cursor = max(0, e.picker.cursor-1)
			case key.Matches(press, keys.down):
				e.picker.cursor = min(len(e.picker.items)-1, e.picker.cursor+1)
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
		return []string{title, fit(e.input.View(), width), styleDim.Render("enter writes it, esc cancels")}
	}

	lines := []string{title}
	for at, item := range e.picker.items {
		marker := "  "
		// padded before it is styled: a style is escape codes, and they are
		// not columns.
		label := pad(item.label, 24)
		if at == e.picker.cursor {
			marker = "> "
			label = styleCursor.Render(label)
		}
		lines = append(lines, marker+label+styleDim.Render(item.dim))
	}
	return append(lines, styleDim.Render("enter writes it, esc cancels"))
}
