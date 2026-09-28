package jiratest_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func createBody(fields map[string]any) map[string]any {
	f := map[string]any{"project": map[string]any{"key": "PROJ"}, "issuetype": map[string]any{"id": "10002"}}
	for k, v := range fields {
		f[k] = v
	}
	return map[string]any{"fields": f}
}

func TestCreateIssue(t *testing.T) {
	s := newServer(t)

	r := do(t, s, http.MethodPost, "/rest/api/3/issue", createBody(map[string]any{
		"summary":                 "  Checkout fails  ",
		"description":             adf("Steps"),
		"labels":                  []string{"guest", "checkout"},
		"duedate":                 "2026-10-15",
		"assignee":                map[string]any{"accountId": jiratest.RaviID},
		jiratest.FieldStoryPoints: 3,
	}))
	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	m := r.obj(t)
	require.Equal(t, "10001", m["id"])
	require.Equal(t, "PROJ-1", m["key"])
	require.Equal(t, s.URL()+"/rest/api/3/issue/10001", m["self"])

	is := s.Issue("PROJ-1")
	require.Equal(t, "Checkout fails", is.Summary, "trimmed")
	require.Equal(t, []string{"checkout", "guest"}, is.Labels, "sorted")
	require.Equal(t, "To Do", is.Status)
	require.Equal(t, "Medium", is.Priority, "the default priority")
	require.Equal(t, jiratest.MiaID, is.Reporter)
	require.Equal(t, 3.0, is.Custom[jiratest.FieldStoryPoints])
	require.Empty(t, is.Changelog, "a create has no history")
	require.Equal(t, 1, s.Writes())
}

func TestCreateIssueErrors(t *testing.T) {
	s := newServer(t)
	epic := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "epic"})

	cases := []struct {
		name  string
		body  map[string]any
		field string
		msg   string
	}{
		{"no summary", createBody(nil), "summary", "You must specify a summary of the issue."},
		{"a string description", createBody(map[string]any{"summary": "x", "description": "plain"}),
			"description", "Operation value must be an Atlassian Document (see the Atlassian Document Format)"},
		{"a summary too long", createBody(map[string]any{"summary": strings.Repeat("x", 256)}), "summary", "255"},
		{"a label with a space", createBody(map[string]any{"summary": "x", "labels": []string{"a b"}}), "labels", "spaces"},
		{"a field not on the create screen", createBody(map[string]any{"summary": "x", jiratest.FieldSprint: 37}),
			jiratest.FieldSprint, "cannot be set. It is not on the appropriate screen, or unknown."},
		{"status is not a field", createBody(map[string]any{"summary": "x", "status": map[string]any{"id": "3"}}),
			"status", "cannot be set"},
		{"a required field", map[string]any{"fields": map[string]any{"project": map[string]any{"key": "PROJ"},
			"issuetype": map[string]any{"id": "10004"}, "summary": "bug", "priority": nil}}, "priority", "Priority is required."},
		{"the issue type by name", map[string]any{"fields": map[string]any{"project": map[string]any{"key": "PROJ"},
			"issuetype": map[string]any{"name": "Task"}, "summary": "x"}}, "issuetype", "Specify an issue type"},
		{"a sub-task without a parent", map[string]any{"fields": map[string]any{"project": map[string]any{"key": "PROJ"},
			"issuetype": map[string]any{"id": "10003"}, "summary": "x"}}, "parent", "must have a parent"},
		{"a sub-task under an epic", map[string]any{"fields": map[string]any{"project": map[string]any{"key": "PROJ"},
			"issuetype": map[string]any{"id": "10003"}, "summary": "x", "parent": map[string]any{"key": epic}}},
			"parent", "hierarchy"},
		{"an unknown project", map[string]any{"fields": map[string]any{"project": map[string]any{"key": "NOPE"},
			"issuetype": map[string]any{"id": "10002"}, "summary": "x"}}, "project", "project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := do(t, s, http.MethodPost, "/rest/api/3/issue", tc.body)
			require.Equal(t, http.StatusBadRequest, r.status, string(r.body))
			_, errs := errorBody(t, r)
			require.Contains(t, errs[tc.field], tc.msg)
		})
	}
	require.Equal(t, []string{epic}, s.Keys(), "nothing was created")
}

