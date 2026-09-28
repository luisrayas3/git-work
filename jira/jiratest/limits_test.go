package jiratest_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func TestRateLimitNext(t *testing.T) {
	s := newServer(t, jiratest.WithRetryAfter(7))
	s.RateLimitNext(2)
	for i := 0; i < 2; i++ {
		r := get(t, s, "/rest/api/3/myself")
		require.Equal(t, http.StatusTooManyRequests, r.status)
		require.Equal(t, "7", r.header.Get("Retry-After"))
		require.Equal(t, "0", r.header.Get("X-RateLimit-Remaining"))
		require.Equal(t, "jira-burst-based", r.header.Get("RateLimit-Reason"))
		_, err := time.Parse(time.RFC3339, r.header.Get("X-RateLimit-Reset"))
		require.NoError(t, err)
		require.NotEmpty(t, r.header.Get("X-RateLimit-Limit"))
	}
	require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/myself").status)
}

func TestRateLimitWindow(t *testing.T) {
	s := newServer(t, jiratest.WithRateLimit(3, 10*time.Second))
	for i := 0; i < 3; i++ {
		r := get(t, s, "/rest/api/3/serverInfo")
		require.Equal(t, http.StatusOK, r.status)
		require.Equal(t, fmt.Sprint(2-i), r.header.Get("X-RateLimit-Remaining"))
	}
	r := get(t, s, "/rest/api/3/serverInfo")
	require.Equal(t, http.StatusTooManyRequests, r.status)
	require.Equal(t, "10", r.header.Get("Retry-After"))
	s.Advance(10 * time.Second)
	require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/serverInfo").status)
}

// TestPerIssueWriteLimit is api.md §12.1's 20 writes per 2 s on one
// issue, on a frozen clock.
func TestPerIssueWriteLimit(t *testing.T) {
	s := newServer(t, jiratest.WithAutoAdvance(0))
	a := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
	b := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b"})
	edit := func(key string, i int) resp {
		return do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{"summary": fmt.Sprint(i)}})
	}
	for i := 0; i < 20; i++ {
		require.Equal(t, http.StatusNoContent, edit(a, i).status)
	}
	r := edit(a, 20)
	require.Equal(t, http.StatusTooManyRequests, r.status)
	require.Equal(t, "jira-per-issue-on-write", r.header.Get("RateLimit-Reason"))
	require.Equal(t, "2", r.header.Get("Retry-After"))
	require.Equal(t, http.StatusNoContent, edit(b, 0).status, "the limit is per issue")
	s.Advance(2 * time.Second)
	require.Equal(t, http.StatusNoContent, edit(a, 21).status)

	t.Run("disabled", func(t *testing.T) {
		s := newServer(t, jiratest.WithAutoAdvance(0), jiratest.WithPerIssueWriteLimits())
		key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
		for i := 0; i < 30; i++ {
			r := do(t, s, http.MethodPut, "/rest/api/3/issue/"+key, map[string]any{"fields": map[string]any{"summary": fmt.Sprint(i)}})
			require.Equal(t, http.StatusNoContent, r.status)
		}
	})
}

func TestChangelogPaging(t *testing.T) {
	s := newServer(t, jiratest.WithCustomFieldIDs())
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s", Labels: []string{"a"}})
	s.Edit(key, map[string]any{"labels": []string{"a", "b"}})
	s.Edit(key, map[string]any{jiratest.FieldSprint: 37})
	s.Edit(key, map[string]any{jiratest.FieldSprint: 38})
	s.Edit(key, map[string]any{jiratest.FieldStoryPoints: 2})
	s.Transition(key, "In Progress")

	base := "/rest/api/3/issue/" + key + "/changelog"
	m := get(t, s, base+"?maxResults=2").obj(t)
	require.EqualValues(t, 5, m["total"])
	require.Equal(t, false, m["isLast"])
	require.Contains(t, m["nextPage"], "startAt=2")
	values := m["values"].([]any)
	require.Len(t, values, 2)

	labels := path(values[0], "items").([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"field": "labels", "fieldtype": "jira", "fieldId": "labels",
		"from": nil, "fromString": "a", "to": nil, "toString": "a b"}, labels, "whole sets, space-joined")
	require.Equal(t, jiratest.RaviID, path(values[0], "author", "accountId"))
	_, err := time.Parse("2006-01-02T15:04:05.000-0700", path(values[0], "created").(string))
	require.NoError(t, err)

	sprint := path(values[1], "items").([]any)[0].(map[string]any)
	require.Equal(t, "Sprint", sprint["field"])
	require.Equal(t, "custom", sprint["fieldtype"])
	require.Equal(t, jiratest.FieldSprint, sprint["fieldId"])
	require.Nil(t, sprint["from"])
	require.Equal(t, "37", sprint["to"])

	m = get(t, s, base+"?startAt=2&maxResults=2").obj(t)
	sprint = path(m["values"].([]any)[0], "items").([]any)[0].(map[string]any)
	require.Equal(t, "37", sprint["from"])
	require.Equal(t, "38", sprint["to"])
	require.Equal(t, "PROJ Sprint 5, the long one", sprint["toString"], "parse to, never toString")

	m = get(t, s, base+"?startAt=4").obj(t)
	require.Equal(t, true, m["isLast"])
	require.NotContains(t, m, "nextPage")

	t.Run("custom fieldIds go missing by default (C8)", func(t *testing.T) {
		s := newServer(t, jiratest.WithSeed(3))
		key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s"})
		missing := 0
		for i := 1; i <= 12; i++ {
			s.Edit(key, map[string]any{jiratest.FieldStoryPoints: i})
		}
		for _, h := range s.Issue(key).Changelog {
			if h.Items[0].FieldID == "" {
				missing++
			}
		}
		require.Positive(t, missing)
		require.Less(t, missing, 12)
	})
}
