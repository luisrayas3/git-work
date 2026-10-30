package host

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// TestListReadsTheSchemaAsItIs: `$schema` is the schema each run loads, so
// the same program orders by the new values once an import changes them
// (doc/design/schema-in-jq.md, J1).
func TestListReadsTheSchemaAsItIs(t *testing.T) {
	repo, author := atTestRepo(t)

	importSchema := func(priorities string) {
		t.Helper()
		doc, err := schema.ParseDocument([]byte(`
types:
  task:
    fields:
      priority: {kind: ordinal-enum, values: ` + priorities + `}
`))
		require.NoError(t, err)
		_, _, err = SchemaImport(repo, doc, false, false)
		require.NoError(t, err)
	}
	importSchema(`[{id: high}, {id: low}]`)

	now := time.Now()
	for _, p := range []string{"low", "high"} {
		newIssueAt(t, repo, author, now, p, map[string]issue.Value{"type": str("task"), "priority": str(p)})
	}

	const program = `sort_by(ordinal("priority")) | map(.fields.priority)`
	values, err := IssueList(repo, program, false)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"high", "low"}}, values)

	importSchema(`[{id: low}, {id: high}]`)
	values, err = IssueList(repo, program, false)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"low", "high"}}, values, "the import's order")

	// under --at, the schema is today's
	values, err = IssueListAt(repo, program, now.Add(time.Hour), false)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"low", "high"}}, values)
}
