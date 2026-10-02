package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
)

// A drop on a store where nothing has a rank ranks what is drawn above it,
// so that the screen reads as it was drawn plus the move (rank.go).
//
// What the tests check is the store, not the drawing: how many operations
// each issue has, because the rule is about which issues a drop writes, and
// a view that redrew right having written four issues to move one would
// still be wrong.

// writes counts the operations on an issue, which is how many times a drop
// wrote it: an issue nobody touched keeps the one its creation made.
func writes(t *testing.T, repo *cache.RepoCache, id string) int {
	t.Helper()

	ops, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	return len(ops)
}

// barIds is the gantt's rows in the order they are drawn.
func barIds(p *ganttPage) []string {
	ids := make([]string, 0, len(p.order))
	for _, at := range p.order {
		ids = append(ids, p.bars[at].id)
	}
	return ids
}

// fourUnranked is four issues, titled so that a query can order them, and
// none of them ranked — which is every store until somebody drags something.
func fourUnranked(t *testing.T, repo *cache.RepoCache) (a, b, c, d string) {
	t.Helper()

	return newIssue(t, repo, map[string]any{"title": "a", "status": "to-do"}),
		newIssue(t, repo, map[string]any{"title": "b", "status": "to-do"}),
		newIssue(t, repo, map[string]any{"title": "c", "status": "to-do"}),
		newIssue(t, repo, map[string]any{"title": "d", "status": "to-do"})
}

// TestListDropRanksTheUnrankedRowsAbove: the third row dragged to second
// ranks the first row too, and nothing else, so the order after the refresh
// is the order it was dropped in.
func TestListDropRanksTheUnrankedRowsAbove(t *testing.T) {
	repo := testRepo(t)
	a, b, c, d := fourUnranked(t, repo)

	page := list(t, repo, `{"query":"sort_by(.fields.title)","fields":["title"]}`)
	require.Equal(t, []string{a, b, c, d}, drawnIds(page))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*listPage)
	require.Equal(t, -1, page.grabbed)

	require.Equal(t, []string{a, c, b, d}, drawnIds(page))
	require.Less(t, fieldOf(t, repo, a, "rank"), fieldOf(t, repo, c, "rank"))
	require.Equal(t, "", fieldOf(t, repo, b, "rank"), "below the drop point: still the query's order")
	require.Equal(t, "", fieldOf(t, repo, d, "rank"))

	require.Equal(t, was+1, writes(t, repo, a), "one commit per issue")
	require.Equal(t, was+1, writes(t, repo, c))
	require.Equal(t, was, writes(t, repo, b))
	require.Equal(t, was, writes(t, repo, d))
	require.Contains(t, plainView(page), "1 ranked")
}

// TestListDropAtTheTopRanksOnlyTheRowItMoved: nothing is drawn above the
// top, so the drop that always worked is the drop that still works.
func TestListDropAtTheTopRanksOnlyTheRowItMoved(t *testing.T) {
	repo := testRepo(t)
	a, b, c, d := fourUnranked(t, repo)

	page := list(t, repo, `{"query":"sort_by(.fields.title)","fields":["title"]}`)
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "k", "space").(*listPage)

	require.Equal(t, []string{c, a, b, d}, drawnIds(page))
	require.NotEqual(t, "", fieldOf(t, repo, c, "rank"))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
		require.Equal(t, "", fieldOf(t, repo, id, "rank"))
	}
	require.NotContains(t, plainView(page), "ranked", "nothing was filled, so nothing is counted")
}

// TestListDropOnARankedStoreWritesOneKey: where every row has a rank there
// is nothing to fill, and the drop is the single write it has always been.
func TestListDropOnARankedStoreWritesOneKey(t *testing.T) {
	repo := testRepo(t)
	a := newIssue(t, repo, map[string]any{"title": "a", "rank": "a"})
	b := newIssue(t, repo, map[string]any{"title": "b", "rank": "b"})
	c := newIssue(t, repo, map[string]any{"title": "c", "rank": "c"})
	d := newIssue(t, repo, map[string]any{"title": "d", "rank": "d"})

	page := list(t, repo, `{"fields":["title"]}`)
	require.Equal(t, []string{a, b, c, d}, drawnIds(page))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*listPage)

	require.Equal(t, []string{a, c, b, d}, drawnIds(page))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
	}
}

