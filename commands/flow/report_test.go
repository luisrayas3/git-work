package flowcmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
)

// TestReportNamesIssuesByHumanId: the repository's report flow names an issue
// by its human_id, so with git-work.display.id = jira a linked issue is its
// key and a local one its short hash, and a parent drawn as a relation is its
// key too (doc/design/alias-ids.md A9).
func TestReportNamesIssuesByHumanId(t *testing.T) {
	env := newTestEnv(t)
	require.NoError(t, env.Repo.LocalConfig().StoreString(cache.DisplayIdKey, "jira"))

	parent, _, err := env.Backend.Issues().NewWithMetadata("the story", "", map[string]issue.Value{},
		map[string]string{cache.AliasMetadataPrefix + "jira": "PROJ-1"})
	require.NoError(t, err)
	_, _, err = env.Backend.Issues().NewWithMetadata("keyed", "", map[string]issue.Value{
		"parent": issue.StringValue(parent.Id().String()),
	}, map[string]string{cache.AliasMetadataPrefix + "jira": "PROJ-9"})
	require.NoError(t, err)
	local, _, err := env.Backend.Issues().New("local", "", map[string]issue.Value{})
	require.NoError(t, err)

	importFlows(t, env, flowImportOptions{}, filepath.Join("testdata", "report.star"))
	env.Out.Reset()
	require.NoError(t, runFlowRun(env, []string{"report", `{"from_":"1d"}`}))

	out := env.Out.String()
	require.Contains(t, out, "- PROJ-9 keyed")
	require.Contains(t, out, "- PROJ-1 the story")
	require.Contains(t, out, "- "+local.Id().Human()+" local")
	require.Contains(t, out, "with parent PROJ-1 the story")
}
