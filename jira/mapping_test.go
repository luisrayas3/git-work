package jira

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
)

func eid(c byte) entity.Id { return entity.Id(strings.Repeat(string(c), 64)) }

func sv(s string) issue.Value { return issue.StringValue(s) }

func items(vs ...string) issue.Value {
	var out []issue.Value
	for _, v := range vs {
		out = append(out, sv(v))
	}
	return sortedItems(out)
}

// convSite is the company site with a select and a text field on the story
// create screen, so that every kind JS7 maps has a field.
func convSite() jiratest.Site {
	site := jiratest.CompanySite()
	site.Fields = append(site.Fields,
		jiratest.CustomField{ID: "customfield_10050", Name: "Team", Kind: jiratest.KindOption,
			Options: []jiratest.SelectOption{{ID: "10100", Value: "Red"}, {ID: "10101", Value: "Blue"}}},
		jiratest.CustomField{ID: "customfield_10051", Name: "Code name", Kind: jiratest.KindString})
	types := site.Projects[0].IssueTypes
	for i := range types {
		if types[i].Name == "Story" {
			types[i].Fields = append(append([]string(nil), types[i].Fields...), "customfield_10050", "customfield_10051")
			types[i].CreateScreen = append(append([]string(nil), types[i].CreateScreen...), "customfield_10050", "customfield_10051")
		}
	}
	return site
}

type conv struct {
	t   *testing.T
	srv *jiratest.Server
	c   *jiraapi.Client
	p   *Project
	m   *Mapping
	ix  *Index
}

func newConv(t *testing.T) *conv {
	srv := jiratest.New(t, jiratest.WithSite(convSite()))
	email, token := srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetries: -1})
	p, err := Discover(context.Background(), c, "PROJ")
	require.NoError(t, err)
	s, _, _, _ := derived(t, starts(t)["preset"], p)
	m, _, err := Compile(s.schema(t), p)
	require.NoError(t, err)
	ix := IndexOf(nil, map[string]entity.Id{jiratest.MiaID: eid('1'), jiratest.RaviID: eid('2')}, []entity.Id{eid('e')})
	return &conv{t: t, srv: srv, c: c, p: p, m: m, ix: ix}
}

func (cv *conv) raw(key string) *jiraapi.Issue {
	ri, err := cv.c.GetIssue(context.Background(), key, cv.m.Request(), nil)
	require.NoError(cv.t, err)
	return ri
}

func (cv *conv) get(key string) Doc {
	cs, err := cv.c.Comments(context.Background(), key)
	require.NoError(cv.t, err)
	return cv.m.FromJira(cv.raw(key), cs, cv.ix)
}

// create posts Create's body and checks that every key it carried reads back
// byte-equal: Jira's normal form of our create is our value (P1).
func (cv *conv) create(local Doc, id entity.Id) string {
	t := cv.t
	body, keys, skips := cv.m.Create(local, id, cv.ix)
	require.Empty(t, skips)
	ref, err := cv.c.CreateIssue(context.Background(), body.Fields, body.Properties)
	require.NoError(t, err)
	cv.ix.AddIssue(ref.ID, id)
	remote := cv.get(ref.Key)
	require.Empty(t, remote.Skip)
	require.Equal(t, local.Type, remote.Type)
	for _, k := range keys {
		require.Equal(t, string(local.Fields[k]), string(remote.Fields[k]), k)
	}
	require.Equal(t, local.Body, remote.Body)
	return ref.Key
}

// apply is the engine's step 4 in miniature: one PUT, the transition, links.
func (cv *conv) apply(key string, ws []Write) {
	t, ctx := cv.t, context.Background()
	fields, update := map[string]any{}, map[string][]jiraapi.Op{}
	for _, w := range ws {
		switch w.Kind {
		case WriteEdit:
			if w.Set != nil {
				fields[w.Field] = w.Set
				continue
			}
			var ops []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(w.Update, &ops))
			for _, op := range ops {
				for verb, v := range op {
					update[w.Field] = append(update[w.Field], jiraapi.Op{Verb: verb, Value: v})
				}
			}
		case WriteTransition:
			trs, err := cv.c.Transitions(ctx, key)
			require.NoError(t, err)
			found := false
			for _, tr := range trs {
				if tr.To.ID == w.Status {
					require.NoError(t, cv.c.DoTransition(ctx, key, tr.ID, nil))
					found = true
					break
				}
			}
			require.True(t, found, "a transition to %s", w.Status)
		case WriteLink:
			for _, l := range w.Add {
				require.NoError(t, cv.c.CreateIssueLink(ctx, l.LinkType, l.Source, l.Destination))
			}
			for _, id := range w.Remove {
				require.NoError(t, cv.c.DeleteIssueLink(ctx, id))
			}
		}
	}
	if len(fields) > 0 || len(update) > 0 {
		require.NoError(t, cv.c.EditIssue(ctx, key, fields, update))
	}
}

