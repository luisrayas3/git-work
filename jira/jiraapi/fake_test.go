package jiraapi_test

// The cross-check of the client against the fake Jira of package jiratest.
// The two were written independently from .jira-work/api.md and its
// vetting; a test failing here means one of them misread the reference.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
)

var ctx = context.Background()

// sleeper records the client's sleeps and moves the fake's clock by them,
// so a Retry-After is honoured without waiting.
type sleeper struct {
	mu     sync.Mutex
	srv    *jiratest.Server
	sleeps []time.Duration
}

func (s *sleeper) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	s.sleeps = append(s.sleeps, d)
	s.mu.Unlock()
	s.srv.Advance(d)
	return nil
}

func client(t *testing.T, srv *jiratest.Server) (*jiraapi.Client, *sleeper) {
	t.Helper()
	email, token := srv.Credentials()
	sl := &sleeper{srv: srv}
	c := jiraapi.New(jiraapi.Config{
		BaseURL: srv.URL(), Email: email, Token: token,
		Sleep: sl.sleep,
		Rand:  func() float64 { return 0 },
	})
	return c, sl
}

// must panics on err: the tests read better with it inline, and a panic
// still fails the test with the line.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantStatus(t *testing.T, err error, status int) *jiraapi.Error {
	t.Helper()
	var e *jiraapi.Error
	if !errors.As(err, &e) || e.StatusCode != status {
		t.Fatalf("got %v, want a %d *Error", err, status)
	}
	return e
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func search(t *testing.T, c *jiraapi.Client, s jiraapi.Search) (keys []string, pages int) {
	t.Helper()
	if s.Fields == nil {
		s.Fields = []string{"summary"} // the server default is the id alone
	}
	noErr(t, c.SearchJQL(ctx, s, func(p jiraapi.SearchPage) error {
		pages++
		for _, is := range p.Issues {
			keys = append(keys, is.Key)
		}
		return nil
	}))
	return keys, pages
}

// transport strips the Authorization header, as a dropped env var would.
type stripAuth struct{}

func (stripAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Del("Authorization")
	return http.DefaultTransport.RoundTrip(r)
}

func TestFakeAuth(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)

	me := must(c.Myself(ctx))
	eq(t, "accountId", me.AccountID, jiratest.MiaID)
	eq(t, "email", me.EmailAddress, jiratest.MiaEmail)
	eq(t, "accountType", me.AccountType, "atlassian")
	loc := must(me.Location())
	eq(t, "zone", loc.String(), "Europe/Berlin")
	eq(t, "server date", c.ServerDate().Equal(srv.Now().Truncate(time.Second)), true)

	si := must(c.ServerInfo(ctx))
	eq(t, "deploymentType", si.DeploymentType, "Cloud")
	eq(t, "serverTimeZone", si.ServerTimeZone, "America/Los_Angeles")
	eq(t, "serverTime", si.ServerTime.Equal(srv.Now()), true)
	if _, off := si.ServerTime.Zone(); off != -7*3600 {
		t.Errorf("serverTime offset %d, want the site's -0700", off)
	}

	bad := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: jiratest.MiaEmail, Token: "wrong"})
	_, err := bad.Myself(ctx)
	if e := wantStatus(t, err, 401); !strings.Contains(e.Error(), "token invalid or expired") {
		t.Errorf("401 error %q does not hint at the token", e)
	}
	// Empty credentials still send a header, so they are rejected, never anonymous.
	_, err = jiraapi.New(jiraapi.Config{BaseURL: srv.URL()}).Myself(ctx)
	wantStatus(t, err, 401)

	// A missing header runs anonymously: /myself refuses, search is an
	// empty 200, which is why Myself comes first (api-vetting.md §4.14).
	srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "hidden"})
	anon := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), HTTPClient: &http.Client{Transport: stripAuth{}}})
	_, err = anon.Myself(ctx)
	wantStatus(t, err, 401)
	srv.Advance(time.Minute)
	keys, pages := search(t, anon, jiraapi.Search{JQL: "project = PROJ", Fields: []string{"summary"}})
	eq(t, "anonymous hits", len(keys), 0)
	eq(t, "anonymous pages", pages, 1)
	_, err = anon.GetIssue(ctx, "PROJ-1", nil, nil)
	wantStatus(t, err, 404)
	_, err = anon.CreateIssue(ctx, map[string]any{"summary": "x"}, nil)
	wantStatus(t, err, 401)

	strict := jiratest.New(t, jiratest.WithMissingAuth(jiratest.RejectMissingAuth))
	anon = jiraapi.New(jiraapi.Config{BaseURL: strict.URL(), HTTPClient: &http.Client{Transport: stripAuth{}}})
	err = anon.SearchJQL(ctx, jiraapi.Search{JQL: "project = PROJ"}, func(jiraapi.SearchPage) error { return nil })
	wantStatus(t, err, 401)
}

func TestFakeSearchPaging(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0))
	c, _ := client(t, srv)
	const n = 100
	var want []string
	for i := range n {
		want = append(want, srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: fmt.Sprint("task ", i)}))
	}
	// Ask for more than the fake's page cap of 37: pages come short.
	var got []string
	var sizes []int
	noErr(t, c.SearchJQL(ctx, jiraapi.Search{JQL: "project = PROJ ORDER BY key ASC", Fields: []string{"summary", "updated"},
		Expand: []string{"names", "schema"}, MaxResults: 100}, func(p jiraapi.SearchPage) error {
		sizes = append(sizes, len(p.Issues))
		for _, is := range p.Issues {
			got = append(got, is.Key)
			if _, ok := is.Fields["summary"]; !ok {
				t.Fatalf("%s: no summary in %v", is.Key, is.Fields)
			}
		}
		if p.Names["summary"] != "Summary" || p.Schema["updated"].Type != "datetime" {
			t.Errorf("names/schema not decoded: %v %v", p.Names, p.Schema)
		}
		return nil
	}))
	slices.SortFunc(want, func(a, b string) int { return keyNum(a) - keyNum(b) })
	if !slices.Equal(got, want) {
		t.Fatalf("keys %v, want %v", got, want)
	}
	if !slices.Equal(sizes, []int{37, 37, 26}) {
		t.Errorf("page sizes %v", sizes)
	}

	// An error from fn stops the search and comes back.
	stop := errors.New("stop")
	pages := 0
	err := c.SearchJQL(ctx, jiraapi.Search{JQL: "project = PROJ"}, func(jiraapi.SearchPage) error { pages++; return stop })
	if !errors.Is(err, stop) || pages != 1 {
		t.Errorf("got %v after %d pages", err, pages)
	}
	// An unbounded query is the fake's 400.
	err = c.SearchJQL(ctx, jiraapi.Search{JQL: "ORDER BY key"}, func(jiraapi.SearchPage) error { return nil })
	wantStatus(t, err, 400)
}

