package jira_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// The schema half through the real path of JS5, a repository and
// host.SchemaImport: Derive, import, and the fixpoint and alias
// preservation that the in-memory tests assume.
func TestDeriveThroughImport(t *testing.T) {
	srv := jiratest.New(t)
	email, token := srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetries: -1})
	p, err := jira.Discover(context.Background(), c, "PROJ")
	require.NoError(t, err)

	repo, err := cache.NewRepoCacheNoEvents(repository.CreateGoGitTestRepo(t, false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	me, err := repo.Identities().New("Mia", "mia@example.com")
	require.NoError(t, err)
	require.NoError(t, repo.SetUserIdentity(me))

	data, err := os.ReadFile("../schema.yaml")
	require.NoError(t, err)
	tracker, err := schema.ParseDocument(data)
	require.NoError(t, err)
	_, _, err = host.SchemaImport(repo, tracker, false, false)
	require.NoError(t, err)

	derive := func() *schema.Document {
		current, _, err := host.SchemaExport(repo)
		require.NoError(t, err)
		doc, _, err := jira.Derive(current, p)
		require.NoError(t, err)
		return doc
	}
	changes, _, err := host.SchemaImport(repo, derive(), false, false)
	require.NoError(t, err)
	require.NotEmpty(t, changes)

	// D3: Derive(export(import(Derive(x)))) writes nothing
	changes, _, err = host.SchemaImport(repo, derive(), false, true)
	require.NoError(t, err)
	require.Empty(t, changes)

	// D5: the alias-free schema.yaml unmaps nothing, and export | import is zero operations
	_, _, err = host.SchemaImport(repo, tracker, false, false)
	require.NoError(t, err)
	s, err := repo.LoadSchema()
	require.NoError(t, err)
	require.Equal(t, "10002", s.Types["task"].Aliases["jira"])
	require.Equal(t, "customfield_10016", s.Types["task"].Fields["estimate"].Aliases["jira"])
	done, _ := s.Types["task"].Fields["status"].Value("done")
	require.Equal(t, "10002", done.Aliases["jira"])
	exported, _, err := host.SchemaExport(repo)
	require.NoError(t, err)
	changes, _, err = host.SchemaImport(repo, exported, false, true)
	require.NoError(t, err)
	require.Empty(t, changes)

	m, _, err := jira.Compile(s, p)
	require.NoError(t, err)
	local(t, repo, m)
}

// Local and NewIndex over a real store: canonical values, pairings read
// from operation metadata, and the index's tables.
func local(t *testing.T, repo *cache.RepoCache, m *jira.Mapping) {
	me, err := repo.GetUserIdentity()
	require.NoError(t, err)
	linked, _, err := repo.Issues().NewWithMetadata("Linked", "the body", map[string]issue.Value{
		"type": issue.StringValue("task"), "labels": issue.Value(`["b","a"]`), "estimate": issue.Value("3"),
	}, map[string]string{jira.MetaId: "10500", jira.MetaAlias: "PROJ-7"})
	require.NoError(t, err)
	_, paired, err := linked.AddCommentRaw(me, 1700000000, "from Jira", nil,
		map[string]string{jira.MetaCommentId: "20001"})
	require.NoError(t, err)
	edited, _, err := linked.AddComment("mine")
	require.NoError(t, err)
	_, err = linked.EditComment(edited, "mine, edited")
	require.NoError(t, err)
	_, _, err = linked.AddCommentRaw(me, 1700000001, "a note", nil,
		map[string]string{jira.MetaNote: "conflict"})
	require.NoError(t, err)

	ravi, err := repo.Identities().NewRaw("Ravi", "", "", "", nil, map[string]string{jira.MetaAccountId: jiratest.RaviID})
	require.NoError(t, err)

	ix, err := jira.NewIndex(repo)
	require.NoError(t, err)
	id, ok := ix.Issue("10500")
	require.True(t, ok)
	require.Equal(t, linked.Id(), id)
	user, ok := ix.User(jiratest.RaviID)
	require.True(t, ok)
	require.Equal(t, ravi.Id(), user)
	account, ok := ix.Account(ravi.Id())
	require.True(t, ok)
	require.Equal(t, jiratest.RaviID, account)

	doc := m.Local(linked.Snapshot(), "task")
	require.Equal(t, `["a","b"]`, string(doc.Fields["labels"]))
	require.Equal(t, `3`, string(doc.Fields["estimate"]))
	require.Equal(t, `null`, string(doc.Fields["assignee"]))
	require.Equal(t, `"task"`, string(doc.Fields["type"]))
	require.Equal(t, `"Linked"`, string(doc.Fields["title"]))
	_, ok = doc.Fields["area"]
	require.False(t, ok, "a local-only field is not in the merge")
	require.Equal(t, `"the body"`, string(doc.Fields[jira.BodyKey]))
	require.Len(t, doc.Comments, 3)
	require.Equal(t, "20001", doc.Comments[0].JiraId)
	require.Equal(t, paired.Id(), doc.Comments[0].Op)
	require.Equal(t, "mine, edited", doc.Comments[1].Text.Text)
	require.False(t, doc.Comments[1].Edited.IsZero())
	require.Equal(t, me.Id().String(), doc.Comments[1].Editor)
	require.True(t, doc.Comments[2].Note)
}
