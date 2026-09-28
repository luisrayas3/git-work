package jira_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
	"github.com/git-bug/git-bug/repository"
)

func str(s string) issue.Value { return issue.StringValue(s) }

// imported is a Jira task, synced once.
func (w *world) imported(summary string) (string, *cache.IssueCache) {
	w.t.Helper()
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: summary, Description: "the body"})
	w.mustSync(jira.Options{})
	return key, w.byKey(key)
}

func notes(ic *cache.IssueCache, kind string) []string {
	var out []string
	snap := ic.Snapshot()
	for _, c := range snap.Comments[1:] {
		for _, op := range snap.Operations {
			if op.Id() == c.TargetId() {
				if v, _ := op.GetMetadata(jira.MetaNote); v == kind {
					out = append(out, c.Message)
				}
			}
		}
	}
	return out
}

// E2: a Jira-only edit is imported, and importing it echoes nothing back.
func TestJiraEditImported(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Imported")
	w.srv.Edit(key, map[string]any{"summary": "Renamed in Jira", "labels": []string{"ui"}})
	w.srv.Transition(key, "In Progress")
	lines, _ := w.mustSync(jira.Options{})
	require.Len(t, lines, 1)
	require.Equal(t, jira.ActionUpdated, lines[0].Action)
	require.Equal(t, `"Renamed in Jira"`, field(t, ic, "title"))
	require.Equal(t, `"in-progress"`, field(t, ic, "status"))
	require.Equal(t, `["ui"]`, field(t, ic, "labels"))
	w.quiet()
}

// E1: a local-only edit is exported, and the next run writes nothing.
func TestLocalEditExported(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Exported")
	w.set(ic.Id(), "title", str("Renamed locally"))
	w.set(ic.Id(), "priority", str("high"))
	w.set(ic.Id(), "status", str("in-progress"))
	lines, _ := w.mustSync(jira.Options{})
	require.Len(t, lines, 1)
	require.Contains(t, lines[0].Exported, "title")
	require.Empty(t, lines[0].Pending)
	got := w.srv.Issue(key)
	require.Equal(t, "Renamed locally", got.Summary)
	require.Equal(t, "High", got.Priority)
	require.Equal(t, "In Progress", got.Status)
	w.quiet()
}

// A double edit takes Jira's value, notes it, and loses nothing.
func TestDoubleEditJiraWins(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Both")
	w.set(ic.Id(), "priority", str("low"))
	w.srv.Edit(key, map[string]any{"priority": map[string]string{"name": "Highest"}})
	lines, sum := w.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Conflicts)
	require.Equal(t, "priority", lines[0].Conflicts[0].Key)
	require.Equal(t, `"highest"`, field(t, ic, "priority"))
	require.Equal(t, "Highest", w.srv.Issue(key).Priority, "Jira untouched")
	n := notes(ic, jira.NoteConflict)
	require.Len(t, n, 1)
	require.Contains(t, n[0], `priority: local "low" -> Jira "highest"`)

	// the overridden value is still in the log
	lost := false
	for _, op := range ic.Snapshot().Operations {
		if sf, ok := op.(*issue.SetFieldOperation); ok && sf.Key == "priority" && string(sf.Value) == `"low"` {
			lost = true
		}
	}
	require.True(t, lost)
	w.quiet()
}

// E15: the overlap re-returns every synced issue; none is read again.
func TestOverlapReadsNothing(t *testing.T) {
	w := newWorld(t)
	w.imported("One")
	w.imported("Two")
	w.quiet()
	for _, r := range w.srv.Requests() {
		require.NotRegexp(t, `^/rest/api/3/issue/(PROJ-)?\d+`, r.Path, "no GET for a skipped hit")
	}
}

