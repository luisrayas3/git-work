package jira_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
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

// ---- re-pointing (repoint.md) ----

// twoCopies is E27's store before its consolidating run, with four
// children under the adopted copy y: two Sub-tasks, whose parent the policy
// refuses (an epic), and two Tasks. x is the original, the winner.
func twoCopies(t *testing.T) (b *world, x, y entity.Id, kids []string) {
	a, b := twoClones(t, jiratest.WithLooseHierarchy())
	x = a.newLocal("Parent", "", map[string]issue.Value{"type": str("epic")})
	a.mustSync(jira.Options{})
	pkey := jiraKeyOf(t, mustIssue(t, a.c, x))
	for i, typ := range []string{"Sub-task", "Task", "Sub-task", "Task"} {
		kids = append(kids, a.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: typ, Summary: fmt.Sprintf("Child %d", i), Parent: pkey}))
	}
	b.srv.Advance(8 * 24 * time.Hour)
	b.mustSync(jira.Options{Adopt: days(0)})
	y = b.byKey(pkey).Id()
	require.NotEqual(t, x, y)
	for _, k := range kids {
		require.Equal(t, `"`+y.String()+`"`, field(t, b.byKey(k), "parent"))
	}
	_, err := a.c.Push("origin")
	require.NoError(t, err)
	require.NoError(t, b.c.Pull("origin"))
	return b, x, y, kids
}

// shapeSet writes a field under the shape check only, as another clone's
// pull would have.
func shapeSet(t *testing.T, w *world, ic *cache.IssueCache, key string, v issue.Value) {
	t.Helper()
	me, err := w.c.GetUserIdentity()
	require.NoError(t, err)
	require.NoError(t, ic.UpdateShape(func(*issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{issue.NewSetFieldOp(me, time.Now().Unix(), key, v)}, nil
	}))
}

func repointed(lines []jira.Line) []jira.Line {
	var out []jira.Line
	for _, l := range lines {
		if l.Action == jira.ActionRepointed {
			out = append(out, l)
		}
	}
	return out
}

func repointedOf(t *testing.T, lines []jira.Line, key string) jira.Line {
	t.Helper()
	for _, l := range repointed(lines) {
		if l.Jira == key {
			return l
		}
	}
	require.Failf(t, "no repointed line", "%s in %+v", key, lines)
	return jira.Line{}
}

// E29: a child whose parent the policy refuses is re-pointed under the
// shape check and reported off-schema; the loser is consolidated and every
// child after it is re-pointed too.
func TestRepointOffSchema(t *testing.T) {
	b, x, y, kids := twoCopies(t)
	lines, sum := b.mustSync(jira.Options{})
	require.Equal(t, 1, sum.Consolidated, "%+v", lines)
	require.Equal(t, len(kids), sum.Repointed, "%+v", lines)
	require.Equal(t, `true`, field(t, mustIssue(t, b.c, y), "archived"))
	for i, k := range kids {
		require.Equal(t, `"`+x.String()+`"`, field(t, b.byKey(k), "parent"))
		l := repointedOf(t, lines, k)
		require.Equal(t, `"`+x.String()+`"`, string(l.Imported["parent"]))
		if i%2 == 0 { // the Sub-tasks
			require.Len(t, l.OffSchema, 1, "%+v", l)
			require.Equal(t, "parent", l.OffSchema[0].Key)
			require.Contains(t, l.OffSchema[0].Reason, "is a epic")
		} else {
			require.Empty(t, l.OffSchema)
		}
	}
	b.mustSync(jira.Options{})
	b.still()
}

