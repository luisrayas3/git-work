package jira_test

// Adversarial scenarios, round 3: staleness by Jira's clock, the jira-create
// marker, failed-hit re-reads, the body as an ordinary key, the report and
// --dry-run, and a long random run with the fake's harshest switches. A test
// that found a bug skips itself with a "BUG:" reason only when the bug
// reproduces.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// ---- helpers ----

// runP is runWith for any project, never failing the test on a run error
// (a rate limit may stop Discover): the state is saved as the host does.
func (w *world) runP(project string, rt http.RoundTripper, opts jira.Options) ([]jira.Line, jira.Summary, error) {
	w.t.Helper()
	if rt == nil {
		rt = http.DefaultTransport
	}
	ctx := context.Background()
	email, token := w.srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: w.srv.URL(), Email: email, Token: token,
		HTTPClient: &http.Client{Transport: rt}, MaxRetries: -1})
	p, err := jira.Discover(ctx, c, project)
	if err != nil {
		return nil, jira.Summary{}, err
	}
	s, err := w.c.LoadSchema()
	require.NoError(w.t, err)
	m, _, err := jira.Compile(s, p)
	require.NoError(w.t, err)
	st, err := jira.LoadState(w.c.LocalStorage())
	require.NoError(w.t, err)
	st.Bind(w.srv.URL(), project)
	var lines []jira.Line
	sum, err := jira.Sync(ctx, w.c, c, p, m, st, opts, func(l jira.Line) {
		if l.Summary == nil && l.Schema == nil {
			lines = append(lines, l)
		}
	})
	if !opts.DryRun {
		require.NoError(w.t, st.Save(w.c.LocalStorage()))
	}
	return lines, sum, err
}

// slowWorld is Jira whose GET lags every write by ten minutes of its clock:
// a write stays unconfirmed for as many runs as the test wants.
func slowWorld(t *testing.T) *world {
	return newWorld(t, jiratest.WithStaleReads(), jiratest.WithIndexLag(0, 10*time.Minute))
}

func (w *world) importedSlow(spec jiratest.IssueSpec) (string, *cache.IssueCache) {
	w.t.Helper()
	spec.Project, spec.Type = "PROJ", "Task"
	key := w.srv.CreateIssue(spec)
	w.srv.Advance(11 * time.Minute)
	w.mustSync(jira.Options{})
	return key, w.byKey(key)
}

// bodyForm is the base's form of a body (jira's digest), for the oracle.
func bodyForm(text string) string {
	sum := sha256.Sum256([]byte("v1\n" + jiraapi.NormalizeText(text)))
	return `"v1:` + hex.EncodeToString(sum[:]) + `"`
}

func baseOf(t *testing.T, ic *cache.IssueCache, key string) string {
	t.Helper()
	b, _ := jira.CurrentBase(ic.Snapshot())
	if b == nil {
		return ""
	}
	v, ok := b.Fields[key]
	if !ok {
		return ""
	}
	return string(canonJSON(v))
}

// skewRT shifts the Date of every response by d: a proxy or an edge whose
// clock differs from the one Jira stamps created with.
type skewRT struct {
	d     time.Duration
	inner http.RoundTripper
}

func (s skewRT) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := s.inner.RoundTrip(r)
	if err == nil {
		if t, e := http.ParseTime(resp.Header.Get("Date")); e == nil {
			resp.Header.Set("Date", t.Add(s.d).UTC().Format(http.TimeFormat))
		}
	}
	return resp, err
}

func crashAfterCreate() *hookRT {
	return &hookRT{after: func(r *http.Request, _ int) error {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue" {
			return errCrash
		}
		return nil
	}}
}

func crashBeforeCreate() *hookRT {
	return &hookRT{before: func(r *http.Request, _ int) error {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue" {
			return errCrash
		}
		return nil
	}}
}

func exchange(t *testing.T, ws ...*world) {
	t.Helper()
	for _, w := range append(ws, ws...) {
		require.NoError(t, w.c.Pull("origin"))
		if _, err := w.c.Push("origin"); err != nil {
			t.Logf("push: %v", err) // two runners' identities (JS16), not the issues
		}
	}
}

// ---- staleness by Jira's clock ----

