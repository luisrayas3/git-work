package jira_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// A status Jira added to a workflow is a value of the schema the dry run
// derives: an issue in it is imported, not pending, and the stored schema is
// unchanged (doc/design/jira.md, "A dry run plans against the derived schema").
func TestDryRunPlansAgainstDerivedSchema(t *testing.T) {
	w := newWorld(t)
	w.srv.AddStatus("PROJ", "Task", jiratest.Status{ID: "10099", Name: "Blocked", Category: jiratest.CategoryIndeterminate})
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Waiting"})
	w.srv.Transition(key, "Blocked")

	before, _, err := host.SchemaExport(w.c, true)
	require.NoError(t, err)
	refs := w.refs()

	var schemaLines int
	var lines []jira.Line
	sum, _, err := host.JiraSync(t.Context(), w.c, jira.Options{DryRun: true}, func(l jira.Line) {
		switch {
		case l.Schema != nil:
			schemaLines++
		case l.Summary == nil:
			lines = append(lines, l)
		}
	})
	require.NoError(t, err)
	require.Equal(t, 1, schemaLines, "the derived change is reported")
	require.Len(t, lines, 1)
	require.Equal(t, key, lines[0].Jira)
	require.Equal(t, jira.ActionImported, lines[0].Action, "%+v", lines[0])
	require.Empty(t, lines[0].Pending)
	require.Zero(t, sum.Pending)
	planned := lines[0].Imported["status"]
	require.NotEmpty(t, planned)

	after, _, err := host.SchemaExport(w.c, true)
	require.NoError(t, err)
	require.Equal(t, before, after, "the dry run writes no schema")
	require.Equal(t, refs, w.refs(), "the dry run writes nothing")

	// the real run does what the dry run planned
	real, _ := w.mustSync(jira.Options{})
	require.Len(t, real, 1)
	require.Equal(t, jira.ActionImported, real[0].Action)
	require.Equal(t, planned, real[0].Imported["status"])
}
