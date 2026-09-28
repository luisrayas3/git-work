package jiraapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// exchange is one expected request and the response to it. query and body
// are compared as parsed values; "" expects none.
type exchange struct {
	method, path, query, body string
	status                    int
	header                    map[string]string
	resp                      string
}

type fake struct {
	t      *testing.T
	mu     sync.Mutex
	script []exchange
	sleeps []time.Duration
}

// newFake serves script in order and returns a client whose sleeps are
// recorded instead of slept, with jitter fixed at r.
func newFake(t *testing.T, r float64, script ...exchange) (*Client, *fake) {
	f := &fake{t: t, script: script}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		srv.Close()
		if len(f.script) > 0 {
			t.Errorf("%d expected requests not made, next %s %s", len(f.script), f.script[0].method, f.script[0].path)
		}
	})
	c := New(Config{
		BaseURL: srv.URL + "/", Email: "fred@example.com", Token: "freds_api_token",
		HTTPClient: srv.Client(),
		Sleep:      func(_ context.Context, d time.Duration) error { f.sleeps = append(f.sleeps, d); return nil },
		Rand:       func() float64 { return r },
	})
	return c, f
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.t
	if len(f.script) == 0 {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(500)
		return
	}
	x := f.script[0]
	f.script = f.script[1:]
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("fred@example.com:freds_api_token"))
	if got := r.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if r.Method != x.method || r.URL.Path != x.path {
		t.Errorf("request %s %s, want %s %s", r.Method, r.URL.Path, x.method, x.path)
	}
	wq, _ := url.ParseQuery(x.query)
	if gq := r.URL.Query(); !(len(gq) == 0 && len(wq) == 0) && !reflect.DeepEqual(gq, wq) {
		t.Errorf("%s %s: query %v, want %v", x.method, x.path, gq, wq)
	}
	body, _ := io.ReadAll(r.Body)
	switch {
	case x.body == "" && len(body) > 0:
		t.Errorf("%s %s: unexpected body %s", x.method, x.path, body)
	case x.body != "":
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var g, e any
		if err := json.Unmarshal(body, &g); err != nil {
			t.Errorf("%s %s: body %s: %v", x.method, x.path, body, err)
		}
		if err := json.Unmarshal([]byte(x.body), &e); err != nil {
			t.Fatalf("bad expected body %s: %v", x.body, err)
		}
		if !reflect.DeepEqual(g, e) {
			t.Errorf("%s %s: body\n got %s\nwant %s", x.method, x.path, body, x.body)
		}
	}
	for k, v := range x.header {
		w.Header().Set(k, v)
	}
	if x.resp != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	if x.status == 0 {
		x.status = 200
	}
	w.WriteHeader(x.status)
	_, _ = io.WriteString(w, x.resp)
}

var ctx = context.Background()

