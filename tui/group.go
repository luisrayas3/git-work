package tui

import (
	"sort"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// A grabbed row carried past the edge of its group enters the next one, and
// the drop writes the `group_by` field (2026-10-02,
// doc/design/terminal-renderer.md, Rank).
//
// It is the board's rule, which the board has had since `0740bf3`: a card
// carried into another column is dropped there, and the drop sets the field
// the columns are of. A group is a column drawn the other way, so a list's
// sections, a gantt's row groups and a board's swimlanes all move the same
// way, and one drop is still one commit — the rank and the field in one
// `IssueSet`.
//
// What a group is on the screen is the cell its rows draw, so what the drop
// writes is the value a row already in that group holds: the enum's value
// id, the relation's or the identity's id, the bool. `(none)` is the group
// of the rows with no value at all, and writes null.

// crossing is the group a grabbed row has been carried into: the header it
// is drawn under now, and the value its `group_by` field takes at the drop.
type crossing struct {
	group string
	value issue.Value
}

// crossRefusal is why a grabbed row may not change group, "" when it may.
//
// `type` is not a field a move gets to change: it decides which fields an
// issue has, so dragging it across a lane would be a schema change by
// accident. A set-valued field is not one value, so a drag cannot say which
// item it meant — the same reason space on a `multi-relation` rings — and
// `issue add`/`remove` is the answer there as it is everywhere else.
func crossRefusal(repo *cache.RepoCache, typeKey, groupBy string) string {
	if groupBy == schema.TypeKey {
		return "type cannot be changed"
	}
	kind, known := fieldKind(repo, typeKey, groupBy)
	if !known {
		return groupBy + " is not a field of " + typeKey
	}
	if kind.IsMulti() {
		return groupBy + ": use git work issue add/remove"
	}
	return ""
}

// groupValue is what a drop into a group writes: the value a row already in
// it holds, as it is stored, and null for `(none)`.
func groupValue(raw any, group string) issue.Value {
	if group == noGroup {
		return issue.MustValue(nil)
	}
	return issue.MustValue(raw)
}

// groupLabel is the group an issue is drawn under: its `group_by` field as a
// cell draws it (cellText, so a relation is the issue it names, as on any
// cell), and (none) when the field is empty or nothing groups the view.
//
// The label only names the group. What a drop or a ghost writes is never
// read back from it, but from the raw value of a row already in the group
// (groupValue, groupPrefill), and the matrix's query keeps that raw value too.
func (k *kinds) groupLabel(typeKey, groupBy string, fields map[string]any) string {
	if groupBy == "" {
		return noGroup
	}
	if text := k.cellText(typeKey, groupBy, fields[groupBy]); text != "" {
		return text
	}
	return noGroup
}

// Groups are ordered by the natural order of the value they stand for, never
// by where the query first put a row in them (E4, doc/design/empty-groups.md):
// an enum in schema order, a bool false then true, a relation by the issue it
// names in (rank, id), a number or a date ascending, anything else by the
// label it draws, and (none) last, because the issues nobody has filed under
// the grouping field yet are what a session works through. The order the
// groups first appear in only breaks a tie — two unranked issues, two values
// the schema does not list — so a group's place does not depend on the
// query and is still stable across a refresh. It is one rule for the list,
// the gantt, the board and the matrix (Q2).

// groupSort is where one group goes among the others, compared by lessGroup.
type groupSort struct {
	// none is the group of the rows with no value, drawn last.
	none bool
	// known says the value has a place of its own; one with none (an
	// unranked issue) follows the ones that do, in first-seen order.
	known bool
	ints  []int
	num   float64
	text  string
}

// lessGroup says whether one group is drawn before another; two groups
// neither of which is less are a tie, which a stable sort keeps in the order
// they first appeared.
func lessGroup(left, right groupSort) bool {
	if left.none != right.none {
		return right.none
	}
	if left.known != right.known {
		return left.known
	}
	if !left.known {
		return false
	}
	for i := 0; i < len(left.ints) && i < len(right.ints); i++ {
		if left.ints[i] != right.ints[i] {
			return left.ints[i] < right.ints[i]
		}
	}
	if len(left.ints) != len(right.ints) {
		return len(left.ints) < len(right.ints)
	}
	if left.num != right.num {
		return left.num < right.num
	}
	return left.text < right.text
}

// sortGroups orders groups, given in the order they first appear, by the
// values they stand for (E4).
func sortGroups[T any](groups []T, sortOf func(T) groupSort) {
	sort.SliceStable(groups, func(i, j int) bool {
		return lessGroup(sortOf(groups[i]), sortOf(groups[j]))
	})
}

// fieldValues is what the schema says about one field over the types a view
// draws, the way a board reads its columns: the kind, the enum values in
// schema order, and their names. Every type owns its own field (e7e58f2), so
// the first type's order wins and later types only add what it did not have;
// with no types drawn at all, every type counts.
func fieldValues(repo *cache.RepoCache, key string, drawn map[string]bool) (kind schema.Kind, order []string, names map[string]string) {
	names = map[string]string{}
	s, err := repo.LoadSchema()
	if err != nil {
		kind, _ = schema.BuiltinKind(key)
		return kind, nil, names
	}
	for _, typeKey := range s.TypeKeys() {
		if len(drawn) > 0 && !drawn[typeKey] {
			continue
		}
		field, ok := s.Field(typeKey, key)
		if !ok {
			continue
		}
		if kind == "" {
			kind = field.Kind
		}
		for _, value := range field.Values {
			if _, named := names[value.Id]; !named {
				names[value.Id] = value.Name
				order = append(order, value.Id)
			}
		}
	}
	if kind == "" {
		kind, _ = schema.BuiltinKind(key)
	}
	return kind, order, names
}

// isEnum says whether a kind's values are the ones the schema lists.
func isEnum(kind schema.Kind) bool {
	return kind == schema.KindEnum || kind == schema.KindOrdinalEnum || kind == schema.KindMultiEnum
}

// groupOrder is the natural order of one `group_by` field's values over the
// types a view draws.
type groupOrder struct {
	repo  *cache.RepoCache
	kind  schema.Kind
	place map[string]int
	ranks map[string]string
}

func newGroupOrder(repo *cache.RepoCache, groupBy string, drawn map[string]bool) *groupOrder {
	kind, order, _ := fieldValues(repo, groupBy, drawn)
	if groupBy == schema.TypeKey {
		// the built-in type lists no values: the types are, in schema order
		order = nil
		if s, err := repo.LoadSchema(); err == nil {
			order = s.TypeKeys()
		}
	}
	o := &groupOrder{repo: repo, kind: kind, place: map[string]int{}, ranks: map[string]string{}}
	for at, value := range order {
		o.place[value] = at
	}
	return o
}

// sortOf is where the group of one value goes: raw is the value as it is
// stored, label the group it is drawn under.
func (o *groupOrder) sortOf(raw any, label string) groupSort {
	if label == noGroup {
		return groupSort{none: true}
	}
	switch {
	case isEnum(o.kind):
		var ints []int
		for _, value := range axisValuesOf(raw) {
			at, listed := o.place[value]
			if !listed {
				// after every listed value, tied with the other unlisted
				at = len(o.place)
			}
			ints = append(ints, at)
		}
		return groupSort{known: true, ints: ints}
	case o.kind == schema.KindBool:
		if raw == true {
			return groupSort{known: true, ints: []int{1}}
		}
		return groupSort{known: true, ints: []int{0}}
	case isRelation(o.kind):
		// a multi-relation by the first issue it names
		ids := linkIds(raw)
		if len(ids) == 0 {
			return groupSort{}
		}
		rank := o.rankOf(ids[0])
		if rank == "" {
			return groupSort{}
		}
		return groupSort{known: true, text: rank + "\x00" + ids[0]}
	case o.kind == schema.KindNumber:
		if number, ok := asNumber(raw); ok {
			return groupSort{known: true, num: number}
		}
		return groupSort{}
	case o.kind == schema.KindDate:
		return groupSort{known: true, text: plainValue(raw)}
	}
	// text, an identity by the name it draws, and anything else
	return groupSort{known: true, text: label}
}

// rankOf is the rank of the issue a relation group names, "" when it has
// none or this clone has not pulled it.
func (o *groupOrder) rankOf(id string) string {
	if rank, seen := o.ranks[id]; seen {
		return rank
	}
	rank := ""
	if excerpt, err := o.repo.Issues().ResolveExcerpt(entity.Id(id)); err == nil {
		rank, _ = excerpt.FieldString(schema.RankKey)
	}
	o.ranks[id] = rank
	return rank
}

// placeGroups gives every grouped row of a list or a gantt its group's place
// (E4), each layer's `group_by` ordered over the types of the rows that
// layer draws.
func placeGroups(repo *cache.RepoCache, nodes []treeRow, groupByAt func(level int) string,
	rowAt func(index int) (typeKey string, fields map[string]any)) {
	drawn := map[string]map[string]bool{}
	for index, node := range nodes {
		groupBy := groupByAt(node.level)
		if groupBy == "" || node.ghost {
			continue
		}
		if drawn[groupBy] == nil {
			drawn[groupBy] = map[string]bool{}
		}
		typeKey, _ := rowAt(index)
		drawn[groupBy][typeKey] = true
	}
	orders := map[string]*groupOrder{}
	for groupBy, types := range drawn {
		orders[groupBy] = newGroupOrder(repo, groupBy, types)
	}
	for index := range nodes {
		node := &nodes[index]
		groupBy := groupByAt(node.level)
		if groupBy == "" || node.ghost {
			continue
		}
		_, fields := rowAt(index)
		node.groupSort = orders[groupBy].sortOf(fields[groupBy], node.group)
	}
}

// rootGroups is the groups a list's or a gantt's roots fall in, in the order
// they first appear (arrange draws them in their own order), and the types of the roots in each, which is what a
// ghost per group is made of (ghost.go): one group, (none), when there are
// no roots at all.
func rootGroups(nodes []treeRow, typeOf func(index int) string) ([]string, map[string][]string) {
	groups := []string{}
	types := map[string][]string{}
	for index, node := range nodes {
		if node.level != 0 || node.ghost {
			continue
		}
		if _, seen := types[node.group]; !seen {
			groups = append(groups, node.group)
		}
		types[node.group] = append(types[node.group], typeOf(index))
	}
	if len(groups) == 0 {
		groups = append(groups, noGroup)
	}
	return groups, types
}
