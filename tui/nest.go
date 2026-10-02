package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// Nesting: rows under rows, a relation per level
// (doc/design/terminal-renderer.md, Nesting).
//
// `expand` is a layer spec: a relation, the list's own arguments for the rows
// that relation brings — `query`, `fields`, `details`, `group_by`, `rank` —
// and an `expand` of its own for the level below. The call's own arguments
// are level 0, the spec's first layer level 1, and a level the spec does not
// describe is leaves. The query selects the roots; a matched issue that is
// another matched issue's child shows nested under it, once, and a child the
// layer's own query kept still shows under its parent, because a parent's
// children are the reason to expand a parent. The list and the gantt both
// nest, so what is shared is here: the layers, the tree, the drawing order
// over it, and moving a row among its siblings.

// nestLayer is one level as the renderer uses it: the spec's layer with its
// query compiled and the keys it inherits filled in.
type nestLayer struct {
	// relation is "" on level 0, whose rows are the view's own query.
	relation string
	// program is the layer's `query`, run over the array of a row's candidate
	// children; nil is every one of them.
	program *jq.Program
	fields  []string
	details []string
	groupBy string
	rankKey string
}

// nesting is `expand` resolved: a layer per level, and whether the last of
// them repeats (`"expand": "self"`).
type nesting struct {
	layers []nestLayer
	repeat bool
}

// newNesting resolves the root's own arguments and the spec's layers.
//
// A layer that names no `fields`, `details` or `rank` takes the layer
// above's, so a bare `"expand": "children"` is the uniform tree nesting was
// before the spec, and a layer that names them draws its own columns under
// the parent. `group_by` is not inherited: a level is sectioned because that
// level was asked to be.
func newNesting(root nestLayer, spec *view.Layer) (*nesting, error) {
	n := &nesting{layers: []nestLayer{root}}
	if spec == nil {
		return n, nil
	}

	layers, repeat := spec.Layers()
	n.repeat = repeat
	for _, layer := range layers {
		above := n.layers[len(n.layers)-1]
		resolved := nestLayer{
			relation: layer.Relation,
			fields:   layer.Fields,
			details:  layer.Details,
			groupBy:  layer.GroupBy,
			rankKey:  layer.Rank,
		}
		if resolved.fields == nil {
			resolved.fields = above.fields
		}
		if resolved.details == nil {
			resolved.details = above.details
		}
		if resolved.rankKey == "" {
			resolved.rankKey = above.rankKey
		}
		if layer.Query != "" {
			program, err := jq.Compile(layer.Query)
			if err != nil {
				return nil, fmt.Errorf("expand: %w", err)
			}
			resolved.program = program
		}
		n.layers = append(n.layers, resolved)
	}
	return n, nil
}

// expanded reports whether anything nests at all.
func (n *nesting) expanded() bool {
	return n != nil && len(n.layers) > 1
}

// at is the layer a row at this level belongs to, nil where the spec ends
// above it, which is what makes the level above it leaves.
func (n *nesting) at(level int) *nestLayer {
	if n == nil {
		return nil
	}
	if level < len(n.layers) {
		return &n.layers[level]
	}
	if n.repeat {
		return &n.layers[len(n.layers)-1]
	}
	return nil
}

// index is a level's layer by position, which every level past a repeating
// last layer shares, so that one set of column widths serves them all.
func (n *nesting) index(level int) int {
	if n == nil || len(n.layers) == 0 {
		return 0
	}
	return min(level, len(n.layers)-1)
}

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

