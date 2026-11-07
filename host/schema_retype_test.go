package host

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/schema"
)

// TestSchemaImportRetypeSystems: a type with a non-empty alias for a system
// may list as retype targets only types synced to that same system; a
// local-only or ""-aliased target is an import error naming both, checked
// against the types already in the store; an unaliased type may list
// anything (doc/design/schema.md, "A type says what it may become").
func TestSchemaImportRetypeSystems(t *testing.T) {
	repo, _ := atTestRepo(t)

	base := `
types:
  story:
    aliases: {jira: "10001"}
    retype_targets: [task]
  task:
    aliases: {jira: "10002"}
    retype_targets: [story]
  note:
    retype_targets: [story, chore]
  chore:
    aliases: {jira: ""}
`
	doc, err := schema.ParseDocument([]byte(base))
	require.NoError(t, err)
	_, _, err = SchemaImport(repo, doc, false, false)
	require.NoError(t, err, "an unaliased type may list anything")

	for src, says := range map[string]string{
		"types:\n  story:\n    retype_targets: [task, note]\n":  "type story: retype target note does not sync to jira, as story does",
		"types:\n  story:\n    retype_targets: [task, chore]\n": "type story: retype target chore does not sync to jira, as story does",
		// the target loses its alias in the same file
		"types:\n  task:\n    aliases: {jira: \"\"}\n": "type story: retype target task does not sync to jira, as story does",
	} {
		doc, err := schema.ParseDocument([]byte(src))
		require.NoError(t, err)
		_, _, err = SchemaImport(repo, doc, false, true)
		require.ErrorContains(t, err, says, src)
	}

	// a target the same file aliases is fine
	doc, err = schema.ParseDocument([]byte("types:\n  story:\n    retype_targets: [task, chore]\n  chore:\n    aliases: {jira: \"10003\"}\n"))
	require.NoError(t, err)
	_, _, err = SchemaImport(repo, doc, false, true)
	require.NoError(t, err)
}
