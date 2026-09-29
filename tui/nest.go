package tui

import (
	"sort"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// Nesting: rows under rows, along one relation
// (doc/design/terminal-renderer.md, Nesting).
//
// `expand` names a relation field and `depth` how far to follow it. The
// query selects the roots; a matched issue that is another matched issue's
// child shows nested under it, once, and a child the query did not match
// still shows under its parent, because a parent's children are the reason
// to expand a parent. The list and the gantt both nest, so what is shared
// is here: the tree, the drawing order over it, and moving a row among its
// siblings.

// nested is one issue in the tree, in pre-order: a row and where it sits.
type nested struct {
	item map[string]any
	id   string
	// level is 0 for a root; parent the id of the row it is under.
	level  int
	parent string
	// children is how many the row has, shown or folded away.
	children int
	folded   bool
	// hidden marks a row under a folded parent: it is in the tree, so that
	// a folded parent can still draw the envelope of its children, and it
	// is never drawn itself.
	hidden bool
}

// nestDepth reads `depth` as the table means it: 1 when it is not named,
// and 0 or less for no limit, because a depth of nothing is not naming
// `expand` at all.
func nestDepth(call *view.Call) int {
	if !call.Has("depth") {
		return 1
	}
	return call.Int("depth")
}

// nest unfolds the items the query returned along the relation `expand`.
//
// Without an `expand` it is the items, each a root. With one, the children
// of a row are the targets of its own `expand` field, and the issues whose
// stored relation has `expand` as its inverse and names the row — the
// inverse side is derived, never stored (schema.yaml, D4), so `children`
// is read off every `parent`. A cycle is cut at the repeat, and an issue
// shows once: under the first matched row that reaches it. The rows under
// a folded parent are in the tree, hidden.
func nest(repo *cache.RepoCache, items []map[string]any, expand string, depth int, folded map[string]bool) []nested {
	if expand == "" {
		out := make([]nested, 0, len(items))
		for _, item := range items {
			out = append(out, nested{item: item, id: host.StringOr(item["id"], "")})
		}
		return out
	}

	all, order := allIssues(repo)
	kids := childrenIndex(repo, all, order, expand)

	matched := map[string]map[string]any{}
	for _, item := range items {
		matched[host.StringOr(item["id"], "")] = item
	}
	// a matched issue shows the query's version of itself, which a jq
	// program may have shaped; a child the query did not match is read
	// off the store
	pick := func(id string) map[string]any {
		if item, ok := matched[id]; ok {
			return item
		}
		return all[id]
	}
	within := func(level int) bool {
		return depth <= 0 || level < depth
	}

	// the roots are the matched issues no other matched issue reaches
	// within the depth, whichever comes first in the query
	under := map[string]bool{}
	for _, item := range items {
		var reach func(id string, level int, path map[string]bool)
		reach = func(id string, level int, path map[string]bool) {
			if !within(level) {
				return
			}
			path[id] = true
			for _, kid := range kids[id] {
				if all[kid] == nil || path[kid] {
					continue
				}
				under[kid] = true
				reach(kid, level+1, path)
			}
			delete(path, id)
		}
		reach(host.StringOr(item["id"], ""), 0, map[string]bool{})
	}

	var out []nested
	seen := map[string]bool{}
	var walk func(id string, level int, parent string, hidden bool, path map[string]bool)
	walk = func(id string, level int, parent string, hidden bool, path map[string]bool) {
		item := pick(id)
		if item == nil || seen[id] {
			return
		}
		seen[id] = true
		path[id] = true

		n := nested{item: item, id: id, level: level, parent: parent, folded: folded[id], hidden: hidden}
		var below []string
		for _, kid := range kids[id] {
			if all[kid] != nil && !path[kid] {
				below = append(below, kid)
			}
		}
		// a row at the depth is a leaf: what is under it is not the tree's
		n.children = len(below)
		if !within(level) {
			n.children = 0
		}
		out = append(out, n)

		if within(level) {
			for _, kid := range below {
				walk(kid, level+1, id, hidden || n.folded, path)
			}
		}
		delete(path, id)
	}
	for _, item := range items {
		if id := host.StringOr(item["id"], ""); !under[id] {
			walk(id, 0, "", false, map[string]bool{})
		}
	}
	// a matched issue every other matched issue reaches is a cycle with no
	// way in: it is a root after all
	for _, item := range items {
		walk(host.StringOr(item["id"], ""), 0, "", false, map[string]bool{})
	}
	return out
}

// allIssues is the store as the default program sees it, less the archived,
// by id and in creation order.
func allIssues(repo *cache.RepoCache) (map[string]map[string]any, []string) {
	input, err := host.IssueListInput(repo)
	if err != nil {
		return nil, nil
	}
	values, _ := input.([]any)
	items, _ := host.IssueItems(values)

	all := make(map[string]map[string]any, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		fields, _ := item["fields"].(map[string]any)
		if archived, _ := fields[schema.ArchivedKey].(bool); archived {
			continue
		}
		id := host.StringOr(item["id"], "")
		all[id] = item
		order = append(order, id)
	}
	return all, order
}

// childrenIndex maps every issue to its children along `expand`: the
// targets of its own field, then the issues whose stored relation has
// `expand` as its inverse and names it, in the store's order.
func childrenIndex(repo *cache.RepoCache, all map[string]map[string]any, order []string, expand string) map[string][]string {
	known := newKinds(repo)

	// the stored keys, per type, whose derived side reads as `expand`
	inverses := map[string][]string{}
	if s, err := repo.LoadSchema(); err == nil {
		for _, typeKey := range s.TypeKeys() {
			t, _ := s.Type(typeKey)
			for _, key := range t.FieldKeys() {
				if field, ok := t.Field(key); ok && field.Inverse == expand && isRelation(field.Kind) {
					inverses[typeKey] = append(inverses[typeKey], key)
				}
			}
		}
	}

	index := map[string][]string{}
	for _, id := range order {
		item := all[id]
		fields, _ := item["fields"].(map[string]any)
		typeKey := host.StringOr(fields[schema.TypeKey], "")
		if isRelation(known.of(typeKey, expand)) {
			index[id] = append(index[id], linkIds(fields[expand])...)
		}
		for _, key := range inverses[typeKey] {
			for _, target := range linkIds(fields[key]) {
				index[target] = append(index[target], id)
			}
		}
	}
	return index
}

// treeRow is what the drawing order needs to know about one row: where it
// sits in the tree, its group and rank, and its text for the filter.
type treeRow struct {
	id     string
	parent string
	level  int

	children int
	folded   bool
	hidden   bool

	group string
	rank  string
	// text is everything the row draws, folded, for the filter to search.
	text string
}

// treeOrder is the drawing order over rows in tree pre-order: a row that
// matches the filter, or has a descendant that does, is drawn, never one
// under a folded parent; the roots
// are grouped in the order their groups first appear with (none) last; a
// bound rank orders the roots within a group and the children under a
// parent by (rank, id); and a subtree follows its root wherever the root
// goes.
func treeOrder(rows []treeRow, filter string, ranked bool) []int {
	needle := strings.ToLower(strings.TrimSpace(filter))
	byId := make(map[string]int, len(rows))
	for at, row := range rows {
		byId[row.id] = at
	}

	visible := make([]bool, len(rows))
	for at, row := range rows {
		if row.hidden || (needle != "" && !strings.Contains(row.text, needle)) {
			continue
		}
		// the row, and every row above it: a match needs its parent on the
		// screen to be under
		for ok := true; ok; {
			visible[at] = true
			at, ok = byId[rows[at].parent]
		}
	}

	childrenOf := map[string][]int{}
	for at, row := range rows {
		if visible[at] && !row.hidden {
			childrenOf[row.parent] = append(childrenOf[row.parent], at)
		}
	}
	rank := func(members []int) {
		if !ranked {
			return
		}
		sort.SliceStable(members, func(i, j int) bool {
			left, right := rows[members[i]], rows[members[j]]
			return lessByRank(left.rank, left.id, right.rank, right.id)
		})
	}

	roots := childrenOf[""]
	groups := make([]string, 0, 4)
	members := map[string][]int{}
	for _, at := range roots {
		group := rows[at].group
		if _, seen := members[group]; !seen {
			groups = append(groups, group)
		}
		members[group] = append(members[group], at)
	}
	// the rows with no value for the grouping field come last: they are the
	// ones nobody has filed yet, and they are what a session works through
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[j] == noGroup && groups[i] != noGroup
	})

	order := make([]int, 0, len(rows))
	var emit func(at int)
	emit = func(at int) {
		order = append(order, at)
		kids := childrenOf[rows[at].id]
		rank(kids)
		for _, kid := range kids {
			emit(kid)
		}
	}
	for _, group := range groups {
		rank(members[group])
		for _, at := range members[group] {
			emit(at)
		}
	}
	return order
}

