package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// The ghost: a dim `+` row at the foot of each scope a view draws — a group
// on a list and a gantt, a column of a lane on a board — that opens the
// creator prefilled with the drop's write set, so that the issue it creates
// is already where it was added (doc/design/create.md, C6 and C7).
//
// A scope is a level, a parent and a group (C8): the roots, or one opened
// parent's drawn children, and, where that level's layer groups, one group
// of them. Every scope ends in a ghost, at every level of a nested list and
// of a gantt, and the roots are the scope whose parent is no row. Folded,
// a scope's ghost is hidden with it.
//
// It is a row the cursor reaches, enter opens the creator, space rings, copy
// copies nothing, and it is never a rank sibling: a grab skips it, a drop
// above it is a drop at the foot, and the rank fills do not count it. The
// filter hides it with the rows it narrows away. No rank is written for the
// issue it creates: a null rank already sorts last, where the ghost stood.

// ghostPrefix starts every ghost's key, which no issue id can start with,
// and which a key a query gives a row may not (doc/design/query-rows.md, R1).
const ghostPrefix = host.KeyPrefix

// ghostLabel is what a ghost draws where a row draws its first field.
const ghostLabel = "(new)"

// ghostId is a ghost's key: unique per scope, because the drawing order keys
// rows by key, and never an issue's or a query's. A ghost stands for no
// issue, so its id is empty.
func ghostId(scope ...string) string {
	return ghostPrefix + strings.Join(scope, "\x00")
}

// sharedType is the type every row of a scope has, or "" where they differ
// or there are none: the rows a query produced are the best witness of what
// it selects, since a jq program cannot be inverted (C6).
func sharedType(typeKeys []string) string {
	shared := ""
	for _, typeKey := range typeKeys {
		switch {
		case typeKey == "":
			return ""
		case shared == "":
			shared = typeKey
		case shared != typeKey:
			return ""
		}
	}
	return shared
}

// groupPrefill is what a ghost in a group prefills the `group_by` field
// with: the value a row already in the group holds, as a drop writes it
// (group.go); nothing for (none), and nothing for a set-valued field, which
// a drop refuses too.
func groupPrefill(repo *cache.RepoCache, typeKey, groupBy, group string, raw any) (issue.Value, bool) {
	if groupBy == "" || group == noGroup || groupBy == schema.TypeKey {
		return nil, false
	}
	kind, known := fieldKind(repo, typeKey, groupBy)
	if !known || kind.IsMulti() {
		return nil, false
	}
	value := groupValue(raw, group)
	if issue.IsNull(value) {
		return nil, false
	}
	return value, true
}

// placement is where a ghost puts the issue it creates: the draft the
// creator opens on, and, for a relation stored on the parent, the write the
// opener makes on the parent once the issue exists (C8).
type placement struct {
	draft host.IssueDocument
	link  *parentLink
}

// parentLink is a relation stored on the parent, written after Create as a
// person would write it: `issue add` for a multi-relation, whose set
// semantics merge with a concurrent add, `issue set` for a single relation,
// one commit on the parent after the create's one commit on the new issue.
type parentLink struct {
	parent string
	field  string
	multi  bool
	// replacing is the issue a set single relation holds now, which the
	// new issue takes the place of in the field and nothing else
	replacing string
}

// label is what the ghost draws: `(new)`, or, where it replaces what a single
// relation holds, which issue leaves the field, because a `+` that silently
// swapped a link would be the one ghost that does more than it says.
func (pl *placement) label(repo *cache.RepoCache) string {
	if pl != nil && pl.link != nil && pl.link.replacing != "" {
		return "(new, replacing " + humanOf(repo, pl.link.replacing) + ")"
	}
	return ghostLabel
}

// relationSide is how a relation is written from a parent of one type (C8),
// decided as view.InverseSources decides it for show: where the parent's
// type has a stored relation field of that name, it is that field;
// otherwise it is the inverse, stored on the child under one of sources.
type relationSide struct {
	onParent bool
	field    string
	multi    bool
	// targets are the parent field's target_types, nil for any type
	targets []string
	// sources are, by type, the child's stored keys whose inverse it is
	sources map[string][]string
}

