package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// Link fields (doc/design/link-fields.md): a relation marked `link: true` —
// `blocks`, `clones`, the fields a Jira link type becomes — is not a row of
// show's fields table. Every link of the issue is folded into one row,
// `links`, a count and then each relation with entries and its own count,
// at the place the first link field would have had (L3). The issues
// themselves are side tables, one per relation with entries, wherever a
// spec's `expand` names `links` (L4). Enter on the row goes to those tables,
// and space opens a picker of the link relations, stored and inverse, whose
// choice opens the relation's membership picker; a write there is one add or
// one remove, on this issue for a stored relation and on the other one for
// an inverse, which is that issue's stored field (L5).

// linkRel is one link relation of an issue, with what it holds.
type linkRel struct {
	schema.LinkRelation
	// ids are the issues it links to: a stored field's targets as the issue
	// holds them, or the unarchived issues whose stored field names this one
	ids []string
}

// issueLinks reads one issue's links: its stored link fields off its own
// fields, and every inverse off the issues pointing at it. A draft (id "")
// has no inverses: nothing can point at an issue that does not exist yet.
func issueLinks(repo *cache.RepoCache, typeKey, id string, fields map[string]issue.Value) []linkRel {
	s, err := repo.LoadSchema()
	if err != nil {
		return nil
	}
	relations := s.Links(typeKey)
	out := make([]linkRel, 0, len(relations))
	inverse := false
	for _, relation := range relations {
		rel := linkRel{LinkRelation: relation}
		if relation.Stored {
			value, _ := decodeValue(fields[relation.Name])
			rel.ids = linkIds(value)
		} else {
			if id == "" {
				continue
			}
			inverse = true
		}
		out = append(out, rel)
	}
	if !inverse {
		return out
	}

	all, order := allIssues(repo, false)
	for _, other := range order {
		if other == id {
			continue
		}
		otherFields, _ := all[other]["fields"].(map[string]any)
		otherType := host.StringOr(otherFields[schema.TypeKey], "")
		for at := range out {
			if out[at].Stored {
				continue
			}
			source, ok := out[at].Sources[otherType]
			if ok && slices.Contains(linkIds(otherFields[source.Key]), id) {
				out[at].ids = append(out[at].ids, other)
			}
		}
	}
	return out
}

// linksLabel is the `links` row's value: the count, then each relation with
// entries and its own, stored first, or `—` where there is none in either
// direction, so the row says so rather than leaving it in doubt (L3). A link
// field the call's fields draw as its own row is left out of the count.
func linksLabel(rels []linkRel, drawn []string) string {
	total := 0
	var parts []string
	for _, rel := range rels {
		if rel.Stored && slices.Contains(drawn, rel.Name) {
			continue
		}
		if n := len(rel.ids); n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%s %d", rel.Name, n))
		}
	}
	if total == 0 {
		return "—"
	}
	return strings.Join(append([]string{fmt.Sprint(total)}, parts...), " · ")
}

// linkFieldRows lays the `links` row into a fields table: in place of the
// type's link fields, at the first one's position, where the call names no
// fields; where it does, at `links` if it is named, and a link field it
// names is a row of its own. A type with links only by inverse has the row
// last.
//
// keys are the fields in order, and row draws one field: the caller's own
// drawing, which this does not touch.
func linkFieldRows(keys, order []string, rels []linkRel, row func(key string) []tableRow) []tableRow {
	stored := map[string]bool{}
	for _, rel := range rels {
		if rel.Stored {
			stored[rel.Name] = true
		}
	}
	linksRow := tableRow{key: schema.LinksKey, label: linksLabel(rels, order), first: true}

	var out []tableRow
	drawn := false
	for _, key := range keys {
		folded := key == schema.LinksKey || (len(order) == 0 && stored[key])
		if !folded {
			out = append(out, row(key)...)
			continue
		}
		if !drawn {
			out = append(out, linksRow)
			drawn = true
		}
	}
	if !drawn && len(order) == 0 && len(rels) > 0 {
		out = append(out, linksRow)
	}
	return out
}

// linkEdit is the link picker's state on an editor: first the relations,
// then one relation's members.
type linkEdit struct {
	rels []linkRel
	// rel is the relation chosen, nil while the relations are the choices
	rel *linkRel
}

// linkRelationsEditor is space on the `links` row: every link relation,
// stored and inverse, with its count (L5).
func linkRelationsEditor(rels []linkRel) (*editor, string) {
	if len(rels) == 0 {
		return nil, "no link relation for this type"
	}
	items := make([]choice, 0, len(rels))
	for _, rel := range rels {
		dim := fmt.Sprint(len(rel.ids))
		if !rel.Stored {
			dim += "  inverse"
		}
		items = append(items, choice{label: rel.Name, dim: dim, value: rel.Name})
	}
	return &editor{key: schema.LinksKey, picker: newPicker(items, ""), link: &linkEdit{rels: rels}}, ""
}

