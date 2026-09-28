package jiratest_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func byID(t *testing.T, list []any, id string) map[string]any {
	t.Helper()
	for _, v := range list {
		if m := v.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("no %s", id)
	return nil
}

func TestFields(t *testing.T) {
	s := newServer(t)
	fields := get(t, s, "/rest/api/3/field").arr(t)
	sprint := byID(t, fields, jiratest.FieldSprint)
	require.Equal(t, true, sprint["custom"])
	require.Equal(t, []any{"cf[10020]", "Sprint"}, sprint["clauseNames"])
	require.Equal(t, "com.pyxis.greenhopper.jira:gh-sprint", path(sprint, "schema", "custom"))
	require.Equal(t, "com.pyxis.greenhopper.jira:jsw-story-points", path(byID(t, fields, jiratest.FieldStoryPoints), "schema", "custom"))
	require.Equal(t, "com.pyxis.greenhopper.jira:gh-lexo-rank", path(byID(t, fields, jiratest.FieldRank), "schema", "custom"))
	require.Equal(t, "summary", path(byID(t, fields, "summary"), "schema", "system"))
	require.Equal(t, false, byID(t, fields, "comment")["navigable"])

	team := newServer(t, jiratest.WithSite(jiratest.TeamSite()))
	points := byID(t, get(t, team, "/rest/api/3/field").arr(t), "customfield_10036")
	require.Equal(t, "Story point estimate", points["name"])
	require.Equal(t, "com.atlassian.jira.plugin.system.customfieldtypes:float", path(points, "schema", "custom"), "C10")
	require.Equal(t, "10010", path(points, "scope", "project", "id"))
}

func TestProject(t *testing.T) {
	s := newServer(t)
	m := get(t, s, "/rest/api/3/project/PROJ").obj(t)
	require.Equal(t, "10000", m["id"])
	require.Equal(t, "classic", m["style"])
	require.Equal(t, false, m["simplified"])
	types := m["issueTypes"].([]any)
	require.EqualValues(t, 1, byID(t, types, "10000")["hierarchyLevel"])
	require.EqualValues(t, -1, byID(t, types, "10003")["hierarchyLevel"])
	require.Equal(t, true, byID(t, types, "10003")["subtask"])
	require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/project/10000").status)
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/project/NOPE").status)

	team := newServer(t, jiratest.WithSite(jiratest.TeamSite()))
	m = get(t, team, "/rest/api/3/project/TEAM").obj(t)
	require.Equal(t, "next-gen", m["style"])
	require.Equal(t, true, m["simplified"])
	require.NotEmpty(t, byID(t, m["issueTypes"].([]any), "10020")["entityId"])

	h := get(t, s, "/rest/api/3/project/10000/hierarchy").obj(t)
	require.EqualValues(t, 10000, h["projectId"])
	require.EqualValues(t, -1, path(h["hierarchy"].([]any)[0], "level"))

	sts := get(t, s, "/rest/api/3/project/PROJ/statuses").arr(t)
	story := byID(t, sts, "10001")
	qa := byID(t, story["statuses"].([]any), "10001")
	require.Equal(t, "Ready for QA", qa["name"])
	require.Equal(t, "indeterminate", path(qa, "statusCategory", "key"))
	epic := byID(t, sts, "10000")
	require.Len(t, epic["statuses"], 3)

	its := get(t, s, "/rest/api/3/issuetype/project?projectId=10000&level=-1").arr(t)
	require.Len(t, its, 1)
}

func TestCreateMeta(t *testing.T) {
	s := newServer(t)
	m := get(t, s, "/rest/api/3/issue/createmeta/PROJ/issuetypes?maxResults=2").obj(t)
	require.Len(t, m["issueTypes"], 2)
	require.EqualValues(t, 5, m["total"])
	require.EqualValues(t, 2, m["maxResults"])
	r := get(t, s, "/rest/api/3/issue/createmeta/PROJ/issuetypes?maxResults=201")
	require.Equal(t, http.StatusBadRequest, r.status)
	msgs, _ := errorBody(t, r)
	require.Equal(t, "Parameter 'maxResults' must not exceed the limit '200'", msgs[0])

	f := get(t, s, "/rest/api/3/issue/createmeta/PROJ/issuetypes/10004").obj(t)
	fields := f["fields"].([]any)
	var ids []string
	for _, x := range fields {
		ids = append(ids, x.(map[string]any)["fieldId"].(string))
	}
	require.Contains(t, ids, jiratest.FieldStoryPoints)
	require.NotContains(t, ids, jiratest.FieldSprint, "not on the create screen")
	for _, x := range fields {
		fm := x.(map[string]any)
		switch fm["fieldId"] {
		case "summary", "priority":
			require.Equal(t, true, fm["required"], fm["fieldId"])
		case "labels":
			require.Equal(t, []any{"add", "set", "remove"}, fm["operations"])
		}
		if fm["fieldId"] == "priority" {
			require.Len(t, fm["allowedValues"], 5)
		}
	}
	sub := get(t, s, "/rest/api/3/issue/createmeta/PROJ/issuetypes/10003").obj(t)
	for _, x := range sub["fields"].([]any) {
		if fm := x.(map[string]any); fm["fieldId"] == "parent" {
			require.Equal(t, true, fm["required"])
		}
	}
}

func TestEditMeta(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s"})
	f := get(t, s, "/rest/api/3/issue/"+key+"/editmeta").obj(t)["fields"].(map[string]any)
	require.Contains(t, f, jiratest.FieldSprint)
	require.NotContains(t, f, "status")
	require.NotContains(t, f, jiratest.FieldRank)
	require.Equal(t, []any{"add", "set", "remove"}, path(f, "labels", "operations"))
}

func TestSiteMetadata(t *testing.T) {
	s := newServer(t)
	cats := get(t, s, "/rest/api/3/statuscategory").arr(t)
	require.Len(t, cats, 4)
	require.Equal(t, "indeterminate", get(t, s, "/rest/api/3/statuscategory/4").obj(t)["key"])

	p := get(t, s, "/rest/api/3/priority/search?maxResults=2").obj(t)
	require.EqualValues(t, 5, p["total"])
	require.Equal(t, false, p["isLast"])
	require.Len(t, p["values"], 2)
	require.Len(t, get(t, s, "/rest/api/3/priority").arr(t), 5)
	require.Equal(t, true, byID(t, get(t, s, "/rest/api/3/priority/search").obj(t)["values"].([]any), "3")["isDefault"])

	require.Len(t, get(t, s, "/rest/api/3/resolution").arr(t), 4)
	require.Len(t, get(t, s, "/rest/api/3/resolution/search?id=10001").obj(t)["values"], 1)
	require.Len(t, get(t, s, "/rest/api/3/status").arr(t), 5)
}

func TestUsers(t *testing.T) {
	s := newServer(t)
	u := get(t, s, "/rest/api/3/user?accountId="+url.QueryEscape(jiratest.JoID)).obj(t)
	require.Equal(t, false, u["active"])
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/user?accountId=nobody").status)

	find := func(q string) []any { return get(t, s, "/rest/api/3/user/search?query="+url.QueryEscape(q)).arr(t) }
	require.Len(t, find("mi"), 1)
	require.Len(t, find("patel"), 1)
	require.Empty(t, find("ravi@"), "a hidden email needs the exact address")
	require.Len(t, find("ravi@example.com"), 1)
	require.Equal(t, http.StatusBadRequest, get(t, s, "/rest/api/3/user/search").status)
	apps := find("automation")
	require.Len(t, apps, 1)
	require.Equal(t, "app", apps[0].(map[string]any)["accountType"])
}

func TestDeny(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	s.Deny("getFields", "getUser", "findUsers", "getTransitions", "getIssue", "searchAndReconsileIssuesUsingJql",
		"getCreateIssueMetaIssueTypes", "getProject", "editIssue")

	fields := get(t, s, "/rest/api/3/field").arr(t)
	for _, f := range fields {
		require.Equal(t, false, f.(map[string]any)["custom"], "system fields only")
	}
	require.Equal(t, http.StatusForbidden, get(t, s, "/rest/api/3/user?accountId="+jiratest.MiaID).status)
	require.Empty(t, get(t, s, "/rest/api/3/user/search?query=mia").arr(t))
	require.Empty(t, get(t, s, "/rest/api/3/issue/"+key+"/transitions").obj(t)["transitions"])
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issue/"+key).status)
	r := search(t, s, url.Values{"jql": {"project = PROJ"}})
	require.Equal(t, http.StatusBadRequest, r.status)
	msgs, _ := errorBody(t, r)
	require.Equal(t, "The value 'PROJ' does not exist for the field 'project'.", msgs[0])
	require.Empty(t, get(t, s, "/rest/api/3/issue/createmeta/PROJ/issuetypes").obj(t)["issueTypes"])
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/project/PROJ").status)
	require.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPut, "/rest/api/3/issue/"+key,
		map[string]any{"fields": map[string]any{"summary": "x"}}).status)

	s.Allow("getIssue")
	require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/issue/"+key).status)
}