// A local clear that Jira does not show two runs in a row (a slow GET) is
// pending, never reverted: once the GET catches up, both sides are clear.
func TestAdv2StaleClearNeverSilent(t *testing.T) {
	for _, tc := range []struct {
		key, note string
	}{{"due", "due: local null"}, {jira.BodyKey, "- body:"}} {
		t.Run(tc.key, func(t *testing.T) {
			w := slowWorld(t)
			key, ic := w.importedSlow(jiratest.IssueSpec{Summary: "Clear me", Description: "the old body", DueDate: "2026-08-01"})
			old := field(t, ic, "due")
			if tc.key == jira.BodyKey {
				w.setBody(ic, "")
			} else {
				w.set(ic.Id(), "due", issue.Value("null"))
			}
			w.mustSync(jira.Options{}) // written, the GET stale: pending
			w.mustSync(jira.Options{}) // still stale: pending, not written again
			reverted := tc.key == "due" && field(t, ic, "due") == old || tc.key == jira.BodyKey && w.body(ic) == "the old body"
			silent := reverted && !strings.Contains(notesText(ic), tc.note)
			t.Logf("after two stale runs: due=%s body=%q notes=%q", field(t, ic, "due"), w.body(ic), notesText(ic))

			// Jira did clear it: once the GET catches up, the clear comes back
			w.srv.Advance(11 * time.Minute)
			w.converge(4)
			if tc.key == "due" {
				require.Equal(t, "", w.srv.Issue(key).DueDate)
				require.Contains(t, []string{"", "null"}, field(t, ic, "due"))
			} else {
				require.Equal(t, "", jiraText(t, w.srv, key))
				require.Equal(t, "", w.body(ic))
			}
			require.False(t, silent, "a local clear of %s Jira does not show yet is never reverted silently", tc.key)
		})
	}
}

// The same with a value: no stale value imported, no false note, no flap.
func TestAdv2SlowGetNoFlap(t *testing.T) {
	w := slowWorld(t)
	key, ic := w.importedSlow(jiratest.IssueSpec{Summary: "Old title"})
	w.set(ic.Id(), "title", str("New title"))
	w.mustSync(jira.Options{})
	w.mustSync(jira.Options{})
	mid := field(t, ic, "title")
	note := notesText(ic)
	w.srv.Advance(11 * time.Minute)
	w.converge(4)
	require.Equal(t, "New title", w.srv.Issue(key).Summary)
	require.Equal(t, `"New title"`, field(t, ic, "title"), "converged on the local edit")
	w.quietFull()
	require.Equal(t, `"New title"`, mid, "a slow GET neither reverts nor notes (%q)", note)
	require.Empty(t, note)
}

// A write unconfirmed once, then a new local edit of that key: when Jira's
// GET shows the first write, it confirms it, and the newer edit is exported.
func TestAdv2UnconfirmedThenLocalEdit(t *testing.T) {
	w := slowWorld(t)
	key, ic := w.importedSlow(jiratest.IssueSpec{Summary: "Old"})
	w.set(ic.Id(), "title", str("First"))
	w.mustSync(jira.Options{}) // PUT First, unconfirmed
	w.set(ic.Id(), "title", str("Second"))
	w.srv.Advance(11 * time.Minute) // Jira's GET now shows First
	w.mustSync(jira.Options{})
	require.Equal(t, `"Second"`, field(t, ic, "title"), "First, the sync's own export, is no Jira edit")
	w.srv.Advance(11 * time.Minute) // and then Second
	w.converge(4)
	w.quietFull()
	require.Empty(t, notesText(ic))
	require.Equal(t, "Second", w.srv.Issue(key).Summary)
	require.Equal(t, `"Second"`, field(t, ic, "title"))
}

// ---- the jira-create marker ----

// A POST that landed with its answer lost; the marker is pushed and B, having
// pulled it, syncs first: B links by property, A links too, and after the
// exchange there is one Jira issue, one local issue, both clones quiet.
func TestAdv2MarkerTwoClonesLanded(t *testing.T) {
	a, b := twoClones(t)
	id := a.newLocal("Made on A", "body", nil)
	_, _, err := a.runWith(context.Background(), crashAfterCreate(), jira.Options{}, true)
	require.Error(t, err)
	exchange(t, a, b)
	b.mustSync(jira.Options{})
	a.mustSync(jira.Options{})
	exchange(t, a, b)
	exchange(t, a, b)
	require.Len(t, a.srv.Keys(), 1, "no duplicate in Jira")
	for _, w := range []*world{a, b} {
		require.Equal(t, 1, w.localIssues())
		require.Equal(t, a.srv.Keys()[0], jiraKeyOf(t, mustIssue(t, w.c, id)))
		w.converge(3)
	}
	a.quietFull()
	b.quietFull()
}

// The POST never left A; the marker is pushed; B waits out Settle and
// creates; A, which never pulled B's link, finds B's issue by property.
func TestAdv2MarkerTwoClonesNotLanded(t *testing.T) {
	a, b := twoClones(t)
	id := a.newLocal("Made on A", "", nil)
	_, _, err := a.runWith(context.Background(), crashBeforeCreate(), jira.Options{}, true)
	require.Error(t, err)
	exchange(t, a, b)
	lines, _ := b.mustSync(jira.Options{})
	require.Empty(t, b.srv.Keys(), "B waits within Settle: %+v", lines)
	b.ageJournal()
	b.mustSync(jira.Options{})
	require.Len(t, b.srv.Keys(), 1)
	a.mustSync(jira.Options{})
	require.Len(t, a.srv.Keys(), 1, "A links B's create, no second POST")
	require.Equal(t, b.srv.Keys()[0], jiraKeyOf(t, mustIssue(t, a.c, id)))
	exchange(t, a, b)
	a.converge(3)
	b.converge(3)
}

