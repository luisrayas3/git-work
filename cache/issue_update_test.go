package cache

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// newUpdateTestIssue gives the schema test repository plus one task.
func newUpdateTestIssue(t *testing.T) (*RepoCache, *IssueCache, identity.Interface) {
	t.Helper()

	c := newSchemaTestCache(t)
	i, _, err := c.Issues().New("title", "message", map[string]issue.Value{
		"type":   issue.StringValue("task"),
		"status": issue.StringValue("to-do"),
	})
	require.NoError(t, err)

	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	return c, i, me
}

func issueRef(t *testing.T, c *RepoCache, i *IssueCache) repository.Hash {
	t.Helper()
	hash, err := c.repo.ResolveRef("refs/" + issue.Namespace + "/" + i.Id().String())
	require.NoError(t, err)
	return hash
}

// TestIssueUpdateDecidesOnTheFreshIssue is the race Update exists for: a
// second process commits between the moment a writer read the issue and the
// moment it writes. The writer's decision sees that commit, and both survive.
func TestIssueUpdateDecidesOnTheFreshIssue(t *testing.T) {
	first, i, _ := newUpdateTestIssue(t)

	second, err := NewRepoCacheNoEvents(first.repo)
	require.NoError(t, err)
	me, err := second.Identities().Resolve(i.Snapshot().Author.Id())
	require.NoError(t, err)
	require.NoError(t, second.SetUserIdentity(me))

	// the second process reads the issue, then the first one writes to it
	stale, err := second.Issues().Resolve(i.Id())
	require.NoError(t, err)
	require.Equal(t, "to-do", mustFieldString(t, stale.Snapshot(), "status"))

	ops, err := i.PlanSetFields(map[string]issue.Value{"status": issue.StringValue("done")})
	require.NoError(t, err)
	require.NoError(t, i.CommitOperations(ops))

	var seen string
	err = stale.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		seen = mustFieldString(t, snap, "status")
		now := time.Now().Unix()
		return []issue.Operation{
			issue.NewAddValueOp(me, now, "labels", issue.StringValue("core")),
			issue.NewNoOpOp(me, now, map[string]string{"jira-sync": `{"v":1}`}),
		}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "done", seen, "fn sees the commit made after the stale read")

	reread, err := issue.Read(first.repo, i.Id())
	require.NoError(t, err)
	snap := reread.Compile()
	require.Equal(t, "done", mustFieldString(t, snap, "status"))
	require.Len(t, snap.Items("labels"), 1)

	last := snap.Operations[len(snap.Operations)-1]
	require.Equal(t, issue.NoOpOp, last.Type())
	marker, ok := last.GetMetadata("jira-sync")
	require.True(t, ok)
	require.Equal(t, `{"v":1}`, marker)

	// and the second cache's own copy is the committed one
	require.Len(t, stale.Snapshot().Items("labels"), 1)
	require.False(t, stale.NeedCommit())
}

func TestIssueUpdateWithNoOperationsWritesNothing(t *testing.T) {
	c, i, _ := newUpdateTestIssue(t)
	before := issueRef(t, c, i)

	require.NoError(t, i.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return nil, nil
	}))
	require.Equal(t, before, issueRef(t, c, i))

	// fn's error is Update's, and nothing is written either
	boom := errors.New("boom")
	require.ErrorIs(t, i.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return nil, boom
	}), boom)
	require.Equal(t, before, issueRef(t, c, i))
}

func TestIssueUpdateRefusedBySchemaWritesNothing(t *testing.T) {
	c, i, me := newUpdateTestIssue(t)
	before := issueRef(t, c, i)

	err := i.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		now := time.Now().Unix()
		return []issue.Operation{
			issue.NewAddCommentOp(me, now, "not schema-checked, not written either", nil),
			issue.NewSetFieldOp(me, now, "status", issue.StringValue("bogus")),
			issue.NewAddValueOp(me, now, "labels", issue.StringValue("nope")),
		}, nil
	})
	var problems *schema.Problems
	require.ErrorAs(t, err, &problems)
	require.Len(t, problems.List, 2, "every problem at once")

	require.Equal(t, before, issueRef(t, c, i))
	require.False(t, i.NeedCommit())
	require.Len(t, i.Snapshot().Comments, 1)

	// an operation that is not valid at all is refused the same way
	err = i.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{issue.NewSetFieldOp(me, time.Now().Unix(), "Not A Key", issue.StringValue("x"))}, nil
	})
	require.Error(t, err)
	require.Equal(t, before, issueRef(t, c, i))
}

// TestIssueUpdateChecksAgainstTheTypeTheBatchSets: a batch is one change, so
// a type set in it is the type the batch's other keys belong to.
func TestIssueUpdateChecksAgainstTheTypeTheBatchSets(t *testing.T) {
	c, i, me := newUpdateTestIssue(t)

	_, _, err := c.Schema().New(config.ShapeType, "bug", map[string]config.Value{
		schema.AttrName:    config.StringValue("Bug"),
		schema.AttrOrdinal: config.MustValue(20),
	})
	require.NoError(t, err)
	_, _, err = c.Schema().New(config.ShapeField, "bug/severity", map[string]config.Value{
		schema.AttrKind: config.StringValue(string(schema.KindEnum)),
		"values/high":   config.MustValue(schema.ValueAttr{Ordinal: 10}),
	})
	require.NoError(t, err)

	update := func(fields ...string) error {
		return i.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
			var ops []issue.Operation
			for at := 0; at < len(fields); at += 2 {
				ops = append(ops, issue.NewSetFieldOp(me, time.Now().Unix(), fields[at], issue.StringValue(fields[at+1])))
			}
			return ops, nil
		})
	}

	// a task has no severity, and a bug has no status
	require.Error(t, update("severity", "high"))
	require.Error(t, update("type", "bug", "status", "done"))

	require.NoError(t, update("type", "bug", "severity", "high"))
	snap := i.Snapshot()
	require.Equal(t, "bug", mustFieldString(t, snap, "type"))
	require.Equal(t, "high", mustFieldString(t, snap, "severity"))
}

func mustFieldString(t *testing.T, snap *issue.Snapshot, key string) string {
	t.Helper()
	value, ok := snap.FieldString(key)
	require.True(t, ok, "field %s", key)
	return value
}