// relationSideOf is the side a relation is stored on from a parent of this
// type, false where no write can name it.
func relationSideOf(s *schema.Schema, parentType, relation string) (relationSide, bool) {
	if s == nil || relation == "" {
		return relationSide{}, false
	}
	if t, ok := s.Type(parentType); ok {
		if field, ok := t.Field(relation); ok && field.Kind.IsRelation() {
			return relationSide{onParent: true, field: relation, multi: field.Kind.IsMulti(), targets: field.TargetTypes}, true
		}
	}
	sources := view.InverseSources(s, parentType, relation)
	if len(sources) == 0 {
		return relationSide{}, false
	}
	return relationSide{sources: sources}, true
}

// holds says an issue of this type can stand in the relation: the parent's
// field admits it, or the type stores the inverse.
func (r relationSide) holds(typeKey string) bool {
	if r.onParent {
		return len(r.targets) == 0 || contains(r.targets, typeKey)
	}
	_, ok := r.sources[typeKey]
	return ok
}

// only is the one type the schema says can stand in the relation, "" where
// there are several or any.
func (r relationSide) only() string {
	if r.onParent {
		if len(r.targets) == 1 {
			return r.targets[0]
		}
		return ""
	}
	if len(r.sources) == 1 {
		for typeKey := range r.sources {
			return typeKey
		}
	}
	return ""
}

// childKey is the stored key on the child: the type's, or, with no type,
// the one every type that stores the inverse agrees on. False where it
// cannot be named, which is no ghost (S6).
func (r relationSide) childKey(s *schema.Schema, typeKey string) (key string, multi bool, ok bool) {
	consider := r.sources
	if typeKey != "" {
		consider = map[string][]string{typeKey: r.sources[typeKey]}
	}
	for t, keys := range consider {
		for _, k := range keys {
			field, _ := s.Field(t, k)
			isMulti := field != nil && field.Kind.IsMulti()
			switch {
			case key == "":
				key, multi = k, isMulti
			case key != k || multi != isMulti:
				return "", false, false
			}
		}
	}
	return key, multi, key != ""
}

// scopeParent is the row a child scope stands under, and the relation of
// the layer its children are read through.
type scopeParent struct {
	id       string
	typeKey  string
	relation string
	// held is what a field of the parent holds now, by id
	held func(field string) []string
}

// composeDraft is a scope's placement, built in one order for every ghost,
// the roots' and every child scope's alike (C9):
//
//  1. the call's `draft`;
//  2. the type the scope's rows share, where `draft` names none, and under
//     a parent one that can hold the relation, else the one the schema says
//     can (C8);
//  3. the group values, which win on their keys, because the ghost stands
//     in that group;
//  4. under a parent, the relation: the stored key on the child set to the
//     parent, or the link on the parent, written after Create.
//
// Then what the scope's issue cannot hold is dropped: a `draft` type that
// cannot hold the relation gave way in step 2, and a field the resulting type
// does not have goes. False where there is nothing to write: a parent with
// no id, or a relation whose write set cannot be named.
func composeDraft(s *schema.Schema, base host.IssueDocument, types []string, prefill map[string]issue.Value, parent *scopeParent) (*placement, bool) {
	doc := cloneDoc(base)
	typeKey, _ := issue.String(doc.Fields[schema.TypeKey])

	var side relationSide
	if parent != nil {
		var ok bool
		if parent.id == "" {
			return nil, false
		}
		if side, ok = relationSideOf(s, parent.typeKey, parent.relation); !ok {
			return nil, false
		}
		if typeKey != "" && !side.holds(typeKey) {
			// the draft's type cannot stand here: it gives way
			typeKey = ""
		}
		if typeKey == "" {
			if shared := sharedType(types); shared != "" && side.holds(shared) {
				typeKey = shared
			} else {
				typeKey = side.only()
			}
		}
	} else if typeKey == "" {
		typeKey = sharedType(types)
	}
	if typeKey != "" {
		doc.Fields[schema.TypeKey] = issue.StringValue(typeKey)
	} else {
		delete(doc.Fields, schema.TypeKey)
	}

	for key, value := range prefill {
		doc.Fields[key] = value
	}

	out := &placement{}
	if parent != nil {
		if side.onParent {
			link := &parentLink{parent: parent.id, field: side.field, multi: side.multi}
			if !side.multi && parent.held != nil {
				if held := parent.held(side.field); len(held) > 0 {
					link.replacing = held[0]
				}
			}
			out.link = link
		} else {
			key, multi, ok := side.childKey(s, typeKey)
			if !ok {
				return nil, false
			}
			if multi {
				ids, _ := issue.Strings(doc.Fields[key])
				if !contains(ids, parent.id) {
					ids = append(ids, parent.id)
				}
				doc.Fields[key] = issue.MustValue(ids)
			} else {
				doc.Fields[key] = issue.StringValue(parent.id)
			}
		}
	}

	dropForeign(s, typeKey, doc.Fields)
	out.draft = doc
	return out, true
}