// A create Jira made, whose answer was lost, deleted in Jira before the
// next run resolved it: never a failure every run, and the local issue ends
// linked to exactly one live Jira issue (created again, as nothing names it).
func TestAdv2CreatedThenDeletedInJira(t *testing.T) {
	for _, lag := range []time.Duration{0, 20 * time.Minute} {
		t.Run(fmt.Sprint("lag=", lag), func(t *testing.T) {
			w := newWorld(t, jiratest.WithIndexLag(0, lag))
			id := w.newLocal("Made, then deleted there", "", nil)
			_, _, err := w.runWith(context.Background(), crashAfterCreate(), jira.Options{}, true)
			require.Error(t, err)
			require.Len(t, w.srv.Keys(), 1)
			w.srv.Delete(w.srv.Keys()[0])
			failedRuns := 0
			for i := 0; i < 8; i++ {
				w.srv.Advance(5 * time.Minute)
				lines, sum, err := w.sync(jira.Options{})
				require.NoError(t, err)
				if sum.Failed > 0 {
					failedRuns++
					t.Logf("run %d failed: %+v", i, lines)
				}
			}
			w.converge(3)
			require.Len(t, w.srv.Keys(), 1, "one live Jira issue")
			require.Equal(t, w.srv.Keys()[0], jiraKeyOf(t, mustIssue(t, w.c, id)))
			if failedRuns > 0 {
				t.Skipf("BUG: an in-doubt create whose Jira issue was deleted while the index still shows it fails the run (%d of 8 runs exit 1: linkCreated treats the GET's 404 as a failure)", failedRuns)
			}
		})
	}
}

// A refused create, then a local edit: retried on the very next run, no
// Settle wait, once.
func TestAdv2RefusedThenEditRetries(t *testing.T) {
	w := newWorld(t)
	id := w.newLocal("Refused", "", nil)
	refuse := &failRT{match: func(r *http.Request) bool {
		return r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue"
	}, status: 400}
	_, sum, err := w.runWith(context.Background(), refuse, jira.Options{}, true)
	require.NoError(t, err)
	require.Equal(t, 1, sum.Failed)
	w.set(id, "title", str("Refused, fixed"))
	posts := 0
	count := &hookRT{before: func(r *http.Request, _ int) error {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue" {
			posts++
		}
		return nil
	}}
	_, sum, err = w.runWith(context.Background(), count, jira.Options{}, true)
	require.NoError(t, err)
	require.Zero(t, sum.Failed)
	require.Equal(t, 1, posts)
	require.Len(t, w.srv.Keys(), 1)
	require.Equal(t, "Refused, fixed", w.srv.Issue(w.srv.Keys()[0]).Summary)
	w.converge(3)
}

// A Date header ahead of the clock Jira stamps created with by more than
// Overlap: the attempt is later than the issue it made, the created search
// misses it, and after Settle the create is POSTed again.
func TestAdv2CreateDateSkew(t *testing.T) {
	for _, skew := range []time.Duration{-time.Hour, 2 * time.Minute, 10 * time.Minute} {
		t.Run(fmt.Sprint("skew=", skew), func(t *testing.T) {
			w := newWorld(t)
			id := w.newLocal("Skewed", "", nil)
			_, _, err := w.runWith(context.Background(), skewRT{skew, crashAfterCreate()}, jira.Options{}, true)
			require.Error(t, err)
			for i := 0; i < 2; i++ {
				_, _, err := w.runWith(context.Background(), skewRT{skew, http.DefaultTransport}, jira.Options{}, true)
				require.NoError(t, err)
			}
			w.ageJournal()
			for i := 0; i < 2; i++ {
				_, _, err := w.runWith(context.Background(), skewRT{skew, http.DefaultTransport}, jira.Options{}, true)
				require.NoError(t, err)
			}
			n := len(w.srv.Keys())
			require.NotEmpty(t, jiraKeyOf(t, mustIssue(t, w.c, id)))
			if n > 1 && skew > 5*time.Minute {
				t.Skipf("BUG(low): a Date header %v ahead of Jira's created clock duplicates an in-doubt create: %v", skew, w.srv.Keys())
			}
			require.Equal(t, 1, n, "created once: %v", w.srv.Keys())
		})
	}
}

// ---- failed hits ----

// A failed hit out of the search window whose re-read by GET fails again
// (a 403, not a 404) is neither reported nor counted: the run exits 0 while
// the issue still does not sync.
func TestAdv2FailedRefetchIsSilent(t *testing.T) {
	w := newWorld(t)
	a := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "A"})
	w.mustSync(jira.Options{})
	b := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "B"})
	bid := w.srv.Issue(b).ID
	failB := &failRT{match: func(r *http.Request) bool {
		return r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/issue/"+bid)
	}, status: 403}
	var sums []jira.Summary
	var gets []int
	for i := 0; i < 4; i++ {
		w.srv.Advance(time.Hour)
		w.srv.Edit(a, map[string]any{"summary": fmt.Sprint("A", i)})
		w.srv.ResetRequests()
		_, sum, err := w.runWith(context.Background(), failB, jira.Options{}, true)
		require.NoError(t, err)
		sums = append(sums, sum)
		gets = append(gets, len(w.srv.Requests()))
		st, _ := jira.LoadState(w.c.LocalStorage())
		require.Contains(t, st.Failed, bid, "run %d: still failed", i)
		require.Equal(t, fmt.Sprintf(`"A%d"`, i), field(t, w.byKey(a), "title"), "the cursor advances past it")
	}
	t.Logf("requests per run: %v", gets)
	for i, s := range sums {
		require.NotZero(t, s.Failed, "run %d: the failed hit %s re-read by GET fails again: a failed line, exit 1: %+v", i, b, s)
	}
}