func keyNum(k string) int {
	n, _ := strconv.Atoi(k[strings.IndexByte(k, '-')+1:])
	return n
}

// TestFakeIndexLag: our own write is invisible to the next search unless
// its id is reconciled, and the reconcile list goes with every page.
func TestFakeIndexLag(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(1, time.Hour))
	c, _ := client(t, srv)
	for i := range 40 {
		srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: fmt.Sprint("old ", i)})
	}
	srv.Advance(2 * time.Hour)
	search(t, c, jiraapi.Search{JQL: "project = PROJ"}) // let the index catch up
	ref := must(c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "PROJ"}, "issuetype": map[string]string{"id": "10002"}, "summary": "fresh",
	}, nil))
	keys, _ := search(t, c, jiraapi.Search{JQL: "project = PROJ", Fields: []string{"summary"}})
	if slices.Contains(keys, ref.Key) || len(keys) != 40 {
		t.Fatalf("lagging index shows %d issues, %v", len(keys), slices.Contains(keys, ref.Key))
	}
	id := must(strconv.ParseInt(ref.ID, 10, 64))
	keys, pages := search(t, c, jiraapi.Search{JQL: "project = PROJ ORDER BY created DESC", Fields: []string{"summary"},
		ReconcileIssues: []int64{id}})
	if len(keys) != 41 || keys[0] != ref.Key || pages != 2 {
		t.Fatalf("reconciled search: %d keys over %d pages, first %v", len(keys), pages, keys[0])
	}
	// The fake refuses a reconcile list that changes between pages (§4.8), so
	// the second page above proves the client resent the same list.
	for _, r := range srv.Requests() {
		if r.Op == "searchAndReconsileIssuesUsingJqlPost" && r.Status != 200 {
			t.Errorf("search answered %d: %s", r.Status, r.Body)
		}
	}
}

