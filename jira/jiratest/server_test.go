package jiratest_test

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func TestMyself(t *testing.T) {
	s := newServer(t)
	r := get(t, s, "/rest/api/3/myself")
	require.Equal(t, http.StatusOK, r.status)
	m := r.obj(t)
	require.Equal(t, jiratest.MiaID, m["accountId"])
	require.Equal(t, "Mia Krystof", m["displayName"])
	require.Equal(t, jiratest.MiaEmail, m["emailAddress"])
	require.Equal(t, "Europe/Berlin", m["timeZone"], "the zone JQL literals are read in (C6)")
	require.Equal(t, "atlassian", m["accountType"])
	require.Equal(t, jiratest.MiaID, r.header.Get("X-AAccountId"))
}

func TestAuth(t *testing.T) {
	s := newServer(t)
	s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "one"})
	bad := "Basic " + base64.StdEncoding.EncodeToString([]byte(jiratest.MiaEmail+":wrong"))

	t.Run("bad credentials are 401", func(t *testing.T) {
		r := get(t, s, "/rest/api/3/myself", withAuth(bad))
		require.Equal(t, http.StatusUnauthorized, r.status)
		errorBody(t, r)
		r = search(t, s, url.Values{"jql": {"project = PROJ"}}, withAuth(bad))
		require.Equal(t, http.StatusUnauthorized, r.status)
	})
	t.Run("a bearer token works", func(t *testing.T) {
		r := get(t, s, "/rest/api/3/myself", withAuth("Bearer "+jiratest.MiaToken))
		require.Equal(t, http.StatusOK, r.status)
	})
	t.Run("no credentials run anonymously", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, get(t, s, "/rest/api/3/myself", noAuth).status)
		r := search(t, s, url.Values{"jql": {"project = PROJ"}, "fields": {"summary"}}, noAuth)
		require.Equal(t, http.StatusOK, r.status, "200 with nothing in it (§17.5)")
		require.Empty(t, r.obj(t)["issues"])
		require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issue/PROJ-1", noAuth).status)
		require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/serverInfo", noAuth).status)
		w := do(t, s, http.MethodPut, "/rest/api/3/issue/PROJ-1", map[string]any{"fields": map[string]any{"summary": "x"}}, noAuth)
		require.Equal(t, http.StatusUnauthorized, w.status)
	})
	t.Run("or are rejected", func(t *testing.T) {
		s := newServer(t, jiratest.WithMissingAuth(jiratest.RejectMissingAuth))
		r := search(t, s, url.Values{"jql": {"project = PROJ"}}, noAuth)
		require.Equal(t, http.StatusUnauthorized, r.status)
	})
}

func TestDateHeaderAndClock(t *testing.T) {
	start := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	s := newServer(t, jiratest.WithStart(start))
	r := get(t, s, "/rest/api/3/serverInfo")
	date, err := http.ParseTime(r.header.Get("Date"))
	require.NoError(t, err)
	require.True(t, date.Equal(start))

	m := r.obj(t)
	require.Equal(t, "Cloud", m["deploymentType"])
	require.Equal(t, "America/Los_Angeles", m["serverTimeZone"])
	require.Equal(t, "2026-01-15T04:00:00.000-0800", m["serverTime"], "the site zone, no colon, never +0000")

	s.Advance(90 * time.Minute)
	date, _ = http.ParseTime(get(t, s, "/rest/api/3/serverInfo").header.Get("Date"))
	require.True(t, date.Equal(start.Add(90*time.Minute)))

	t.Run("an injected clock", func(t *testing.T) {
		now := start
		s := newServer(t, jiratest.WithClock(func() time.Time { return now }))
		now = now.Add(time.Hour)
		require.Equal(t, now, s.Now())
	})
}

func TestOldSearchIsGone(t *testing.T) {
	s := newServer(t)
	r := get(t, s, "/rest/api/3/search?jql=project%3DPROJ")
	require.Equal(t, http.StatusGone, r.status)
	msgs, _ := errorBody(t, r)
	require.Contains(t, msgs[0], "has been removed")
	r = do(t, s, http.MethodPost, "/rest/api/3/search", map[string]any{"jql": "project = PROJ"})
	require.Equal(t, http.StatusGone, r.status)
}

func TestUnknownRoutes(t *testing.T) {
	s := newServer(t)
	require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/workflow/search").status)
	require.Equal(t, http.StatusMethodNotAllowed, do(t, s, http.MethodPatch, "/rest/api/3/myself", nil).status)
}

func TestRequestLog(t *testing.T) {
	s := newServer(t)
	s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "one"})
	require.Zero(t, s.Writes(), "a helper is not an API write")
	get(t, s, "/rest/api/3/issue/PROJ-1?fields=summary")
	do(t, s, http.MethodPut, "/rest/api/3/issue/PROJ-1", map[string]any{"fields": map[string]any{"summary": "two"}})
	do(t, s, http.MethodPost, "/rest/api/3/search/jql", map[string]any{"jql": "project = PROJ"})

	reqs := s.Requests()
	require.Len(t, reqs, 3)
	require.Equal(t, "getIssue", reqs[0].Op)
	require.Equal(t, "summary", reqs[0].Query.Get("fields"))
	require.Equal(t, "editIssue", reqs[1].Op)
	require.JSONEq(t, `{"fields":{"summary":"two"}}`, string(reqs[1].Body))
	require.Equal(t, http.StatusNoContent, reqs[1].Status)
	require.Equal(t, 1, s.Writes(), "a POST search is a read")

	s.ResetRequests()
	require.Empty(t, s.Requests())
	require.Zero(t, s.Writes())
}