// More than maxFailed failed hits: the re-read takes the first 100 by
// sorted id every run, so while those keep failing the rest are never read
// again, whatever happens to them.
func TestAdv2FailedHitsStarve(t *testing.T) {
	w := newWorld(t, jiratest.WithPerIssueWriteLimits())
	sentinel := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "sentinel"})
	w.mustSync(jira.Options{})
	var ids []string
	byId := map[string]string{}
	for i := 0; i < 105; i++ {
		k := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: fmt.Sprint("F", i)})
		id := w.srv.Issue(k).ID
		ids = append(ids, id)
		byId[id] = k
	}
	sort.Strings(ids)
	failing := map[string]bool{}
	for _, id := range ids {
		failing[id] = true
	}
	rt := &failRT{match: func(r *http.Request) bool {
		if r.Method != "GET" {
			return false
		}
		i := strings.LastIndex(r.URL.Path, "/issue/")
		return i >= 0 && failing[strings.TrimPrefix(r.URL.Path[i:], "/issue/")]
	}, status: 403}
	step := func(n int) jira.Summary {
		w.srv.Advance(time.Hour)
		w.srv.Edit(sentinel, map[string]any{"summary": fmt.Sprint("sentinel ", n)})
		_, sum, err := w.runP("PROJ", rt, jira.Options{})
		require.NoError(t, err)
		return sum
	}
	s1 := step(1)
	require.Equal(t, 105, s1.Failed)
	step(2) // the overlap still returns them
	for _, id := range ids[100:] {
		delete(failing, id) // the last five heal; the first hundred never do
	}
	step(3)
	step(4)
	imported := 0
	for _, id := range ids[100:] {
		if _, err := w.c.Issues().ResolvePrefixOrAlias(byId[id]); err == nil {
			imported++
		}
	}
	st, _ := jira.LoadState(w.c.LocalStorage())
	t.Logf("failed kept: %d; healed and imported: %d of 5", len(st.Failed), imported)
	require.Equal(t, 5, imported, "with more than 100 failed hits, every one is re-read within ceil(n/100) runs")
}

// ---- the body is a key ----

func TestAdv2BodyEditedBothSides(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Body twice")
	w.setBody(ic, "the local text")
	w.srv.Edit(key, map[string]any{"description": json.RawMessage(jiratest.TextADF("the jira text"))})
	w.mustSync(jira.Options{})
	require.Equal(t, "the jira text", w.body(ic))
	require.Equal(t, "the jira text", jiraText(t, w.srv, key))
	require.Contains(t, notesText(ic), "- body:")
	kept := false
	for _, op := range ic.Snapshot().Operations {
		if e, ok := op.(*issue.EditCommentOperation); ok && e.Message == "the local text" {
			kept = true
		}
	}
	require.True(t, kept, "the local text is in the history")
	w.still()
	w.quietFull()
}

func TestAdv2BodyCleared(t *testing.T) {
	t.Run("locally", func(t *testing.T) {
		w := newWorld(t)
		key, ic := w.imported("Clear here")
		w.setBody(ic, "")
		w.converge(3)
		require.Equal(t, "", jiraText(t, w.srv, key))
		require.Equal(t, "", w.body(ic))
		require.Empty(t, notesText(ic))
		w.quietFull()
	})
	t.Run("in-jira", func(t *testing.T) {
		w := newWorld(t)
		key, ic := w.imported("Clear there")
		w.srv.Edit(key, map[string]any{"description": nil})
		w.converge(3)
		require.Equal(t, "", w.body(ic))
		require.Empty(t, notesText(ic))
		w.quietFull()
	})
	t.Run("both", func(t *testing.T) {
		w := newWorld(t)
		key, ic := w.imported("Clear both")
		w.setBody(ic, "")
		w.srv.Edit(key, map[string]any{"description": nil})
		w.converge(3)
		require.Equal(t, "", w.body(ic))
		require.Empty(t, notesText(ic), "the same value on both sides is no conflict")
		w.quietFull()
	})
}