// E30: an archived loser and a stale relation, no loser in scan: the sweep
// re-points it, and the next run writes nothing.
func TestRepointSweep(t *testing.T) {
	b, x, y, kids := twoCopies(t)
	b.mustSync(jira.Options{})
	b.mustSync(jira.Options{})
	b.still()
	// a re-point that never happened: another clone's, or an older binary's
	shapeSet(t, b, b.byKey(kids[1]), "parent", str(y.String()))

	lines, sum := b.mustSync(jira.Options{})
	require.Zero(t, sum.Consolidated)
	require.Equal(t, 1, sum.Repointed, "%+v", lines)
	l := repointedOf(t, lines, kids[1])
	require.Contains(t, l.Imported, "parent")
	require.Equal(t, `"`+x.String()+`"`, field(t, b.byKey(kids[1]), "parent"))
	b.still()
}

// E31: a relation to an archived issue that has no duplicate is a
// relation, and the sweep leaves it alone.
func TestRepointArchivedNoDuplicate(t *testing.T) {
	w := newWorld(t)
	ekey := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Epic", Summary: "Epic"})
	ckey := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "Child", Parent: ekey})
	w.mustSync(jira.Options{})
	epic := w.byKey(ekey).Id()
	w.set(epic, "archived", issue.Value("true"))
	before := field(t, w.byKey(ckey), "parent")
	require.Equal(t, `"`+epic.String()+`"`, before)

	lines, sum := w.mustSync(jira.Options{})
	require.Zero(t, sum.Repointed, "%+v", lines)
	require.Empty(t, repointed(lines))
	require.Equal(t, before, field(t, w.byKey(ckey), "parent"))
}

// E32: a multi-relation holding the loser among other items: the loser's
// item is replaced and the others are kept, the set in its stored order.
func TestRepointMulti(t *testing.T) {
	b, x, y, kids := twoCopies(t)
	holder := b.byKey(kids[1])
	k0, k3 := b.byKey(kids[0]).Id().String(), b.byKey(kids[3]).Id().String()
	shapeSet(t, b, holder, "blocks", issue.Value(`["`+k3+`","`+y.String()+`","`+k0+`"]`))

	lines, _ := b.mustSync(jira.Options{})
	l := repointedOf(t, lines, kids[1])
	require.Contains(t, l.Imported, "blocks")
	require.Contains(t, l.Imported, "parent")
	want := []string{k3, x.String(), k0}
	slices.Sort(want)
	got, ok := issue.Strings(issue.Value(field(t, holder, "blocks")))
	require.True(t, ok)
	require.Equal(t, want, got)
	require.JSONEq(t, field(t, holder, "blocks"), string(l.Imported["blocks"]))
}

// E33: a dry run lists the re-points and commits nothing.
func TestRepointDryRun(t *testing.T) {
	b, x, y, kids := twoCopies(t)
	before := b.refs()
	lines, sum, err := b.sync(jira.Options{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, len(kids), sum.Repointed, "%+v", lines)
	for _, k := range kids {
		l := repointedOf(t, lines, k)
		require.True(t, l.DryRun)
		require.Equal(t, `"`+x.String()+`"`, string(l.Imported["parent"]))
		require.Equal(t, `"`+y.String()+`"`, field(t, b.byKey(k), "parent"))
	}
	require.Equal(t, before, b.refs(), "a dry run writes nothing")
}

// E34: two runs over one store list the same repointed lines in the same
// order: two dry runs, and the run that writes.
func TestRepointOrder(t *testing.T) {
	b, _, y, kids := twoCopies(t)
	b.mustSync(jira.Options{})
	for _, k := range kids {
		shapeSet(t, b, b.byKey(k), "parent", str(y.String()))
	}
	order := func(lines []jira.Line) []entity.Id {
		var ids []entity.Id
		for _, l := range repointed(lines) {
			ids = append(ids, l.Issue)
		}
		return ids
	}
	first, _, err := b.sync(jira.Options{DryRun: true})
	require.NoError(t, err)
	second, _, err := b.sync(jira.Options{DryRun: true})
	require.NoError(t, err)
	written, _ := b.mustSync(jira.Options{})
	require.Len(t, order(first), len(kids))
	require.True(t, slices.IsSorted(order(first)))
	require.Equal(t, order(first), order(second))
	require.Equal(t, order(first), order(written))
}