func TestMyselfAuthAndServerDate(t *testing.T) {
	c, _ := newFake(t, 0, exchange{method: "GET", path: "/rest/api/3/myself",
		header: map[string]string{"Date": "Mon, 28 Sep 2026 10:03:11 GMT"},
		resp: `{"self":"https://your-domain.atlassian.net/rest/api/3/user?accountId=5b10a2844c20165700ede21g",
			"accountId":"5b10a2844c20165700ede21g","accountType":"atlassian","emailAddress":"mia@example.com",
			"avatarUrls":{"16x16":"…"},"displayName":"Mia Krystof","active":true,"timeZone":"Australia/Sydney",
			"locale":"en_US","groups":{"size":3,"items":[]},"applicationRoles":{"size":1,"items":[]}}`})
	if !c.ServerDate().IsZero() {
		t.Fatal("ServerDate before any response")
	}
	u, err := c.Myself(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if u.AccountID != "5b10a2844c20165700ede21g" || u.DisplayName != "Mia Krystof" || u.EmailAddress != "mia@example.com" || !u.Active {
		t.Errorf("user %+v", u)
	}
	if loc, err := u.Location(); err != nil || loc.String() != "Australia/Sydney" {
		t.Errorf("Location = %v, %v", loc, err)
	}
	if want := time.Date(2026, 9, 28, 10, 3, 11, 0, time.UTC); !c.ServerDate().Equal(want) {
		t.Errorf("ServerDate = %v, want %v", c.ServerDate(), want)
	}
}

func TestErrorDecoding(t *testing.T) {
	c, _ := newFake(t, 0,
		exchange{method: "GET", path: "/rest/api/3/issue/PROJ-9", status: 404,
			resp: `{"errorMessages":["Issue does not exist or you do not have permission to see it."],"errors":{}}`},
		exchange{method: "POST", path: "/rest/api/3/issue", status: 400,
			body: `{"fields":{"project":{"key":"PROJ"}}}`,
			resp: `{"errorMessages":[],"errors":{"summary":"You must specify a summary of the issue.","issuetype":"Specify an issue type"}}`},
		exchange{method: "GET", path: "/rest/api/3/serverInfo", status: 401, resp: `not json`},
	)
	_, err := c.GetIssue(ctx, "PROJ-9", nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.StatusCode != 404 || StatusCode(err) != 404 ||
		!reflect.DeepEqual(e.Messages, []string{"Issue does not exist or you do not have permission to see it."}) {
		t.Fatalf("err = %#v", err)
	}
	_, err = c.CreateIssue(ctx, map[string]any{"project": map[string]string{"key": "PROJ"}}, nil)
	if !errors.As(err, &e) || e.Fields["summary"] != "You must specify a summary of the issue." {
		t.Fatalf("err = %#v", err)
	}
	if got := err.Error(); got != "jira: POST /rest/api/3/issue: 400 Bad Request: issuetype: Specify an issue type: summary: You must specify a summary of the issue." {
		t.Errorf("Error() = %q", got)
	}
	_, err = c.ServerInfo(ctx)
	if !errors.As(err, &e) || e.StatusCode != 401 || string(e.Body) != "not json" || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
	if StatusCode(errors.New("x")) != 0 {
		t.Error("StatusCode of a plain error")
	}
}

func TestRetry429HonoursRetryAfter(t *testing.T) {
	create := `{"fields":{"summary":"s"}}`
	limited := exchange{method: "POST", path: "/rest/api/3/issue", body: create, status: 429,
		header: map[string]string{"Retry-After": "1", "X-RateLimit-Limit": "350", "X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset": "2026-01-01T01:01:01Z", "RateLimit-Reason": "jira-burst-based"},
		resp: `{"errorMessages":["Rate limit exceeded."]}`}
	limited3 := limited
	limited3.header = map[string]string{"Retry-After": "3"}
	c, f := newFake(t, 0.5, limited, limited3, exchange{method: "POST", path: "/rest/api/3/issue", body: create, status: 201,
		resp: `{"id":"10000","key":"ED-24","self":"https://your-domain.atlassian.net/rest/api/3/issue/10000"}`})
	ref, err := c.CreateIssue(ctx, map[string]any{"summary": "s"}, nil)
	if err != nil || ref.ID != "10000" || ref.Key != "ED-24" {
		t.Fatalf("CreateIssue = %+v, %v", ref, err)
	}
	// Retry-After is the minimum, jittered up by 30% × 0.5.
	if want := []time.Duration{1150 * time.Millisecond, 3450 * time.Millisecond}; !reflect.DeepEqual(f.sleeps, want) {
		t.Errorf("sleeps %v, want %v", f.sleeps, want)
	}
}

func TestRetryBounds(t *testing.T) {
	busy := exchange{method: "GET", path: "/rest/api/3/field", status: 503}
	c, f := newFake(t, 0.5, busy, busy, busy, busy, busy)
	if _, err := c.Fields(ctx); StatusCode(err) != 503 {
		t.Fatalf("err = %v", err)
	}
	// No Retry-After: 2s doubling to 30s, jitter ×(0.7+0.6×0.5) = ×1; 4 retries.
	if want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}; !reflect.DeepEqual(f.sleeps, want) {
		t.Errorf("sleeps %v, want %v", f.sleeps, want)
	}

	// A Retry-After beyond MaxRetryWait is handed back, not slept through.
	c, f = newFake(t, 0, exchange{method: "GET", path: "/rest/api/3/field", status: 429, header: map[string]string{"Retry-After": "1847"}})
	_, err := c.Fields(ctx)
	var e *Error
	if !errors.As(err, &e) || e.RetryAfter != 1847*time.Second || len(f.sleeps) != 0 {
		t.Fatalf("err = %v, sleeps %v", err, f.sleeps)
	}
}

func TestPostNotRetriedOn503(t *testing.T) {
	c, f := newFake(t, 0,
		exchange{method: "POST", path: "/rest/api/3/issue/PROJ-1/comment", body: `{"body":{"type":"doc","version":1,"content":[]}}`,
			status: 503, header: map[string]string{"Retry-After": "1"}},
		exchange{method: "POST", path: "/rest/api/3/issue/PROJ-1/transitions", body: `{"transition":{"id":"31"}}`, status: 503})
	if _, err := c.AddComment(ctx, "PROJ-1", TextToADF("")); StatusCode(err) != 503 {
		t.Fatalf("AddComment err = %v", err)
	}
	if err := c.DoTransition(ctx, "PROJ-1", "31", nil); StatusCode(err) != 503 {
		t.Fatalf("DoTransition err = %v", err)
	}
	if len(f.sleeps) != 0 {
		t.Errorf("slept %v", f.sleeps)
	}
}

func TestSearchJQLPagination(t *testing.T) {
	const first = `{"jql":"project = PROJ AND updated >= \"2026/09/27 10:00\" ORDER BY updated ASC, key ASC",
		"fields":["summary","updated"],"expand":"names,schema","maxResults":100,"reconcileIssues":[10042,10043]}`
	next := func(tok string) string { return first[:len(first)-1] + `,"nextPageToken":"` + tok + `"}` }
	page := func(id, tail string) string {
		return `{"issues":[{"expand":"operations","id":"` + id + `","self":"https://x/rest/api/3/issue/` + id + `","key":"PROJ-` + id +
			`","fields":{"summary":"s","updated":"2026-09-27T10:03:11.123-0700"}}]` + tail + `}`
	}
	for _, c := range []struct {
		name, last string
	}{
		{"isLast true", `,"nextPageToken":"ignored","isLast":true`},
		{"token absent", ``},
		{"token null", `,"nextPageToken":null,"isLast":false`},
		{"token empty", `,"nextPageToken":"","isLast":false`},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl, _ := newFake(t, 0,
				exchange{method: "POST", path: "/rest/api/3/search/jql", body: first,
					resp: page("1", `,"nextPageToken":"CAEaAggD","isLast":false,"names":{"summary":"Summary"},"schema":{"summary":{"type":"string","system":"summary"}}`)},
				exchange{method: "POST", path: "/rest/api/3/search/jql", body: next("CAEaAggD"), resp: `{"issues":[],"nextPageToken":"T2","isLast":false}`},
				exchange{method: "POST", path: "/rest/api/3/search/jql", body: next("T2"), resp: page("3", c.last)})
			var ids []string
			pages := 0
			err := cl.SearchJQL(ctx, Search{
				JQL:    `project = PROJ AND updated >= ` + JQLTime(time.Date(2026, 9, 27, 10, 0, 59, 0, time.UTC), nil) + ` ORDER BY updated ASC, key ASC`,
				Fields: []string{"summary", "updated"}, Expand: []string{"names", "schema"},
				MaxResults: 100, ReconcileIssues: []int64{10042, 10043},
			}, func(p SearchPage) error {
				for _, is := range p.Issues {
					ids = append(ids, is.ID)
				}
				if pages++; pages == 1 && (p.Names["summary"] != "Summary" || p.Schema["summary"].System != "summary") {
					t.Errorf("names/schema %v %v", p.Names, p.Schema)
				}
				return nil
			})
			if err != nil || !reflect.DeepEqual(ids, []string{"1", "3"}) {
				t.Fatalf("ids %v, err %v", ids, err)
			}
		})
	}
}