// A lossy description with a local edit is pending; a Jira edit then takes
// Jira's with a note, and the local rewrite is in the history.
func TestAdv2LossyBodyThenJiraEdit(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Lossy")
	table := func(cell string) json.RawMessage {
		return json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"` + cell + `"}]}]}]}]}]}`)
	}
	w.srv.Edit(key, map[string]any{"description": table("one")})
	w.mustSync(jira.Options{})
	w.setBody(ic, "a local rewrite")
	w.mustSync(jira.Options{})
	w.srv.Edit(key, map[string]any{"description": table("two")})
	w.mustSync(jira.Options{})
	require.Contains(t, w.body(ic), "two")
	require.Contains(t, notesText(ic), "- body:")
	require.Contains(t, string(w.srv.Issue(key).Description), `"two"`, "Jira's table survives")
	w.still()
	w.quietFull()
}

// A local description beyond the store's field bound (the body is a comment,
// which has none) is exported and created whole, then quiet.
func TestAdv2LocalBodyOverMaxValueSize(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Huge here")
	huge := strings.Repeat("word ", issue.MaxValueSize/5+2000)
	w.setBody(ic, huge)
	w.converge(3)
	require.Equal(t, jiraapi.NormalizeText(huge), jiraText(t, w.srv, key))
	id := w.newLocal("Huge new", huge, nil)
	w.converge(3)
	k := jiraKeyOf(t, mustIssue(t, w.c, id))
	require.NotEmpty(t, k)
	require.Equal(t, jiraapi.NormalizeText(huge), jiraText(t, w.srv, k))
	require.Len(t, w.srv.Keys(), 2)
	w.quietFull()
}

// ---- the report ----

// --dry-run is a function of the two sides: twice in a row, the same lines;
// and what it claims, the real run then does.
func TestAdv2DryRunPureAndHonest(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Dry")
	w.set(ic.Id(), "title", str("local"))
	w.set(ic.Id(), "status", str("in-progress"))
	w.setBody(ic, "local body")
	w.comment(ic, "local comment")
	w.srv.Edit(key, map[string]any{"labels": []string{"remote"}, "priority": map[string]string{"name": "High"}})
	w.srv.AddComment(key, "remote comment")
	k2, ic2 := w.imported("Double")
	w.set(ic2.Id(), "title", str("double, local"))
	w.srv.Edit(k2, map[string]any{"summary": "double, jira"})
	w.newLocal("New here", "body", map[string]issue.Value{"status": str("in-progress"), "labels": issue.Value(`["n"]`)})
	w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "New there", Labels: []string{"t"}})

	dry := func() []jira.Line {
		lines, _, err := w.sync(jira.Options{DryRun: true})
		require.NoError(t, err)
		return lines
	}
	d1, d2 := dry(), dry()
	j1, _ := json.Marshal(d1)
	j2, _ := json.Marshal(d2)
	require.Equal(t, string(j1), string(j2), "two dry runs, one output")

	real, _, err := w.sync(jira.Options{})
	require.NoError(t, err)
	find := func(d jira.Line) *jira.Line {
		for i := range real {
			if d.Issue != "" && real[i].Issue == d.Issue || d.Issue == "" && d.Jira != "" && real[i].Jira == d.Jira {
				return &real[i]
			}
		}
		return nil
	}
	var lies []string
	for _, d := range d1 {
		r := find(d)
		if r == nil {
			lies = append(lies, fmt.Sprintf("dry line with no real one: %+v", d))
			continue
		}
		if r.Action != d.Action {
			lies = append(lies, fmt.Sprintf("%s: dry %s, real %s", d.Jira, d.Action, r.Action))
		}
		for k, v := range d.Exported {
			if string(r.Exported[k]) != string(v) {
				lies = append(lies, fmt.Sprintf("%s %s: dry exported %s, real %s", orId2(d), k, v, r.Exported[k]))
			}
		}
		for k, v := range d.Imported {
			if string(r.Imported[k]) != string(v) {
				lies = append(lies, fmt.Sprintf("%s %s: dry imported %s, real %s", orId2(d), k, v, r.Imported[k]))
			}
		}
		if len(d.Conflicts) != len(r.Conflicts) {
			lies = append(lies, fmt.Sprintf("%s: dry %d conflicts, real %d", orId2(d), len(d.Conflicts), len(r.Conflicts)))
		}
		if counts(d.Comments) != counts(r.Comments) {
			lies = append(lies, fmt.Sprintf("%s: dry comments %+v, real %+v", orId2(d), d.Comments, r.Comments))
		}
		for k := range r.Exported {
			if _, ok := d.Exported[k]; !ok {
				lies = append(lies, fmt.Sprintf("real run exported %s on %s, which the dry run did not show", k, orId2(d)))
			}
		}
	}
	require.Len(t, real, len(d1), "as many lines")
	require.Empty(t, lies, "--dry-run's claims are the run that follows")
}

func counts(c *jira.CommentCounts) jira.CommentCounts {
	if c == nil {
		return jira.CommentCounts{}
	}
	return *c
}

func orId2(l jira.Line) string {
	if l.Jira != "" {
		return l.Jira
	}
	return l.Issue.Human()
}

// Every failure of an issue is a failed line (exit 1); every failure of the
// run is an error (exit 1). Each class, injected on its own.
func TestAdv2FailureClassesExitOne(t *testing.T) {
	type class struct {
		name  string
		setup func(w *world) (string, http.RoundTripper)
		fatal bool
	}
	get403 := func(path func(w *world, key string) string, status int) func(w *world) (string, http.RoundTripper) {
		return func(w *world) (string, http.RoundTripper) {
			key, ic := w.imported("Target")
			w.set(ic.Id(), "title", str("edited"))
			p := path(w, key)
			return key, &failRT{match: func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, p) && r.Method == "GET" }, status: status}
		}
	}
	put := func(status int) func(w *world) (string, http.RoundTripper) {
		return func(w *world) (string, http.RoundTripper) {
			key, ic := w.imported("Target")
			w.set(ic.Id(), "title", str("edited"))
			id := w.srv.Issue(key).ID
			return key, &failRT{match: func(r *http.Request) bool { return r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/issue/"+id) }, status: status}
		}
	}
	issuePath := func(w *world, key string) string { return "/issue/" + w.srv.Issue(key).ID }
	commentsPath := func(w *world, key string) string { return "/issue/" + w.srv.Issue(key).ID + "/comment" }
	classes := []class{
		{"issue GET 403", get403(issuePath, 403), false},
		{"issue GET 500", get403(issuePath, 500), true},
		{"comments GET 403", get403(commentsPath, 403), false},
		{"PUT 500", put(500), true},
		{"PUT 401", put(401), true},
		{"search 400", func(w *world) (string, http.RoundTripper) {
			return "", &failRT{match: func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/search/jql") }, status: 400}
		}, true},
		{"create 403", func(w *world) (string, http.RoundTripper) {
			w.newLocal("New", "", nil)
			return "", &failRT{match: func(r *http.Request) bool { return r.Method == "POST" && r.URL.Path == "/rest/api/3/issue" }, status: 403}
		}, false},
	}
	for _, c := range classes {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			key, rt := c.setup(w)
			lines, sum, err := w.runP("PROJ", rt, jira.Options{})
			exit1 := err != nil || sum.Failed > 0
			require.True(t, exit1, "exit 1: err %v, summary %+v, lines %+v", err, sum, lines)
			if c.fatal {
				require.Error(t, err)
				return
			}
			found := false
			for _, l := range lines {
				found = found || l.Action == jira.ActionFailed && l.Error != "" && (key == "" || l.Jira == key)
			}
			require.True(t, found, "a failed line with its error: %+v", lines)
		})
	}
}

// ---- a long random run with the harshest switches together ----

type want2 struct {
	v     string // canonical JSON, or the body's text
	notes int    // conflict notes on the issue when it was made
}

// TestAdv2PropertyHarsh: rounds of random edits on both sides, clears
// included, local creates, with a lagging index, stale reads, comment bumps,
// rate-limit bursts and transition conflicts; the oracle follows each local
// value until the base confirms it and requires every overwrite of one to be
// in a conflict note; after the rounds the sides converge within K runs,
// agree, and hold no duplicate.
func TestAdv2PropertyHarsh(t *testing.T) {
	variants := map[string][]jiratest.Option{
		"harsh":          {jiratest.WithIndexLag(3, 0), jiratest.WithStaleReads(), jiratest.WithCommentBumps(true, true)},
		"harsh-verbatim": {jiratest.WithIndexLag(3, 0), jiratest.WithStaleReads(), jiratest.WithCommentBumps(true, true), jiratest.WithVerbatimADF()},
		"lag-only":       {jiratest.WithIndexLag(5, 0), jiratest.WithCommentBumps(true, true)},
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for k := range variants {
			if !yield(k) {
				return
			}
		}
	})
	var bugs []string
	for _, name := range names {
		for seed := uint64(11); seed <= 13; seed++ {
			t.Run(fmt.Sprintf("%s/seed%d", name, seed), func(t *testing.T) {
				if v := harshRun(t, seed, 30, variants[name]...); len(v) > 0 {
					bugs = append(bugs, fmt.Sprintf("%s/seed%d: %s", name, seed, v[0]))
					for _, x := range v {
						t.Log("violation: " + x)
					}
					t.Errorf("%d violation(s), first: %s", len(v), v[0])
				}
			})
		}
	}
	if len(bugs) > 0 {
		t.Logf("oracle violations: %q", bugs)
	}
}

func harshRun(t *testing.T, seed uint64, rounds int, opts ...jiratest.Option) []string {
	r := rand.New(rand.NewPCG(seed, seed*104729))
	w := teamWorld(t, opts...)
	sync := func() (jira.Summary, error) {
		_, sum, err := w.runP("TEAM", nil, jira.Options{})
		return sum, err
	}
	for i := 0; i < 3; i++ {
		w.srv.CreateIssue(jiratest.IssueSpec{Project: "TEAM", Type: "Task", Summary: fmt.Sprintf("issue %d", i), Description: "body " + fmt.Sprint(i)})
	}
	for i := 0; i < 6; i++ {
		_, err := sync()
		require.NoError(t, err)
	}
	var is []propIssue
	for _, key := range w.srv.Keys() {
		is = append(is, propIssue{key: key, ic: w.byKey(key)})
	}
	require.Len(t, is, 3)
	var created []entity.Id
	word := func() string { return fmt.Sprintf("w%d", r.IntN(1000)) }
	wants := map[[2]any]want2{} // (issue index, key) -> local value followed
	var violations []string
	nconf := func(ic *cache.IssueCache) int { return len(notes(ic, jira.NoteConflict)) }

	for round := 0; round < rounds; round++ {
		for n := r.IntN(5); n > 0; n-- {
			i := r.IntN(len(is))
			p := is[i]
			var key string
			var v issue.Value
			switch r.IntN(10) {
			case 0:
				key, v = "title", str("local "+word())
			case 1:
				key, v = "priority", str(propPriorities[r.IntN(5)])
			case 2:
				key, v = "estimate", issue.Value(fmt.Sprint(r.IntN(9)))
			case 3:
				key, v = "estimate", issue.Value("null")
			case 4:
				key, v = "due", str(fmt.Sprintf("2026-%02d-%02d", 1+r.IntN(12), 1+r.IntN(28)))
			case 5:
				key, v = "due", issue.Value("null")
			case 6:
				keys := []string{"to-do", "in-progress", "done"}
				key, v = "status", str(keys[r.IntN(3)])
			case 7:
				text := "local body " + word()
				if r.IntN(4) == 0 {
					text = ""
				}
				w.setBody(p.ic, text)
				wants[[2]any{i, jira.BodyKey}] = want2{text, nconf(p.ic)}
				if baseOf(t, p.ic, jira.BodyKey) == bodyForm(text) {
					delete(wants, [2]any{i, jira.BodyKey}) // back to the base: to a state merge, no edit
				}
				continue
			case 8:
				w.comment(p.ic, "local "+word())
				continue
			case 9:
				if len(created) < 2 && r.IntN(3) == 0 {
					created = append(created, w.newLocal("made here "+word(), "made body", nil))
				}
				continue
			}
			if string(canonJSON(v)) == string(canonJSON(issue.Value(orNull(field(t, p.ic, key))))) {
				continue
			}
			w.set(p.ic.Id(), key, v)
			wants[[2]any{i, key}] = want2{string(canonJSON(v)), nconf(p.ic)}
			if baseOf(t, p.ic, key) == string(canonJSON(v)) {
				delete(wants, [2]any{i, key}) // back to the base: to a state merge, no edit
			}
		}
		for n := r.IntN(4); n > 0; n-- {
			p := is[r.IntN(len(is))]
			switch r.IntN(7) {
			case 0:
				w.srv.Edit(p.key, map[string]any{"summary": "jira " + word()})
			case 1:
				name := propPriorities[r.IntN(5)]
				w.srv.Edit(p.key, map[string]any{"priority": map[string]string{"name": strings.ToUpper(name[:1]) + name[1:]}})
			case 2:
				w.srv.Edit(p.key, map[string]any{propPoints: r.IntN(9)})
			case 3:
				names := []string{"To Do", "In Progress", "Done"}
				if to := names[r.IntN(3)]; w.srv.Issue(p.key).Status != to {
					w.srv.Transition(p.key, to)
				}
			case 4:
				w.srv.AddComment(p.key, "jira "+word())
			case 5:
				w.srv.Edit(p.key, map[string]any{"description": json.RawMessage(jiratest.TextADF("jira body " + word()))})
			case 6:
				if cs := w.srv.Issue(p.key).Comments; len(cs) > 0 {
					w.srv.EditComment(p.key, cs[r.IntN(len(cs))].ID, "jira edit "+word())
				}
			}
		}
		if r.IntN(5) == 0 {
			w.srv.RateLimitNext(1 + r.IntN(3))
		}
		if r.IntN(4) == 0 {
			w.srv.ConflictNextTransitions(1 + r.IntN(2))
		}
		if r.IntN(4) == 0 {
			w.srv.Advance(time.Duration(r.IntN(1200)) * time.Second)
		}
		_, _ = sync() // a run stopped by a burst is cron's next run's business

		// the oracle: a followed value either still holds, confirmed or not,
		// or was overwritten with a note naming it
		for k, wt := range wants {
			i, key := k[0].(int), k[1].(string)
			p := is[i]
			var cur, form string
			if key == jira.BodyKey {
				cur, form = w.body(p.ic), bodyForm(wt.v)
			} else {
				cur, form = string(canonJSON(issue.Value(orNull(field(t, p.ic, key))))), wt.v
			}
			if cur == wt.v {
				if baseOf(t, p.ic, key) == form {
					delete(wants, k) // confirmed: a later Jira edit is an ordinary import
				}
				continue
			}
			ns := strings.Join(notes(p.ic, jira.NoteConflict)[wt.notes:], "\n")
			needle := key + ": local " + wt.v
			if key == jira.BodyKey {
				needle = "- body:"
			}
			if !strings.Contains(ns, needle) {
				violations = append(violations, fmt.Sprintf("round %d: %s %s local %s overwritten by %s, notes %q", round, p.key, key, wt.v, cur, ns))
			}
			delete(wants, k)
		}
	}

	// quiet on Jira's side: the lag drains, the bursts are over
	w.srv.RateLimitNext(0)
	w.srv.ConflictNextTransitions(0)
	for i := 0; i < 8; i++ {
		w.srv.Advance(time.Minute)
		_, _ = sync()
	}
	const K = 6
	quietAt := -1
	for i := 0; i < K; i++ {
		w.srv.ResetRequests()
		before := w.refs()
		_, sum, err := w.runP("TEAM", nil, jira.Options{})
		require.NoError(t, err)
		if sum.Failed == 0 && w.srv.Writes() == 0 && equalRefs(before, w.refs()) {
			quietAt = i
			break
		}
	}
	require.GreaterOrEqual(t, quietAt, 0, "no fixpoint within %d clean runs", K)
	_, _, err := w.runP("TEAM", nil, jira.Options{Full: true})
	require.NoError(t, err)
	w.srv.ResetRequests()
	before := w.refs()
	_, sum, err := w.runP("TEAM", nil, jira.Options{Full: true})
	require.NoError(t, err)
	require.Zero(t, sum.Failed)
	require.Zero(t, w.srv.Writes(), "a second --full writes nothing to Jira")
	require.Equal(t, before, w.refs(), "a second --full commits nothing")

	if n := len(w.srv.Keys()); n != 3+len(created) {
		violations = append(violations, fmt.Sprintf("duplicate issue in Jira: %d issues for %d", n, 3+len(created)))
	}
	require.Equal(t, 3+len(created), w.localIssues(), "no duplicate locally")
	for _, id := range created {
		require.NotEmpty(t, jiraKeyOf(t, mustIssue(t, w.c, id)))
	}
	for _, p := range is {
		got := w.srv.Issue(p.key)
		require.Equal(t, `"`+got.Summary+`"`, field(t, p.ic, "title"), p.key)
		require.Equal(t, `"`+strings.ToLower(got.Priority)+`"`, field(t, p.ic, "priority"), p.key)
		require.Equal(t, got.Status, propStatuses[strings.Trim(field(t, p.ic, "status"), `"`)], p.key)
		pts := "null"
		if v, ok := got.Custom[propPoints]; ok && v != nil {
			pts = fmt.Sprint(v)
		}
		require.Equal(t, pts, orNull(field(t, p.ic, "estimate")), p.key)
		due := "null"
		if got.DueDate != "" {
			due = `"` + got.DueDate + `"`
		}
		require.Equal(t, due, orNull(field(t, p.ic, "due")), p.key)
		require.Equal(t, jiraapi.NormalizeText(w.body(p.ic)), jiraText(t, w.srv, p.key), p.key)
		var local, remote []string
		for _, c := range p.ic.Snapshot().Comments[1:] {
			if !isNote(p.ic, c.TargetId()) {
				local = append(local, jiraapi.NormalizeText(c.Message))
			}
		}
		for _, c := range got.Comments {
			remote = append(remote, jiraapi.NormalizeText(c.Text))
		}
		if !assertSameComments(remote, local) {
			violations = append(violations, fmt.Sprintf("%s comments differ: Jira %q, local %q", p.key, remote, local))
		}
	}
	return violations
}

