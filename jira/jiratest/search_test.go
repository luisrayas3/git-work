package jiratest_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

func seedTasks(t *testing.T, s *jiratest.Server, n int) []string {
	var keys []string
	for i := 0; i < n; i++ {
		keys = append(keys, s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: fmt.Sprintf("task %d", i)}))
	}
	return keys
}

func TestSearchDefaultsToIDs(t *testing.T) {
	s := newServer(t)
	seedTasks(t, s, 2)
	m := search(t, s, url.Values{"jql": {"project = PROJ ORDER BY id ASC"}}).obj(t)
	require.Equal(t, []any{map[string]any{"id": "10001"}, map[string]any{"id": "10002"}}, m["issues"],
		"by default, IDs only (api.md §2.1)")
	require.Equal(t, true, m["isLast"])
	require.NotContains(t, m, "nextPageToken", "omitted on the last page (R13)")
	require.NotContains(t, m, "total")
}

func TestSearchFieldSelection(t *testing.T) {
	s := newServer(t)
	key := seedTasks(t, s, 1)[0]
	s.AddComment(key, "hello")

	fields := func(sel ...string) map[string]any {
		m := search(t, s, url.Values{"jql": {"project = PROJ"}, "fields": sel}).obj(t)
		return path(m["issues"].([]any)[0], "fields").(map[string]any)
	}
	f := fields("summary,status")
	require.Len(t, f, 2)
	f = fields("summary", "updated")
	require.Len(t, f, 2, "repeated as well as comma-separated")
	f = fields("*navigable")
	require.Contains(t, f, "summary")
	require.NotContains(t, f, "comment", "comment is not navigable")
	f = fields("*all")
	require.Contains(t, f, "comment")
	require.Contains(t, f, jiratest.FieldSprint)
	f = fields("*all", "-comment", "-description")
	require.NotContains(t, f, "comment")
	require.NotContains(t, f, "description")

	t.Run("names and schema are top level", func(t *testing.T) {
		r := do(t, s, http.MethodPost, "/rest/api/3/search/jql", map[string]any{
			"jql": "project = PROJ", "fields": []string{"summary", jiratest.FieldSprint}, "expand": "names,schema"})
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		m := r.obj(t)
		require.Equal(t, "Sprint", path(m, "names", jiratest.FieldSprint))
		require.Equal(t, "string", path(m, "schema", "summary", "type"))
		require.NotContains(t, m["issues"].([]any)[0], "names")
	})
	t.Run("expand is a string in the POST body", func(t *testing.T) {
		r := do(t, s, http.MethodPost, "/rest/api/3/search/jql", map[string]any{"jql": "project = PROJ", "expand": []string{"names"}})
		require.Equal(t, http.StatusBadRequest, r.status)
	})
}

// TestSearchPaging walks the tokens: pages are short (maxResults is
// advisory), the token goes on the next request, and the last page has
// isLast and no token.
func TestSearchPaging(t *testing.T) {
	s := newServer(t, jiratest.WithSearchPageCap(4))
	keys := seedTasks(t, s, 10)
	var got []string
	params := url.Values{"jql": {"project = PROJ ORDER BY key ASC"}, "fields": {"key"}, "maxResults": {"100"}}
	pages := 0
	for {
		m := search(t, s, params).obj(t)
		pages++
		got = append(got, keysOf(t, m)...)
		if m["isLast"] == true {
			require.NotContains(t, m, "nextPageToken")
			break
		}
		require.LessOrEqual(t, len(m["issues"].([]any)), 4)
		params.Set("nextPageToken", m["nextPageToken"].(string))
	}
	require.Equal(t, keys, got)
	require.Equal(t, 3, pages)

	t.Run("maxResults smaller than the cap", func(t *testing.T) {
		m := search(t, s, url.Values{"jql": {"project = PROJ"}, "maxResults": {"2"}}).obj(t)
		require.Len(t, m["issues"], 2)
		require.Equal(t, false, m["isLast"])
	})
	t.Run("a page is fixed at the first request", func(t *testing.T) {
		p := url.Values{"jql": {"project = PROJ ORDER BY key ASC"}, "fields": {"summary"}, "maxResults": {"5"}}
		first := search(t, s, p).obj(t)
		s.Edit(keys[7], map[string]any{"summary": "edited"})
		p.Set("nextPageToken", first["nextPageToken"].(string))
		second := search(t, s, p).obj(t)
		require.Equal(t, "task 7", path(second["issues"].([]any)[3], "fields", "summary"))
	})
	t.Run("a bad token", func(t *testing.T) {
		r := search(t, s, url.Values{"jql": {"project = PROJ"}, "nextPageToken": {"nope"}})
		require.Equal(t, http.StatusBadRequest, r.status)
	})
	t.Run("the same reconcileIssues on every page", func(t *testing.T) {
		p := url.Values{"jql": {"project = PROJ"}, "maxResults": {"2"}, "reconcileIssues": {"10001"}}
		first := search(t, s, p).obj(t)
		p.Set("nextPageToken", first["nextPageToken"].(string))
		p.Set("reconcileIssues", "10001,10002")
		require.Equal(t, http.StatusBadRequest, search(t, s, p).status)
	})
}