func TestFakeGetIssue(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	epic := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "Checkout"})
	story := srv.CreateIssue(jiratest.IssueSpec{
		Project: "PROJ", Type: "Story", Summary: "Guest checkout", Description: "As a guest\nI pay",
		Status: "Ready for QA", Priority: "High", Assignee: jiratest.RaviID, Labels: []string{"web", "pay"},
		DueDate: "2026-07-15", Parent: epic,
		Fields: map[string]any{jiratest.FieldStoryPoints: 3, jiratest.FieldSprint: 37, jiratest.FieldStartDate: "2026-07-02"},
	})
	other := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Payments API"})
	srv.Link(story, "Blocks", other)
	srv.AddComment(story, "first")
	want := srv.Issue(story)

	is := must(c.GetIssue(ctx, story, nil, []string{"changelog", "names"}))
	eq(t, "id", is.ID, want.ID)
	eq(t, "key", is.Key, story)
	sys := must(is.System())
	eq(t, "summary", sys.Summary, "Guest checkout")
	eq(t, "issuetype", sys.IssueType.Name, "Story")
	eq(t, "hierarchyLevel", sys.IssueType.HierarchyLevel, 0)
	eq(t, "project", sys.Project.Key, "PROJ")
	eq(t, "status", sys.Status.Name, "Ready for QA")
	eq(t, "status category", sys.Status.StatusCategory.Key, "indeterminate")
	eq(t, "priority", sys.Priority.Name, "High")
	eq(t, "assignee", sys.Assignee.AccountID, jiratest.RaviID)
	eq(t, "hidden email", sys.Assignee.EmailAddress, "")
	eq(t, "assignee zone", sys.Assignee.TimeZone, "Asia/Kolkata")
	eq(t, "reporter", sys.Reporter.AccountID, jiratest.RaviID)
	eq(t, "creator", sys.Creator.AccountID, jiratest.RaviID)
	eq(t, "labels", strings.Join(sys.Labels, ","), strings.Join(want.Labels, ","))
	eq(t, "duedate", sys.DueDate.String(), "2026-07-15")
	eq(t, "created", sys.Created.Equal(want.Created), true)
	eq(t, "updated", sys.Updated.Equal(want.Updated), true)
	if _, off := sys.Created.Zone(); off != -7*3600 {
		t.Errorf("created offset %d, want the site zone's, not Mia's", off)
	}
	eq(t, "resolution", sys.Resolution == nil, true)
	eq(t, "resolutiondate", sys.ResolutionDate.IsZero(), true)
	eq(t, "parent", sys.Parent.Key, epic)
	eq(t, "parent type level", sys.Parent.Fields.IssueType.HierarchyLevel, 1)
	eq(t, "subtasks", len(sys.Subtasks), 0)
	if len(sys.IssueLinks) != 1 || sys.IssueLinks[0].OutwardIssue == nil || sys.IssueLinks[0].InwardIssue != nil ||
		sys.IssueLinks[0].OutwardIssue.Key != other || sys.IssueLinks[0].Type.Outward != "blocks" {
		t.Errorf("issuelinks from the source: %+v", sys.IssueLinks)
	}
	text, lossless := jiraapi.ADFToText(sys.Description)
	eq(t, "description", text, "As a guest\n\nI pay")
	eq(t, "description lossless", lossless, true)

	var comments struct {
		Comments []jiraapi.Comment `json:"comments"`
		Total    int               `json:"total"`
	}
	must(is.Decode("comment", &comments))
	if comments.Total != 1 || len(comments.Comments) != 1 {
		t.Fatalf("comment page %+v", comments)
	}
	if text, _ := jiraapi.ADFToText(comments.Comments[0].Body); text != "first" {
		t.Errorf("comment text %q", text)
	}
	eq(t, "comment author", comments.Comments[0].Author.AccountID, jiratest.RaviID)

	var points float64
	must(is.Decode(jiratest.FieldStoryPoints, &points))
	eq(t, "story points", points, 3.0)
	var start jiraapi.Date
	must(is.Decode(jiratest.FieldStartDate, &start))
	eq(t, "start date", start.String(), "2026-07-02")
	var rank string
	must(is.Decode(jiratest.FieldRank, &rank))
	if !strings.HasPrefix(rank, "0|") {
		t.Errorf("rank %q", rank)
	}
	var sprints []struct {
		ID        int          `json:"id"`
		Name      string       `json:"name"`
		State     string       `json:"state"`
		StartDate jiraapi.Time `json:"startDate"`
		EndDate   jiraapi.Time `json:"endDate"`
	}
	must(is.Decode(jiratest.FieldSprint, &sprints))
	if len(sprints) != 1 || sprints[0].ID != 37 || sprints[0].State != "active" ||
		!sprints[0].StartDate.Equal(time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("sprint %+v", sprints)
	}
	var none any
	if ok, err := is.Decode("no_such_field", &none); ok || err != nil {
		t.Errorf("absent field: %v %v", ok, err)
	}
	eq(t, "names", is.Names[jiratest.FieldStoryPoints], "Story point estimate")

	// The changelog: status moved on create, then the link; sort it ourselves (C8).
	if is.Changelog == nil || is.Changelog.Total != len(want.Changelog) || len(is.Changelog.Histories) != is.Changelog.Total {
		t.Fatalf("changelog %+v, want %d", is.Changelog, len(want.Changelog))
	}
	var statusItem *jiraapi.ChangeItem
	for _, h := range is.Changelog.Histories {
		for i, it := range h.Items {
			if it.FieldKey() == "status" {
				statusItem = &h.Items[i]
			}
		}
	}
	if statusItem == nil || statusItem.ToString != "Ready for QA" {
		t.Errorf("no status item to Ready for QA in %+v", is.Changelog.Histories)
	}

	// A field list restricts the fields.
	is = must(c.GetIssue(ctx, story, []string{"summary", jiratest.FieldSprint}, nil))
	if len(is.Fields) != 2 || is.Changelog != nil {
		t.Errorf("fields %v", slices.Collect(maps.Keys(is.Fields)))
	}
	// From the destination, the link is inward.
	sys = must(must(c.GetIssue(ctx, other, []string{"issuelinks", "parent"}, nil)).System())
	if len(sys.IssueLinks) != 1 || sys.IssueLinks[0].InwardIssue == nil || sys.IssueLinks[0].InwardIssue.Key != story {
		t.Errorf("issuelinks from the destination: %+v", sys.IssueLinks)
	}
	eq(t, "no parent", sys.Parent == nil, true)

	// A moved issue answers its old key with the new one.
	srv.Seed(jiratest.Project{ID: "10020", Key: "NEW", Name: "New", IssueTypes: jiratest.CompanySite().Projects[0].IssueTypes,
		Statuses: jiratest.CompanySite().Projects[0].Statuses})
	moved := srv.Move(other, "NEW")
	is = must(c.GetIssue(ctx, other, []string{"project"}, nil))
	eq(t, "moved key", is.Key, moved)

	_, err := c.GetIssue(ctx, "PROJ-999", nil, nil)
	wantStatus(t, err, 404)
}