// E9: a new local issue is created in Jira with its fields, its status
// written in the same run, the link and alias on its create op, and Jira's
// default priority imported rather than cleared.
func TestLocalIssueCreated(t *testing.T) {
	w := newWorld(t)
	parentKey := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "The parent epic"})
	w.mustSync(jira.Options{})
	id := w.newLocal("Made here", "a local body", map[string]issue.Value{
		"status": str("in-progress"), "labels": issue.ItemsValue([]issue.Value{str("x")}),
		"parent": str(w.byKey(parentKey).Id().String()),
	})
	lines, sum := w.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Created, "%+v", lines)
	ic, err := w.c.Issues().Resolve(id)
	require.NoError(t, err)
	alias, ok := ic.Snapshot().GetCreateMetadata(jira.MetaAlias)
	require.True(t, ok)
	jid, _ := ic.Snapshot().GetCreateMetadata(jira.MetaId)
	require.NotEmpty(t, jid)
	require.Equal(t, id, w.byKey(alias).Id(), "resolvable by key")

	got := w.srv.Issue(alias)
	require.Equal(t, "Made here", got.Summary)
	require.Equal(t, "In Progress", got.Status)
	require.Equal(t, []string{"x"}, got.Labels)
	require.Equal(t, parentKey, got.Parent)
	require.Equal(t, `"medium"`, field(t, ic, "priority"), "Jira's default imported")
	w.quiet()
}

// E10: a Jira issue is imported by its reporter at its created time, its
// comments by their authors; a child seen before its parent resolves in the
// same run.
func TestJiraIssueImported(t *testing.T) {
	w := newWorld(t)
	epic := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "Epic"})
	child := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Story", Summary: "Child", Parent: epic})
	w.srv.Edit(epic, map[string]any{"summary": "Epic, later"}) // the child now sorts first
	cid := w.srv.AddComment(child, "a comment from Ravi")
	require.NotEmpty(t, cid)

	w.mustSync(jira.Options{})
	ic := w.byKey(child)
	snap := ic.Snapshot()
	require.Equal(t, "Ravi Patel", snap.Author.Name())
	require.Equal(t, w.srv.Issue(child).Created.Unix(), snap.CreateTime.Unix())
	require.Len(t, snap.Comments, 2)
	require.Equal(t, "a comment from Ravi", snap.Comments[1].Message)
	require.Equal(t, "Ravi Patel", snap.Comments[1].Author.Name())
	require.Equal(t, `"`+w.byKey(epic).Id().String()+`"`, field(t, ic, "parent"))
	w.quiet()
}

// Comments both ways, edits both ways, and a Jira delete as a tombstone.
func TestComments(t *testing.T) {
	w := newWorld(t, jiratest.WithCommentBumps(true, true))
	key, ic := w.imported("Talk")
	fromJira := w.srv.AddComment(key, "from Jira")
	_, _, err := ic.AddComment("from here")
	require.NoError(t, err)
	require.NoError(t, ic.Commit())
	w.mustSync(jira.Options{})
	require.Len(t, ic.Snapshot().Comments, 3)
	cs := w.srv.Issue(key).Comments
	require.Len(t, cs, 2)
	require.Equal(t, "from here", cs[1].Text)
	w.quiet()

	// edits both ways
	w.srv.EditComment(key, fromJira, "from Jira, edited")
	local := ic.Snapshot().Comments[1]
	_, err = ic.EditComment(local.CombinedId(), "from here, edited")
	require.NoError(t, err)
	require.NoError(t, ic.Commit())
	w.mustSync(jira.Options{})
	require.Equal(t, "from Jira, edited", ic.Snapshot().Comments[2].Message)
	require.Equal(t, "from here, edited", w.srv.Issue(key).Comments[1].Text)
	w.quiet()

	// a Jira delete tombstones the local copy
	w.srv.DeleteComment(key, fromJira)
	w.mustSync(jira.Options{})
	require.True(t, strings.HasPrefix(ic.Snapshot().Comments[2].Message, jira.TombstonePrefix))
	require.Len(t, w.srv.Issue(key).Comments, 1, "never re-exported")
	w.quiet()
}

// E4: a crash after the Jira writes and before the local commit. The next
// run finds Jira already holding the local values: converged, no duplicate.
func TestCrashAfterJiraWrites(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Crash")
	w.set(ic.Id(), "title", str("Written, then crashed"))
	_, _, err := ic.AddComment("posted, then crashed")
	require.NoError(t, err)
	require.NoError(t, ic.Commit())
	// what step 4 did before the crash, as the token's account
	mia := w.srv.As(jiratest.MiaID)
	mia.Edit(key, map[string]any{"summary": "Written, then crashed"})
	mia.AddComment(key, "posted, then crashed")

	w.srv.ResetRequests()
	w.mustSync(jira.Options{})
	require.Zero(t, w.srv.Writes(), "nothing written twice")
	require.Len(t, w.srv.Issue(key).Comments, 1)
	snap := ic.Snapshot()
	require.Len(t, snap.Comments, 2, "the comment paired, not imported")
	w.quiet()
}

