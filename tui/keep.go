package tui

// A keyed row reorders for this view only (doc/design/query-rows.md, R5,
// settled 2026-10-08).
//
// A row whose key is not its id, or that has no id, stands for a share of an
// issue or for none, so writing the issue's rank would reorder every row
// standing for it. Such a row is still grabbed and moved, but the drop
// writes nothing: it records the scope's order, by key, in the view
// instance, and that order holds across refreshes until the view is quit.
// A key it does not know keeps its (rank, id) place after the ones it does.
// Nothing persists between invocations.

// keptSaid is the status line of a drop the view holds.
const keptSaid = "order kept for this view"

// keepOrder records the scope of the row at order[at], as it is drawn now,
// by key: its siblings' places, the row's own among them.
func keepOrder(rows []treeRow, order []int, at int, kept map[string]int) {
	scope, _ := scopeOf(rows, order, at)
	for place, s := range scope {
		kept[rows[order[s]].key] = place
	}
	applyKept(rows, kept)
}

// scopeKept says a drop in the scope of the row at order[at] has to be kept
// by the view as it is drawn, whatever it also writes: the view holds an
// order for some row of it already, or some row of it is keyed, whose place
// no rank written can hold.
func scopeKept(rows []treeRow, order []int, at int) bool {
	scope, _ := scopeOf(rows, order, at)
	for _, s := range scope {
		if row := rows[order[s]]; row.known || row.keyed() {
			return true
		}
	}
	return false
}

// applyKept marks the rows the view holds an order for with their place in
// it, which is what arrange orders them by.
func applyKept(rows []treeRow, kept map[string]int) {
	for at := range rows {
		rows[at].kept, rows[at].known = kept[rows[at].key]
	}
}

// lessInScope is the order of one scope's siblings: the rows the view holds
// an order for first, in that order, then the rest by (rank, id), the
// unranked keeping the query's order at the end.
func lessInScope(left, right treeRow) bool {
	if left.known != right.known {
		return left.known
	}
	if left.known {
		return left.kept < right.kept
	}
	return lessByRank(left.rank, left.tie(), right.rank, right.tie())
}