func TestSearchJQLStops(t *testing.T) {
	stop := errors.New("stop")
	c, _ := newFake(t, 0, exchange{method: "POST", path: "/rest/api/3/search/jql", body: `{"jql":"project = X","fields":["id"]}`,
		resp: `{"issues":[],"nextPageToken":"A","isLast":false}`})
	if err := c.SearchJQL(ctx, Search{JQL: "project = X", Fields: []string{"id"}}, func(SearchPage) error { return stop }); err != stop {
		t.Fatalf("err = %v", err)
	}
	c, _ = newFake(t, 0,
		exchange{method: "POST", path: "/rest/api/3/search/jql", body: `{"jql":"project = X"}`, resp: `{"issues":[],"nextPageToken":"A"}`},
		exchange{method: "POST", path: "/rest/api/3/search/jql", body: `{"jql":"project = X","nextPageToken":"A"}`, resp: `{"issues":[],"nextPageToken":"A"}`})
	if err := c.SearchJQL(ctx, Search{JQL: "project = X"}, func(SearchPage) error { return nil }); err == nil {
		t.Fatal("a repeated token must not loop")
	}
}

// The realistic shape of api.md §3.1, not the spec's bogus example.
const issue12 = `{
  "expand": "renderedFields,names,schema,operations,editmeta,changelog,versionedRepresentations",
  "id": "10042", "key": "PROJ-12", "self": "https://your-domain.atlassian.net/rest/api/3/issue/10042",
  "fields": {
    "summary": "Checkout fails for guest users",
    "description": {"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Steps…"}]}]},
    "issuetype": {"self": "…/issuetype/10001", "id": "10001", "description": "A small piece of work.", "name": "Task", "subtask": false,
      "avatarId": 10318, "hierarchyLevel": 0, "entityId": "9d7dd6f7-e8b6-4247-954b-7b2c9b2a5ba2", "scope": {"type": "PROJECT", "project": {"id": "10000"}}},
    "project": {"id": "10000", "key": "PROJ", "name": "Project", "projectTypeKey": "software", "simplified": false},
    "status": {"name": "In Progress", "id": "3", "statusCategory": {"id": 4, "key": "indeterminate", "colorName": "yellow", "name": "In Progress"}},
    "priority": {"name": "Medium", "id": "3"},
    "assignee": {"accountId": "5b10a2844c20165700ede21g", "displayName": "Mia Krystof", "active": true, "timeZone": "Australia/Sydney", "accountType": "atlassian"},
    "reporter": {"accountId": "r1", "displayName": "R", "active": true, "accountType": "atlassian", "emailAddress": null},
    "labels": ["checkout", "guest"],
    "duedate": "2026-10-15",
    "created": "2026-09-01T09:12:44.513+0200",
    "updated": "2026-09-27T10:03:11.123+0200",
    "resolution": null, "resolutiondate": null,
    "parent": {"id": "10097", "key": "PROJ-3", "self": "…", "fields": {"summary": "Guest checkout epic",
      "issuetype": {"id": "10000", "name": "Epic", "subtask": false, "hierarchyLevel": 1}}},
    "subtasks": [],
    "issuelinks": [{"id": "10001", "self": "…/issueLink/10001",
      "type": {"id": "10000", "name": "Blocks", "inward": "is blocked by", "outward": "blocks"},
      "outwardIssue": {"id": "10060", "key": "PROJ-20", "self": "…", "fields": {"summary": "…"}}}],
    "comment": {"comments": [], "self": "…/issue/10042/comment", "maxResults": 0, "total": 0, "startAt": 0},
    "customfield_10020": [{"id": 37, "name": "PROJ Sprint 4", "state": "active", "boardId": 5, "startDate": "2026-09-22T08:00:00.000Z"}],
    "customfield_10016": 3.0
  },
  "changelog": {"startAt": 0, "maxResults": 1, "total": 1, "histories": [{"id": "10001",
    "author": {"accountId": "5b10a2844c20165700ede21g", "displayName": "Mia Krystof", "active": true},
    "created": "2026-09-27T10:03:11.123+0000",
    "items": [{"field": "IssueParentAssociation", "fieldtype": "jira", "from": null, "fromString": null, "to": "10097", "toString": "PROJ-3"},
      {"field": "Sprint", "fieldtype": "custom", "fieldId": "customfield_10020", "from": "76, 79", "fromString": "Sprint 12, Sprint 13", "to": "76, 80", "toString": "Sprint 12, Sprint 14"}]}]}
}`