// dropForeign takes out of a draft the fields its type does not have, as a
// change of type on the creator does (C3): nothing where the type is unknown.
func dropForeign(s *schema.Schema, typeKey string, fields map[string]issue.Value) {
	if s == nil || typeKey == "" {
		return
	}
	t, ok := s.Type(typeKey)
	if !ok {
		return
	}
	for key := range fields {
		if _, builtin := schema.BuiltinKind(key); builtin {
			continue
		}
		if _, has := t.Field(key); !has {
			delete(fields, key)
		}
	}
}

// ghostScope is one scope of a nested list or gantt: the rows at one level
// under one parent, and one group of them where that level's layer groups.
type ghostScope struct {
	level int
	// parent is the index of the row the scope is under, -1 at the roots
	parent  int
	group   string
	members []int
}

// ghostScopes is every scope the drawn tree has, in one walk: the roots'
// groups, one (none) when there are no roots, and under every opened parent
// that may have children of its layer — not folded, not hidden under a fold
// above, and with a write set (nestable) — its children's groups, or one
// (none) for an empty child table.
func ghostScopes(nodes []treeRow) []ghostScope {
	var out []ghostScope
	at := map[string]int{}
	add := func(level, parent int, parentKey, group string, member int) {
		id := parentKey + "\x00" + group
		i, ok := at[id]
		if !ok {
			i = len(out)
			at[id] = i
			out = append(out, ghostScope{level: level, parent: parent, group: group})
		}
		if member >= 0 {
			out[i].members = append(out[i].members, member)
		}
	}

	kids := map[string][]int{}
	roots := false
	for index, node := range nodes {
		switch {
		case node.ghost:
		case node.level == 0:
			roots = true
			add(0, -1, "", node.group, index)
		default:
			kids[node.parent] = append(kids[node.parent], index)
		}
	}
	if !roots {
		add(0, -1, "", noGroup, -1)
	}

	for index, node := range nodes {
		if node.ghost || node.hidden || node.folded || !node.nestable {
			continue
		}
		if len(kids[node.key]) == 0 {
			add(node.level+1, index, node.key, noGroup, -1)
			continue
		}
		for _, kid := range kids[node.key] {
			add(node.level+1, index, node.key, nodes[kid].group, kid)
		}
	}
	return out
}

// scopeRow is what a scope's ghost reads off a row: its key and id, its type
// and its fields as the query handed them over.
type scopeRow struct {
	key     string
	id      string
	typeKey string
	fields  map[string]any
}

// treeGhost is one ghost of a nested list or gantt: its place in the tree
// and the placement it opens.
type treeGhost struct {
	node  treeRow
	place *placement
}

// treeGhosts is a ghost per scope of a list or a gantt, each with its draft
// built on the call's `draft` (composeDraft). A scope with nothing to write
// has none.
func treeGhosts(repo *cache.RepoCache, nodes []treeRow, n *nesting, base host.IssueDocument, rowAt func(index int) scopeRow) []treeGhost {
	s, _ := repo.LoadSchema()
	var out []treeGhost
	for _, scope := range ghostScopes(nodes) {
		layer := n.at(scope.level)
		if layer == nil {
			continue
		}
		types := make([]string, 0, len(scope.members))
		for _, member := range scope.members {
			types = append(types, rowAt(member).typeKey)
		}
		prefill := map[string]issue.Value{}
		if layer.groupBy != "" && len(scope.members) > 0 {
			row := rowAt(scope.members[0])
			if value, ok := groupPrefill(repo, row.typeKey, layer.groupBy, scope.group, row.fields[layer.groupBy]); ok {
				prefill[layer.groupBy] = value
			}
		}

		var parent *scopeParent
		key := ghostId(scope.group)
		parentKey := ""
		if scope.parent >= 0 {
			row := rowAt(scope.parent)
			parent = &scopeParent{id: row.id, typeKey: row.typeKey, relation: layer.relation,
				held: func(field string) []string { return linkIds(row.fields[field]) }}
			parentKey = row.key
			key = ghostId(row.key, scope.group)
		}
		place, ok := composeDraft(s, base, types, prefill, parent)
		if !ok {
			continue
		}
		out = append(out, treeGhost{
			node: treeRow{key: key, parent: parentKey, level: scope.level, group: scope.group,
				grouped: layer.groupBy != "", ghost: true},
			place: place,
		})
	}
	return out
}

