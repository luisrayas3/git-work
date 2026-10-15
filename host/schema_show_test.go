package host

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/schema"
)

const showSchema = `
types:
  epic:
    fields:
      status: {kind: enum, values: [{id: open}]}
  story:
    show:
      fields: [status, links]
      expand:
        - {relation: children, fields: [status]}
        - links
    fields:
      status: {kind: enum, values: [{id: open}]}
      parent: {kind: relation, inverse: children, target_types: [epic, story]}
      blocks: {kind: multi-relation, inverse: blocked-by, link: true}
`

// TestSchemaImportTypeShow: a type's show is written, read back and exported
// as it was given, so export | import is a no-op; it is checked as a view's
// shows entry is, against the schema the import leaves, before anything is
// written (doc/design/app.md A7).
func TestSchemaImportTypeShow(t *testing.T) {
	repo, _ := atTestRepo(t)

	doc, err := schema.ParseDocument([]byte(showSchema))
	require.NoError(t, err)
	_, _, err = SchemaImport(repo, doc, false, false)
	require.NoError(t, err)

	s, err := repo.LoadSchema()
	require.NoError(t, err)
	story, _ := s.Type("story")
	require.JSONEq(t, `{"fields":["status","links"],"expand":[{"relation":"children","fields":["status"]},"links"]}`, string(story.Show))
	epic, _ := s.Type("epic")
	require.Nil(t, epic.Show)

	exported := schema.Export(s)
	require.Contains(t, exported.String(), "show:")
	again, _, err := SchemaImport(repo, exported, false, true)
	require.NoError(t, err)
	require.Empty(t, again, "export | import writes nothing")

	for src, says := range map[string]string{
		"types:\n  epic:\n    show: {fields: [colour]}\n":     "type epic: show fields names colour, which is not a field of epic",
		"types:\n  epic:\n    show: {expand: nephews}\n":      "type epic: show expand names nephews",
		"types:\n  epic:\n    show: {expand: [[children]]}\n": "type epic: show expand",
		"types:\n  epic:\n    show: {colour: red}\n":          "unknown field",
	} {
		doc, err := schema.ParseDocument([]byte(src))
		if err == nil {
			_, _, err = SchemaImport(repo, doc, false, true)
		}
		require.ErrorContains(t, err, says, src)
	}

	// a field the same file adds is one the show may name
	doc, err = schema.ParseDocument([]byte("types:\n  epic:\n    show: {fields: [due]}\n    fields:\n      due: {kind: date}\n"))
	require.NoError(t, err)
	_, _, err = SchemaImport(repo, doc, false, true)
	require.NoError(t, err)
}
