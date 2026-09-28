package schema

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
)

// aliasedDocument is sampleDocument mapped onto a Jira project (JS2).
const aliasedDocument = `
types:
  epic:
    name: Epic
    aliases: {jira: "10000"}
    fields:
      status:
        kind: enum
        name: Status
        aliases: {jira: status}
        values:
          - {id: to-do,       name: To Do,       category: unstarted, aliases: {jira: "1"}}
          - {id: in-progress, name: In Progress, category: started,   aliases: {jira: "3"}}
          - {id: done,        name: Done,        category: completed, aliases: {jira: ""}}
      zebra: {kind: text, name: Zebra}
  story:
    name: Story
    aliases: {jira: "10001", linear: ""}
    fields:
      status:
        kind: enum
        name: Status
        aliases: {jira: status}
        values:
          - {id: to-do,       name: To Do,       category: unstarted}
          - {id: in-progress, name: In Progress, category: started}
          - {id: done,        name: Done,        category: completed}
      parent: {kind: relation, inverse: children, target_types: [epic], aliases: {jira: parent}}
`

func TestAliasNames(t *testing.T) {
	require.Equal(t, "alias_jira", AliasName("jira"))
	require.Equal(t, "alias_jira/in-progress", ValueAliasName("jira", "in-progress"))
	require.NoError(t, config.ValidateName(ValueAliasName("jira", "in-progress")))
}

func TestAliasesRoundTrip(t *testing.T) {
	doc, err := ParseDocument([]byte(aliasedDocument))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))

	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))
	byKey := map[string]Entry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	require.JSONEq(t, `"10000"`, string(byKey["epic"].Attributes["alias_jira"]))
	require.JSONEq(t, `""`, string(byKey["story"].Attributes["alias_linear"]))
	require.JSONEq(t, `"status"`, string(byKey["epic/status"].Attributes["alias_jira"]))
	require.JSONEq(t, `"3"`, string(byKey["epic/status"].Attributes["alias_jira/in-progress"]))
	require.JSONEq(t, `""`, string(byKey["epic/status"].Attributes["alias_jira/done"]))

	compiled, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, compiled.Problems)

	epic, _ := compiled.Type("epic")
	require.Equal(t, map[string]string{"jira": "10000"}, epic.Aliases)
	status, _ := compiled.Field("epic", "status")
	require.Equal(t, map[string]string{"jira": "status"}, status.Aliases)
	inProgress, _ := status.Value("in-progress")
	require.Equal(t, map[string]string{"jira": "3"}, inProgress.Aliases)
	done, _ := status.Value("done")
	require.Equal(t, map[string]string{"jira": ""}, done.Aliases, "the empty string is a stated alias")
	storyStatus, _ := compiled.Field("story", "status")
	toDo, _ := storyStatus.Value("to-do")
	require.Nil(t, toDo.Aliases)

	// export prints them, and export | import writes nothing
	exported := Export(compiled)
	require.Contains(t, exported.String(), "aliases:")
	require.Empty(t, mustReconcile(t, exported, entries, true))

	// and the rendering parses back to the same document, in both formats
	for _, format := range []string{"yaml", "json"} {
		raw, err := exported.Marshal(format)
		require.NoError(t, err)
		again, err := ParseDocument(raw)
		require.NoError(t, err)
		require.Equal(t, exported.String(), again.String())
		require.Empty(t, mustReconcile(t, again, entries, true))
	}
}

// TestAliasesAreNeverRemoved: an alias-free file over a mapped store unmaps
// nothing, and a stated alias is written as one attribute of one entity.
func TestAliasesAreNeverRemoved(t *testing.T) {
	aliased, err := ParseDocument([]byte(aliasedDocument))
	require.NoError(t, err)
	entries := applyChanges(t, nil, mustReconcile(t, aliased, nil, false))

	plain, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)
	require.Empty(t, mustReconcile(t, plain, entries, false))
	require.Empty(t, mustReconcile(t, plain, entries, true), "not even under prune")

	// a new alias, and a changed one
	edited, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    aliases: {jira: "10009"}
    fields:
      zebra: {kind: text, name: Zebra, aliases: {jira: customfield_10016}}