// D11 and P1: every kind converts to Jira and back to the same canonical
// value, through the fake's normalisation, for a create and for edits.
func TestConversionRoundTrip(t *testing.T) {
	cv := newConv(t)
	epic := Doc{Type: "epic", Body: Text{Text: "The *big* one.\n\n- a\n- b", Lossless: true}, Fields: map[string]issue.Value{
		"title": sv("Checkout"), "type": sv("epic"), "priority": sv("high"), "labels": items("web", "Q3"),
		"due": sv("2026-10-01"), "start-date": sv("2026-09-01"), "assignee": sv(eid('2').String()),
	}}
	epicKey := cv.create(epic, eid('e'))

	story := Doc{Type: "story", Body: Text{Text: "", Lossless: true}, Fields: map[string]issue.Value{
		"title": sv("Guest checkout"), "type": sv("story"), "parent": sv(eid('e').String()),
		"estimate": issue.Value("3.5"), "labels": items("a", "b"), "assignee": sv(eid('1').String()),
		"team": sv("red"), "code-name": sv("Heron"), "due": sv("2026-11-30"),
	}}
	storyKey := cv.create(story, eid('s'))

	changes := []Change{
		{Key: "title", Set: sv("Guest checkout, v2")},
		{Key: "status", Set: sv("in-progress")},
		{Key: "priority", Set: sv("lowest")},
		{Key: "labels", Add: []issue.Value{sv("c")}, Remove: []issue.Value{sv("a")}},
		{Key: "estimate", Set: issue.Value("2.0")},
		{Key: "due", Set: null},
		{Key: "assignee", Set: null},
		{Key: "parent", Set: null},
		{Key: "blocks", Add: []issue.Value{sv(eid('e').String())}},
		{Key: "team", Set: sv("blue")},
		{Key: "code-name", Set: sv("Falcon")},
		{Key: "start-date", Set: sv("2026-10-02")},
	}
	ws, skips := cv.m.ToJira("story", changes, cv.raw(storyKey), cv.ix)
	require.Empty(t, skips)
	require.Len(t, ws, len(changes))
	cv.apply(storyKey, ws)

	after := cv.get(storyKey)
	require.Empty(t, after.Skip)
	for key, want := range map[string]issue.Value{
		"title": sv("Guest checkout, v2"), "status": sv("in-progress"), "priority": sv("lowest"),
		"labels": items("b", "c"), "estimate": issue.Value("2"), "due": null, "assignee": null,
		"parent": null, "blocks": items(eid('e').String()), "team": sv("blue"), "code-name": sv("Falcon"),
		"start-date": sv("2026-10-02"),
	} {
		require.Equal(t, string(want), string(after.Fields[key]), key)
	}
	// C1: the link is stored on its source only
	require.Equal(t, "[]", string(cv.get(epicKey).Fields["blocks"]))

	// converting Jira's value back and forth is a fixpoint
	var again []Change
	for _, key := range []string{"title", "priority", "estimate", "team", "code-name", "start-date"} {
		again = append(again, Change{Key: key, Set: after.Fields[key]})
	}
	ws, skips = cv.m.ToJira("story", again, cv.raw(storyKey), cv.ix)
	require.Empty(t, skips)
	cv.apply(storyKey, ws)
	require.Equal(t, after.Fields, cv.get(storyKey).Fields)

	ws, skips = cv.m.ToJira("story", []Change{{Key: "blocks", Remove: []issue.Value{sv(eid('e').String())}}}, cv.raw(storyKey), cv.ix)
	require.Empty(t, skips)
	require.Len(t, ws[0].Remove, 1)
	cv.apply(storyKey, ws)
	require.Equal(t, "[]", string(cv.get(storyKey).Fields["blocks"]))
}

// C1 from the other side: a link made in Jira's UI reads on its source.
func TestLinkDirection(t *testing.T) {
	cv := newConv(t)
	a := cv.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "A"})
	b := cv.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "B"})
	cv.ix.AddIssue(cv.raw(a).ID, eid('a'))
	cv.ix.AddIssue(cv.raw(b).ID, eid('b'))
	cv.srv.Link(a, "Blocks", b)
	require.Equal(t, string(items(eid('b').String())), string(cv.get(a).Fields["blocks"]))
	require.Equal(t, "[]", string(cv.get(b).Fields["blocks"]))
}