// blockEnd is where the subtree drawn from order[at] ends, exclusive: the
// rows after it that are deeper are its own.
func blockEnd(rows []treeRow, order []int, at int) int {
	level := rows[order[at]].level
	end := at + 1
	for end < len(order) && rows[order[end]].level > level {
		end++
	}
	return end
}

// siblingAt is the position in order of the previous (by < 0) or next
// sibling of the row at order[at] — same parent, same level, and for a
// root the same group — or -1 when there is none.
func siblingAt(rows []treeRow, order []int, at, by int) int {
	row := rows[order[at]]
	sibling := func(other treeRow) bool {
		return other.level == row.level && other.parent == row.parent &&
			(row.level > 0 || other.group == row.group)
	}
	if by < 0 {
		for i := at - 1; i >= 0; i-- {
			other := rows[order[i]]
			if other.level < row.level {
				return -1
			}
			if other.level == row.level {
				if sibling(other) {
					return i
				}
				return -1
			}
		}
		return -1
	}
	i := blockEnd(rows, order, at)
	if i < len(order) && sibling(rows[order[i]]) {
		return i
	}
	return -1
}

// moveBlock moves the subtree at order[at] one sibling up or down, on the
// screen only, and returns where it is now: the same place when there is no
// sibling that way.
func moveBlock(rows []treeRow, order []int, at, by int) int {
	end := blockEnd(rows, order, at)
	block := append([]int(nil), order[at:end]...)
	if by < 0 {
		to := siblingAt(rows, order, at, -1)
		if to < 0 {
			return at
		}
		copy(order[to+len(block):end], order[to:at])
		copy(order[to:], block)
		return to
	}
	next := siblingAt(rows, order, at, 1)
	if next < 0 {
		return at
	}
	nextEnd := blockEnd(rows, order, next)
	copy(order[at:], order[next:nextEnd])
	copy(order[at+nextEnd-next:nextEnd], block)
	return at + nextEnd - next
}