func TestGetIssue(t *testing.T) {
	c, _ := newFake(t, 0, exchange{method: "GET", path: "/rest/api/3/issue/PROJ-12",
		query: "fields=summary,status,parent&expand=changelog,names", resp: issue12})
	is, err := c.GetIssue(ctx, "PROJ-12", []string{"summary", "status", "parent"}, []string{"changelog", "names"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := is.System()
	if err != nil {
		t.Fatal(err)
	}
	if is.ID != "10042" || s.Summary != "Checkout fails for guest users" || s.IssueType.ID != "10001" ||
		s.Status.StatusCategory.Key != "indeterminate" || s.Priority.Name != "Medium" || s.Resolution != nil ||
		s.Assignee.AccountID != "5b10a2844c20165700ede21g" || s.Reporter.EmailAddress != "" ||
		!reflect.DeepEqual(s.Labels, []string{"checkout", "guest"}) || s.DueDate.String() != "2026-10-15" ||
		s.Parent.Key != "PROJ-3" || s.Parent.Fields.IssueType.HierarchyLevel != 1 || s.Project.Key != "PROJ" ||
		len(s.IssueLinks) != 1 || s.IssueLinks[0].OutwardIssue.Key != "PROJ-20" || s.IssueLinks[0].InwardIssue != nil ||
		!s.ResolutionDate.IsZero() {
		t.Errorf("system fields %+v", s)
	}
	if _, off := s.Updated.Zone(); off != 2*3600 || !s.Updated.Equal(time.Date(2026, 9, 27, 8, 3, 11, 123e6, time.UTC)) {
		t.Errorf("updated %v", s.Updated)
	}
	if text, ok := ADFToText(s.Description); text != "Steps…" || !ok {
		t.Errorf("description %q %v", text, ok)
	}
	var points float64
	if ok, err := is.Decode("customfield_10016", &points); !ok || err != nil || points != 3 {
		t.Errorf("Decode points = %v %v %v", points, ok, err)
	}
	var none any
	if ok, _ := is.Decode("resolution", &none); ok {
		t.Error("null field decoded")
	}
	h := is.Changelog.Histories[0]
	if h.Items[0].FieldKey() != "issueparentassociation" || h.Items[0].From != "" || h.Items[1].FieldKey() != "customfield_10020" || h.Items[1].To != "76, 80" {
		t.Errorf("changelog %+v", h)
	}
}

// TestMetaPaging: createmeta pages by total, priority/search by isLast; the
// fake's short lists never reach a second page.
func TestMetaPaging(t *testing.T) {
	c, _ := newFake(t, 0,
		exchange{method: "GET", path: "/rest/api/3/issue/createmeta/EX/issuetypes", query: "startAt=0&maxResults=200",
			resp: `{"issueTypes":[{"id":"3","name":"Task","subtask":false,"hierarchyLevel":0}],"startAt":0,"maxResults":1,"total":2}`},
		exchange{method: "GET", path: "/rest/api/3/issue/createmeta/EX/issuetypes", query: "startAt=1&maxResults=200",
			resp: `{"issueTypes":[{"id":"10000","name":"Epic","subtask":false,"hierarchyLevel":1}],"startAt":1,"maxResults":1,"total":2}`},
		exchange{method: "GET", path: "/rest/api/3/priority/search", query: "projectId=10000&startAt=0&maxResults=50",
			resp: `{"isLast":false,"maxResults":1,"startAt":0,"total":2,"values":[{"id":"1","name":"Highest","statusColor":"#d04437"}]}`},
		exchange{method: "GET", path: "/rest/api/3/priority/search", query: "projectId=10000&startAt=1&maxResults=50",
			resp: `{"isLast":true,"maxResults":1,"startAt":1,"total":2,"values":[{"id":"3","name":"Medium","isDefault":true}]}`},
	)
	its, err := c.CreateMetaIssueTypes(ctx, "EX")
	if err != nil || len(its) != 2 || its[1].Name != "Epic" {
		t.Fatalf("CreateMetaIssueTypes = %+v, %v", its, err)
	}
	pr, err := c.Priorities(ctx, "10000")
	if err != nil || len(pr) != 2 || pr[1].Name != "Medium" {
		t.Fatalf("Priorities = %+v, %v", pr, err)
	}
}
