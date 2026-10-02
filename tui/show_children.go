package tui

import (
	"slices"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// Children on show: the issues that point at the shown one, a section each
// (doc/design/terminal-renderer.md, Show, Children; Luis, 2026-09-29).
//
// A section is more rows of the fields table, after the stored fields: the
// other side of a relation is drawn the way a relation is, a line per issue,
// each a link, so the cursor stands on it and enter follows it through the
// table's own handling, and copy copies its id. The rows are derived, never
// stored, so they are not a field: enter on an empty section rings, and so
// does space, because nothing edits them.

// newShowView is the show page a call describes, `children` included: the
// entry point from the command and from a flow, where the list's enter opens
// a bare page with newShowPage.
func newShowView(repo *cache.RepoCache, call *view.Call) (*showPage, error) {
	var sections []view.Children
	if call.Has("children") {
		s, err := repo.LoadSchema()
		if err != nil {
			return nil, err
		}
		sections, err = view.ResolveChildren(s, call.ChildList())
		if err != nil {
			return nil, err
		}
	}

	p, err := newShowPage(repo, call.String("id"), call.Strings("fields"))
	if err != nil || len(sections) == 0 {
		return p, err
	}
	p.children = sections
	p.call.Args["children"] = call.Raw("children")
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// childRows is every section, a row per issue that points at this one, in
// the store's order, the archived left out as a list leaves them out; a
// section with none is one row saying so, because an empty section that is
// not drawn reads as a section that was never asked for.
func (p *showPage) childRows() []tableRow {
	if len(p.children) == 0 {
		return nil
	}
	all, order := allIssues(p.repo)
	known := newKinds(p.repo)

	var out []tableRow
	for _, section := range p.children {
		var ids []string
		for _, id := range order {
			fields, _ := all[id]["fields"].(map[string]any)
			typeKey := host.StringOr(fields[schema.TypeKey], "")
			for _, key := range section.Sources[typeKey] {
				if slices.Contains(linkIds(fields[key]), p.id) {
					ids = append(ids, id)
					break
				}
			}
		}

		if len(ids) == 0 {
			out = append(out, tableRow{key: section.Heading, label: "(none)", first: true, derived: true})
			continue
		}
		for at, id := range ids {
			label := linkLabel(p.repo, id)
			fields, _ := all[id]["fields"].(map[string]any)
			typeKey := host.StringOr(fields[schema.TypeKey], "")
			var extra []string
			for _, key := range section.Fields {
				value := fields[key]
				text := known.cellText(typeKey, key, value)
				if isRelation(known.of(typeKey, key)) {
					text = linkText(p.repo, linkIds(value))
				}
				if text != "" {
					extra = append(extra, text)
				}
			}
			if len(extra) > 0 {
				label += " · " + strings.Join(extra, " · ")
			}
			out = append(out, tableRow{key: section.Heading, label: label, first: at == 0, link: id, derived: true})
		}
	}
	return out
}
