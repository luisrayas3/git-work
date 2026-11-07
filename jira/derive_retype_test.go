package jira

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
)

// each mapped type's retype_targets is the other mapped types of its hierarchy
// level, in the document's order; a local-only type, unaliased or aliased "",
// keeps its own and is never listed; a list widened by hand is taken back, with
// a note, and a second derivation writes nothing
// (doc/design/jira.md, "Retype targets follow Jira's levels and workflows").
func TestDeriveRetypeTargets(t *testing.T) {
	p, _ := discover(t, "company")
	// the preset's initiative adopts a level above the epic; with bug
	// excluded below, the mapped levels are 2, 1, 0, 0, -1
	p.IssueTypes = append([]IssueType{{Id: "10010", Name: "Initiative", Level: 2}}, p.IssueTypes...)

	start := starts(t)["repo"]
	decision := typeDoc(t, start, "decision")
	decision.RetypeTargets = &[]string{"task"}
	start.SetType("decision", decision)
	bug := typeDoc(t, start, "bug")
	bug.Aliases = map[string]string{system: ""}
	start.SetType("bug", bug)

	s, doc, notes, _ := derived(t, start, p)
	want := map[string][]string{
		"initiative": {},
		"epic":       {},
		"story":      {"task"},
		"task":       {"story"},
		"subtask":    {},
	}
	for _, d := range []*schema.Document{doc, s.export(t)} {
		for key, targets := range want {
			got := typeDoc(t, d, key).RetypeTargets
			require.NotNil(t, got, key)
			require.Equal(t, targets, *got, key)
		}
		require.Equal(t, []string{"task"}, *typeDoc(t, d, "decision").RetypeTargets, "a local-only type's own")
		require.Nil(t, typeDoc(t, d, "bug").RetypeTargets, `a type aliased "" is local-only`)
		require.Nil(t, typeDoc(t, d, "allocation").RetypeTargets)
	}
	require.True(t, hasNote(notes, LevelInfo, "story", "retype_targets task after Jira levels"))
	require.True(t, hasNote(notes, LevelInfo, "epic", "retype_targets (none) after Jira levels"))

	again, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again), "a fixpoint")
	require.False(t, hasNote(notes, LevelInfo, "story", "retype_targets"), "no change, no note")

	// widened by hand to a mapped type of another level, which a synced type's
	// targets allow (doc/design/schema.md, "A type says what it may become")
	widened := s.export(t)
	story := typeDoc(t, widened, "story")
	story.RetypeTargets = &[]string{"task", "epic"}
	widened.SetType("story", story)
	s.importDoc(t, widened)

	doc, notes, err = Derive(s.export(t), p)
	require.NoError(t, err)
	require.Equal(t, []string{"task"}, *typeDoc(t, doc, "story").RetypeTargets)
	require.True(t, hasNote(notes, LevelInfo, "story", "retype_targets task after Jira levels"))
	changes := s.importDoc(t, doc)
	require.Len(t, changes, 1)
	require.JSONEq(t, `["task"]`, string(changes[0].Set[schema.AttrRetypeTargets]))
}

// two types of one level whose workflows have different sets of statuses
// do not list each other; the same set in another order does
// (doc/design/jira.md, "Retype targets follow Jira's levels and workflows").
func TestDeriveRetypeTargetsWorkflow(t *testing.T) {
	p, _ := discover(t, "company")
	byName := map[string]*IssueType{}
	for i := range p.IssueTypes {
		byName[p.IssueTypes[i].Name] = &p.IssueTypes[i]
	}
	story, task := byName["Story"], byName["Task"]
	require.NotNil(t, story)
	require.NotNil(t, task)
	require.Equal(t, story.Level, task.Level)
	require.NotEmpty(t, story.Statuses)

	// the same statuses in another order are the same workflow here
	task.Statuses = slices.Clone(story.Statuses)
	slices.Reverse(task.Statuses)
	_, doc, _, _ := derived(t, starts(t)["repo"], p)
	require.Contains(t, *typeDoc(t, doc, "story").RetypeTargets, "task")
	require.Contains(t, *typeDoc(t, doc, "task").RetypeTargets, "story")

	// one status more is another workflow
	task.Statuses = append(slices.Clone(story.Statuses), jiraapi.Status{ID: "99999", Name: "Parked"})
	_, doc, notes, _ := derived(t, starts(t)["repo"], p)
	require.NotContains(t, *typeDoc(t, doc, "story").RetypeTargets, "task")
	require.Empty(t, *typeDoc(t, doc, "task").RetypeTargets)
	require.True(t, hasNote(notes, LevelInfo, "task", "retype_targets (none) after Jira levels"))
}
