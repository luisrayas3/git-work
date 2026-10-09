package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
)

// Groups are ordered by the value they stand for, never by where the query
// first put a row in them (doc/design/empty-groups.md, E4), on every view
// (Q2).

// rootGroupOrder is the groups a list's or a gantt's roots are drawn in.
func rootGroupOrder(nodes []treeRow, order []int) []string {
	var groups []string
	for _, index := range order {
		node := nodes[index]
		if node.level != 0 {
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1] != node.group {
			groups = append(groups, node.group)
		}
	}
	return groups
}

// priorities is three tasks the query puts in the order low, highest,
// medium, and one with no priority first of all.
func priorities(t *testing.T) (repo *cache.RepoCache) {
	t.Helper()

	repo = testRepo(t)
	withDates(t, repo)
	dated := func(title, priority string) {
		fields := map[string]any{"title": title, "start": "2026-09-07", "stop": "2026-09-13", "status": "to-do"}
		if priority != "" {
			fields["priority"] = priority
		}
		newIssue(t, repo, fields)
	}
	dated("a", "")
	dated("b", "low")
	dated("c", "highest")
	dated("d", "medium")
	return repo
}

const byTitle = `"query":"sort_by(.fields.title)"`

func TestGroupsFollowTheSchemaOrder(t *testing.T) {
	repo := priorities(t)
	want := []string{"highest", "medium", "low", noGroup}

	b := board(t, repo, `{"columns":"status","group_by":"priority",`+byTitle+`}`)
	require.Equal(t, want, laneLabels(b), "the board's lanes")

	l := list(t, repo, `{"fields":["title"],"group_by":"priority",`+byTitle+`}`)
	require.Equal(t, want, rootGroupOrder(l.nodes, l.order), "the list's groups")

	g := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"priority",`+byTitle+`}`)
	require.Equal(t, want, rootGroupOrder(g.nodes, g.order), "the gantt's groups")

	m := matrix(t, repo, `{"rows":"status","columns":"status","group_by":"priority",`+byTitle+`}`)
	var blocks []string
	for _, block := range m.groups {
		blocks = append(blocks, block.label)
	}
	require.Equal(t, want, blocks, "the matrix's blocks")
}

// TestGroupsByABool: false, then true, whatever the query says.
func TestGroupsByABool(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "a", "status": "to-do", "archived": true})
	newIssue(t, repo, map[string]any{"title": "b", "status": "to-do", "archived": false})

	b := board(t, repo, `{"columns":"status","group_by":"archived","include_archive":true,`+byTitle+`}`)
	require.Equal(t, []string{"false", "true"}, laneLabels(b))
}

// TestRelationLanesByRank: a lane over a relation is the issue it names, in
// (rank, id); the unranked follow in the order they first appear.
func TestRelationLanesByRank(t *testing.T) {
	repo := testRepo(t)
	first := newTyped(t, repo, "epic", map[string]any{"title": "ranked first", "rank": "a"})
	second := newTyped(t, repo, "epic", map[string]any{"title": "ranked second", "rank": "b"})
	seen := newTyped(t, repo, "epic", map[string]any{"title": "unranked seen first"})
	later := newTyped(t, repo, "epic", map[string]any{"title": "unranked seen later"})

	// the query meets them in the order seen, later, second, first
	newIssue(t, repo, map[string]any{"title": "1", "status": "to-do", "parent": seen})
	newIssue(t, repo, map[string]any{"title": "2", "status": "to-do", "parent": later})
	newIssue(t, repo, map[string]any{"title": "3", "status": "to-do", "parent": second})
	newIssue(t, repo, map[string]any{"title": "4", "status": "to-do", "parent": first})

	page := board(t, repo, `{"columns":"status","group_by":"parent",`+
		`"query":"map(select(.fields.type == \"task\")) | sort_by(.fields.title)"}`)
	lanes := laneLabels(page)
	require.Len(t, lanes, 4)
	for at, title := range []string{"ranked first", "ranked second", "unranked seen first", "unranked seen later"} {
		require.Contains(t, lanes[at], title, "lane %d", at)
	}
	require.Equal(t, first, page.lanes[0].raw, "the lane carries the full id")
}