func TestIndexLag(t *testing.T) {
	s := jiratest.New(t, jiratest.WithIndexLag(2, 0))
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "fresh"})
	id := s.Issue(key).ID
	q := url.Values{"jql": {"project = PROJ"}, "fields": {"summary"}}

	require.Empty(t, search(t, s, q).obj(t)["issues"], "the index has not seen the create")
	require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/issue/"+key).status, "GET reads the database")

	r := q
	r.Set("reconcileIssues", id)
	require.Equal(t, []string{key}, keysOf(t, search(t, s, r).obj(t)), "reconcileIssues reads it fresh")
	q.Del("reconcileIssues")
	require.Equal(t, []string{key}, keysOf(t, search(t, s, q).obj(t)), "two searches later the index has it")

	s.Edit(key, map[string]any{"summary": "edited"})
	m := search(t, s, q).obj(t)
	require.Equal(t, "fresh", path(m["issues"].([]any)[0], "fields", "summary"), "a stale copy")

	s.Delete(key)
	require.Len(t, search(t, s, q).obj(t)["issues"], 1, "a deleted issue lingers until indexed")
	search(t, s, q)
	require.Empty(t, search(t, s, q).obj(t)["issues"], "then vanishes, with no tombstone")

	t.Run("a lag in time", func(t *testing.T) {
		s := jiratest.New(t, jiratest.WithIndexLag(0, 30*time.Second))
		s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "x"})
		require.Empty(t, search(t, s, q).obj(t)["issues"])
		s.Advance(30 * time.Second)
		require.Len(t, search(t, s, q).obj(t)["issues"], 1)
	})
	t.Run("stale reads", func(t *testing.T) {
		s := jiratest.New(t, jiratest.WithIndexLag(1, 0), jiratest.WithStaleReads())
		key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "x"})
		require.Equal(t, http.StatusNotFound, get(t, s, "/rest/api/3/issue/"+key).status)
		search(t, s, q)
		search(t, s, q)
		require.Equal(t, http.StatusOK, get(t, s, "/rest/api/3/issue/"+key).status)
	})
}

