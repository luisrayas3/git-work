package jira_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// A status Jira takes out of a workflow is archived by the next sync,
// whether or not issues hold it: an issue holding it still reads and keeps
// it, a write introducing it is refused, and the sync's note names the
// issue (doc/design/jira.md, "What Jira drops is archived"; doc/design/schema.md,
// "Archiving refuses only what is new").
func TestDroppedStatusIsArchived(t *testing.T) {
	w := newWorld(t)
	held := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "In QA"})
	w.srv.Transition(held, "In Progress")
	w.srv.Transition(held, "Ready for QA")
	other := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Not started"})
	w.mustSync(jira.Options{})

	holder := w.byKey(held)
	value, ok := issue.String(holder.Snapshot().Fields["status"])
	require.True(t, ok)
	require.NotEmpty(t, value)

	w.srv.RemoveStatus("PROJ", "Task", "Ready for QA")
	sum, notes, err := host.JiraSync(t.Context(), w.c, jira.Options{}, func(jira.Line) {})
	require.NoError(t, err)
	require.Zero(t, sum.Failed)

	var note *jira.Note
	for i, n := range notes {
		if n.Key == "task/status:"+value {
			note = &notes[i]
		}
	}
	require.NotNil(t, note, "%+v", notes)
	require.Equal(t, jira.LevelWarn, note.Level)
	require.True(t, strings.HasPrefix(note.Message, "archived: "), note.Message)
	require.Contains(t, note.Message, "still held by "+w.c.IssueHumanId(holder.Id()))
	require.NotContains(t, note.Message, w.c.IssueHumanId(w.byKey(other).Id()))

	s, err := w.c.LoadSchema()
	require.NoError(t, err)
	v, ok := s.Types["task"].Fields["status"].Value(value)
	require.True(t, ok)
	require.True(t, v.Archived)
	sv, _ := s.Types["story"].Fields["status"].Value(value)
	require.False(t, sv.Archived, "another type's workflow keeps it")

	// it reads, and a write that keeps it is accepted
	require.Equal(t, `"`+value+`"`, field(t, w.byKey(held), "status"))
	_, err = w.byKey(held).PlanSetFields(map[string]issue.Value{"status": issue.StringValue(value)})
	require.NoError(t, err)

	// a write that introduces it is refused, naming what is accepted
	_, err = w.byKey(other).PlanSetFields(map[string]issue.Value{"status": issue.StringValue(value)})
	require.ErrorContains(t, err, `"`+value+`" is archived; valid values: `)

	// a second run archives nothing more and says nothing
	_, notes, err = host.JiraSync(t.Context(), w.c, jira.Options{}, func(jira.Line) {})
	require.NoError(t, err)
	require.Empty(t, notes)
}
