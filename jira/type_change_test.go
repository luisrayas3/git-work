package jira_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
	"github.com/git-bug/git-bug/schema"
)

// retypeSite is the company site with the differences a type change needs
// to show: Task has no story points, so a key Story maps is local-only
// under Task; Bug is on another workflow, so Jira refuses Story → Bug
// although both are level 0; and an Initiative sits above the Epic.
func retypeSite() jiratest.Site {
	site := jiratest.CompanySite()
	p := &site.Projects[0]
	var epic jiratest.IssueType
	for i := range p.IssueTypes {
		it := &p.IssueTypes[i]
		switch it.Name {
		case "Epic":
			epic = *it
		case "Task":
			noPoints := func(id string) bool { return id == jiratest.FieldStoryPoints }
			it.Fields = slices.DeleteFunc(slices.Clone(it.Fields), noPoints)
			it.CreateScreen = slices.DeleteFunc(slices.Clone(it.CreateScreen), noPoints)
		}
	}
	for i := range p.IssueTypes {
		if p.IssueTypes[i].Name == "Bug" {
			p.IssueTypes[i].Workflow = epic.Workflow
		}
	}
	p.IssueTypes = append(p.IssueTypes, jiratest.IssueType{ID: "10005", Name: "Initiative", HierarchyLevel: 2,
		Fields: epic.Fields, Workflow: epic.Workflow})
	return site
}

// puts are the bodies of the issue edits served, in order.
func puts(srv *jiratest.Server) []map[string]map[string]json.RawMessage {
	var out []map[string]map[string]json.RawMessage
	for _, r := range srv.Requests() {
		if r.Op != "editIssue" {
			continue
		}
		var body map[string]map[string]json.RawMessage
		_ = json.Unmarshal(r.Body, &body)
		out = append(out, body)
	}
	return out
}

func storyIssue(t *testing.T, w *world, summary string) (string, *issueRef) {
	t.Helper()
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: summary})
	w.mustSync(jira.Options{})
	ic := w.byKey(key)
	require.Equal(t, `"story"`, field(t, ic, "type"))
	return key, &issueRef{w: w, key: key}
}

// issueRef sets fields of one issue in one change, as `issue set` does.
type issueRef struct {
	w   *world
	key string
}

func (r *issueRef) set(fields map[string]issue.Value) error {
	ic := r.w.byKey(r.key)
	ops, err := ic.PlanSetFields(fields)
	if err != nil {
		return err
	}
	return ic.CommitOperations(ops)
}

// retarget widens a type's retype_targets by hand after the import, as TC3
// allows; the next sync's derive takes it back.
func retarget(t *testing.T, w *world, typ string, targets ...string) {
	t.Helper()
	ce, err := w.c.Schema().ResolveKey(config.ShapeType, typ)
	require.NoError(t, err)
	_, err = ce.Set(schema.AttrRetypeTargets, config.MustValue(targets))
	require.NoError(t, err)
}

// TC4, TC8: Story → Task writes issuetype alone and first, then the rest
// under Task's mapping; a key Task lacks in Jira is not a key of that merge,
// so nothing is pending for it, and it stays on the issue. A dry run first
// reports the same.
func TestTypeChangeExported(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Becomes a task")

	require.NoError(t, ref.set(map[string]issue.Value{
		"type": str("task"), "labels": issue.Value(`["moved"]`), "estimate": issue.MustValue(3),
	}))

	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{DryRun: true})
	require.Zero(t, w.srv.Writes())
	require.Len(t, lines, 1)
	require.Empty(t, lines[0].Pending)
	require.JSONEq(t, `"task"`, string(lines[0].Exported["type"]))
	require.Contains(t, lines[0].Exported, "labels")
	require.NotContains(t, lines[0].Exported, "estimate")

	w.srv.ResetRequests()
	lines, _ = w.mustSync(jira.Options{})

	edits := puts(w.srv)
	require.Len(t, edits, 2)
	require.Equal(t, map[string]json.RawMessage{"issuetype": json.RawMessage(`{"id":"10002"}`)}, edits[0]["fields"],
		"the type alone, first")
	require.Empty(t, edits[0]["update"])
	_, labels := edits[1]["fields"]["labels"]
	_, labelOps := edits[1]["update"]["labels"]
	require.True(t, labels || labelOps, "%v", edits[1])
	require.NotContains(t, edits[1]["fields"], jiratest.FieldStoryPoints)
	require.NotContains(t, edits[1]["fields"], "issuetype")

	got := w.srv.Issue(key)
	require.Equal(t, "Task", got.Type)
	require.Equal(t, []string{"moved"}, got.Labels)

	require.Len(t, lines, 1)
	require.Empty(t, lines[0].Pending, "estimate is not a key of Task's merge")
	require.Empty(t, lines[0].Conflicts)
	require.JSONEq(t, `"task"`, string(lines[0].Exported["type"]))
	require.NotContains(t, lines[0].Exported, "estimate")

	ic := w.byKey(key)
	require.Equal(t, `"task"`, field(t, ic, "type"))
	require.Equal(t, `3`, field(t, ic, "estimate"), "kept locally, unsynced")
	w.quiet()
}