// E5: a crash after POST /issue, before the commit, with the index lagging:
// the created issue is linked by its property, never created twice.
func TestCrashAfterCreate(t *testing.T) {
	w := newWorld(t, jiratest.WithIndexLag(1, 0))
	id := w.newLocal("Created, then crashed", "", nil)
	mia := w.srv.As(jiratest.MiaID)
	key := mia.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Created, then crashed"})
	mia.SetProperty(key, jira.PropertyKey, map[string]string{"id": id.String()})
	st, err := jira.LoadState(w.c.LocalStorage())
	require.NoError(t, err)
	st.Bind(w.srv.URL(), "PROJ")
	st.Creating[id] = w.srv.Now()
	require.NoError(t, st.Save(w.c.LocalStorage()))

	// inside Overlap and lagging: neither found nor created again
	w.mustSync(jira.Options{})
	require.Len(t, w.srv.Keys(), 1)
	// the index caught up: linked
	w.mustSync(jira.Options{})
	require.Len(t, w.srv.Keys(), 1)
	ic, err := w.c.Issues().Resolve(id)
	require.NoError(t, err)
	alias, _ := ic.Snapshot().GetCreateMetadata(jira.MetaAlias)
	require.Equal(t, key, alias)
	w.quiet()
}

// E6: no transition to the target status: pending with the reason, the
// other keys synced.
func TestTransitionUnavailable(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Stuck")
	w.set(ic.Id(), "status", str("done")) // To Do -> Done has no transition for a task
	w.set(ic.Id(), "priority", str("low"))
	lines, sum := w.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Pending)
	require.Equal(t, "status", lines[0].Pending[0].Key)
	require.Contains(t, lines[0].Pending[0].Reason, "no transition")
	require.Equal(t, "Low", w.srv.Issue(key).Priority)
	require.Equal(t, "To Do", w.srv.Issue(key).Status)
	require.Equal(t, `"done"`, field(t, ic, "status"), "kept locally")

	// it stays pending every run
	lines, _ = w.mustSync(jira.Options{})
	require.Len(t, lines, 1)
	require.Equal(t, "status", lines[0].Pending[0].Key)
}

// E12: a Jira delete is found under --full only, and marks the issue gone.
func TestJiraDeleteGone(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Doomed")
	w.srv.Delete(key)
	w.quiet() // incremental runs do not see a delete
	lines, sum := w.mustSync(jira.Options{Full: true})
	require.Equal(t, 1, sum.Gone, "%+v", lines)
	require.Equal(t, `"canceled"`, field(t, ic, "status"))
	require.Len(t, notes(ic, jira.NoteDeleted), 1)
	b, _ := jira.CurrentBase(ic.Snapshot())
	require.Equal(t, jira.GoneDeleted, b.Gone)

	// never exported
	w.set(ic.Id(), "title", str("edited after"))
	w.srv.ResetRequests()
	lines, _ = w.mustSync(jira.Options{})
	require.Zero(t, w.srv.Writes())
	require.Len(t, lines, 1)
	require.NotEmpty(t, lines[0].Pending)
}

