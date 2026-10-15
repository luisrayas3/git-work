package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// A show page's own palette commands (doc/design/app.md A6, A7):
//
//   - `l` is the links row's picker (links.go, link-fields.md L5) reached from
//     anywhere on the page: a link relation, stored or inverse, picked by
//     typing, then the issue in the finder, one add or remove, an inverse
//     written on the other issue. It is that picker, not a second link path,
//     so it offers the link relations only, as the row does.
//   - `u` picks one of the issue's links, across every link relation, stored
//     and inverse, and removes it: one write, an inverse's to the other issue.
//   - `e` changes the spec of the issue's type for this page and the pages it
//     opens: a picker over the relations the schema gives the type, stored
//     and inverse, and `links`, the ones the spec expands marked, where enter
//     toggles one and the page is redrawn at once, the picker staying open
//     until esc. Nothing is written: the changed spec goes into the page's
//     `shows`, whole, so the first line still spells the call that drew it.

// unlinkSep joins a relation and an issue in an unlink choice's value.
const unlinkSep = "\n"

// startUnlink is `u`: every link of the issue, each its relation and the
// issue it links, to pick one to remove.
func (p *showPage) startUnlink() tea.Cmd {
	var items []choice
	for _, rel := range p.links {
		dim := rel.Name
		if !rel.Stored {
			dim += "  inverse"
		}
		for _, id := range rel.ids {
			items = append(items, choice{label: linkLabel(p.repo, id), dim: dim, also: id,
				value: rel.Name + unlinkSep + id})
		}
	}
	if len(items) == 0 {
		p.status = "no link to remove"
		return bell()
	}
	p.editor = &editor{issueId: p.id, key: "unlink", picker: newPicker(items, ""),
		link: &linkEdit{rels: p.links, unlink: true}}
	p.status = ""
	return nil
}

// unlinkChosen removes the link chosen: one write, on this issue for a
// stored relation and on the other one for an inverse (writeLink).
func (p *showPage) unlinkChosen(ed *editor) tea.Cmd {
	name, id, _ := strings.Cut(ed.linkChoice().value, unlinkSep)
	rel := ed.link.relation(name)
	if rel == nil || id == "" {
		return bell()
	}
	said, err := writeLink(p.repo, p.id, rel, choice{value: id, current: true})
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	p.status = said
	if err := p.load(); err != nil {
		p.status = err.Error()
	}
	return nil
}

// shownSpec is the spec the page is drawn by, and its issue's type.
func (p *showPage) shownSpec() (*view.Spec, *schema.Schema, string) {
	s, _ := p.repo.LoadSchema()
	typeKey, _ := issue.String(p.snapshot.Fields[schema.TypeKey])
	spec, _ := view.SpecFor(p.call.Raw("shows"), s, typeKey)
	return spec, s, typeKey
}

// startExpand is `e`: the relations the type has, stored and inverse, then
// links, the ones the spec expands marked, the cursor on at.
func (p *showPage) startExpand(at string) tea.Cmd {
	spec, s, typeKey := p.shownSpec()
	var t *schema.Type
	ok := false
	if s != nil {
		t, ok = s.Type(typeKey)
	}
	if !ok {
		p.status = "no type " + typeKey + " in the schema to expand by"
		return bell()
	}
	expanded := spec.ExpandNames()
	var items []choice
	for _, name := range append(view.TypeRelations(s, typeKey), schema.LinksKey) {
		dim := "inverse"
		if name == schema.LinksKey {
			dim = "a table per link relation"
		} else if _, stored := t.Field(name); stored {
			dim = "stored"
		}
		items = append(items, choice{label: name, dim: dim, value: name, current: slices.Contains(expanded, name)})
	}
	picker := newPicker(items, at)
	picker.mark = "● expanded"
	p.editor = &editor{issueId: p.id, key: "expand " + typeKey, picker: picker, expand: true}
	return nil
}

// expandChosen toggles the relation chosen in the spec, redraws the page by
// it, and opens the picker again on the same relation, so that several are
// toggled in one go.
func (p *showPage) expandChosen(ed *editor) tea.Cmd {
	relation := ed.linkChoice().value
	spec, s, typeKey := p.shownSpec()
	raw, err := view.ToggleExpand(spec, relation)
	if err == nil {
		var changed *view.Spec
		if changed, err = view.ParseSpec(raw); err == nil {
			err = view.CheckSpec(changed, s, typeKey)
		}
	}
	var shows []byte
	if err == nil {
		shows, err = view.WithSpec(p.call.Raw("shows"), typeKey, raw)
	}
	if err == nil {
		err = p.applyShows(shows)
	}
	if err != nil {
		p.status = err.Error()
		return bell()
	}
	if slices.Contains(spec.ExpandNames(), relation) {
		p.status = relation + " no longer expanded"
	} else {
		p.status = "expanded " + relation
	}
	return p.startExpand(relation)
}