// linkMembersEditor is the chosen relation's membership picker, the finder
// (doc/design/app.md A5): the issues it may link, the ones it links marked,
// where enter adds or removes the one under the cursor. For a stored link it
// is narrowed to the field's target types; for an inverse, to the types that
// store the relation, since the write is to that issue's field.
func linkMembersEditor(repo *cache.RepoCache, self string, rels []linkRel, rel *linkRel) *editor {
	members := map[string]bool{}
	for _, id := range rel.ids {
		members[id] = true
	}

	scope := finderScope{exclude: self}
	if rel.Stored {
		scope.types = rel.Field.TargetTypes
	} else {
		for typeKey := range rel.Sources {
			scope.types = append(scope.types, typeKey)
		}
		sort.Strings(scope.types)
	}

	picker := newFinder(repo, scope, nil, "")
	// the members are marked, and with nothing typed listed first, so what
	// is linked is in sight
	search := picker.search
	picker.search = func(text string) ([]choice, int) {
		found, more := search(text)
		for at := range found {
			found[at].current = members[found[at].value]
		}
		if text != "" {
			// typed, the surest match comes first, linked or not
			return found, more
		}
		var linked, rest []choice
		for _, item := range found {
			if item.current {
				linked = append(linked, item)
			} else {
				rest = append(rest, item)
			}
		}
		return append(linked, rest...), more
	}
	picker.mark = "● linked"
	picker.refresh()
	return &editor{issueId: self, key: rel.Name, picker: picker, link: &linkEdit{rels: rels, rel: rel}}
}

// linkChoice reads what the link editor's enter chose: the relation, while
// the relations are the choices, or the issue to add or remove.
func (e *editor) linkChoice() choice {
	chosen, _ := e.picker.chosen()
	return chosen
}

// linkRelation finds a relation by name among an editor's.
func (l *linkEdit) relation(name string) *linkRel {
	for at := range l.rels {
		if l.rels[at].Name == name {
			return &l.rels[at]
		}
	}
	return nil
}

// writeLink is one membership change, through the writer every edit goes
// through: on this issue's field for a stored relation, on the other
// issue's stored field for an inverse, one commit to one entity either way
// (L5; app.md A6). It says what it wrote, naming the issue written.
func writeLink(repo *cache.RepoCache, self string, rel *linkRel, target choice) (string, error) {
	written, key, item := self, rel.Name, target.value
	field := rel.Field
	if !rel.Stored {
		otherType := storedType(repo, target.value)
		field = rel.Sources[otherType]
		if field == nil {
			return "", fmt.Errorf("%s: %s has no field whose inverse is %s", rel.Name, human(target.value), rel.Name)
		}
		written, key, item = target.value, field.Key, self
	}

	remove := target.current
	var err error
	switch {
	case field.Kind == schema.KindMultiRelation && remove:
		_, err = host.IssueRemove(repo, written, map[string][]issue.Value{key: {issue.StringValue(item)}}, false)
	case field.Kind == schema.KindMultiRelation:
		_, err = host.IssueAdd(repo, written, map[string][]issue.Value{key: {issue.StringValue(item)}}, false)
	case remove:
		_, err = host.IssueSet(repo, written, map[string]issue.Value{key: issue.MustValue(nil)}, false)
	default:
		_, err = host.IssueSet(repo, written, map[string]issue.Value{key: issue.StringValue(item)}, false)
	}
	if err != nil {
		return "", err
	}
	op := "+="
	if remove {
		op = "-="
	}
	return fmt.Sprintf("%s.%s %s %s", human(written), key, op, human(item)), nil
}

// draftLink is one membership change in a draft, which has no inverse:
// the stored field's value with the issue added or taken out.
func draftLink(current issue.Value, rel *linkRel, target choice) issue.Value {
	value, _ := decodeValue(current)
	ids := linkIds(value)
	if rel.Field != nil && rel.Field.Kind == schema.KindRelation {
		if target.current {
			return issue.MustValue(nil)
		}
		return issue.StringValue(target.value)
	}
	if target.current {
		ids = slices.DeleteFunc(ids, func(id string) bool { return id == target.value })
	} else {
		ids = append(ids, target.value)
	}
	if len(ids) == 0 {
		return issue.MustValue(nil)
	}
	return issue.MustValue(ids)
}
