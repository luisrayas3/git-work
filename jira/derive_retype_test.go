package jira

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/schema"
)

// TC3: each mapped type's retype_targets is the other mapped types of its
// hierarchy level, in the document's order; a local-only type, unaliased or
// aliased "", keeps its own and is never listed; a list widened by hand is
// taken back, with a note, and a second derivation writes nothing.
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

	widened := s.export(t)
	story := typeDoc(t, widened, "story")
	story.RetypeTargets = &[]string{"task", "epic", "decision"}
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