func TestMassDeleteHeld(t *testing.T) {
	w := newWorld(t)
	var keys []string
	for i := 0; i < 3; i++ {
		keys = append(keys, w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Doomed"}))
	}
	w.mustSync(jira.Options{})
	for _, k := range keys {
		w.srv.Delete(k)
	}
	_, _, err := w.sync(jira.Options{Full: true, MaxDeletes: 2})
	require.ErrorIs(t, err, jira.ErrDeletesHeld)
	require.Equal(t, `"to-do"`, field(t, w.byKey(keys[0]), "status"), "none marked")
	_, sum := w.mustSync(jira.Options{Full: true, MaxDeletes: 2, AcceptDeletes: true})
	require.Equal(t, 3, sum.Gone)
}

// E13: a move out of the project is gone: moved.
func TestMovedGone(t *testing.T) {
	w := newWorld(t)
	w.srv.Seed(jiratest.Project{ID: "10099", Key: "OTHER", Name: "Other",
		IssueTypes: []jiratest.IssueType{{ID: "10002", Name: "Task", Workflow: jiratest.Workflow{Initial: "10000"}}},
		Statuses:   []jiratest.Status{{ID: "10000", Name: "To Do", Category: jiratest.CategoryNew}}})
	key, ic := w.imported("Moving")
	w.srv.Move(key, "OTHER")
	_, sum := w.mustSync(jira.Options{Full: true})
	require.Equal(t, 1, sum.Gone)
	b, _ := jira.CurrentBase(ic.Snapshot())
	require.Equal(t, jira.GoneMoved, b.Gone)
}

// 429s are waited out and retried by the client.
func TestRateLimited(t *testing.T) {
	w := newWorld(t, jiratest.WithRetryAfter(0))
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Busy"})
	w.srv.RateLimitNext(2)
	w.mustSync(jira.Options{})
	w.byKey(key)
}

// E18: --dry-run writes nothing anywhere.
func TestDryRun(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Dry")
	w.set(ic.Id(), "title", str("local"))
	w.srv.Edit(key, map[string]any{"labels": []string{"remote"}})
	w.newLocal("New here", "", nil)
	w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "New there"})

	before := w.refs()
	st, _ := jira.LoadState(w.c.LocalStorage())
	w.srv.ResetRequests()
	lines, _ := w.mustSync(jira.Options{DryRun: true})
	require.NotEmpty(t, lines)
	for _, l := range lines {
		require.True(t, l.DryRun)
	}
	require.Zero(t, w.srv.Writes())
	require.Equal(t, before, w.refs())
	after, _ := jira.LoadState(w.c.LocalStorage())
	require.Equal(t, st, after)
}

// E19: ID... syncs only those issues, with no search.
func TestIds(t *testing.T) {
	w := newWorld(t)
	key, ic := w.imported("Chosen")
	other, oc := w.imported("Not chosen")
	w.srv.Edit(key, map[string]any{"summary": "Chosen, edited"})
	w.srv.Edit(other, map[string]any{"summary": "Not chosen, edited"})
	w.srv.ResetRequests()
	w.mustSync(jira.Options{Ids: []entity.Id{ic.Id()}})
	require.Equal(t, `"Chosen, edited"`, field(t, ic, "title"))
	require.Equal(t, `"Not chosen"`, field(t, oc, "title"))
	for _, r := range w.srv.Requests() {
		require.NotContains(t, r.Path, "/search/jql")
	}
}

// E21: a local issue created with aliases: {jira: KEY} is linked, not
// duplicated; an alias naming no Jira issue is skipped, never created.
func TestLinkRequests(t *testing.T) {
	w := newWorld(t)
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Both sides", Priority: "High"})
	ic, _, err := w.c.Issues().NewWithMetadata("Both sides, local", "", map[string]issue.Value{
		"type": str("task"), "priority": str("low"),
	}, map[string]string{jira.MetaAlias: key})
	require.NoError(t, err)
	dead, _, err := w.c.Issues().NewWithMetadata("Dead alias", "", map[string]issue.Value{"type": str("task")},
		map[string]string{jira.MetaAlias: "PROJ-999"})
	require.NoError(t, err)

	lines, sum := w.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Linked, "%+v", lines)
	require.Len(t, w.srv.Keys(), 1, "nothing created")
	require.Equal(t, `"Both sides"`, field(t, ic, "title"), "Jira wins with no base")
	require.Equal(t, `"high"`, field(t, ic, "priority"))
	require.Len(t, notes(ic, jira.NoteConflict), 1)
	_, linked := dead.Snapshot().GetCreateMetadata(jira.MetaId)
	require.False(t, linked)
	w.mustSync(jira.Options{})
	require.Len(t, w.srv.Keys(), 1)
}

