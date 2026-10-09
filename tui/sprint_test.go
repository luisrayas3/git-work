package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/view"
)

// sprintPairs is the workflow's query (doc/design/query-rows.md): one row
// per (person, epic) they hold an open story in, its id the epic, its key
// epic@person, its assignee shaped to the person and its children that
// person's open stories in it, sorted by the epic's initiative; and a
// (none) row per person for their stories with no epic, a row with no id,
// typed as its siblings are, because a level is all keyed rows or none and
// a row with no id names its type.
const sprintPairs = `. as $all
| [$all[] | select(.fields.type == "story" and .fields.status != "done")] as $stories
| [$stories[] | .fields.assignee | select(. != null)] | unique
| map(. as $who
  | [$stories[] | select(.fields.assignee == $who)] as $mine
  | ([$mine[] | .fields.parent | select(. != null)] | unique) as $epics
  | ([$all[] | . as $e | select(any($epics[]; . == $e.id))]
     | sort_by([(.fields.parent as $p | [$all[] | select(.id == $p) | .fields.title] | first) // "", .fields.title])
     | map(. as $e | $e + {
         key: ($e.id + "@" + $who),
         fields: ($e.fields + {assignee: $who}),
         children: [$mine[] | select(.fields.parent == $e.id) | .id]}))
    + ([$mine[] | select(.fields.parent == null) | .id] as $loose
       | if ($loose | length) > 0
         then [{key: ("none@" + $who), fields: {title: "(none)", type: "epic", assignee: $who}, children: $loose}]
         else [] end))
| add`

// TestTheSprintPageByPerson is the workflow the design was written for, end
// to end: under each person a row per epic they hold an open story in,
// sorted by initiative, that person's stories under it and nobody else's,
// a (none) row for the stories with no epic, Big epic 1 drawn twice with
// different children, and the page open (R1 to R5).
func TestTheSprintPageByPerson(t *testing.T) {
	repo := testRepo(t)
	ada := jiraUser(t, repo, "Ada Lovelace")
	grace := jiraUser(t, repo, "Grace Hopper")

	one := newTyped(t, repo, "initiative", map[string]any{"title": "Initiative 1"})
	two := newTyped(t, repo, "initiative", map[string]any{"title": "Initiative 2"})
	cool := newTyped(t, repo, "epic", map[string]any{"title": "Do something cool", "parent": two, "status": "in-progress"})
	big1 := newTyped(t, repo, "epic", map[string]any{"title": "Big epic 1", "parent": one, "status": "in-progress"})
	big2 := newTyped(t, repo, "epic", map[string]any{"title": "Big epic 2", "parent": one, "status": "to-do"})

	story := func(title, epic, who string) string {
		fields := map[string]any{"title": title, "assignee": who, "status": "to-do"}
		if epic != "" {
			fields["parent"] = epic
		}
		return newTyped(t, repo, "story", fields)
	}
	girl := story("Story about a girl", big1, ada)
	story("Story about a boy", big2, ada)
	story("Story about a boy and a girl", big2, ada)
	story("Some story", cool, ada)
	loose := story("A story with no epic", "", ada)
	hers := story("Grace's story in big epic 1", big1, grace)
	newTyped(t, repo, "story", map[string]any{"title": "A done story", "parent": big1, "assignee": ada, "status": "done"})

	page, err := newListPage(repo, call(t, view.KindList, map[string]any{
		"query":    sprintPairs,
		"fields":   []string{"parent", "title", "status"},
		"group_by": "assignee",
		"expand":   map[string]any{"fields": []string{"status", "priority", "title"}},
		"open":     true,
	}))
	require.NoError(t, err)
	page.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	require.Empty(t, page.status)

	// the page reads top to bottom as the design draws it
	// (the people by name, which is how an identity orders its groups)
	drawn := plainView(page)
	adas := []string{
		"Ada Lovelace",
		"Initiative 1", "Big epic 1", "Story about a girl",
		"Initiative 1", "Big epic 2", "Story about a boy", "Story about a boy and a girl",
		"Initiative 2", "Do something cool", "Some story",
		"(none)", "A story with no epic",
	}
	graces := []string{"Grace Hopper", "Initiative 1", "Big epic 1", "Grace's story in big epic 1"}
	sections := append(adas, graces...)
	at := 0
	for _, text := range sections {
		next := strings.Index(drawn[at:], text)
		require.GreaterOrEqual(t, next, 0, "%s after %q", text, drawn[:at])
		at += next + len(text)
	}
	require.NotContains(t, drawn, "A done story")
	require.Equal(t, 1, strings.Count(drawn, "Story about a girl"), "a story is under its own person's epic only")
	require.Equal(t, 1, strings.Count(drawn, "Grace's story"))
	require.Equal(t, 2, strings.Count(drawn, "Big epic 1"), "one epic, drawn once per person")

	// the tree is the cross the query computed, opened
	require.Equal(t, big1+"@"+ada, nodeOf(t, page, girl).parent)
	require.Equal(t, big1+"@"+grace, nodeOf(t, page, hers).parent)
	require.Equal(t, "none@"+ada, nodeOf(t, page, loose).parent)
	for _, key := range []string{big1 + "@" + ada, big2 + "@" + ada, cool + "@" + ada, "none@" + ada, big1 + "@" + grace} {
		require.False(t, nodeOf(t, page, key).folded, key)
	}
	require.Equal(t, 2, nodeOf(t, page, big2+"@"+ada).children)

	// Grace's copy of the epic is the epic: enter opens it, space edits it
	cursorTo(t, page, big1+"@"+grace)
	_, cmd := page.Update(press("enter"))
	require.Equal(t, big1, pushed(cmd))
	require.Equal(t, big1, page.currentId())

	// the (none) row stands for nothing
	cursorTo(t, page, "none@"+ada)
	_, cmd = page.Update(press("enter"))
	require.Equal(t, "", pushed(cmd))

	// Ada's epics reorder for this view only, and the order holds across a
	// refresh
	cursorTo(t, page, cool+"@"+ada)
	page = send(page, "space", "up", "up", "space").(*listPage)
	require.Equal(t, "order kept for this view", page.status)
	require.Equal(t, "", fieldOf(t, repo, cool, "rank"))
	page.Update(refreshMsg{})
	var roots []string
	for _, key := range drawnKeys(page) {
		if nodeOf(t, page, key).level == 0 && strings.HasSuffix(key, "@"+ada) {
			roots = append(roots, key)
		}
	}
	require.Equal(t, []string{cool + "@" + ada, big1 + "@" + ada, big2 + "@" + ada, "none@" + ada}, roots)

	// the person's fold survives the refresh too
	cursorTo(t, page, big2+"@"+ada)
	page = send(page, "right", "space").(*listPage)
	page.Update(refreshMsg{})
	require.True(t, nodeOf(t, page, big2+"@"+ada).folded)
}