func TestFakeCreateEditDelete(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	desc := "Pay as a **guest**\n\n- card\n- wallet"
	ref := must(c.CreateIssue(ctx, map[string]any{
		"project":                 map[string]string{"key": "PROJ"},
		"issuetype":               map[string]string{"id": "10001"},
		"summary":                 "Guest checkout",
		"description":             jiraapi.TextToADF(desc),
		"assignee":                map[string]string{"accountId": jiratest.MiaID},
		"labels":                  []string{"web"},
		"duedate":                 jiraapi.Date{Time: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		jiratest.FieldStoryPoints: 5,
	}, []jiraapi.Property{{Key: "gitwork", Value: map[string]any{"id": "abc", "rev": 1}}}))
	if ref.ID == "" || ref.Key != "PROJ-1" {
		t.Fatalf("created %+v", ref)
	}
	got := srv.Issue(ref.Key)
	eq(t, "assignee", got.Assignee, jiratest.MiaID)
	eq(t, "reporter", got.Reporter, jiratest.MiaID)
	eq(t, "duedate", got.DueDate, "2026-08-01")
	eq(t, "points", fmt.Sprint(got.Custom[jiratest.FieldStoryPoints]), "5")
	eq(t, "property", string(got.Properties["gitwork"]), `{"id":"abc","rev":1}`)
	if text, ok := jiraapi.ADFToText(got.Description); text != desc || !ok {
		t.Errorf("stored description reads %q (lossless %v)", text, ok)
	}

	// Sprint is in the field context but not on the create screen.
	_, err := c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "PROJ"}, "issuetype": map[string]string{"id": "10001"},
		"summary": "x", jiratest.FieldSprint: 37,
	}, nil)
	if e := wantStatus(t, err, 400); e.Fields[jiratest.FieldSprint] == "" {
		t.Errorf("no field error: %v", e)
	}
	// Bug requires a priority.
	_, err = c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "PROJ"}, "issuetype": map[string]string{"id": "10004"}, "summary": "x",
	}, nil)
	if e := wantStatus(t, err, 400); e.Fields["priority"] == "" {
		t.Errorf("no priority error: %v", e)
	}

	noErr(t, c.EditIssue(ctx, ref.Key,
		map[string]any{"summary": "Guest checkout v2", jiratest.FieldSprint: 37, "duedate": nil},
		map[string][]jiraapi.Op{"labels": {jiraapi.OpAdd("pay"), jiraapi.OpRemove("web")}}))
	got = srv.Issue(ref.Key)
	eq(t, "summary", got.Summary, "Guest checkout v2")
	eq(t, "labels", strings.Join(got.Labels, ","), "pay")
	eq(t, "duedate cleared", got.DueDate, "")
	eq(t, "sprint", fmt.Sprint(got.Custom[jiratest.FieldSprint]), "[37]")

	noErr(t, c.EditIssue(ctx, ref.Key, nil, map[string][]jiraapi.Op{"summary": {jiraapi.OpSet("v3")}}))
	eq(t, "summary by set op", srv.Issue(ref.Key).Summary, "v3")

	err = c.EditIssue(ctx, ref.Key, map[string]any{"labels": []string{"x"}},
		map[string][]jiraapi.Op{"labels": {jiraapi.OpAdd("y")}})
	if e := wantStatus(t, err, 400); e.Fields["labels"] == "" {
		t.Errorf("fields and update on one field: %v", e)
	}
	err = c.EditIssue(ctx, ref.Key, map[string]any{"status": map[string]string{"id": "10002"}}, nil)
	wantStatus(t, err, 400)
	err = c.EditIssue(ctx, ref.Key, map[string]any{"description": "plain"}, nil)
	if e := wantStatus(t, err, 400); !strings.Contains(e.Fields["description"], "Atlassian Document") {
		t.Errorf("string description: %v", e)
	}

	// Deleting a parent with sub-tasks needs deleteSubtasks (§5.4).
	sub := must(c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "PROJ"}, "issuetype": map[string]string{"id": "10003"},
		"summary": "sub", "parent": map[string]string{"key": ref.Key},
	}, nil))
	wantStatus(t, c.DeleteIssue(ctx, ref.Key, false), 400)
	noErr(t, c.DeleteIssue(ctx, ref.Key, true))
	eq(t, "parent gone", srv.Exists(ref.Key), false)
	eq(t, "sub-task gone", srv.Exists(sub.Key), false)
	_, err = c.GetIssue(ctx, ref.Key, nil, nil)
	wantStatus(t, err, 404)
}

func TestFakeTransitions(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "s"})

	ts := must(c.Transitions(ctx, key))
	ids := map[string]jiraapi.Transition{}
	for _, tr := range ts {
		ids[tr.ID] = tr
	}
	if _, ok := ids["11"]; !ok || len(ts) != 2 || !ids["51"].IsGlobal || ids["51"].To.StatusCategory.Key != "done" {
		t.Fatalf("transitions from To Do: %+v", ts)
	}
	e := wantStatus(t, c.DoTransition(ctx, key, "31", nil), 400)
	if !slices.ContainsFunc(e.Messages, func(m string) bool { return strings.Contains(m, "is not valid for this issue") }) {
		t.Errorf("unavailable transition: %v", e)
	}
	noErr(t, c.DoTransition(ctx, key, "11", nil))
	noErr(t, c.DoTransition(ctx, key, "21", nil))
	eq(t, "status", srv.Issue(key).Status, "Ready for QA")

	ts = must(c.Transitions(ctx, key))
	var done *jiraapi.Transition
	for i := range ts {
		if ts[i].ID == "31" {
			done = &ts[i]
		}
	}
	if done == nil || !done.HasScreen || !done.Fields["resolution"].Required {
		t.Fatalf("Done transition: %+v", done)
	}
	e = wantStatus(t, c.DoTransition(ctx, key, "31", nil), 400)
	if e.Fields["resolution"] == "" {
		t.Errorf("missing resolution: %v", e)
	}
	noErr(t, c.DoTransition(ctx, key, "31", map[string]any{"resolution": map[string]string{"id": "10000"}}))
	got := srv.Issue(key)
	eq(t, "status", got.Status, "Done")
	eq(t, "category", got.Category, "done")
	eq(t, "resolution", got.Resolution, "Done")
	sys := must(must(c.GetIssue(ctx, key, []string{"resolution", "resolutiondate", "status"}, nil)).System())
	eq(t, "resolution read", sys.Resolution.Name, "Done")
	eq(t, "resolutiondate read", sys.ResolutionDate.IsZero(), false)

	// A 409 is a concurrent transition: returned, not retried (C9).
	srv.ConflictNextTransitions(1)
	srv.ResetRequests()
	wantStatus(t, c.DoTransition(ctx, key, "41", nil), 409)
	eq(t, "requests after 409", len(srv.Requests()), 1)
	noErr(t, c.DoTransition(ctx, key, "41", nil))
	eq(t, "reopened", srv.Issue(key).Resolution, "")
}

// adfCorpus is text in the model of adf.go, one construct per entry.
var adfCorpus = []string{
	"",
	"plain",
	"two\nlines",
	"para one\n\npara two",
	"# Heading\n\n###### Six",
	"---",
	"**strong** _em_ ~~strike~~ `code`",
	"**bold _and em_** after",
	"[a link](https://example.com/x?y=1) and [`code link`](https://example.com)",
	"@[Ravi Patel](" + jiratest.RaviID + ") :smile: <https://example.com/card>",
	"- one\n- two\n  - nested\n- three",
	"3. third\n4. fourth",
	"> quoted\n>\n> - item",
	"```go\nfunc main() {\n\n\tprintln(1)\n}\n```",
	"```\n```",
	"trailing spaces   \n\n\n\nextra blank lines",
	"a**b** c_d_ e~~f~~",
	"unicode é ✓ 🎉",
}