// E14: two clones on one site, exchanging through a bare remote, converge:
// B edits before pulling A's import of a Jira edit, and both values survive.
//
// Each side diverges by one commit before a merge: entity/dag (pristine)
// misreads a merged history whose branches have unequal lengths ("creation
// lamport time not set"), independently of the sync.
func TestTwoClones(t *testing.T) {
	a := newWorld(t)
	key, _ := a.imported("Shared")
	remote := repository.CreateGoGitTestRepo(t, true)
	require.NoError(t, a.repo.AddRemote("origin", remote.GetLocalRemote()))
	_, err := a.c.Push("origin")
	require.NoError(t, err)

	brepo := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, brepo.AddRemote("origin", remote.GetLocalRemote()))
	b := newClone(t, a.srv, brepo, false)
	require.NoError(t, b.c.Pull("origin"))
	b.quiet() // B reads A's markers as its base: nothing to do

	a.srv.Edit(key, map[string]any{"summary": "Edited in Jira"})
	a.mustSync(jira.Options{})
	_, err = a.c.Push("origin")
	require.NoError(t, err)

	b.set(b.byKey(key).Id(), "priority", str("low"))
	require.NoError(t, b.c.Pull("origin")) // one commit each side
	b.mustSync(jira.Options{})
	require.Equal(t, "Low", a.srv.Issue(key).Priority)
	_, err = b.c.Push("origin")
	require.NoError(t, err)

	require.NoError(t, a.c.Pull("origin"))
	ia := a.byKey(key)
	require.Equal(t, `"Edited in Jira"`, field(t, ia, "title"))
	require.Equal(t, `"low"`, field(t, ia, "priority"))
	a.quiet()
	b.quiet()
}

// E22: a run whose credential /myself refuses writes nothing.
func TestBadCredential(t *testing.T) {
	w := newWorld(t)
	t.Setenv("JIRA_API_TOKEN", "wrong")
	w.srv.ResetRequests()
	_, _, err := w.sync(jira.Options{})
	require.Error(t, err)
	require.Zero(t, w.srv.Writes())
	for _, r := range w.srv.Requests() {
		require.NotContains(t, r.Path, "search")
	}
}

// JS12: a crash after POST …/comment, before the commit. The comment's
// git-work property names the local op, so the next run pairs it and posts
// nothing; with the property unreturned, the token's authorship and the
// text pair it all the same.
func TestCrashAfterCommentPost(t *testing.T) {
	for _, props := range []bool{true, false} {
		w := newWorld(t, jiratest.WithCommentProperties(props))
		key, ic := w.imported("Crash")
		_, op, err := ic.AddComment("posted, then crashed")
		require.NoError(t, err)
		require.NoError(t, ic.Commit())

		email, token := w.srv.Credentials()
		c := jiraapi.New(jiraapi.Config{BaseURL: w.srv.URL(), Email: email, Token: token})
		_, err = c.AddComment(context.Background(), key, jiraapi.TextToADF("posted, then crashed"),
			jiraapi.Property{Key: jira.PropertyKey, Value: map[string]string{"op": op.Id().String()}})
		require.NoError(t, err)

		w.srv.ResetRequests()
		w.mustSync(jira.Options{})
		require.Zero(t, w.srv.Writes(), "properties %v: nothing posted twice", props)
		require.Len(t, w.srv.Issue(key).Comments, 1)
		require.Len(t, ic.Snapshot().Comments, 2, "paired, not imported")
		w.quiet()
	}
}

// E16: 429s past the client's retries stop the run, and the cursor does not
// pass the issue it stopped on.
func TestRateLimitExhausted(t *testing.T) {
	w := newWorld(t, jiratest.WithRetryAfter(0))
	w.imported("First")
	st, err := jira.LoadState(w.c.LocalStorage())
	require.NoError(t, err)
	cursor := st.Cursor
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Unreached"})
	w.srv.RateLimitNext(1000)
	_, _, err = w.sync(jira.Options{})
	require.Error(t, err)
	w.srv.RateLimitNext(0)
	st, err = jira.LoadState(w.c.LocalStorage())
	require.NoError(t, err)
	require.False(t, st.Cursor.After(cursor))
	w.mustSync(jira.Options{})
	w.byKey(key)
}

// A10: a second run while one holds the sync lock does nothing and says so;
// two runs would both POST one new issue.
func TestConcurrentRunRefused(t *testing.T) {
	w := newWorld(t)
	w.newLocal("Once", "", nil)
	path := filepath.Join(w.c.LocalStorage().Root(), "jira", "sync.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	held := flock.New(path)
	ok, err := held.TryLock()
	require.NoError(t, err)
	require.True(t, ok)
	_, _, err = w.sync(jira.Options{})
	require.ErrorContains(t, err, "already running")
	require.Empty(t, w.srv.Keys())
	_, _, err = w.sync(jira.Options{DryRun: true})
	require.NoError(t, err, "a dry run takes no lock")
	require.NoError(t, held.Unlock())
	w.mustSync(jira.Options{})
	require.Len(t, w.srv.Keys(), 1)
}
