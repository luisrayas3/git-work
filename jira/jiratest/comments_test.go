package jiratest_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func TestComments(t *testing.T) {
	s := newServer(t)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	base := "/rest/api/3/issue/" + key + "/comment"

	updated := s.Issue(key).Updated
	r := do(t, s, http.MethodPost, base, map[string]any{"body": adf("first")})
	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	c := r.obj(t)
	id := c["id"].(string)
	require.Equal(t, jiratest.MiaID, path(c, "author", "accountId"))
	require.Equal(t, c["created"], c["updated"])
	require.Equal(t, true, c["jsdPublic"])
	require.Equal(t, "doc", path(c, "body", "type"))
	require.Contains(t, path(c, "body", "content").([]any)[0], "attrs", "Jira normalises ADF (§4.12)")
	after := s.Issue(key).Updated
	require.True(t, after.After(updated), "adding a comment bumps updated")
	require.Empty(t, s.Issue(key).Changelog, "comments are not in the changelog")

	r = do(t, s, http.MethodPost, base, map[string]any{"body": "plain"})
	require.Equal(t, http.StatusBadRequest, r.status)
	_, errs := errorBody(t, r)
	require.Contains(t, errs["comment"], "Atlassian Document")

	r = do(t, s, http.MethodPut, base+"/"+id, map[string]any{"body": adf("edited")})
	require.Equal(t, http.StatusOK, r.status)
	e := r.obj(t)
	require.NotEqual(t, e["created"], e["updated"])
	require.Equal(t, after, s.Issue(key).Updated, "an edit does not bump updated (the harder default)")

	for i := 0; i < 4; i++ {
		s.AddComment(key, fmt.Sprintf("c%d", i))
	}
	page := get(t, s, base+"?startAt=1&maxResults=2&orderBy=-created").obj(t)
	require.EqualValues(t, 5, page["total"])
	require.EqualValues(t, 1, page["startAt"])
	require.NotContains(t, page, "isLast", "the legacy offset shape")
	cs := page["comments"].([]any)
	require.Len(t, cs, 2)
	require.Equal(t, "c2", path(cs[0], "body", "content").([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
	require.Equal(t, http.StatusBadRequest, get(t, s, base+"?orderBy=updated").status)

	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodDelete, base+"/"+id, nil).status)
	require.Equal(t, http.StatusNotFound, do(t, s, http.MethodDelete, base+"/"+id, nil).status)
	require.Len(t, s.Issue(key).Comments, 4)
	require.Equal(t, http.StatusMethodNotAllowed, do(t, s, http.MethodDelete, base+"/"+s.Issue(key).Comments[0].ID, nil, noAuth).status)
}

func TestCommentBumpKnob(t *testing.T) {
	s := newServer(t, jiratest.WithCommentBumps(true, true))
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	id := s.AddComment(key, "x")
	u0 := s.Issue(key).Updated
	s.EditComment(key, id, "y")
	u1 := s.Issue(key).Updated
	require.True(t, u1.After(u0))
	s.DeleteComment(key, id)
	require.True(t, s.Issue(key).Updated.After(u1))
}

func TestVerbatimADF(t *testing.T) {
	s := newServer(t, jiratest.WithVerbatimADF())
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	r := do(t, s, http.MethodPost, "/rest/api/3/issue/"+key+"/comment", map[string]any{"body": adf("x")})
	require.JSONEq(t, `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`,
		string(mustJSON(t, r.obj(t)["body"])))
}