func TestCreateSubtaskAndEpicChild(t *testing.T) {
	s := newServer(t)
	epic := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "epic"})
	story := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "story", Parent: epic})
	r := do(t, s, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": map[string]any{
		"project": map[string]any{"id": "10000"}, "issuetype": map[string]any{"id": "10003"},
		"summary": "sub", "parent": map[string]any{"key": story}}})
	require.Equal(t, http.StatusCreated, r.status, string(r.body))

	m := get(t, s, "/rest/api/3/issue/"+story+"?fields=parent,subtasks").obj(t)
	parent := path(m, "fields", "parent").(map[string]any)
	require.Equal(t, epic, parent["key"])
	require.Equal(t, "epic", path(parent, "fields", "summary"))
	require.EqualValues(t, 1, path(parent, "fields", "issuetype", "hierarchyLevel"))
	require.Contains(t, path(parent, "fields"), "status")
	require.Contains(t, path(parent, "fields"), "priority")
	subs := path(m, "fields", "subtasks").([]any)
	require.Len(t, subs, 1)
	require.Equal(t, true, path(subs[0], "fields", "issuetype", "subtask"))

	e := get(t, s, "/rest/api/3/issue/"+epic+"?fields=parent,summary").obj(t)
	require.NotContains(t, e["fields"], "parent", "absent with no parent")
}

func TestGetIssueShapes(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "story",
		Fields: map[string]any{jiratest.FieldSprint: 37, jiratest.FieldStoryPoints: 5}})

	r := get(t, s, "/rest/api/3/issue/"+key)
	require.Equal(t, http.StatusOK, r.status)
	m := r.obj(t)
	f := m["fields"].(map[string]any)

	require.Equal(t, "2026-07-01T09:00:00.000-0700", f["created"], "site zone, ms, no colon (C4)")
	_, err := time.Parse("2006-01-02T15:04:05.000-0700", f["updated"].(string))
	require.NoError(t, err)

	require.Equal(t, map[string]any{"id": 2.0, "key": "new", "name": "To Do", "colorName": "blue-gray",
		"self": s.URL() + "/rest/api/3/statuscategory/2"}, path(f, "status", "statusCategory"))
	require.EqualValues(t, 0, path(f, "issuetype", "hierarchyLevel"))
	require.Equal(t, false, path(f, "issuetype", "subtask"))

	reporter := f["reporter"].(map[string]any)
	require.Equal(t, jiratest.RaviID, reporter["accountId"])
	require.NotContains(t, reporter, "emailAddress", "hidden email is omitted (R5)")

	sprints := f[jiratest.FieldSprint].([]any)
	require.Len(t, sprints, 1)
	sp := sprints[0].(map[string]any)
	require.EqualValues(t, 37, sp["id"])
	require.Equal(t, "2026-06-22T08:00:00.000Z", sp["startDate"], "sprint dates are UTC Z (C3)")
	require.Regexp(t, `^0\|[0-9a-z]+:$`, f[jiratest.FieldRank])
	require.Equal(t, 5.0, f[jiratest.FieldStoryPoints])
	require.Equal(t, 0.0, path(f, "comment", "total"), "comment is a page object (R3)")
	require.Nil(t, f["resolution"])
	require.Nil(t, f["duedate"])
	require.Equal(t, []any{}, f["labels"])

}

func TestGetIssueNamesSchema(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "story"})
	m := get(t, s, "/rest/api/3/issue/"+key+"?fields=summary,"+jiratest.FieldSprint+"&expand=names,schema").obj(t)
	require.Len(t, m["fields"], 2)
	require.Equal(t, "Sprint", path(m, "names", jiratest.FieldSprint))
	require.Equal(t, map[string]any{"type": "array", "items": "json",
		"custom": "com.pyxis.greenhopper.jira:gh-sprint", "customId": 10020.0}, path(m, "schema", jiratest.FieldSprint))
}

func TestGetIssueNotFound(t *testing.T) {
	s := newServer(t)
	r := get(t, s, "/rest/api/3/issue/PROJ-99")
	require.Equal(t, http.StatusNotFound, r.status)
	msgs, _ := errorBody(t, r)
	require.Equal(t, "Issue does not exist or you do not have permission to see it.", msgs[0])
}