// TestFakeADFRoundTrip is the property the sync rests on: text written
// through TextToADF reads back, after the fake's normalisation (localIds,
// merged text nodes), as NormalizeText of itself, losslessly; and writing
// that text again produces the same document, so a sync does not ping-pong.
func TestFakeADFRoundTrip(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "adf"})
	for i, text := range adfCorpus {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			norm := jiraapi.NormalizeText(text)
			check := func(what string, body json.RawMessage) {
				t.Helper()
				if strings.Contains(string(body), `"content":null`) {
					t.Errorf("%s: stored content null, which Jira never sends: %s", what, body)
				}
				got, lossless := jiraapi.ADFToText(body)
				if got != norm || !lossless {
					t.Errorf("%s: read back %q (lossless %v), want %q\nstored %s", what, got, lossless, norm, body)
				}
				if again := jiraapi.TextToADF(got); string(again) != string(jiraapi.TextToADF(text)) {
					t.Errorf("%s: re-export differs:\n%s\n%s", what, again, jiraapi.TextToADF(text))
				}
			}
			cm := must(c.AddComment(ctx, key, jiraapi.TextToADF(text)))
			check("add response", cm.Body)
			cs := must(c.Comments(ctx, key))
			check("comments", cs[len(cs)-1].Body)
			up := must(c.UpdateComment(ctx, key, cm.ID, jiraapi.TextToADF(text+"\n\nedited")))
			if got, _ := jiraapi.ADFToText(up.Body); got != jiraapi.NormalizeText(text+"\n\nedited") {
				t.Errorf("updated comment reads %q", got)
			}
			noErr(t, c.DeleteComment(ctx, key, cm.ID))

			noErr(t, c.EditIssue(ctx, key, map[string]any{"description": jiraapi.TextToADF(text)}, nil))
			sys := must(must(c.GetIssue(ctx, key, []string{"description"}, nil)).System())
			check("description", sys.Description)
		})
	}
	eq(t, "comments left", len(must(c.Comments(ctx, key))), 0)
}

func TestFakeComments(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithAutoAdvance(time.Millisecond))
	c, _ := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "chatty"})
	// More than one page of 100, in the legacy shape with no isLast.
	for i := range 105 {
		srv.AddComment(key, fmt.Sprint("comment ", i))
	}
	// The web UI's plain text is a paragraph per line; it reads back stably.
	ui := srv.AddComment(key, "line one\nline two")
	cs := must(c.Comments(ctx, key))
	if len(cs) != 106 {
		t.Fatalf("%d comments", len(cs))
	}
	for i, cm := range cs[:105] {
		if text, _ := jiraapi.ADFToText(cm.Body); text != fmt.Sprint("comment ", i) {
			t.Fatalf("comment %d reads %q", i, text)
		}
		if i > 0 && cm.Created.Before(cs[i-1].Created.Time) {
			t.Fatalf("comments out of order at %d", i)
		}
	}
	last := cs[105]
	eq(t, "ui comment id", last.ID, ui)
	text, lossless := jiraapi.ADFToText(last.Body)
	eq(t, "ui comment", text, "line one\n\nline two")
	eq(t, "ui comment lossless", lossless, true)

	_, err := c.UpdateComment(ctx, key, "999999", jiraapi.TextToADF("x"))
	wantStatus(t, err, 404)
	_, err = c.AddComment(ctx, key, json.RawMessage(`"not adf"`))
	wantStatus(t, err, 400)
	srv.Deny("updateComment")
	_, err = c.UpdateComment(ctx, key, ui, jiraapi.TextToADF("x"))
	wantStatus(t, err, 400) // lacking permission is a 400 here (§7.2)
}

func TestFakeProperties(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0))
	c, _ := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "p"})
	before := srv.Issue(key).Updated

	_, err := c.GetProperty(ctx, key, "gitwork")
	wantStatus(t, err, 404)
	eq(t, "created", must(c.SetProperty(ctx, key, "gitwork", map[string]any{"id": "e1", "rev": 1})), true)
	eq(t, "replaced", must(c.SetProperty(ctx, key, "gitwork", map[string]any{"id": "e1", "rev": 2})), false)
	eq(t, "value", string(must(c.GetProperty(ctx, key, "gitwork"))), `{"id":"e1","rev":2}`)
	eq(t, "updated not bumped", srv.Issue(key).Updated.Equal(before), true)

	noErr(t, c.SearchJQL(ctx, jiraapi.Search{JQL: "key = " + key, Fields: []string{"summary"}, Properties: []string{"gitwork"}},
		func(p jiraapi.SearchPage) error {
			if len(p.Issues) != 1 || string(p.Issues[0].Properties["gitwork"]) != `{"id":"e1","rev":2}` {
				t.Errorf("search properties: %+v", p.Issues)
			}
			return nil
		}))

	noErr(t, c.DeleteProperty(ctx, key, "gitwork"))
	_, err = c.GetProperty(ctx, key, "gitwork")
	wantStatus(t, err, 404)
}

