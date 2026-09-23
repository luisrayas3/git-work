package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleDocument = `
shared:
  status: &status
    kind: enum
    name: Status
    values:
      - {id: to-do,       name: To Do,       category: unstarted}
      - {id: in-progress, name: In Progress, category: started}
      - {id: done,        name: Done,        category: completed}
types:
  epic:
    name: Epic
    fields:
      status: *status
      zebra: {kind: text, name: Zebra}
  story:
    name: Story
    fields:
      status: *status
      parent: {kind: relation, inverse: children, target_types: [epic]}
`

func TestParseDocumentKeepsOrder(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)

	// mapping order is the file's, not the alphabet's
	require.Equal(t, []string{"epic", "story"}, doc.Types.Keys())

	epic, ok := doc.Types.Get("epic")
	require.True(t, ok)
	require.Equal(t, "Epic", epic.Name)
	require.Equal(t, []string{"status", "zebra"}, epic.Fields.Keys())

	// an anchor reads exactly as a spelled-out field does
	status, ok := epic.Fields.Get("status")
	require.True(t, ok)
	require.Equal(t, "enum", status.Kind)
	require.Len(t, status.Values, 3)
	require.Equal(t, "to-do", status.Values[0].Id)
	require.Equal(t, "unstarted", status.Values[0].Category)

	story, _ := doc.Types.Get("story")
	parent, _ := story.Fields.Get("parent")
	require.Equal(t, []string{"epic"}, parent.TargetTypes)
	require.Equal(t, "children", parent.Inverse)
}

func TestParseDocumentRefusals(t *testing.T) {
	_, err := ParseDocument([]byte("types:\n  epic:\n    naem: Epic\n"))
	require.Error(t, err, "an unknown key is a mistake, not a no-op")

	_, err = ParseDocument([]byte("typs:\n  epic: {}\n"))
	require.Error(t, err)

	_, err = ParseDocument([]byte("not: [a, document"))
	require.Error(t, err)
}

func TestDocumentValidate(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))

	cases := map[string]string{
		"unknown kind": `
types:
  epic:
    fields:
      status: {kind: enum-with-category}
`,
		"unknown category": `
types:
  epic:
    fields:
      status: {kind: enum, values: [{id: a, category: shipped}]}
`,
		"dangling target type": `
types:
  story:
    fields:
      parent: {kind: relation, target_types: [epic]}
`,
		"values on a kind with none": `
types:
  epic:
    fields:
      due: {kind: date, values: [{id: a}]}
`,
		"a built-in's kind changed": `
types:
  epic:
    fields:
      title: {kind: number}
`,
		"an inverse used twice": `
types:
  epic:
    fields:
      parent: {kind: relation, inverse: children}
      owner: {kind: relation, inverse: children}
`,
		"a value id twice": `
types:
  epic:
    fields:
      status: {kind: enum, values: [{id: a}, {id: a}]}
`,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(src))
			require.NoError(t, err)
			require.Error(t, doc.Validate(nil))
		})
	}

	// a target type already in the store is fine: a partial file is legal
	doc, err = ParseDocument([]byte(`
types:
  story:
    fields:
      parent: {kind: relation, target_types: [epic]}
`))
	require.NoError(t, err)
	require.NoError(t, doc.Validate([]string{"epic"}))
}

func TestDocumentMarshal(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)

	raw, err := doc.Marshal("yaml")
	require.NoError(t, err)
	require.NotContains(t, string(raw), "shared", "the parser expanded the anchors")
	require.NotContains(t, string(raw), "ordinal", "order is position, never a hand-written number")

	// the rendering parses back to the same document
	again, err := ParseDocument(raw)
	require.NoError(t, err)
	require.Equal(t, doc.Types.Keys(), again.Types.Keys())
	require.Equal(t, doc.String(), again.String())

	// JSON is the same document, in order
	asJSON, err := doc.Marshal("json")
	require.NoError(t, err)
	fromJSON, err := ParseDocument(asJSON)
	require.NoError(t, err)
	require.Equal(t, doc.String(), fromJSON.String())
}