func TestEditIssue(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "story", Labels: []string{"a", "b"}})
	before := s.Issue(key).Updated

	r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{
		"fields": map[string]any{"summary": "renamed", jiratest.FieldSprint: 37},
		"update": map[string]any{"labels": []any{map[string]any{"add": "c"}, map[string]any{"remove": "a"}}},
	})
	require.Equal(t, http.StatusNoContent, r.status, string(r.body))
	require.Empty(t, r.body)
	is := s.Issue(key)
	require.Equal(t, "renamed", is.Summary)
	require.Equal(t, []string{"b", "c"}, is.Labels)
	require.Equal(t, []int{37}, is.Custom[jiratest.FieldSprint])
	require.True(t, is.Updated.After(before), "a write bumps updated")

	t.Run("sprint is a number, not an array (R18)", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{jiratest.FieldSprint: []int{38}}})
		require.Equal(t, http.StatusBadRequest, r.status)
		_, errs := errorBody(t, r)
		require.Contains(t, errs[jiratest.FieldSprint], "Number value expected")
		require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key,
			map[string]any{"fields": map[string]any{jiratest.FieldSprint: 38}}).status)
		require.Equal(t, []int{38}, s.Issue(key).Custom[jiratest.FieldSprint], "one open sprint at a time")
	})
	t.Run("all or nothing", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{
			"summary": "never", "duedate": "tomorrow"}})
		require.Equal(t, http.StatusBadRequest, r.status)
		require.Equal(t, "renamed", s.Issue(key).Summary)
	})
	t.Run("status goes through transitions", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{"status": map[string]any{"id": "3"}}})
		require.Equal(t, http.StatusBadRequest, r.status)
		_, errs := errorBody(t, r)
		require.Contains(t, errs["status"], "cannot be set")
	})
	t.Run("PUT does not check the create screen", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key,
			map[string]any{"fields": map[string]any{jiratest.FieldSprint: nil}}).status)
	})
	t.Run("rank is read only", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{jiratest.FieldRank: "0|a:"}})
		require.Equal(t, http.StatusBadRequest, r.status)
	})
	t.Run("notifyUsers=false needs an admin (C5)", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key+"?notifyUsers=false", map[string]any{"fields": map[string]any{"summary": "x"}})
		require.Equal(t, http.StatusBadRequest, r.status)
		msgs, _ := errorBody(t, r)
		require.Equal(t, "To discard the user notification either admin or project admin permissions are required.", msgs[0])
		require.Equal(t, "renamed", s.Issue(key).Summary)
	})
	t.Run("returnIssue", func(t *testing.T) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key+"?returnIssue=true", map[string]any{"fields": map[string]any{"summary": "back"}})
		require.Equal(t, http.StatusOK, r.status)
		require.Equal(t, "back", path(r.obj(t), "fields", "summary"))
	})
	t.Run("an empty body", func(t *testing.T) {
		require.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, nil).status)
	})
	t.Run("user fields take accountId or id", func(t *testing.T) {
		for _, u := range []map[string]any{{"accountId": jiratest.MiaID}, {"id": jiratest.RaviID}} {
			require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key,
				map[string]any{"fields": map[string]any{"assignee": u}}).status)
		}
		require.Equal(t, jiratest.RaviID, s.Issue(key).Assignee)
	})
}

func TestEditParent(t *testing.T) {
	s := newServer(t)
	e1 := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "e1"})
	e2 := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "e2"})
	story := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s", Parent: e1})

	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPut, "/rest/api/3/issue/"+story,
		map[string]any{"fields": map[string]any{"parent": map[string]any{"key": e2}}}).status)
	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPut, "/rest/api/3/issue/"+story,
		map[string]any{"update": map[string]any{"parent": []any{map[string]any{"set": map[string]any{"none": true}}}}}).status)
	require.Empty(t, s.Issue(story).Parent)

	// C2: IssueParentAssociation, parent ids in from/to, keys in the strings, no fieldId.
	cl := get(t, s, "/rest/api/3/issue/"+story+"/changelog").obj(t)
	values := cl["values"].([]any)
	require.Len(t, values, 2)
	it := path(values[0], "items").([]any)[0].(map[string]any)
	require.Equal(t, "IssueParentAssociation", it["field"])
	require.NotContains(t, it, "fieldId")
	require.Equal(t, s.Issue(e1).ID, it["from"])
	require.Equal(t, e1, it["fromString"])
	require.Equal(t, s.Issue(e2).ID, it["to"])
	require.Equal(t, e2, it["toString"])
	it = path(values[1], "items").([]any)[0].(map[string]any)
	require.Nil(t, it["to"])
}

func TestDeleteIssue(t *testing.T) {
	s := newServer(t)
	story := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s"})
	sub := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Sub-task", Summary: "sub", Parent: story})

	r := do(t, s, http.MethodDelete, "/rest/api/3/issue/"+story, nil)
	require.Equal(t, http.StatusBadRequest, r.status, "sub-tasks need deleteSubtasks")
	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodDelete, "/rest/api/3/issue/"+story+"?deleteSubtasks=true", nil).status)
	require.False(t, s.Exists(story))
	require.False(t, s.Exists(sub))
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issue/"+story).status)
}