func TestFakeMeta(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)

	fs := must(c.Fields(ctx))
	byID := map[string]jiraapi.Field{}
	for _, f := range fs {
		byID[f.ID] = f
	}
	eq(t, "story points key", byID[jiratest.FieldStoryPoints].Schema.Custom, "com.pyxis.greenhopper.jira:jsw-story-points")
	eq(t, "sprint key", byID[jiratest.FieldSprint].Schema.Custom, "com.pyxis.greenhopper.jira:gh-sprint")
	eq(t, "sprint items", byID[jiratest.FieldSprint].Schema.Items, "json")
	eq(t, "rank key", byID[jiratest.FieldRank].Schema.Custom, "com.pyxis.greenhopper.jira:gh-lexo-rank")
	eq(t, "custom", byID[jiratest.FieldSprint].Custom, true)
	eq(t, "summary system", byID["summary"].Schema.System, "summary")
	eq(t, "customId", byID[jiratest.FieldStoryPoints].Schema.CustomID, int64(10016))

	p := must(c.Project(ctx, "PROJ"))
	eq(t, "project id", p.ID, "10000")
	eq(t, "style", p.Style, "classic")
	eq(t, "simplified", p.Simplified, false)
	eq(t, "lead", p.Lead.AccountID, jiratest.RaviID)
	eq(t, "types", len(p.IssueTypes), 5)
	_, err := c.Project(ctx, "NOPE")
	wantStatus(t, err, 404)

	sts := must(c.ProjectStatuses(ctx, "PROJ"))
	for _, it := range sts {
		if it.Name == "Story" {
			i := slices.IndexFunc(it.Statuses, func(s jiraapi.Status) bool { return s.Name == "Ready for QA" })
			if i < 0 || it.Statuses[i].StatusCategory.Key != "indeterminate" {
				t.Errorf("Story statuses %+v", it.Statuses)
			}
		}
		if it.Name == "Sub-task" && !it.Subtask {
			t.Errorf("Sub-task not a sub-task")
		}
	}

	types := must(c.CreateMetaIssueTypes(ctx, "PROJ"))
	if len(types) != 5 {
		t.Fatalf("createmeta types %+v", types)
	}
	for _, ty := range types {
		if ty.Name == "Sub-task" && (ty.HierarchyLevel != -1 || !ty.Subtask) {
			t.Errorf("Sub-task %+v", ty)
		}
	}
	fm := must(c.CreateMetaFields(ctx, "PROJ", "10004"))
	req := map[string]bool{}
	for _, f := range fm {
		if f.FieldID != f.Key {
			t.Errorf("fieldId %q key %q", f.FieldID, f.Key)
		}
		req[f.FieldID] = f.Required
	}
	eq(t, "priority required on Bug", req["priority"], true)
	eq(t, "summary required", req["summary"], true)
	if _, ok := req[jiratest.FieldSprint]; ok {
		t.Errorf("sprint on the create screen")
	}

	prs := must(c.Priorities(ctx, "10000"))
	if len(prs) != 5 || prs[0].Name != "Highest" || prs[2].StatusColor == "" {
		t.Errorf("priorities %+v", prs)
	}
	lts := must(c.IssueLinkTypes(ctx))
	i := slices.IndexFunc(lts, func(l jiraapi.IssueLinkType) bool { return l.Name == "Blocks" })
	if i < 0 || lts[i].Outward != "blocks" || lts[i].Inward != "is blocked by" {
		t.Errorf("link types %+v", lts)
	}

	team := jiratest.New(t, jiratest.WithSite(jiratest.TeamSite()))
	tc, _ := client(t, team)
	tp := must(tc.Project(ctx, "TEAM"))
	eq(t, "team style", tp.Style, "next-gen")
	eq(t, "team simplified", tp.Simplified, true)
	if tp.IssueTypes[0].Scope == nil || tp.IssueTypes[0].Scope.Type != "PROJECT" || tp.IssueTypes[0].Scope.Project.ID != "10010" {
		t.Errorf("team type scope %+v", tp.IssueTypes[0].Scope)
	}
	for _, f := range must(tc.Fields(ctx)) {
		if f.ID == "customfield_10036" && (f.Scope == nil || f.Scope.Project.ID != "10010" ||
			!strings.HasSuffix(f.Schema.Custom, ":float") || f.Schema.Type != "number") {
			t.Errorf("team story points %+v %+v", f.Scope, f.Schema)
		}
	}
	tsts := must(tc.ProjectStatuses(ctx, "TEAM"))
	if len(tsts) == 0 || tsts[0].Statuses[0].StatusCategory.Key != "new" {
		t.Errorf("team statuses %+v", tsts)
	}
}

func TestFakeLinks(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	a := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "A"})
	b := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "B"})

	noErr(t, c.CreateIssueLink(ctx, "Blocks", a, b)) // A blocks B
	noErr(t, c.CreateIssueLink(ctx, "Blocks", a, b)) // a duplicate creates nothing
	la := srv.Issue(a).Links
	if len(la) != 1 || !la[0].Outward || la[0].Other != b || la[0].Type != "Blocks" {
		t.Fatalf("A's links %+v", la)
	}
	if lb := srv.Issue(b).Links; len(lb) != 1 || lb[0].Outward || lb[0].Other != a {
		t.Fatalf("B's links %+v", lb)
	}
	// By link type id and issue ids too.
	idB := srv.Issue(b).ID
	idA := srv.Issue(a).ID
	noErr(t, c.CreateIssueLink(ctx, "10003", idB, idA)) // B relates to A
	sys := must(must(c.GetIssue(ctx, a, []string{"issuelinks"}, nil)).System())
	if len(sys.IssueLinks) != 2 {
		t.Fatalf("A's issuelinks %+v", sys.IssueLinks)
	}
	var blocks jiraapi.IssueLink
	for _, l := range sys.IssueLinks {
		if l.Type.Name == "Blocks" {
			blocks = l
		}
	}
	if blocks.OutwardIssue == nil || blocks.OutwardIssue.Key != b || blocks.OutwardIssue.Fields.Status.Name != "To Do" {
		t.Errorf("A blocks B read as %+v", blocks)
	}
	noErr(t, c.DeleteIssueLink(ctx, blocks.ID))
	if la := srv.Issue(a).Links; len(la) != 1 || la[0].Type != "Relates" {
		t.Errorf("after delete %+v", la)
	}
	wantStatus(t, c.DeleteIssueLink(ctx, blocks.ID), 404)
	wantStatus(t, c.CreateIssueLink(ctx, "Nope", a, b), 404)
	for _, r := range srv.Requests() {
		if r.Op == "linkIssues" && strings.Contains(string(r.Body), "comment") {
			t.Errorf("link sent a comment: %s", r.Body)
		}
	}
}

func TestFakeUser(t *testing.T) {
	srv := jiratest.New(t)
	c, _ := client(t, srv)
	u := must(c.User(ctx, jiratest.RaviID))
	eq(t, "name", u.DisplayName, "Ravi Patel")
	eq(t, "hidden email", u.EmailAddress, "")
	eq(t, "zone", u.TimeZone, "Asia/Kolkata")
	eq(t, "active", u.Active, true)
	eq(t, "inactive", must(c.User(ctx, jiratest.JoID)).Active, false)
	eq(t, "app", must(c.User(ctx, jiratest.AutomationID)).AccountType, "app")
	_, err := c.User(ctx, "nobody")
	wantStatus(t, err, 404)
	srv.Deny("getUser")
	_, err = c.User(ctx, jiratest.RaviID)
	wantStatus(t, err, 403)
}