// TC8: a type change Jira refuses is final: Jira's type is taken back
// locally and reported as a conflict with Jira's message, the rest merges
// under the old type (Story's estimate is its story points), and the next
// run is quiet.
func TestTypeChangeRefused(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Not a bug in Jira")

	// Bug is on another workflow, so derive does not list it (TC7)
	require.ErrorContains(t, ref.set(map[string]issue.Value{"type": str("bug")}), "type change from story to bug is not allowed")
	retarget(t, w, "story", "task", "bug")
	require.NoError(t, ref.set(map[string]issue.Value{
		"type": str("bug"), "labels": issue.Value(`["kept"]`), "estimate": issue.MustValue(5),
	}))
	lines, _ := w.mustSync(jira.Options{})

	got := w.srv.Issue(key)
	require.Equal(t, "Story", got.Type)
	require.Equal(t, []string{"kept"}, got.Labels)
	require.EqualValues(t, 5, got.Custom[jiratest.FieldStoryPoints], "merged under Story's mapping")
	require.Len(t, lines, 1)
	reason := "Jira refused issuetype: The issue type selected is invalid."
	require.Equal(t, []jira.Conflict{{Key: "type", Local: str("bug"), Jira: str("story"), Reason: reason}}, lines[0].Conflicts)
	require.Empty(t, lines[0].Pending)
	require.Empty(t, lines[0].OffSchema)
	require.JSONEq(t, `"story"`, string(lines[0].Imported["type"]))

	ic := w.byKey(key)
	require.Equal(t, `"story"`, field(t, ic, "type"), "taken back")
	require.Contains(t, notesText(ic), "- type: local \"bug\" -> Jira \"story\" ("+reason+")")
	w.quiet()
}

// TC6 makes a linked issue retyped to a local-only type impossible by a
// local write; written into the store by hand around the import check, it
// is a refusal like Jira's (TC8): taken back with a conflict, nothing asked
// of Jira, and the next run quiet.
func TestTypeChangeToLocalOnly(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Becomes local")

	retarget(t, w, "story", "task", "iteration")
	require.NoError(t, ref.set(map[string]issue.Value{"type": str("iteration")}))

	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{DryRun: true})
	require.Zero(t, w.srv.Writes())
	require.Len(t, lines, 1)
	reason := "type iteration is not mapped to a Jira issue type"
	want := []jira.Conflict{{Key: "type", Local: str("iteration"), Jira: str("story"), Reason: reason}}
	require.Equal(t, want, lines[0].Conflicts, "a dry run reports what the run does")

	retarget(t, w, "story", "task", "iteration")
	w.srv.ResetRequests()
	lines, _ = w.mustSync(jira.Options{})
	require.Zero(t, w.srv.Writes())
	require.Equal(t, "Story", w.srv.Issue(key).Type)
	require.Len(t, lines, 1)
	require.Equal(t, want, lines[0].Conflicts)
	require.Empty(t, lines[0].Pending)
	require.Equal(t, `"story"`, field(t, w.byKey(key), "type"))
	w.quiet()
}

// TC2 and TC5: a Move in Jira across levels is imported though the schema
// forbids it locally; a type changed on both sides is Jira's, a conflict,
// and nothing is written for it.
func TestTypeChangePulled(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "Grows"})
	w.mustSync(jira.Options{})
	ref := &issueRef{w: w, key: key}
	require.EqualError(t, ref.set(map[string]issue.Value{"type": str("initiative")}),
		"type: type change from epic to initiative is not allowed; allowed: (none)")

	w.srv.MoveType(key, "Initiative")
	w.mustSync(jira.Options{})
	require.Equal(t, `"initiative"`, field(t, w.byKey(key), "type"))
	w.quiet()

	key, ref = storyIssue(t, w, "Both sides")
	require.NoError(t, ref.set(map[string]issue.Value{"type": str("task")}))
	w.srv.MoveType(key, "Bug")
	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{})
	require.Zero(t, w.srv.Writes())
	require.Equal(t, `"bug"`, field(t, w.byKey(key), "type"))
	require.Len(t, lines, 1)
	require.Equal(t, []jira.Conflict{{Key: "type", Local: str("task"), Jira: str("bug")}}, lines[0].Conflicts)
}