// TestListNestedDropRanksOnlyTheSiblingsAbove: the ordering scope is the
// group and, nested, the parent — so a child's drop never ranks a root, nor
// another parent's children.
func TestListNestedDropRanksOnlyTheSiblingsAbove(t *testing.T) {
	repo := testRepo(t)
	first := newTyped(t, repo, "story", map[string]any{"title": "first story"})
	elder := newTyped(t, repo, "task", map[string]any{"title": "elder", "parent": first})
	second := newTyped(t, repo, "story", map[string]any{"title": "second story"})
	a := newTyped(t, repo, "task", map[string]any{"title": "a", "parent": second})
	b := newTyped(t, repo, "task", map[string]any{"title": "b", "parent": second})
	c := newTyped(t, repo, "task", map[string]any{"title": "c", "parent": second})
	d := newTyped(t, repo, "task", map[string]any{"title": "d", "parent": second})

	page := list(t, repo, `{"fields":["title"],`+
		`"expand":{"relation":"children","query":"sort_by(.fields.title)"},`+
		`"query":"map(select(.fields.type == \"story\")) | sort_by(.fields.title)"}`)
	page = send(page, "Z").(*listPage) // a nested view opens folded
	require.Equal(t, []string{first, elder, second, a, b, c, d}, drawnIds(page))
	was := writes(t, repo, a)

	// into the second story's children, down to c, and up one
	page = send(page, "j", "tab", "j", "j", "space", "k", "space").(*listPage)

	require.Equal(t, []string{first, elder, second, a, c, b, d}, drawnIds(page))
	require.Less(t, fieldOf(t, repo, a, "rank"), fieldOf(t, repo, c, "rank"))
	require.Equal(t, was+1, writes(t, repo, a))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{first, elder, second, b, d} {
		require.Equal(t, was, writes(t, repo, id), "another scope, or below the drop point")
		require.Equal(t, "", fieldOf(t, repo, id, "rank"))
	}
}

// TestBoardDropRanksTheUnrankedCardsAbove: a board's scope is the stack —
// one lane's one column — and the cards above the drop point in it.
func TestBoardDropRanksTheUnrankedCardsAbove(t *testing.T) {
	repo := testRepo(t)
	a, b, c, d := fourUnranked(t, repo)
	other := newIssue(t, repo, map[string]any{"title": "other", "status": "done"})

	page := board(t, repo, `{"query":"sort_by(.fields.title)","columns":"status"}`)
	require.Equal(t, []string{a, b, c, d}, stackIds(page, 0, 1))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*boardPage)
	require.Equal(t, -1, page.grabbed)

	require.Equal(t, []string{a, c, b, d}, stackIds(page, 0, 1))
	require.Less(t, fieldOf(t, repo, a, "rank"), fieldOf(t, repo, c, "rank"))
	require.Equal(t, was+1, writes(t, repo, a))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{b, d, other} {
		require.Equal(t, was, writes(t, repo, id), "another stack, or below the drop point")
		require.Equal(t, "", fieldOf(t, repo, id, "rank"))
	}
	require.Contains(t, plainView(page), "1 ranked")
}

// TestBoardDropAtTheTopRanksOnlyTheCardItMoved: nothing is drawn above the
// top of a stack either.
func TestBoardDropAtTheTopRanksOnlyTheCardItMoved(t *testing.T) {
	repo := testRepo(t)
	a, b, c, d := fourUnranked(t, repo)

	page := board(t, repo, `{"query":"sort_by(.fields.title)","columns":"status"}`)
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "k", "space").(*boardPage)

	require.Equal(t, []string{c, a, b, d}, stackIds(page, 0, 1))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
	}
}

// TestBoardDropOnARankedStoreWritesOneKey: unchanged behavior, which is the
// behavior a board that has been dragged in once already has.
func TestBoardDropOnARankedStoreWritesOneKey(t *testing.T) {
	repo := testRepo(t)
	a := newIssue(t, repo, map[string]any{"title": "a", "status": "to-do", "rank": "a"})
	b := newIssue(t, repo, map[string]any{"title": "b", "status": "to-do", "rank": "b"})
	c := newIssue(t, repo, map[string]any{"title": "c", "status": "to-do", "rank": "c"})
	d := newIssue(t, repo, map[string]any{"title": "d", "status": "to-do", "rank": "d"})

	page := board(t, repo, `{"columns":"status"}`)
	require.Equal(t, []string{a, b, c, d}, stackIds(page, 0, 1))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*boardPage)

	require.Equal(t, []string{a, c, b, d}, stackIds(page, 0, 1))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
	}
}

