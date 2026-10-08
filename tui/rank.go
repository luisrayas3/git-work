package tui

import (
	"fmt"
	"slices"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/rank"
)

// The drop's arithmetic, shared by the three kinds that drag a row:
// the list, the board and the gantt.
//
// A rank is null until a drop writes one (`configurable-schema.md` D8) and
// null sorts after every key, so on a store nobody has ranked — which is
// every store until somebody drags something — the issue dropped first would
// become the only ranked issue in its scope and jump to the top of it,
// whatever position it was dropped at. So a drop that writes a rank first
// gives one to every row drawn **above** the drop point in the same ordering
// scope that has none, in the order they are drawn, and only then computes
// the dropped row's own key between its neighbours: after the drop the screen
// reads exactly as it did, plus the move.
//
// Rows below the drop point keep no rank. They already sort after everything
// ranked, in the query's own order, which is where they are drawn.

// rankFill is one key a drop writes: an issue, and the rank it is given.
type rankFill struct {
	id  string
	key string
}

// fillRanks reads one ordering scope — a group and nesting level of a list
// or a gantt, a stack of a board — in the order it is drawn, with the
// grabbed entry already moved to `at`, and answers the whole drop:
// the keys for the unranked entries drawn above it, topmost first,
// and then the grabbed entry's own key, between its new neighbours.
//
// Each key it returns is one issue's, so each is its own `set`:
// there is no multi-entity commit (AGENTS.md).
func fillRanks(ids, ranks []string, at int) (above []rankFill, key string, err error) {
	if at < 0 || at >= len(ranks) {
		return nil, "", fmt.Errorf("nothing to rank at %d of %d", at, len(ranks))
	}

	// The nearest stored rank below each entry, the grabbed one excepted:
	// its key is computed from the fills, so it constrains nothing.
	below := make([]string, len(ranks))
	next := ""
	for i := len(ranks) - 1; i >= 0; i-- {
		below[i] = next
		if i != at && ranks[i] != "" {
			next = ranks[i]
		}
	}

	// Top down, so each key is above the one before it: a stored rank sets
	// the floor for what follows, and a filled one raises it the same way.
	lo := ""
	for i := range at {
		if ranks[i] != "" {
			lo = ranks[i]
			continue
		}
		filled, err := rank.Between(lo, below[i])
		if err != nil {
			return nil, "", err
		}
		above = append(above, rankFill{id: ids[i], key: filled})
		lo = filled
	}

	key, err = rank.Between(lo, below[at])
	if err != nil {
		return nil, "", err
	}
	return above, key, nil
}

// scopeOf is the siblings of the row at order[at] — its group and its
// nesting level under its parent — as positions in order, in the order they
// are drawn, and where the row itself is among them.
func scopeOf(rows []treeRow, order []int, at int) (scope []int, me int) {
	for i := siblingAt(rows, order, at, -1); i >= 0; i = siblingAt(rows, order, i, -1) {
		scope = append(scope, i)
	}
	slices.Reverse(scope)
	me = len(scope)
	scope = append(scope, at)
	for i := siblingAt(rows, order, at, 1); i >= 0; i = siblingAt(rows, order, i, 1) {
		scope = append(scope, i)
	}
	return scope, me
}

// scopeFills is fillRanks over a tree: the siblings of the row at order[at],
// which is its group and its nesting level, in the order they are drawn.
// The scope is plain: a level is all keyed rows or none (checkLevels), and
// a keyed level's drop is the view's to hold (keep.go).
func scopeFills(rows []treeRow, order []int, at int) (above []rankFill, key string, err error) {
	scope, me := scopeOf(rows, order, at)

	ids := make([]string, len(scope))
	ranks := make([]string, len(scope))
	for i, s := range scope {
		ids[i] = rows[order[s]].id
		ranks[i] = rows[order[s]].rank
	}
	return fillRanks(ids, ranks, me)
}

// writeFills writes the keys of the rows drawn above the drop point, one
// `set` per issue, before the dropped row's own write. A failure halfway
// stops there: the rows already written keep the order they were drawn in.
func writeFills(repo *cache.RepoCache, above []rankFill, rankKey string) error {
	for _, fill := range above {
		fields := map[string]issue.Value{rankKey: issue.StringValue(fill.key)}
		if _, err := host.IssueSet(repo, fill.id, fields, false); err != nil {
			return err
		}
	}
	return nil
}

// saidAnd adds the rows a drop ranked besides the one that moved to what the
// status line says, because a drop that wrote four issues should not read
// like a drop that wrote one.
func saidAnd(said string, above []rankFill) string {
	if len(above) == 0 {
		return said
	}
	return fmt.Sprintf("%s · %d ranked", said, len(above))
}