func TestFakeRateLimit(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithRetryAfter(3))
	c, sl := client(t, srv)
	srv.RateLimitNext(2)
	must(c.Myself(ctx))
	if !slices.Equal(sl.sleeps, []time.Duration{3 * time.Second, 3 * time.Second}) {
		t.Errorf("sleeps %v, want Retry-After twice", sl.sleeps)
	}
	// A POST is retried after a 429 too: it was rejected before processing.
	srv.RateLimitNext(1)
	ref := must(c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "PROJ"}, "issuetype": map[string]string{"id": "10002"}, "summary": "once",
	}, nil))
	eq(t, "one issue", len(srv.Keys()), 1)
	eq(t, "key", ref.Key, srv.Keys()[0])

	srv.RateLimitNext(10)
	_, err := c.Myself(ctx)
	if e := wantStatus(t, err, 429); e.RetryAfter != 3*time.Second {
		t.Errorf("RetryAfter %v", e.RetryAfter)
	}
	srv.RateLimitNext(0)

	// A Retry-After beyond MaxRetryWait is returned at once.
	email, token := srv.Credentials()
	impatient := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetryWait: time.Second,
		Sleep: func(context.Context, time.Duration) error { t.Error("slept"); return nil }})
	srv.RateLimitNext(1)
	_, err = impatient.Myself(ctx)
	wantStatus(t, err, 429)

	// The burst window of WithRateLimit: the client waits for the reset.
	burst := jiratest.New(t, jiratest.WithRateLimit(3, 10*time.Second), jiratest.WithAutoAdvance(0))
	bc, bsl := client(t, burst)
	for range 5 {
		must(bc.Myself(ctx))
	}
	if len(bsl.sleeps) != 1 || bsl.sleeps[0] != 10*time.Second {
		t.Errorf("burst sleeps %v", bsl.sleeps)
	}
}

// TestFakePerIssueWriteLimit: the per-issue limit is a 429 with a
// Retry-After until the window frees a slot; the client waits it out.
func TestFakePerIssueWriteLimit(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithAutoAdvance(0),
		jiratest.WithPerIssueWriteLimits(jiratest.WriteLimit{N: 3, Window: 2 * time.Second}))
	c, sl := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "hot"})
	for i := range 3 {
		noErr(t, c.EditIssue(ctx, key, map[string]any{"summary": fmt.Sprint("v", i)}, nil))
	}
	eq(t, "no sleep yet", len(sl.sleeps), 0)
	must(c.AddComment(ctx, key, jiraapi.TextToADF("fourth write")))
	if len(sl.sleeps) != 1 || sl.sleeps[0] != 2*time.Second {
		t.Errorf("sleeps %v, want the window's 2s", sl.sleeps)
	}
	var limited int
	for _, r := range srv.Requests() {
		if r.Status == 429 {
			limited++
			if r.Op != "addComment" {
				t.Errorf("429 on %s", r.Op)
			}
		}
	}
	eq(t, "429s", limited, 1)
	eq(t, "comments", len(srv.Issue(key).Comments), 1)
}

// TestFakeJQLTime: a JQL literal is read in the searching user's zone
// (Mia, Europe/Berlin), not the site's (America/Los_Angeles) the
// timestamps come in (C6). At the boundary minute, formatting in Mia's
// zone selects exactly the issue; formatting in the site zone does not.
func TestFakeJQLTime(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0),
		jiratest.WithStart(time.Date(2026, 7, 1, 16, 0, 30, 0, time.UTC)))
	c, _ := client(t, srv)
	me := must(c.Myself(ctx))
	mia := must(me.Location())
	before := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "before"})
	srv.Advance(time.Minute)
	at := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "at"})
	is := must(c.GetIssue(ctx, at, []string{"updated"}, nil))
	updated := must(is.System()).Updated
	site := updated.Location() // the offset the response came in

	eq(t, "literal", jiraapi.JQLTime(updated.Time, mia), `"2026/07/01 18:01"`)
	jql := func(loc *time.Location) string {
		return "project = PROJ AND updated >= " + jiraapi.JQLTime(updated.Time, loc) + " ORDER BY key"
	}
	keys, _ := search(t, c, jiraapi.Search{JQL: jql(mia)})
	if !slices.Equal(keys, []string{at}) {
		t.Errorf("in Mia's zone: %v, want [%s]", keys, at)
	}
	keys, _ = search(t, c, jiraapi.Search{JQL: jql(site)})
	if !slices.Equal(keys, []string{before, at}) {
		t.Errorf("in the site zone the literal is 9h early and should catch both: %v", keys)
	}
	// The minute after the boundary excludes it.
	keys, _ = search(t, c, jiraapi.Search{JQL: "project = PROJ AND updated >= " + jiraapi.JQLTime(updated.Add(time.Minute), mia)})
	eq(t, "after the boundary", len(keys), 0)
	keys, _ = search(t, c, jiraapi.Search{JQL: "project = PROJ AND updated < " + jiraapi.JQLTime(updated.Time, mia)})
	if !slices.Equal(keys, []string{before}) {
		t.Errorf("before the boundary: %v", keys)
	}
}