`))
	require.NoError(t, err)
	changes := mustReconcile(t, edited, entries, false)
	require.Len(t, changes, 2)
	require.Equal(t, "epic", changes[0].Key)
	require.Equal(t, map[string]config.Value{"alias_jira": config.Value(`"10009"`)}, changes[0].Set)
	require.Empty(t, changes[0].Remove)
	require.Equal(t, "epic/zebra", changes[1].Key)
	require.Equal(t, map[string]config.Value{"alias_jira": config.Value(`"customfield_10016"`)}, changes[1].Set)
	require.Empty(t, changes[1].Remove)

	// a value the document drops goes, and its alias stays behind, unread
	dropped, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    fields:
      status:
        kind: enum
        name: Status
        values:
          - {id: to-do,       name: To Do,       category: unstarted}
          - {id: done,        name: Done,        category: completed}
`))
	require.NoError(t, err)
	changes = mustReconcile(t, dropped, entries, false)
	require.Len(t, changes, 1)
	require.Equal(t, []string{"values/in-progress"}, changes[0].Remove)
	entries = applyChanges(t, entries, changes)
	compiled, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, compiled.Problems)
	status, _ := compiled.Field("epic", "status")
	require.Equal(t, []string{"to-do", "done"}, status.ValueIds())
}

func TestAliasesValidate(t *testing.T) {
	cases := map[string]string{
		"a system name that is no attribute name": `
types:
  epic: {aliases: {Jira: "1"}}
`,
		"two types, one alias": `
types:
  epic: {aliases: {jira: "1"}}
  story: {aliases: {jira: "1"}}
`,
		"two fields of a type, one alias": `
types:
  epic:
    fields:
      a: {kind: number, aliases: {jira: customfield_1}}
      b: {kind: number, aliases: {jira: customfield_1}}
`,
		"two values of a field, one alias": `
types:
  epic:
    fields:
      status: {kind: enum, values: [{id: a, aliases: {jira: "1"}}, {id: b, aliases: {jira: "1"}}]}
`,
		"an alias on a built-in": `
types:
  epic:
    fields:
      title: {kind: text, aliases: {jira: summary}}
`,
		"a value alias too long to store": `
types:
  epic:
    fields:
      status: {kind: enum, values: [{id: a-value-id-that-fits-values-but-not-the-alias-attribute, aliases: {jira: "1"}}]}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(src))
			require.NoError(t, err)
			require.Error(t, doc.Validate(nil))
		})
	}

	// the empty string excludes, so any number of entities state it;
	// fields of two types share an alias as they share a key (E2)
	doc, err := ParseDocument([]byte(`
types:
  epic:
    aliases: {jira: ""}
    fields:
      status: {kind: enum, aliases: {jira: status}, values: [{id: a, aliases: {jira: ""}}, {id: b, aliases: {jira: ""}}]}
  story:
    aliases: {jira: ""}
    fields:
      status: {kind: enum, aliases: {jira: status}}
`))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))
}

func TestDocumentBuiltInCode(t *testing.T) {
	doc := NewDocument()
	epic := TypeDoc{Name: "Epic", Aliases: map[string]string{"jira": "10000"}}
	epic.SetField("status", FieldDoc{Kind: "enum", Name: "Status", Values: []ValueDoc{
		{Id: "to-do", Name: "To Do", Category: "unstarted", Aliases: map[string]string{"jira": "1"}},
	}})
	epic.SetField("zebra", FieldDoc{Kind: "text"})
	doc.SetType("epic", epic)
	doc.SetType("story", TypeDoc{Name: "Story"})
	require.NoError(t, doc.Validate(nil))

	parsed, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    aliases: {jira: "10000"}
    fields:
      status: {kind: enum, name: Status, values: [{id: to-do, name: To Do, category: unstarted, aliases: {jira: "1"}}]}
      zebra: {kind: text}
  story: {name: Story}
`))
	require.NoError(t, err)
	require.Equal(t, parsed.String(), doc.String())

	// a zero Document takes a type too
	var zero Document
	zero.SetType("epic", TypeDoc{})
	require.Equal(t, []string{"epic"}, zero.Types.Keys())
}

func TestFieldValuesInCategory(t *testing.T) {
	doc, err := ParseDocument([]byte(`
types:
  task:
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, category: unstarted}
          - {id: done, category: completed}
          - {id: wont-do, category: canceled}
          - {id: duplicate, category: canceled}
`))
	require.NoError(t, err)
	compiled, err := Compile(applyChanges(t, nil, mustReconcile(t, doc, nil, false)))
	require.NoError(t, err)
	status, _ := compiled.Field("task", "status")

	var ids []string
	for _, value := range status.ValuesInCategory(CategoryCanceled) {
		ids = append(ids, value.Id)
	}
	require.Equal(t, []string{"wont-do", "duplicate"}, ids)
	require.Empty(t, status.ValuesInCategory(CategoryStarted))
}
