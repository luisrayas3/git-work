package jira_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
)

// ---- orphaned exports ----

func days(n int) *time.Duration {
	d := time.Duration(n) * 24 * time.Hour
	return &d
}

func lineOf(lines []jira.Line, jiraKey string) (jira.Line, bool) {
	for _, l := range lines {
		if l.Jira == jiraKey {
			return l, true
		}
	}
	return jira.Line{}, false
}

// orphaned is an epic A exported and never pushed, and a child of it made
// in Jira: to B the epic is an orphan and the child waits on it.
func orphaned(t *testing.T) (a, b *world, pid entity.Id, pkey, ckey string) {
	a, b = twoClones(t)
	pid = a.newLocal("Parent", "", map[string]issue.Value{"type": str("epic")})
	a.mustSync(jira.Options{})
	pkey = jiraKeyOf(t, mustIssue(t, a.c, pid))
	require.NotEmpty(t, pkey)
	ckey = a.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Child", Parent: pkey})
	return a, b, pid, pkey, ckey
}

// E26: an orphan is skipped under the bound and remembered, its child names
// the cause every run, and the bound adopts it, child resolved in the same run.
func TestOrphanSkippedThenAdopted(t *testing.T) {
	_, b, pid, pkey, ckey := orphaned(t)

	lines, sum := b.mustSync(jira.Options{})
	skip, ok := lineOf(lines, pkey)
	require.True(t, ok, "%+v", lines)
	require.Equal(t, jira.ActionSkipped, skip.Action)
	require.Contains(t, skip.Pending[0].Reason, "from issue "+pid.Human()+", which this clone has not pulled")
	require.Contains(t, skip.Pending[0].Reason, "--adopt 0d")
	require.Equal(t, 1, sum.Orphans)
	child, ok := lineOf(lines, ckey)
	require.True(t, ok)
	require.Equal(t, jira.ActionImported, child.Action)
	require.Len(t, child.Pending, 1)
	require.Contains(t, child.Pending[0].Reason, pkey+" is not imported yet: created in Jira on")
	require.Contains(t, child.Pending[0].Reason, "from issue "+pid.Human())

	// the next run is incremental: the orphan is no hit, the child still says why
	lines, sum = b.mustSync(jira.Options{})
	child, ok = lineOf(lines, ckey)
	require.True(t, ok, "%+v", lines)
	require.Contains(t, child.Pending[0].Reason, "from issue "+pid.Human())
	require.Equal(t, 1, sum.Orphans)

	// under the bound: still skipped, under --full too
	b.srv.Advance(3 * 24 * time.Hour)
	lines, sum = b.mustSync(jira.Options{Full: true, Adopt: days(7)})
	skip, ok = lineOf(lines, pkey)
	require.True(t, ok, "%+v", lines)
	require.Equal(t, jira.ActionSkipped, skip.Action)
	require.Contains(t, skip.Pending[0].Reason, "--adopt 3d")
	require.Zero(t, sum.Adopted)
	require.Equal(t, 1, sum.Orphans)

	// past it: adopted, and the child resolves in the same run
	b.srv.Advance(5 * 24 * time.Hour)
	lines, sum = b.mustSync(jira.Options{Full: true, Adopt: days(7)})
	got, ok := lineOf(lines, pkey)
	require.True(t, ok, "%+v", lines)
	require.Equal(t, jira.ActionImported, got.Action)
	require.Equal(t, pid, got.Adopted)
	require.Equal(t, 1, sum.Adopted)
	require.Zero(t, sum.Orphans)
	y := b.byKey(pkey)
	require.Equal(t, `"`+y.Id().String()+`"`, field(t, b.byKey(ckey), "parent"))
	b.still()
}

// E27: the original arrives after the adoption: it reached Jira first, so
// the adopted copy is consolidated into it, whatever a person archived,
// its local-only value carried over and its child re-pointed.
func TestOrphanConsolidatedWhenOriginalArrives(t *testing.T) {
	a, b, pid, pkey, ckey := orphaned(t)
	b.srv.Advance(8 * 24 * time.Hour)
	b.mustSync(jira.Options{Adopt: days(0)})
	y := b.byKey(pkey)
	require.NotEqual(t, pid, y.Id())
	b.set(y.Id(), "rank", str("0|aaa:"))
	require.Equal(t, `"`+y.Id().String()+`"`, field(t, b.byKey(ckey), "parent"))

	_, err := a.c.Push("origin")
	require.NoError(t, err)
	require.NoError(t, b.c.Pull("origin"))
	x := mustIssue(t, b.c, pid)
	b.set(x.Id(), "archived", issue.Value("true")) // a person's archive does not choose the copy

	b.srv.ResetRequests()
	before := b.refs()
	lines, sum, err := b.sync(jira.Options{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, sum.Consolidated, "%+v", lines)
	require.Equal(t, before, b.refs(), "a dry run writes nothing")
	require.Zero(t, b.srv.Writes())

	lines, sum = b.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Consolidated, "%+v", lines)
	l, ok := lineOf(lines, pkey)
	require.True(t, ok)
	require.Equal(t, jira.ActionConsolidated, l.Action)
	require.Equal(t, y.Id(), l.Issue)
	require.Contains(t, l.Imported, "rank")
	require.Equal(t, `true`, field(t, y, "archived"))
	require.Equal(t, `"0|aaa:"`, field(t, x, "rank"))
	require.Len(t, notes(x, jira.NoteConsolidated), 1)
	require.Equal(t, `"`+pid.String()+`"`, field(t, b.byKey(ckey), "parent"))

	b.mustSync(jira.Options{}) // the child's merge: l == r, its base moves
	b.still()
}

// E28: the original arrives unlinked, its export having crashed after the
// POST: it still reached Jira first, so the adopted copy is consolidated
// into it and it links.
func TestOrphanUnlinkedOriginalWins(t *testing.T) {
	a, b := twoClones(t)
	pid := a.newLocal("Parent", "", map[string]issue.Value{"type": str("epic")})
	crash := &hookRT{after: func(r *http.Request, n int) error {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issue") {
			return errCrash
		}
		return nil
	}}
	_, _, err := a.runWith(context.Background(), crash, jira.Options{}, true)
	require.True(t, err == nil || errors.Is(err, errCrash), "%v", err)
	require.Empty(t, jiraKeyOf(t, mustIssue(t, a.c, pid)), "unlinked")
	require.Len(t, a.srv.Keys(), 1, "created in Jira")
	pkey := a.srv.Keys()[0]

	b.srv.Advance(8 * 24 * time.Hour)
	_, sum := b.mustSync(jira.Options{Adopt: days(7)})
	require.Equal(t, 1, sum.Adopted)
	y := b.byKey(pkey)

	_, err = a.c.Push("origin")
	require.NoError(t, err)
	require.NoError(t, b.c.Pull("origin"))
	lines, sum := b.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Consolidated, "%+v", lines)
	x := mustIssue(t, b.c, pid)
	require.Equal(t, pkey, jiraKeyOf(t, x), "the original links")
	require.Equal(t, `true`, field(t, y, "archived"))
	b.still()
}