// callDraft is the call's `draft`, the base of every ghost's draft (C9),
// checked as `issue new` would check it.
func callDraft(repo *cache.RepoCache, call *view.Call) (host.IssueDocument, error) {
	doc, err := host.IssueDraft(repo, call.Raw("draft"))
	if err != nil {
		return doc, fmt.Errorf("view %s: draft %w", call.Kind, err)
	}
	if doc.Fields == nil {
		doc.Fields = map[string]issue.Value{}
	}
	return doc, nil
}

// openNew opens the creator over the view, on a draft, carrying the view's
// `show` for the pages the creator opens (show_map.go).
func openNew(repo *cache.RepoCache, shows json.RawMessage, doc host.IssueDocument) tea.Cmd {
	return openNewAt(repo, shows, &placement{draft: doc})
}

// openNewAt opens the creator on a ghost's placement: its draft, and the
// parent write the creator hands back with the id, which the opener makes
// (C8). The creator itself has no write path but Create's.
func openNewAt(repo *cache.RepoCache, shows json.RawMessage, place *placement) tea.Cmd {
	raw, err := json.Marshal(place.draft)
	if err != nil {
		return nil
	}
	call, err := view.Parse(view.KindNew, map[string]json.RawMessage{"draft": raw})
	if err != nil {
		return nil
	}
	created, err := newNewPage(repo, call)
	if err != nil {
		return func() tea.Msg { return statusMsg(err.Error()) }
	}
	created.shows = shows
	created.link = place.link
	return func() tea.Msg { return pushMsg{page: created} }
}

// linkCreated is the opener's half of a ghost under a parent that stores
// the relation (C8): once the issue exists, the parent's field is written to
// hold it, one commit on the parent through host and the cache, as
// `issue add` (or `set`) would write it. A failure — the field refuses the
// type picked, the parent is gone — leaves the issue created, complete on its
// own, and is said: what is returned is the status line, "" when it landed.
func linkCreated(repo *cache.RepoCache, msg createdMsg) string {
	link := msg.link
	if link == nil {
		return ""
	}
	var err error
	if link.multi {
		_, err = host.IssueAdd(repo, link.parent, map[string][]issue.Value{link.field: {issue.StringValue(msg.id)}}, false)
	} else {
		_, err = host.IssueSet(repo, link.parent, map[string]issue.Value{link.field: issue.StringValue(msg.id)}, false)
	}
	if err != nil {
		return fmt.Sprintf("created %s; not added to %s's %s: %v", human(msg.id), humanOf(repo, link.parent), link.field, err)
	}
	return ""
}

// createdStatus is what a view says after a ghost's issue was created and
// the view reloaded: the parent's refusal, where there was one, else
// created, and whether the view keeps it (C4).
func createdStatus(failed, id string, shown bool) string {
	switch {
	case failed != "":
		return failed
	case shown:
		return "created " + human(id)
	default:
		return "created " + human(id) + ", not in this view"
	}
}

// crossGhost is the step a grabbed root takes over the ghost at the edge of
// its group: the one row between the last of one group and the first of the
// next, so the block swaps places with it and is drawn under the next
// header, which crossGroup relabels it into. It returns where the block is
// now, the same place when no ghost is adjacent.
func crossGhost(rows []treeRow, order []int, at, by int) int {
	end := blockEnd(rows, order, at)
	if by < 0 {
		if at == 0 || !rows[order[at-1]].ghost {
			return at
		}
		ghost := order[at-1]
		copy(order[at-1:end-1], order[at:end])
		order[end-1] = ghost
		return at - 1
	}
	if end >= len(order) || !rows[order[end]].ghost {
		return at
	}
	ghost := order[end]
	copy(order[at+1:end+1], order[at:end])
	order[at] = ghost
	return at + 1
}
