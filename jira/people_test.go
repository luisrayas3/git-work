package jira_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// JS16 end to end: an assignee imported from Jira is an identity named as
// Jira names the account and carrying its account id, so the field holds
// that identity's id and every text surface reads it as the person.
func TestImportedAssigneeIsANamedIdentity(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Assigned in Jira")
	w.srv.Edit(key, map[string]any{"assignee": map[string]string{"accountId": jiratest.RaviID}})
	w.mustSync(jira.Options{})

	id, ok := issue.String(issue.Value(field(t, ic, "assignee")))
	require.True(t, ok, "assignee is %s", field(t, ic, "assignee"))
	person, err := w.c.Identities().Resolve(entity.Id(id))
	require.NoError(t, err)
	require.Equal(t, "Ravi Patel", person.Name())
	require.Equal(t, jiratest.RaviID, person.ImmutableMetadata()[jira.MetaAccountId])
	require.Equal(t, "Ravi Patel", host.UserName(w.c, id))
}