func TestJQLErrors(t *testing.T) {
	s := newServer(t)
	seedTasks(t, s, 1)
	cases := map[string]string{
		"":                                "Unbounded JQL queries are not allowed here",
		"ORDER BY key DESC":               "Unbounded JQL queries are not allowed here",
		"project = NOPE":                  "The value 'NOPE' does not exist for the field 'project'.",
		"project = PROJ AND":              "Error in the JQL Query",
		"project = PROJ foo":              "Expecting either 'OR' or 'AND' but got 'foo'",
		`project = "PROJ`:                 "has not been completed",
		"summary ~ foo":                   "does not implement the JQL field 'summary'",
		"nosuchfield = 1":                 "Field 'nosuchfield' does not exist or you do not have permission to view it.",
		"project ~ PROJ":                  "The operator '~' is not supported by the 'project' field.",
		`updated >= "yesterday"`:          "Date value 'yesterday' for field 'updated' is invalid.",
		"key = PROJ-99":                   "An issue with key 'PROJ-99' does not exist for field 'key'.",
		"project = PROJ ORDER BY summary": "Not able to sort using field 'summary'.",
		"assignee = membersOf(x)":         "JQL function",
		"project = PROJ ORDER BY a,b,c,d,e,f,g,h": "maximum of 7",
	}
	for jql, want := range cases {
		t.Run(jql, func(t *testing.T) {
			r := search(t, s, url.Values{"jql": {jql}})
			require.Equal(t, http.StatusBadRequest, r.status, string(r.body))
			msgs, _ := errorBody(t, r)
			require.Contains(t, msgs[0], want)
		})
	}
}

func TestJQLClauses(t *testing.T) {
	s := newServer(t)
	epic := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "epic"})
	a := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "a", Parent: epic, Labels: []string{"x"}})
	b := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b", Status: "In Progress", Assignee: jiratest.MiaID})
	c := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Bug", Summary: "c", Priority: "High", Status: "Won't Do"})
	ids := map[string]string{}
	for _, k := range []string{epic, a, b, c} {
		ids[k] = s.Issue(k).ID
	}

	cases := []struct {
		jql  string
		want []string
	}{
		{`project = "PROJ" ORDER BY key ASC`, []string{epic, a, b, c}},
		{`project = 10000 ORDER BY key DESC`, []string{c, b, a, epic}},
		{`project in (PROJ) AND key in (` + a + `, ` + c + `) ORDER BY key`, []string{a, c}},
		{`id in (` + ids[b] + `,` + ids[c] + `) ORDER BY id ASC`, []string{b, c}},
		{`issuekey = ` + b, []string{b}},
		{`project = PROJ AND parent = ` + epic, []string{a}},
		{`project = PROJ AND parent is EMPTY ORDER BY key`, []string{epic, b, c}},
		{`project = PROJ AND status = "In Progress"`, []string{b}},
		{`project = PROJ AND statusCategory = done`, []string{c}},
		{`project = PROJ AND issuetype in (Bug, Epic) ORDER BY key`, []string{epic, c}},
		{`project = PROJ AND type != Epic AND NOT (labels = x) ORDER BY key`, []string{b, c}},
		{`project = PROJ AND (labels = x OR assignee = currentUser()) ORDER BY key`, []string{a, b}},
		{`project = PROJ AND assignee is not EMPTY`, []string{b}},
	}
	for _, tc := range cases {
		t.Run(tc.jql, func(t *testing.T) {
			r := search(t, s, url.Values{"jql": {tc.jql}, "fields": {"key"}})
			require.Equal(t, http.StatusOK, r.status, string(r.body))
			require.Equal(t, tc.want, keysOf(t, r.obj(t)))
		})
	}
}

// TestJQLTimeZones: literals are minutes in the caller's profile zone
// (Europe/Berlin) while responses render in the site zone (Los Angeles),
// so a client mixing the two up fails (C6).
func TestJQLTimeZones(t *testing.T) {
	// The clock starts at 16:00:00Z: 18:00 in Berlin, 09:00 in Los Angeles.
	s := newServer(t, jiratest.WithAutoAdvance(0))
	s.Advance(30 * time.Second)
	key := s.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "t"})
	f := get(t, s, "/rest/api/3/issue/"+key+"?fields=updated").obj(t)
	require.Equal(t, "2026-07-01T09:00:30.000-0700", path(f, "fields", "updated"))

	match := func(jql string) bool {
		r := search(t, s, url.Values{"jql": {jql}})
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return len(r.obj(t)["issues"].([]any)) == 1
	}
	require.True(t, match(`project = PROJ AND updated >= "2026/07/01 18:00"`), "the same minute, in Berlin")
	require.True(t, match(`project = PROJ AND updated >= "2026-07-01 18:00"`))
	require.False(t, match(`project = PROJ AND updated >= "2026/07/01 18:01"`))
	require.False(t, match(`project = PROJ AND updated <= "2026/07/01 09:00"`), "the site's wall clock is not the user's")
	require.True(t, match(`project = PROJ AND updated >= "2026/07/01"`), "a date is midnight in the user's zone")
	require.False(t, match(`project = PROJ AND updated >= "2026-07-02"`))
	require.True(t, match(`project = PROJ AND created > "-5m"`))
	require.True(t, match(`project = PROJ AND updated >= -1h`))
	require.False(t, match(`project = PROJ AND updated < "-1d"`))
	require.True(t, match(`project = PROJ AND updated >= "-1w 2d"`))

	t.Run("an anonymous caller is read in the site zone", func(t *testing.T) {
		r := search(t, s, url.Values{"jql": {`project = PROJ AND updated >= "2026/07/01 09:00"`}}, noAuth)
		require.Equal(t, http.StatusOK, r.status)
	})
}

