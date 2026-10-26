package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// TestMatrixDrillDownCarriesTheDraft: every cell drills down, an empty one
// too, and the list it pushes carries the cell in `draft` — each axis value
// the cell names, the block's group_by value and the type every issue on the
// matrix shares, never the value field — so its ghost already holds the
// cell, and the call line still copies as a legal list call (C8).
func TestMatrixDrillDownCarriesTheDraft(t *testing.T) {
	repo := testRepo(t)
	alpha, beta, first, _ := twoByTwo(t, repo)
	newIssue(t, repo, map[string]any{"title": "four", "parent": beta, "iteration": first, "estimate": 1, "priority": "high"})

	page := matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","group_by":"priority","query":"`+tasksOnly+`"}`)
	put(t, page, "alpha", "sprint 2")
	require.Equal(t, "", cellOf(t, page, "alpha", "sprint 2"), "an empty cell")

	_, cmd := page.Update(press("enter"))
	require.NotNil(t, cmd, "an empty cell drills down too")
	pushed := cmd().(pushMsg).page.(*listPage)
	require.Equal(t, 0, pushed.count())
	require.True(t, pushed.call.Has("draft"))

	var draft host.IssueDocument
	require.NoError(t, json.Unmarshal(pushed.call.Raw("draft"), &draft))
	require.Equal(t, issue.StringValue("task"), draft.Fields[schema.TypeKey])
	require.Equal(t, issue.StringValue(alpha), draft.Fields["parent"])
	_, valued := draft.Fields["estimate"]
	require.False(t, valued, "the form invents no number")
	require.Equal(t, issue.StringValue("high"), draft.Fields["priority"],
		"the high block, drawn first, holds an empty alpha row: its value goes in")

	ghost := listGhosts(pushed)[""][0]
	pushed.putCursorOn(ghost.key)
	made := opened(t, pushed, "enter")
	require.Equal(t, "task", made.typeKey())
	require.Equal(t, issue.StringValue(alpha), made.fields["parent"])
	require.Equal(t, issue.StringValue(page.colAxis.values[page.cols[page.col]].value), made.fields["iteration"])
	require.Contains(t, command(pushed.call), `"draft"`, "the call line carries it")

	// the (none) block leaves its field out, and a total carries only the
	// axes it names
	page.row = -1
	for at, row := range page.drawn {
		if row.kind == rowData && page.groups[row.group].none && strings.Contains(row.label, "beta") {
			page.row = at
		}
	}
	require.GreaterOrEqual(t, page.row, 0)
	page.col = len(page.cols)
	call, err := page.cellCall()
	require.NoError(t, err)
	draft = host.IssueDocument{}
	require.NoError(t, json.Unmarshal(call.Raw("draft"), &draft))
	_, grouped := draft.Fields["priority"]
	require.False(t, grouped, "(none) leaves it out")
	_, sprinted := draft.Fields["iteration"]
	require.False(t, sprinted, "the totals column names no sprint")
	require.Equal(t, issue.StringValue(beta), draft.Fields["parent"])
}