// siblingRanks reads the ranks the row at order[at] has to land between:
// its nearest siblings either way that have one; an empty string is the end.
func siblingRanks(rows []treeRow, order []int, at int) (lo, hi string) {
	for i := siblingAt(rows, order, at, -1); i >= 0; i = siblingAt(rows, order, i, -1) {
		if r := rows[order[i]].rank; r != "" {
			lo = r
			break
		}
	}
	for i := siblingAt(rows, order, at, 1); i >= 0; i = siblingAt(rows, order, i, 1) {
		if r := rows[order[i]].rank; r != "" {
			hi = r
			break
		}
	}
	return lo, hi
}

// nestGlyph is the fold marker before a nested row's id: a parent open or
// folded, or a leaf.
func nestGlyph(row treeRow) string {
	switch {
	case row.children == 0:
		return "  "
	case row.folded:
		return "▸ "
	default:
		return "▾ "
	}
}

// indentOf is the room the tree takes before the id: the deepest visible
// level's indent plus the fold marker, nothing when nothing is expanded.
func indentOf(rows []treeRow, order []int, expanded bool) int {
	if !expanded {
		return 0
	}
	deepest := 0
	for _, at := range order {
		deepest = max(deepest, rows[at].level)
	}
	return 2*deepest + 2
}

// nestPrefix is what a row's id is drawn behind: its level's indent and its
// fold marker. The id moves with it, so that a level reads as a level,
// and the page pads the id to the indent it settled on so that the
// columns after it line up.
func nestPrefix(row treeRow) string {
	return strings.Repeat("  ", row.level) + nestGlyph(row)
}
