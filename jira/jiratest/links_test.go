package jiratest_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func issueLinks(t *testing.T, s *jiratest.Server, key string) []any {
	m := get(t, s, "/rest/api/3/issue/"+key+"?fields=issuelinks").obj(t)
	return path(m, "fields", "issuelinks").([]any)
}

// TestLinkOrientation is C1: {inwardIssue: A, outwardIssue: B, Blocks}
// means A blocks B; A shows outwardIssue B, B shows inwardIssue A.
func TestLinkOrientation(t *testing.T) {
	s := newServer(t)
	a := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
	b := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b"})

	body := map[string]any{"type": map[string]any{"name": "Blocks"},
		"inwardIssue": map[string]any{"key": a}, "outwardIssue": map[string]any{"key": b}}
	r := do(t, s, http.MethodPost, "/rest/api/3/issueLink", body)
	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	require.Empty(t, r.body, "201 with an empty body")
	require.Equal(t, http.StatusCreated, do(t, s, http.MethodPost, "/rest/api/3/issueLink", body).status, "a duplicate is a no-op")

	la := issueLinks(t, s, a)
	require.Len(t, la, 1)
	require.Contains(t, la[0], "outwardIssue")
	require.NotContains(t, la[0], "inwardIssue")
	require.Equal(t, b, path(la[0], "outwardIssue", "key"))
	require.Equal(t, "blocks", path(la[0], "type", "outward"))
	require.Contains(t, path(la[0], "outwardIssue", "fields"), "status")

	lb := issueLinks(t, s, b)
	require.Len(t, lb, 1)
	require.Equal(t, a, path(lb[0], "inwardIssue", "key"))
	require.NotContains(t, lb[0], "outwardIssue")
	require.Equal(t, path(la[0], "id"), path(lb[0], "id"), "one link, seen from both ends")

	id := la[0].(map[string]any)["id"].(string)
	l := get(t, s, "/rest/api/3/issueLink/"+id).obj(t)
	require.Equal(t, a, path(l, "inwardIssue", "key"))
	require.Equal(t, b, path(l, "outwardIssue", "key"))

	ia := s.Issue(a)
	require.Equal(t, []jiratest.IssueLink{{ID: id, Type: "Blocks", Outward: true, Other: b}}, ia.Links)
	item := ia.Changelog[0].Items[0]
	require.Equal(t, jiratest.ChangeItem{Field: "Link", FieldType: "jira", To: b, ToString: "This issue blocks " + b}, item)
	require.Equal(t, "This issue is blocked by "+a, s.Issue(b).Changelog[0].Items[0].ToString)

	require.Equal(t, http.StatusNoContent, do(t, s, http.MethodDelete, "/rest/api/3/issueLink/"+id, nil).status)
	require.Empty(t, issueLinks(t, s, a))
	require.Empty(t, issueLinks(t, s, b))
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issueLink/"+id).status)
	require.Equal(t, http.StatusBadRequest, get(t, s, "/rest/api/3/issueLink/abc").status)
	require.Equal(t, "This issue blocks "+b, s.Issue(a).Changelog[1].Items[0].FromString)
}

func TestLinkErrorsAndComment(t *testing.T) {
	s := newServer(t)
	a := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
	b := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b"})

	r := do(t, s, http.MethodPost, "/rest/api/3/issueLink", map[string]any{"type": map[string]any{"name": "Nope"},
		"inwardIssue": map[string]any{"key": a}, "outwardIssue": map[string]any{"key": b}})
	require.Equal(t, http.StatusNotFound, r.status)
	r = do(t, s, http.MethodPost, "/rest/api/3/issueLink", map[string]any{"type": map[string]any{"id": "10003"},
		"inwardIssue": map[string]any{"key": a}, "outwardIssue": map[string]any{"key": "PROJ-99"}})
	require.Equal(t, http.StatusNotFound, r.status)

	// C11: a link comment goes on the outwardIssue, as the spec says.
	r = do(t, s, http.MethodPost, "/rest/api/3/issueLink", map[string]any{"type": map[string]any{"id": "10003"},
		"inwardIssue": map[string]any{"id": s.Issue(a).ID}, "outwardIssue": map[string]any{"key": b},
		"comment": map[string]any{"body": adf("linked")}})
	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	require.Empty(t, s.Issue(a).Comments)
	require.Len(t, s.Issue(b).Comments, 1)

	types := get(t, s, "/rest/api/3/issueLinkType").obj(t)["issueLinkTypes"].([]any)
	require.Equal(t, map[string]any{"id": "10000", "name": "Blocks", "inward": "is blocked by", "outward": "blocks",
		"self": s.URL() + "/rest/api/3/issueLinkType/10000"}, types[0])
}

func TestDeletingAnIssueDropsItsLinks(t *testing.T) {
	s := newServer(t)
	a := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
	b := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b"})
	s.Link(a, "Blocks", b)
	s.Delete(b)
	require.Empty(t, issueLinks(t, s, a))
}
