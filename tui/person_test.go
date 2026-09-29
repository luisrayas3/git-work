package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
)

// jiraUser is an identity the way the Jira sync makes one (JS16): the Jira
// display name, no email, the account id as immutable metadata.
func jiraUser(t *testing.T, repo *cache.RepoCache, name string) string {
	t.Helper()

	i, err := repo.Identities().NewRaw(name, "", "", "", nil, map[string]string{"jira-account-id": "5b10ac8d82e05b22cc7d4ef5"})
	require.NoError(t, err)
	return i.Id().String()
}

// TestAPersonIsDrawnByName: a people field holds an identity's whole id and
// every surface draws the name instead, a group header included.
func TestAPersonIsDrawnByName(t *testing.T) {
	repo := testRepo(t)
	ada := jiraUser(t, repo, "Ada Lovelace")
	id := newIssue(t, repo, map[string]any{"title": "one", "status": "to-do", "assignee": ada})

	drawn := plainView(list(t, repo, `{"fields":["title","assignee"],"group_by":"assignee"}`))
	require.Contains(t, drawn, "Ada Lovelace")
	require.NotContains(t, drawn, ada[:7])

	drawn = plainView(show(t, repo, id, nil))
	require.Contains(t, drawn, "Ada Lovelace")
	require.NotContains(t, drawn, ada[:7])

	page := board(t, repo, `{"columns":"assignee","card":["title","assignee"],"group_by":"assignee"}`)
	require.Equal(t, []string{"Ada Lovelace"}, columnLabels(page))
	drawn = plainView(page)
	require.NotContains(t, drawn, ada[:7])
	require.Contains(t, drawn, "\nAda Lovelace", "the lane is headed by name")
	// the column is still the id: a drop writes the value, not the label
	require.Equal(t, ada, page.columns[0].value)
}

// TestAGanttLabelIsAName: a bar labelled by a people field reads as the
// person.
func TestAGanttLabelIsAName(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	ada := jiraUser(t, repo, "Ada Lovelace")
	newTyped(t, repo, "task", map[string]any{"title": "one", "start": "2026-09-28", "stop": "2026-10-02", "assignee": ada})

	drawn := plainView(gantt(t, repo, `{"start":"start","stop":"stop","label":"assignee"}`))
	require.Contains(t, drawn, "Ada Lovelace")
	require.NotContains(t, drawn, ada[:7])
}

// TestEditAPersonPicksANameAndWritesTheId: the picker lists people by name,
// opens on the one the cell shows, and what it writes is the identity's id.
func TestEditAPersonPicksANameAndWritesTheId(t *testing.T) {
	repo := testRepo(t)
	ada := jiraUser(t, repo, "Ada Lovelace")
	me, err := repo.GetUserIdentity()
	require.NoError(t, err)
	id := newIssue(t, repo, map[string]any{"title": "one", "assignee": ada})

	page := list(t, repo, `{"fields":["title","assignee"]}`)
	page = send(page, "l", "l", "enter").(*listPage)
	require.NotNil(t, page.editor)
	picker := page.editor.picker
	require.Equal(t, ada, picker.items[picker.cursor].value, "opens on the current value")
	require.Equal(t, "Ada Lovelace", picker.items[picker.cursor].label)
	require.Contains(t, plainView(page), "John Doe")

	// the people come in the store's order, so walk to John whichever way
	// he is
	john := -1
	for at, item := range picker.items {
		if item.value == me.Id().String() {
			john = at
		}
	}
	require.NotEqual(t, -1, john)
	for picker.cursor < john {
		page = send(page, "j").(*listPage)
	}
	for picker.cursor > john {
		page = send(page, "k").(*listPage)
	}
	page = send(page, "enter").(*listPage)
	require.Nil(t, page.editor)
	require.Equal(t, me.Id().String(), fieldOf(t, repo, id, "assignee"))

	row := rowOf(page, id)
	require.True(t, strings.Contains(row, "John Doe"), row)
}