// JS17: what cannot convert now is a Skip; an out-of-project relation is dropped.
func TestFromJiraSkips(t *testing.T) {
	cv := newConv(t)
	ri := &jiraapi.Issue{ID: "20000", Key: "PROJ-99", Fields: map[string]json.RawMessage{
		"summary":   json.RawMessage(`"x"`),
		"issuetype": json.RawMessage(`{"id":"10002"}`),
		"status":    json.RawMessage(`{"id":"77","name":"Limbo"}`),
		"assignee":  json.RawMessage(`{"accountId":"nobody"}`),
		"parent":    json.RawMessage(`{"id":"30000","key":"OTHER-1"}`),
		"issuelinks": json.RawMessage(`[{"id":"1","type":{"id":"10000"},"outwardIssue":{"id":"30001","key":"PROJ-5"}},
			{"id":"2","type":{"id":"10003"},"outwardIssue":{"id":"30002","key":"ELSE-5"}}]`),
		"updated": json.RawMessage(`"2026-09-28T16:03:00.000+0200"`),
	}}
	doc := cv.m.FromJira(ri, nil, cv.ix)
	require.Equal(t, "task", doc.Type)
	require.Equal(t, "2026-09-28T14:03:00Z", doc.Updated.Format("2006-01-02T15:04:05Z07:00"))
	skipped := map[string]bool{}
	for _, s := range doc.Skip {
		require.True(t, s.Retry, s.Key)
		skipped[s.Key] = true
	}
	require.Equal(t, map[string]bool{"status": true, "assignee": true, "blocks": true}, skipped)
	_, ok := doc.Fields["parent"]
	require.False(t, ok, "an out-of-project parent is not in the merge at all")
	require.Equal(t, "[]", string(doc.Fields["relates-to"]), "an out-of-project link item is dropped")

	ri.Fields["issuetype"] = json.RawMessage(`{"id":"99999"}`)
	require.Equal(t, "", cv.m.FromJira(ri, nil, cv.ix).Type, "an unmapped type is skipped by the engine")
}

// JS17: a local value Jira cannot hold is a Skip; one about to exist, a Retry.
func TestToJiraSkips(t *testing.T) {
	cv := newConv(t)
	ws, skips := cv.m.ToJira("task", []Change{
		{Key: "title", Set: sv(strings.Repeat("é", 256))},
		{Key: "type", Set: sv("bug")},
		{Key: "status", Set: sv("in-review")},
		{Key: "labels", Add: []issue.Value{sv("two words")}},
		{Key: "assignee", Set: sv(eid('9').String())},
		{Key: "parent", Set: sv(eid('e').String())},
		{Key: "rank", Set: sv("0|a")},
	}, nil, cv.ix)
	require.Empty(t, ws)
	retry := map[string]bool{}
	for _, s := range skips {
		retry[s.Key] = s.Retry
	}
	require.Equal(t, map[string]bool{"title": false, "type": false, "status": false, "labels": false,
		"assignee": false, "parent": true, "rank": false}, retry)

	_, ok := cv.m.IssueType("iteration")
	require.False(t, ok, "a local-only type is never exported")
	_, skips = cv.m.ToJira("iteration", []Change{{Key: "title", Set: sv("x")}}, nil, cv.ix)
	require.Len(t, skips, 1)
}

// JS7: datetime custom fields are RFC 3339 in UTC locally, whatever offset Jira renders.
func TestDatetime(t *testing.T) {
	fm := &fieldMap{key: "at", ref: "customfield_1", kind: "date", jira: jiraapi.FieldSchema{Type: "datetime"}}
	tm := &typeMap{key: "task", fields: map[string]*fieldMap{"at": fm}, keys: []string{"title", "type", "at"}}
	m := &Mapping{project: "PROJ", types: map[string]*typeMap{"task": tm}}
	ri := &jiraapi.Issue{Fields: map[string]json.RawMessage{"customfield_1": json.RawMessage(`"2026-09-28T16:03:00.000+0200"`)}}
	v, skip, _ := m.fromJira(ri, fm, cv0())
	require.Nil(t, skip)
	require.Equal(t, `"2026-09-28T14:03:00Z"`, string(v))

	w, skip := m.toJira(tm, Change{Key: "at", Set: v}, nil, cv0())
	require.Nil(t, skip)
	require.Equal(t, `"2026-09-28T14:03:00.000+0000"`, string(w.Set))
	ri.Fields["customfield_1"] = w.Set
	back, _, _ := m.fromJira(ri, fm, cv0())
	require.Equal(t, string(v), string(back))
}

func cv0() *Index { return IndexOf(nil, nil, nil) }

// JS16: every account an issue and its comments name, once, in order.
func TestUsers(t *testing.T) {
	cv := newConv(t)
	ri := &jiraapi.Issue{Fields: map[string]json.RawMessage{
		"issuetype": json.RawMessage(`{"id":"10002"}`),
		"reporter":  json.RawMessage(`{"accountId":"r"}`),
		"creator":   json.RawMessage(`{"accountId":"r"}`),
		"assignee":  json.RawMessage(`{"accountId":"a"}`),
	}}
	cs := []jiraapi.Comment{{Author: &jiraapi.User{AccountID: "c"}, UpdateAuthor: &jiraapi.User{AccountID: "a"}}}
	var got []string
	for _, u := range cv.m.Users(ri, cs) {
		got = append(got, u.AccountID)
	}
	require.Equal(t, []string{"r", "a", "c"}, got)
}