// TestGanttDropRanksTheUnrankedRowsAbove: the chart reorders by rank like
// the list, so it fills what is above the drop point the same way.
func TestGanttDropRanksTheUnrankedRowsAbove(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	a, b, c, d := fourUnranked(t, repo)

	page := gantt(t, repo, `{"query":"sort_by(.fields.title)","start":"start","stop":"stop"}`)
	require.Equal(t, []string{a, b, c, d}, barIds(page))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*ganttPage)
	require.Equal(t, -1, page.grabbed)

	require.Equal(t, []string{a, c, b, d}, barIds(page))
	require.Less(t, fieldOf(t, repo, a, "rank"), fieldOf(t, repo, c, "rank"))
	require.Equal(t, was+1, writes(t, repo, a))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{b, d} {
		require.Equal(t, was, writes(t, repo, id))
		require.Equal(t, "", fieldOf(t, repo, id, "rank"))
	}
	require.Contains(t, plainView(page), "1 ranked")
}

// TestGanttDropAtTheTopRanksOnlyTheRowItMoved.
func TestGanttDropAtTheTopRanksOnlyTheRowItMoved(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	a, b, c, d := fourUnranked(t, repo)

	page := gantt(t, repo, `{"query":"sort_by(.fields.title)","start":"start","stop":"stop"}`)
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "k", "space").(*ganttPage)

	require.Equal(t, []string{c, a, b, d}, barIds(page))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
	}
}

// TestGanttDropOnARankedStoreWritesOneKey: unchanged behavior on the chart.
func TestGanttDropOnARankedStoreWritesOneKey(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	a := newIssue(t, repo, map[string]any{"title": "a", "rank": "a"})
	b := newIssue(t, repo, map[string]any{"title": "b", "rank": "b"})
	c := newIssue(t, repo, map[string]any{"title": "c", "rank": "c"})
	d := newIssue(t, repo, map[string]any{"title": "d", "rank": "d"})

	page := gantt(t, repo, `{"start":"start","stop":"stop"}`)
	require.Equal(t, []string{a, b, c, d}, barIds(page))
	was := writes(t, repo, a)

	page = send(page, "j", "j", "space", "k", "space").(*ganttPage)

	require.Equal(t, []string{a, c, b, d}, barIds(page))
	require.Equal(t, was+1, writes(t, repo, c))
	for _, id := range []string{a, b, d} {
		require.Equal(t, was, writes(t, repo, id))
	}
}

// TestFillRanksKeepsTheKeysAscending is the arithmetic on its own: whatever
// mix of stored and missing keys a scope has, what comes out reads top down.
func TestFillRanksKeepsTheKeysAscending(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ranks []string
		at    int
		above int
	}{
		{"nothing ranked", []string{"", "", "", ""}, 2, 2},
		{"at the top", []string{"", "", "", ""}, 0, 0},
		{"all ranked", []string{"a", "b", "c", "d"}, 2, 0},
		{"ranked above, not below", []string{"a", "b", "", ""}, 3, 1},
		{"one ranked in the middle", []string{"", "m", "", ""}, 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := make([]string, len(tc.ranks))
			for i := range ids {
				ids[i] = string(rune('1' + i))
			}

			above, key, err := fillRanks(ids, tc.ranks, tc.at)
			require.NoError(t, err)
			require.Len(t, above, tc.above)

			// the keys the drop writes, in the order they are drawn, plus the
			// stored ones they sit among: strictly ascending, top down
			keys := make([]string, len(tc.ranks))
			copy(keys, tc.ranks)
			for _, fill := range above {
				keys[int(fill.id[0]-'1')] = fill.key
			}
			keys[tc.at] = key
			for i := 1; i <= tc.at; i++ {
				require.NotEmpty(t, keys[i-1], "every row above the drop point is ranked")
				require.Less(t, keys[i-1], keys[i])
			}
			for i := tc.at + 1; i < len(keys); i++ {
				if keys[i] != "" {
					require.Less(t, keys[tc.at], keys[i])
				}
			}
		})
	}
}