func TestSearchOrderAndDefaults(t *testing.T) {
	s := newServer(t)
	keys := seedTasks(t, s, 3)
	s.Edit(keys[0], map[string]any{"summary": "latest"})
	m := search(t, s, url.Values{"jql": {"project = PROJ ORDER BY updated DESC, key ASC"}, "fields": {"key"}}).obj(t)
	require.Equal(t, []string{keys[0], keys[2], keys[1]}, keysOf(t, m))
	m = search(t, s, url.Values{"jql": {"project = PROJ"}, "fields": {"key"}}).obj(t)
	require.Equal(t, []string{keys[2], keys[1], keys[0]}, keysOf(t, m), "created DESC by default")
	m = search(t, s, url.Values{"jql": {"project = PROJ ORDER BY Rank"}, "fields": {"key"}}).obj(t)
	require.Equal(t, keys, keysOf(t, m))
}

func TestSearchPostAndCount(t *testing.T) {
	s := newServer(t)
	seedTasks(t, s, 3)
	r := do(t, s, http.MethodPost, "/rest/api/3/search/jql", map[string]any{
		"jql": "project = PROJ", "fields": []string{"summary"}, "maxResults": 2, "reconcileIssues": []int{10001}})
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	m := r.obj(t)
	require.Len(t, m["issues"], 2)
	token := m["nextPageToken"].(string)
	r = do(t, s, http.MethodPost, "/rest/api/3/search/jql", map[string]any{
		"jql": "project = PROJ", "fields": []string{"summary"}, "maxResults": 2, "reconcileIssues": []int{10001}, "nextPageToken": token})
	require.Len(t, r.obj(t)["issues"], 1)

	c := do(t, s, http.MethodPost, "/rest/api/3/search/approximate-count", map[string]any{"jql": "project = PROJ"})
	require.Equal(t, map[string]any{"count": 3.0}, c.obj(t))
	require.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPost, "/rest/api/3/search/approximate-count", map[string]any{"jql": ""}).status)
}

func TestSearchExpandChangelog(t *testing.T) {
	s := newServer(t)
	key := seedTasks(t, s, 1)[0]
	for i := 0; i < 6; i++ {
		s.Edit(key, map[string]any{"summary": fmt.Sprintf("v%d", i)})
	}
	m := search(t, s, url.Values{"jql": {"key = " + key}, "fields": {"summary"}, "expand": {"changelog"}}).obj(t)
	cl := path(m["issues"].([]any)[0], "changelog").(map[string]any)
	require.EqualValues(t, 6, cl["total"])
	var order []string
	for _, h := range cl["histories"].([]any) {
		order = append(order, path(h, "items").([]any)[0].(map[string]any)["toString"].(string))
	}
	require.ElementsMatch(t, []string{"v0", "v1", "v2", "v3", "v4", "v5"}, order)
	require.NotEqual(t, []string{"v5", "v4", "v3", "v2", "v1", "v0"}, order, "shuffled, not sorted (C8)")
	require.True(t, strings.HasPrefix(order[0], "v"))
}
