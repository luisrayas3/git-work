package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// TestIssueTypeChangeChecksRetypeTargets: a set that changes the type is
// refused where the current type's retype_targets does not list the new
// one, whether planned or committed through Update; UpdateShape, a pull's,
// commits it anyway, and a new issue is never checked
// (doc/design/issue.md, "A type change is a set").
func TestIssueTypeChangeChecksRetypeTargets(t *testing.T) {
	c, i, me := newUpdateTestIssue(t)

	for at, key := range []string{"bug", "epic"} {
		_, _, err := c.Schema().New(config.ShapeType, key, map[string]config.Value{
			schema.AttrOrdinal: config.MustValue(20 + at*10),
		})
		require.NoError(t, err)
	}
	task, err := c.Schema().ResolveKey(config.ShapeType, "task")
	require.NoError(t, err)
	_, err = task.Set(schema.AttrRetypeTargets, config.MustValue([]string{"bug"}))
	require.NoError(t, err)

	const refused = "type: type change from task to epic is not allowed; allowed: bug"
	_, err = i.PlanSetFields(map[string]issue.Value{"type": issue.StringValue("epic")})
	require.EqualError(t, err, refused)

	set := func(update func(func(*issue.Snapshot) ([]issue.Operation, error)) error, typ string) error {
		return update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
			return []issue.Operation{issue.NewSetFieldOp(me, time.Now().Unix(), "type", issue.StringValue(typ))}, nil
		})
	}
	before := issueRef(t, c, i)
	require.EqualError(t, set(i.Update, "epic"), refused)
	require.Equal(t, before, issueRef(t, c, i))

	n, _, err := c.Issues().New("t", "", map[string]issue.Value{"type": issue.StringValue("epic")})
	require.NoError(t, err, "issue new is not a type change")
	require.NoError(t, set(n.Update, "task"), "epic has no retype_targets: any type")

	require.NoError(t, set(i.UpdateShape, "epic"), "a pull imports a Move whatever the schema says")
	require.Equal(t, "epic", mustFieldString(t, i.Snapshot(), "type"))
}
