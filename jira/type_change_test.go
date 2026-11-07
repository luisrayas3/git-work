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

// TC4: Story → Task writes issuetype alone and first, then the rest under
// Task's mapping: a key Task lacks in Jira is pending, the others land.
func TestTypeChangeExported(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Becomes a task")

	require.NoError(t, ref.set(map[string]issue.Value{
		"type": str("task"), "labels": issue.Value(`["moved"]`), "estimate": issue.MustValue(3),
	}))
	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{})

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
	require.Equal(t, []jira.Skip{{Key: "estimate", Reason: "task/estimate is local-only"}}, lines[0].Pending)
	require.JSONEq(t, `"task"`, string(lines[0].Exported["type"]))

	ic := w.byKey(key)
	require.Equal(t, `"task"`, field(t, ic, "type"))
	require.Equal(t, `3`, field(t, ic, "estimate"), "kept locally")
	w.quiet()
}

// TC4: a type change Jira refuses is pending with Jira's message, and the
// other fields still land, under the old type.
func TestTypeChangeRefused(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Not a bug in Jira")

	require.NoError(t, ref.set(map[string]issue.Value{"type": str("bug"), "labels": issue.Value(`["kept"]`)}))
	lines, _ := w.mustSync(jira.Options{})

	got := w.srv.Issue(key)
	require.Equal(t, "Story", got.Type)
	require.Equal(t, []string{"kept"}, got.Labels)
	require.Len(t, lines, 1)
	require.Equal(t, []jira.Skip{{Key: "type", Reason: "Jira refused issuetype: The issue type selected is invalid."}},
		lines[0].Pending)
	require.Equal(t, `"bug"`, field(t, w.byKey(key), "type"), "kept locally, pending")
}

// TC4: a type the schema maps to no Jira issue type is pending, and nothing
// is asked of Jira for it.
func TestTypeChangeToLocalOnly(t *testing.T) {
	w := newWorld(t, jiratest.WithSite(retypeSite()))
	key, ref := storyIssue(t, w, "Becomes local")

	// widened by hand after the import, as TC3 allows; the next derive takes it back
	story, err := w.c.Schema().ResolveKey(config.ShapeType, "story")
	require.NoError(t, err)
	_, err = story.Set(schema.AttrRetypeTargets, config.MustValue([]string{"task", "iteration"}))
	require.NoError(t, err)
	require.NoError(t, ref.set(map[string]issue.Value{"type": str("iteration")}))

	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{})
	require.Zero(t, w.srv.Writes())
	require.Equal(t, "Story", w.srv.Issue(key).Type)
	require.Len(t, lines, 1)
	require.Equal(t, []jira.Skip{{Key: "type", Reason: "type iteration is local-only"}}, lines[0].Pending)

	// and it stays pending on the next run, never silently unlinked
	lines, _ = w.mustSync(jira.Options{})
	require.Len(t, lines, 1)
	require.Equal(t, []jira.Skip{{Key: "type", Reason: "type iteration is local-only"}}, lines[0].Pending)
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
