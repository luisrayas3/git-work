package jira_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/schema"
)

// The built-in color is local-only (doc/design/jira.md, "What maps"): a
// synced type keeps it through every sync's schema import, `jira schema`
// pairs nothing with it, and a synced issue with a color set reports no
// pending and no off-schema item for it, and writes nothing on either side.
func TestColorIsLocalOnly(t *testing.T) {
	w := newWorld(t)
	doc, _, err := host.SchemaExport(w.c, false)
	require.NoError(t, err)
	task, ok := doc.Types.Get("task")
	require.True(t, ok)
	task.Color = true
	doc.SetType("task", task)
	_, _, err = host.SchemaImport(w.c, doc, false, false)
	require.NoError(t, err)

	key, ic := w.imported("Colored")
	w.set(ic.Id(), schema.ColorKey, str("bright-blue"))

	lines, sum := w.mustSync(jira.Options{})
	for _, line := range lines {
		for _, skip := range append(line.Pending, line.OffSchema...) {
			require.NotEqual(t, schema.ColorKey, skip.Key, "%+v", line)
		}
		require.NotContains(t, line.Exported, schema.ColorKey)
	}
	require.Zero(t, sum.Pending)
	require.Zero(t, sum.OffSchema)
	w.quiet()
	require.Equal(t, `"bright-blue"`, field(t, ic, schema.ColorKey))
	require.Equal(t, "Colored", w.srv.Issue(key).Summary)

	derived, _, err := host.JiraSchema(context.Background(), w.c)
	require.NoError(t, err)
	task, _ = derived.Types.Get("task")
	require.True(t, task.Color, "the derived schema keeps the switch")
	_, paired := task.Fields.Get(schema.ColorKey)
	require.False(t, paired, "jira schema pairs nothing with the built-in")

	s, err := w.c.LoadSchema()
	require.NoError(t, err)
	_, ok = s.Types["task"].Field(schema.ColorKey)
	require.True(t, ok, "the syncs' imports keep color on")
}