func TestMovedIssueKeepsResolving(t *testing.T) {
	s := newServer(t)
	s.Seed(jiratest.Project{ID: "10001", Key: "NEW", Name: "New", Lead: jiratest.RaviID,
		Statuses:   jiratest.CompanySite().Projects[0].Statuses,
		IssueTypes: jiratest.CompanySite().Projects[0].IssueTypes})
	old := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	id := s.Issue(old).ID
	moved := s.Move(old, "NEW")
	require.Equal(t, "NEW-1", moved)

	m := get(t, s, "/rest/api/3/issue/"+strings.ToLower(old)+"?fields=project").obj(t)
	require.Equal(t, moved, m["key"], "GET by the old key answers the new one, no redirect")
	require.Equal(t, id, m["id"])
	require.Equal(t, "NEW", path(m, "fields", "project", "key"))

	items := s.Issue(moved).Changelog[0].Items
	require.Equal(t, "Key", items[0].Field)
	require.Empty(t, items[0].FieldID)
	require.Equal(t, old, items[0].FromString)
	require.Equal(t, "project", items[1].Field)
}

func TestIssueProperties(t *testing.T) {
	s := newServer(t)
	r := do(t, s, http.MethodPost, "/rest/api/3/issue", map[string]any{
		"fields":     createBody(map[string]any{"summary": "x"})["fields"],
		"properties": []any{map[string]any{"key": "gitwork", "value": map[string]any{"id": "abc", "rev": 1}}},
	})
	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	key := r.obj(t)["key"].(string)
	updated := s.Issue(key).Updated

	p := get(t, s, "/rest/api/3/issue/"+key+"/properties/gitwork")
	require.Equal(t, http.StatusOK, p.status)
	require.Equal(t, map[string]any{"key": "gitwork", "value": map[string]any{"id": "abc", "rev": 1.0}}, p.obj(t))

	require.Equal(t, http.StatusOK, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key+"/properties/gitwork", map[string]any{"rev": 2}).status, "updated")
	require.Equal(t, http.StatusCreated, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key+"/properties/other", `"v"`).status, "created")
	require.Equal(t, updated, s.Issue(key).Updated, "property writes do not bump updated")
	require.Empty(t, s.Issue(key).Changelog)

	m := get(t, s, "/rest/api/3/issue/"+key+"?fields=summary&properties=gitwork").obj(t)
	require.Equal(t, map[string]any{"gitwork": map[string]any{"rev": 2.0}}, m["properties"])
	keys := get(t, s, "/rest/api/3/issue/"+key+"/properties").obj(t)["keys"].([]any)
	require.Len(t, keys, 2)

	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodDelete, "/rest/api/3/issue/"+key+"/properties/other", nil).status)
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issue/"+key+"/properties/other").status)
	require.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key+"/properties/empty", nil).status)
}

// TestDescriptionRoundTrip: Jira stores ADF with attrs added (§4.12), and
// sending the same document again changes nothing.
func TestDescriptionRoundTrip(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	put := func(doc any) {
		r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{"description": doc}})
		require.Equal(t, http.StatusNoContent, r.status, string(r.body))
	}
	put(adf("Steps"))
	stored := path(get(t, s, "/rest/api/3/issue/"+key+"?fields=description").obj(t), "fields", "description")
	require.NotEmpty(t, path(stored.(map[string]any)["content"].([]any)[0], "attrs", "localId"))
	updated := s.Issue(key).Updated

	put(adf("Steps"))
	put(stored)
	require.Equal(t, updated, s.Issue(key).Updated)
	require.Len(t, s.Issue(key).Changelog, 1)
	require.Equal(t, "Steps", s.Issue(key).Changelog[0].Items[0].ToString, "the changelog carries text")

	put(map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph",
		"content": []any{map[string]any{"type": "text", "text": "Ste"}, map[string]any{"type": "text", "text": "ps"}}}}})
	require.Len(t, s.Issue(key).Changelog, 1, "adjacent text nodes are merged")
	put(nil)
	require.Nil(t, s.Issue(key).Description)
}

// TestTypeChange: an edit changes the type within one workflow and level,
// as Jira's does, with its changelog item; anything else is a move.
func TestTypeChange(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "t"})
	put := func(typ map[string]any) resp {
		return do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{"issuetype": typ}})
	}
	r := put(map[string]any{"id": "10004"})
	require.Equal(t, http.StatusNoContent, r.status, string(r.body))
	require.Equal(t, "Bug", s.Issue(key).Type)
	item := s.Issue(key).Changelog[0].Items[0]
	require.Equal(t, "issuetype", item.Field)
	require.Equal(t, "Story", item.FromString)
	require.Equal(t, "Bug", item.ToString)

	for _, typ := range []map[string]any{{"name": "Epic"}, {"name": "Sub-task"}, {"id": "99"}} {
		_, errs := errorBody(t, put(typ))
		require.Equal(t, "The issue type selected is invalid.", errs["issuetype"])
	}
	require.Equal(t, "Bug", s.Issue(key).Type)
}
