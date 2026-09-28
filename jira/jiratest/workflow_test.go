package jiratest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func transitionIDs(t *testing.T, m map[string]any) map[string]string {
	out := map[string]string{}
	for _, tr := range m["transitions"].([]any) {
		tm := tr.(map[string]any)
		out[tm["id"].(string)] = path(tm, "to", "name").(string)
	}
	return out
}

func TestTransitionsAreNotUniversal(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s"})

	r := get(t, s, "/rest/api/3/issue/"+key+"/transitions")
	require.Equal(t, http.StatusOK, r.status)
	m := r.obj(t)
	require.Equal(t, "transitions", m["expand"])
	require.Equal(t, map[string]string{"11": "In Progress", "51": "Won't Do"}, transitionIDs(t, m))
	for _, tr := range m["transitions"].([]any) {
		tm := tr.(map[string]any)
		require.Equal(t, tm["id"] == "51", tm["isGlobal"])
		require.Equal(t, true, tm["isAvailable"])
		require.NotContains(t, tm, "fields", "fields need expand=transitions.fields")
	}

	// Done is two steps away: no direct edge.
	r = do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{"transition": map[string]any{"id": "31"}})
	require.Equal(t, http.StatusBadRequest, r.status)
	msgs, _ := errorBody(t, r)
	require.Equal(t, "Transition id '31' is not valid for this issue.", msgs[0])

	for _, id := range []string{"11", "21"} {
		r = do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{"transition": map[string]any{"id": id}})
		require.Equal(t, http.StatusNoContent, r.status, string(r.body))
	}
	is := s.Issue(key)
	require.Equal(t, "Ready for QA", is.Status)
	require.Equal(t, jiratest.CategoryIndeterminate, is.Category, "a name that is not its category")

	t.Run("a required screen field", func(t *testing.T) {
		m := get(t, s, "/rest/api/3/issue/"+key+"/transitions?expand=transitions.fields").obj(t)
		var done map[string]any
		for _, tr := range m["transitions"].([]any) {
			if tr.(map[string]any)["id"] == "31" {
				done = tr.(map[string]any)
			}
		}
		require.Equal(t, true, done["hasScreen"])
		res := path(done, "fields", "resolution").(map[string]any)
		require.Equal(t, true, res["required"])
		require.NotEmpty(t, res["allowedValues"])

		r := do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{"transition": map[string]any{"id": "31"}})
		require.Equal(t, http.StatusBadRequest, r.status)
		_, errs := errorBody(t, r)
		require.Contains(t, errs, "resolution")

		r = do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{
			"transition": map[string]any{"id": "31"},
			"fields":     map[string]any{"resolution": map[string]any{"name": "Done"}},
			"update":     map[string]any{"comment": []any{map[string]any{"add": map[string]any{"body": adf("Closing")}}}},
		})
		require.Equal(t, http.StatusNoContent, r.status, string(r.body))
		is := s.Issue(key)
		require.Equal(t, "Done", is.Status)
		require.Equal(t, "Done", is.Resolution)
		require.Equal(t, "Closing", is.Comments[0].Text)
		f := get(t, s, "/rest/api/3/issue/"+key+"?fields=resolutiondate,status").obj(t)["fields"].(map[string]any)
		require.NotNil(t, f["resolutiondate"])
		require.Equal(t, "done", path(f, "status", "statusCategory", "key"))
	})
	t.Run("a field not on the screen", func(t *testing.T) {
		r := do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{
			"transition": map[string]any{"id": "41"}, "fields": map[string]any{"summary": "x"}})
		require.Equal(t, http.StatusBadRequest, r.status)
	})
	t.Run("reopening clears the resolution", func(t *testing.T) {
		r := do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", map[string]any{"transition": map[string]any{"id": 41}})
		require.Equal(t, http.StatusNoContent, r.status, "a numeric id is accepted too")
		require.Empty(t, s.Issue(key).Resolution)
	})
	t.Run("a global transition with a post function", func(t *testing.T) {
		s.Transition(key, "Won't Do")
		is := s.Issue(key)
		require.Equal(t, jiratest.CategoryDone, is.Category)
		require.Equal(t, "Won't Do", is.Resolution)
	})

	cl := s.Issue(key).Changelog
	require.Equal(t, "status", cl[0].Items[0].Field)
	require.Equal(t, "status", cl[0].Items[0].FieldID)
	require.Equal(t, "10000", cl[0].Items[0].From)
	require.Equal(t, "In Progress", cl[0].Items[0].ToString)
}

func TestConcurrentTransitionConflict(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	s.ConflictNextTransitions(1)
	body := map[string]any{"transition": map[string]any{"id": "11"}}
	require.Equal(t, http.StatusConflict, do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", body).status)
	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/transitions", body).status)
}

func TestTeamManagedWorkflow(t *testing.T) {
	s := newServer(t, jiratest.WithSite(jiratest.TeamSite()))
	key := s.CreateIssue(jiratest.IssueSpec{Project: "TEAM", Type: "Task", Summary: "t", Status: "Done"})
	m := get(t, s, "/rest/api/3/issue/"+key+"/transitions").obj(t)
	require.Len(t, transitionIDs(t, m), 3, "any status to any")
	m = get(t, s, "/rest/api/3/issue/"+key+"?fields=status,issuetype").obj(t)
	require.Equal(t, "PROJECT", path(m, "fields", "status", "scope", "type"))
	require.Equal(t, "10010", path(m, "fields", "issuetype", "scope", "project", "id"))
}