// TestFakeADFRandom is TestFakeADFRoundTrip over random text built from the
// model's delimiters. A failure without the fake is the converter's own;
// one only through the fake is a normalisation the converter misreads.
func TestFakeADFRandom(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithPerIssueWriteLimits())
	c, _ := client(t, srv)
	key := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "adf"})
	toks := []string{"word", "x", " ", "  ", "\t", "\n", "\n\n", "**", "_", "~~", "`", "[", "](", ")", "https://e.x/a",
		"# ", "## ", "- ", "  - ", "1. ", "2. ", "> ", ">", "```", "```go", "---", "@[", "Ravi", ":smile:", "<", ">", "é", "\r\n"}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 400 {
		var b strings.Builder
		for range 1 + rng.IntN(14) {
			b.WriteString(toks[rng.IntN(len(toks))])
		}
		text := b.String()
		norm := jiraapi.NormalizeText(text)
		doc := jiraapi.TextToADF(text)
		if got, ok := jiraapi.ADFToText(doc); got != norm || !ok {
			t.Errorf("converter alone: %q reads %q (lossless %v)", text, got, ok)
			continue
		}
		cm := must(c.AddComment(ctx, key, doc))
		got, ok := jiraapi.ADFToText(cm.Body)
		if got != norm || !ok {
			t.Errorf("through the fake: %q reads %q (lossless %v)\nsent   %s\nstored %s", text, got, ok, doc, cm.Body)
		} else if again := jiraapi.TextToADF(got); string(again) != string(doc) {
			t.Errorf("through the fake: %q re-exports differently", text)
		}
		noErr(t, c.DeleteComment(ctx, key, cm.ID))
	}
}

// TestFakeJQLQuote: quoted values reach the fake's JQL as the values.
func TestFakeJQLQuote(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0))
	c, _ := client(t, srv)
	qa := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "q", Status: "Ready for QA",
		Labels: []string{`we"ird\label`}})
	srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "other"})
	for _, jql := range []string{
		"status = " + jiraapi.JQLQuote("Ready for QA"),
		"labels = " + jiraapi.JQLQuote(`we"ird\label`),
		"key = " + jiraapi.JQLQuote(qa),
	} {
		keys, _ := search(t, c, jiraapi.Search{JQL: jql})
		if !slices.Equal(keys, []string{qa}) {
			t.Errorf("%s: %v", jql, keys)
		}
	}
}

// TestFakeTeamSite runs the write path on a team-managed project: a
// project-scoped float story-point field, an epic parent, global transitions.
func TestFakeTeamSite(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithSite(jiratest.TeamSite()))
	c, _ := client(t, srv)
	epic := must(c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"key": "TEAM"}, "issuetype": map[string]string{"id": "10020"}, "summary": "E",
	}, nil))
	story := must(c.CreateIssue(ctx, map[string]any{
		"project": map[string]string{"id": "10010"}, "issuetype": map[string]string{"id": "10021"}, "summary": "S",
		"parent": map[string]string{"id": epic.ID}, "customfield_10036": 2.5,
	}, nil))
	is := must(c.GetIssue(ctx, story.Key, []string{"parent", "customfield_10036", "issuetype", "status"}, nil))
	sys := must(is.System())
	eq(t, "parent", sys.Parent.Key, epic.Key)
	eq(t, "type scope", sys.IssueType.Scope.Type, "PROJECT")
	eq(t, "status scope", sys.Status.Name, "To Do")
	var points float64
	must(is.Decode("customfield_10036", &points))
	eq(t, "points", points, 2.5)

	ts := must(c.Transitions(ctx, story.Key))
	if len(ts) != 3 || !ts[2].IsGlobal {
		t.Fatalf("team transitions %+v", ts)
	}
	noErr(t, c.DoTransition(ctx, story.Key, "31", nil))
	got := srv.Issue(story.Key)
	eq(t, "status", got.Status, "Done")
	eq(t, "resolution by post function", got.Resolution, "Done")

	// Clearing with null, and a comment through the update map.
	noErr(t, c.EditIssue(ctx, story.Key, map[string]any{"customfield_10036": nil, "parent": nil},
		map[string][]jiraapi.Op{"comment": {jiraapi.OpAdd(map[string]any{"body": jiraapi.TextToADF("via update")})}}))
	got = srv.Issue(story.Key)
	eq(t, "points cleared", got.Custom["customfield_10036"], nil)
	eq(t, "parent cleared", got.Parent, "")
	if len(got.Comments) != 1 || got.Comments[0].Text != "via update" {
		t.Errorf("comments %+v", got.Comments)
	}

	// Re-sending an unchanged description is no change on the Jira side.
	noErr(t, c.EditIssue(ctx, story.Key, map[string]any{"description": jiraapi.TextToADF("**same**")}, nil))
	before := srv.Issue(story.Key)
	sys = must(must(c.GetIssue(ctx, story.Key, []string{"description"}, nil)).System())
	text, _ := jiraapi.ADFToText(sys.Description)
	noErr(t, c.EditIssue(ctx, story.Key, map[string]any{"description": jiraapi.TextToADF(text)}, nil))
	after := srv.Issue(story.Key)
	eq(t, "updated", after.Updated.Equal(before.Updated), true)
	eq(t, "changelog", len(after.Changelog), len(before.Changelog))
}

// TestFakeJQLTimeFold: in the hour Berlin repeats (2026-10-25, 02:00-03:00
// twice), a literal of the first pass is ambiguous, and the fake reads it
// as the second, as Go and java.util.Calendar do. A watermark there must
// still select everything after it.
func TestFakeJQLTimeFold(t *testing.T) {
	srv := jiratest.New(t, jiratest.WithIndexLag(0, 0),
		jiratest.WithStart(time.Date(2026, 10, 25, 0, 40, 0, 0, time.UTC))) // 02:40 CEST
	c, _ := client(t, srv)
	mia := must(must(c.Myself(ctx)).Location())
	a := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "a"})
	srv.Advance(10 * time.Minute)
	b := srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "b"})
	cursor := srv.Issue(a).Updated.Add(-5 * time.Minute) // design-sync S11's overlap
	keys, _ := search(t, c, jiraapi.Search{JQL: "project = PROJ AND updated >= " + jiraapi.JQLTime(cursor, mia) + " ORDER BY key"})
	if !slices.Equal(keys, []string{a, b}) {
		t.Errorf("watermark %s in the fold selects %v, want [%s %s]", jiraapi.JQLTime(cursor, mia), keys, a, b)
	}
}