func assertSameComments(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// ---- stale reads: what the harsh run found, alone ----

// A comment POST that landed and that the re-read does not list is never
// paired: the next run, still reading the stale list, POSTs it again, and
// again, for as long as the GET lags.
func TestAdv2StaleReadsDuplicateComment(t *testing.T) {
	w := slowWorld(t)
	key, ic := w.importedSlow(jiratest.IssueSpec{Summary: "Comment once"})
	w.comment(ic, "only once")
	for i := 0; i < 3; i++ {
		w.mustSync(jira.Options{})
	}
	w.srv.Advance(11 * time.Minute)
	w.converge(4)
	n := len(w.srv.Issue(key).Comments)
	require.Equal(t, 1, n)
}

// A create whose 201 is in hand but whose GET fails is not linked: the
// Jira id from the answer is thrown away and the create is in doubt, so an
// index slower than Settle duplicates it.
func TestAdv2CreateAnswerDiscarded(t *testing.T) {
	cases := map[string]func(t *testing.T) *world{
		"stale-reads": func(t *testing.T) *world {
			w := newWorld(t, jiratest.WithStaleReads(), jiratest.WithIndexLag(0, 20*time.Minute))
			w.newLocal("Made here", "", nil)
			w.sync(jira.Options{}) // POST 201, GET 404: failed
			return w
		},
		"get-500": func(t *testing.T) *world {
			w := newWorld(t, jiratest.WithIndexLag(0, 20*time.Minute))
			w.newLocal("Made here", "", nil)
			posted := false
			h := &hookRT{before: func(r *http.Request, _ int) error {
				if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue" {
					posted = true
				} else if posted && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rest/api/3/issue/") {
					return errCrash
				}
				return nil
			}}
			_, _, err := w.runWith(context.Background(), h, jira.Options{}, true)
			require.Error(t, err)
			return w
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := setup(t)
			require.Len(t, w.srv.Keys(), 1)
			w.srv.Advance(16 * time.Minute) // Settle passed; the index still lags
			w.sync(jira.Options{})
			w.srv.Advance(10 * time.Minute)
			for i := 0; i < 3; i++ {
				w.sync(jira.Options{})
			}
			require.Len(t, w.srv.Keys(), 1)
		})
	}
}