// nest unfolds the items the query returned along the layers of `expand`.
//
// Without an `expand` it is the items, each a root. With one, the children of
// a row at level n are what the layer for level n+1 names: the targets of the
// row's own field of that name, and the issues whose stored relation has that
// name as its inverse — the inverse side is derived, never stored
// (schema.yaml, D4), so `children` is read off every `parent` — narrowed by
// the layer's own query where it has one. A cycle is cut at the repeat, and
// an issue shows once: under the first matched row that reaches it.
//
// Every parent is folded unless `open` says otherwise, so a tree opens as its
// roots and their counts, which is a summary (Luis, 2026-10-02). The rows
// under a folded parent are in the tree, hidden, because a folded parent
// still draws the envelope of its children on a gantt.
//
// A layer's query that fails is reported and its children are left unnarrowed:
// the error is the status line, and a tree with too much in it reads better
// than an empty one.
func nest(repo *cache.RepoCache, items []map[string]any, n *nesting, open map[string]bool) ([]nested, error) {
	if !n.expanded() {
		out := make([]nested, 0, len(items))
		for _, item := range items {
			out = append(out, nested{item: item, id: host.StringOr(item["id"], "")})
		}
		return out, nil
	}

	all, order := allIssues(repo)

	indexes := map[string]map[string][]string{}
	byRelation := func(relation string) map[string][]string {
		if got, ok := indexes[relation]; ok {
			return got
		}
		got := childrenIndex(repo, all, order, relation)
		indexes[relation] = got
		return got
	}

	// a matched issue shows the query's version of itself, which a jq program
	// may have shaped, and so does a child its layer's query shaped; anything
	// else is read off the store
	shaped := map[string]map[string]any{}
	for _, item := range items {
		shaped[host.StringOr(item["id"], "")] = item
	}
	pick := func(id string) map[string]any {
		if item, ok := shaped[id]; ok {
			return item
		}
		return all[id]
	}

	var failed error
	kids := map[string][]string{}
	childrenOf := func(id string, level int) []string {
		layer := n.at(level + 1)
		if layer == nil {
			return nil
		}
		key := strconv.Itoa(n.index(level+1)) + "\x00" + id
		if got, ok := kids[key]; ok {
			return got
		}

		var candidates []string
		for _, kid := range byRelation(layer.relation)[id] {
			if all[kid] != nil {
				candidates = append(candidates, kid)
			}
		}
		out := candidates
		if layer.program != nil && len(candidates) > 0 {
			picked, shapes, err := runLayerQuery(layer.program, candidates, all)
			if err != nil {
				if failed == nil {
					failed = err
				}
			} else {
				out = picked
				for id, item := range shapes {
					shaped[id] = item
				}
			}
		}
		kids[key] = out
		return out
	}

	// the roots are the matched issues no other matched issue reaches
	under := map[string]bool{}
	for _, item := range items {
		var reach func(id string, level int, path map[string]bool)
		reach = func(id string, level int, path map[string]bool) {
			path[id] = true
			for _, kid := range childrenOf(id, level) {
				if path[kid] {
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

		var below []string
		for _, kid := range childrenOf(id, level) {
			if !path[kid] {
				below = append(below, kid)
			}
		}
		node := nested{
			item:     item,
			id:       id,
			level:    level,
			parent:   parent,
			children: len(below),
			folded:   !open[id],
			hidden:   hidden,
		}
		out = append(out, node)

		for _, kid := range below {
			walk(kid, level+1, id, hidden || node.folded, path)
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
	return out, failed
}

// runLayerQuery narrows one row's candidate children with its layer's query.
//
// The program runs over the array of that row's own children, the way the
// view's query runs over the array of every issue, so the two are written the
// same way and a layer can say `map(select(.fields.status != "done"))`
// without knowing anything about the row it is under.
func runLayerQuery(program *jq.Program, candidates []string, all map[string]map[string]any) ([]string, map[string]map[string]any, error) {
	input := make([]any, 0, len(candidates))
	for _, id := range candidates {
		input = append(input, all[id])
	}

	values, err := program.Run(input)
	if err != nil {
		return nil, nil, err
	}
	// one array level is unwrapped and no more, the way a view's own query is
	// read (host.IssueItems)
	if len(values) == 1 {
		if array, ok := values[0].([]any); ok {
			values = array
		}
	}

	ids := make([]string, 0, len(values))
	shapes := map[string]map[string]any{}
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("expand: a layer's query returned something that is not an issue")
		}
		id, ok := object["id"].(string)
		if !ok {
			return nil, nil, fmt.Errorf("expand: a layer's query returned an issue with no id")
		}
		ids = append(ids, id)
		shapes[id] = object
	}
	return ids, shapes, nil
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

// childrenIndex maps every issue to its children along one relation: the
// targets of its own field, then the issues whose stored relation has that
// name as its inverse and names it, in the store's order.
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
	// grouped says the row's own layer has a `group_by`, so its siblings are
	// drawn in sections under their own headers.
	grouped bool
	rank    string
	// text is everything the row draws, folded, for the filter to search.
	text string
}

// treeOrder is the drawing order over rows in tree pre-order: a row that
// matches the filter, or has a descendant that does, is drawn, never one
// under a folded parent; every set of siblings is grouped in the order their
// groups first appear with (none) last; the rank orders them within a group
// by (rank, id), the unranked keeping the query's order at the end; and a
// subtree follows its root wherever the root goes.
func treeOrder(rows []treeRow, filter string) []int {
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

	order := make([]int, 0, len(rows))
	var emit func(at int)
	emit = func(at int) {
		order = append(order, at)
		for _, kid := range arrange(rows, childrenOf[rows[at].id]) {
			emit(kid)
		}
	}
	for _, at := range arrange(rows, childrenOf[""]) {
		emit(at)
	}
	return order
}

// arrange is one set of siblings as it is drawn: the rank orders them, and
// then, on a layer that groups, their groups in the order the groups first
// appear with (none) last.
//
// Every level is arranged the same way, because every level is a list: the
// roots were the only grouped ones while `expand` was a relation name, and a
// layer that names a `group_by` of its own sections its own rows.
func arrange(rows []treeRow, members []int) []int {
	if len(members) == 0 {
		return members
	}

	sort.SliceStable(members, func(i, j int) bool {
		left, right := rows[members[i]], rows[members[j]]
		return lessByRank(left.rank, left.id, right.rank, right.id)
	})
	if !rows[members[0]].grouped {
		return members
	}

	groups := make([]string, 0, 4)
	of := map[string][]int{}
	for _, at := range members {
		group := rows[at].group
		if _, seen := of[group]; !seen {
			groups = append(groups, group)
		}
		of[group] = append(of[group], at)
	}
	// the rows with no value for the grouping field come last: they are the
	// ones nobody has filed yet, and they are what a session works through
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[j] == noGroup && groups[i] != noGroup
	})

	out := make([]int, 0, len(members))
	for _, group := range groups {
		out = append(out, of[group]...)
	}
	return out
}

// groupHeads marks the places in the drawing order where a group opens: the
// first of each run of siblings that share a group, on a layer that groups.
//
// A set of siblings is drawn group by group (arrange) but is not contiguous
// — each of them carries its own subtree — so the last group drawn is kept
// per parent rather than per line.
func groupHeads(rows []treeRow, order []int) []bool {
	heads := make([]bool, len(order))
	last := map[string]string{}
	for at, index := range order {
		row := rows[index]
		if !row.grouped {
			continue
		}
		if was, seen := last[row.parent]; !seen || was != row.group {
			heads[at] = true
		}
		last[row.parent] = row.group
	}
	return heads
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
// sibling of the row at order[at] — same parent, same level, and, on a layer
// that groups, the same group — or -1 when there is none.
func siblingAt(rows []treeRow, order []int, at, by int) int {
	row := rows[order[at]]
	sibling := func(other treeRow) bool {
		return other.level == row.level && other.parent == row.parent &&
			(!row.grouped || other.group == row.group)
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

// crossGroup is the move past the edge of a group: a grabbed root with no
// sibling that way enters the neighbouring group instead — the one the
// nearest root above or below it is in, at that group's end going up and
// its start going down.
//
// The row keeps its place in the order, because the boundary between two
// groups is one point: the row drawn last under one header is the row drawn
// first under the next, and which group it is in is which header it is
// under. So the caller relabels the row and nothing moves.
//
// It returns the index into rows of the neighbour, whose group and whose
// stored value the row takes on at the drop, and false where there is no
// group that way — and for a nested child, which stays among its siblings
// (doc/design/terminal-renderer.md, Rank).
func crossGroup(rows []treeRow, order []int, at, by int) (int, bool) {
	if rows[order[at]].level != 0 {
		return 0, false
	}
	if by < 0 {
		for i := at - 1; i >= 0; i-- {
			if rows[order[i]].level == 0 {
				return order[i], true
			}
		}
		return 0, false
	}
	for i := blockEnd(rows, order, at); i < len(order); i++ {
		if rows[order[i]].level == 0 {
			return order[i], true
		}
	}
	return 0, false
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

// The tree column is the cell after the id (Luis, 2026-10-02).
//
// The id column stays first and flush, so a list reads and sorts by id
// whether or not anything nests, and the tree cell beside it carries the
// level's indent, the fold arrow, and, folded, how many rows are hidden
// under it. It is a cursor stop like any other cell: `→` reaches it, `Space`
// folds and unfolds there, `Enter` opens the row as it does on the id, and a
// leaf, which draws its indent alone, rings.

// foldAll is `Z`: every parent folded shut, or, where none of them is open,
// every parent opened.
//
// Folding wins the tie, because the tree a list opens with is the summary,
// and getting back to it is what a reader of a tree opened all over wants.
func foldAll(rows []treeRow, open map[string]bool) map[string]bool {
	anyOpen := false
	for _, row := range rows {
		if row.children > 0 && !row.folded {
			anyOpen = true
			break
		}
	}
	if anyOpen {
		return map[string]bool{}
	}

	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.children > 0 {
			out[row.id] = true
		}
	}
	return out
}

// reveal opens the parents between a row and the roots, and says whether it
// had to open any.
//
// A refresh can nest the row the cursor is on under a folded parent — a
// parent given to an issue that had none, a tree opened the same moment
// another process wrote — and a cursor that vanished with it would be a
// cursor moved by somebody else's write. The ancestors of a row that is
// wanted are opened, which is what the filter does for a match.
func reveal(rows []treeRow, open map[string]bool, id string) bool {
	if id == "" {
		return false
	}
	byId := make(map[string]treeRow, len(rows))
	for _, row := range rows {
		byId[row.id] = row
	}
	row, ok := byId[id]
	if !ok || !row.hidden {
		return false
	}

	opened := false
	for {
		above, ok := byId[row.parent]
		if !ok {
			return opened
		}
		if !open[above.id] {
			open[above.id] = true
			opened = true
		}
		row = above
	}
}

// treeCell is one row's tree column: its indent, its arrow, and the count of
// what folding it hides.
func treeCell(row treeRow) string {
	cell := strings.Repeat("  ", row.level)
	switch {
	case row.children == 0:
		return cell
	case row.folded:
		return cell + "▸ " + strconv.Itoa(row.children)
	default:
		return cell + "▾"
	}
}

// treeWidth is the tree column's width: the deepest level drawn, the arrow,
// and room for the largest count, measured over every row rather than over
// the folded ones so that folding never moves the columns beside it. Nothing
// when nothing nests.
func treeWidth(rows []treeRow, order []int, expanded bool) int {
	if !expanded {
		return 0
	}
	deepest, most := 0, 0
	for _, at := range order {
		deepest = max(deepest, rows[at].level)
		most = max(most, rows[at].children)
	}
	width := 2*deepest + 1
	if most > 0 {
		width += 1 + len(strconv.Itoa(most))
	}
	return width
}
