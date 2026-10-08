package issuecmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
)

// TestDisplayIdText: with git-work.display.id = jira, every text form names
// an issue by its key where it has one and by its hash where it has not, the
// JSON keeps id the hash and carries the key in human_id, and get shows both
// (doc/design/alias-ids.md A4, A5).
func TestDisplayIdText(t *testing.T) {
	env := newTestEnv(t)
	require.NoError(t, env.Repo.LocalConfig().StoreString(cache.DisplayIdKey, "jira"))

	keyed := newTestIssue(t, env,
		`{"fields":{"title":"keyed","status":"open"},"body":"b","aliases":{"jira":"PROJ-9"}}`)
	local := newTestIssue(t, env, `{"fields":{"title":"local","status":"open"},"body":"b"}`)

	// the list's text form
	require.NoError(t, runIssueList(env, issueListOptions{format: "text"}, nil))
	out := env.Out.String()
	require.Contains(t, out, "PROJ-9\topen\tkeyed\n")
	require.Contains(t, out, local.Human()+"\topen\tlocal\n")
	env.Out.Reset()

	// the JSON: id is the hash, human_id the drawn id
	require.NoError(t, runIssueList(env, issueListOptions{format: "json"}, nil))
	var items []map[string]any
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &items))
	byTitle := map[string]map[string]any{}
	for _, item := range items {
		byTitle[item["fields"].(map[string]any)["title"].(string)] = item
	}
	require.Equal(t, keyed.String(), byTitle["keyed"]["id"])
	require.Equal(t, "PROJ-9", byTitle["keyed"]["human_id"])
	require.Equal(t, local.Human(), byTitle["local"]["human_id"])
	env.Out.Reset()

	// get: the JSON's human_id, and both in the text header
	require.NoError(t, runIssueGet(env, issueGetOptions{format: "json"}, []string{"PROJ-9"}))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &doc))
	require.Equal(t, keyed.String(), doc["id"])
	require.Equal(t, "PROJ-9", doc["human_id"])
	env.Out.Reset()

	require.NoError(t, runIssueGet(env, issueGetOptions{format: "text"}, []string{"PROJ-9"}))
	require.True(t, strings.HasPrefix(env.Out.String(), "PROJ-9 ("+keyed.Human()+") [open] keyed\n"), env.Out.String())
	env.Out.Reset()

	require.NoError(t, runIssueGet(env, issueGetOptions{format: "text"}, []string{local.Human()}))
	require.True(t, strings.HasPrefix(env.Out.String(), local.Human()+" [open] local\n"), env.Out.String())
	env.Out.Reset()

	// the log's issue column; the operation's own id stays a hash
	require.NoError(t, runIssueLog(env, issueLogOptions{format: "text"}, []string{"PROJ-9"}))
	for _, line := range strings.Split(strings.TrimSpace(env.Out.String()), "\n") {
		require.True(t, strings.HasPrefix(line, "PROJ-9\t"), line)
	}
	env.Out.Reset()
}

// TestDisplayIdOff: with no setting, nothing changes.
func TestDisplayIdOff(t *testing.T) {
	env := newTestEnv(t)
	keyed := newTestIssue(t, env,
		`{"fields":{"title":"keyed","status":"open"},"body":"b","aliases":{"jira":"PROJ-9"}}`)

	require.NoError(t, runIssueList(env, issueListOptions{format: "text"}, nil))
	require.Contains(t, env.Out.String(), keyed.Human()+"\topen\tkeyed\n")
	env.Out.Reset()

	require.NoError(t, runIssueGet(env, issueGetOptions{format: "text"}, []string{"PROJ-9"}))
	require.True(t, strings.HasPrefix(env.Out.String(), keyed.Human()+" [open] keyed\n"), env.Out.String())
}
