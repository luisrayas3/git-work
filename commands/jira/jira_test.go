package jiracmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
	"github.com/git-bug/git-bug/schema"
)

// newBoundEnv is a clone bound to a fake site, with the jira preset.
func newBoundEnv(t *testing.T) (*execenv.Env, *jiratest.Server) {
	t.Helper()
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0))
	env := execenv.NewTestEnv(t)
	me, err := env.Backend.Identities().New("Runner", "runner@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(me))

	email, token := srv.Credentials()
	cfg := env.Repo.LocalConfig()
	require.NoError(t, cfg.StoreString(host.JiraURLKey, srv.URL()))
	require.NoError(t, cfg.StoreString(host.JiraProjectKey, "PROJ"))
	require.NoError(t, cfg.StoreString(host.JiraEmailKey, email))
	t.Setenv(host.JiraTokenEnv, token)

	preset, err := schema.Preset("jira")
	require.NoError(t, err)
	_, _, err = host.SchemaImport(env.Backend, preset, false, false)
	require.NoError(t, err)
	return env, srv
}

func TestJiraSchemaThenSync(t *testing.T) {
	env, srv := newBoundEnv(t)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "From Jira"})

	// the first mapping is reviewed: sync refuses until it is imported
	err := runJiraSync(env, syncOptions{format: "json"}, nil)
	require.ErrorContains(t, err, "git work jira schema > jira.yaml")

	env.Out.Reset()
	require.NoError(t, runJiraSchema(env, "json"))
	doc, err := schema.ParseDocument(env.Out.Bytes())
	require.NoError(t, err)
	_, _, err = host.SchemaImport(env.Backend, doc, false, false)
	require.NoError(t, err)

	// one JSON object per line: issues, then the summary
	env.Out.Reset()
	require.NoError(t, runJiraSync(env, syncOptions{format: "json"}, nil))
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	var first, last jira.Line
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &last))
	require.Equal(t, key, first.Jira)
	require.Equal(t, jira.ActionImported, first.Action)
	require.NotNil(t, last.Summary)
	require.Equal(t, 1, last.Summary.Imported)

	// an ID position takes the Jira key; text is one line per issue
	srv.Edit(key, map[string]any{"summary": "Renamed"})
	env.Out.Reset()
	require.NoError(t, runJiraSync(env, syncOptions{format: "text"}, []string{key}))
	out := env.Out.String()
	require.Contains(t, out, key+"  updated  imported title")
	require.Contains(t, out, "summary: 0 imported")
}

func TestJiraUnbound(t *testing.T) {
	env := execenv.NewTestEnv(t)
	err := runJiraSync(env, syncOptions{format: "json"}, nil)
	require.ErrorContains(t, err, host.JiraURLKey)
}
