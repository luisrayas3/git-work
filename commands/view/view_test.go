package viewcmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
)

const twoIssues = `[
  {"id":"aaaaaaaaaaaaaaaa","human_id":"aaaaaaa","fields":{"title":"first","status":"open"}},
  {"id":"bbbbbbbbbbbbbbbb","human_id":"bbbbbbb","fields":{"title":"second","status":"done"}}
]`

// withItems is an env whose standard input is the items a view takes.
func withItems(t *testing.T, items string) *execenv.Env {
	t.Helper()
	env := execenv.NewTestEnv(t)
	_, err := env.In.(*execenv.TestIn).WriteString(items)
	require.NoError(t, err)
	env.Out.Reset()
	return env
}

func spec(t *testing.T, env *execenv.Env) map[string]any {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &got))
	return got
}

func TestViewBoard(t *testing.T) {
	env := withItems(t, twoIssues)

	require.NoError(t, runView(env, viewOptions{}, "board", []string{`{"columns":"status","card_title":"title"}`}))

	got := spec(t, env)
	require.Equal(t, "board", got["view"])
	require.Equal(t, map[string]any{"columns": "status", "card_title": "title"}, got["bindings"])
	require.Len(t, got["items"], 2)
}

func TestViewGantt(t *testing.T) {
	env := withItems(t, twoIssues)

	require.NoError(t, runView(env, viewOptions{}, "gantt",
		[]string{`{"start":"start_date","end":"due_date","group_by":"parent"}`}))

	got := spec(t, env)
	require.Equal(t, "gantt", got["view"])
	require.Equal(t, "start_date", got["bindings"].(map[string]any)["start"])
}

func TestViewList(t *testing.T) {
	env := withItems(t, twoIssues)

	// list requires no binding at all
	require.NoError(t, runView(env, viewOptions{}, "list", nil))

	got := spec(t, env)
	require.Equal(t, "list", got["view"])
	require.Equal(t, map[string]any{}, got["bindings"])
	require.Len(t, got["items"], 2)
}

func TestViewTakesOneIssueToo(t *testing.T) {
	env := withItems(t, `{"id":"aaaaaaaaaaaaaaaa","fields":{"title":"only"}}`)

	require.NoError(t, runView(env, viewOptions{}, "board", []string{`{"columns":"status"}`}))
	require.Len(t, spec(t, env)["items"], 1)
}

func TestViewRefusals(t *testing.T) {
	// a required binding that is absent
	env := withItems(t, twoIssues)
	err := runView(env, viewOptions{}, "board", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	// a binding the view does not have
	env = withItems(t, twoIssues)
	err = runView(env, viewOptions{}, "board", []string{`{"colums":"status"}`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "colums")

	// items that are not issues
	env = withItems(t, `["not an issue"]`)
	require.Error(t, runView(env, viewOptions{}, "list", nil))

	// nothing on standard input
	env = withItems(t, "")
	require.Error(t, runView(env, viewOptions{}, "list", nil))

	// bindings that are not an object of strings
	env = withItems(t, twoIssues)
	require.Error(t, runView(env, viewOptions{}, "board", []string{`{"columns":3}`}))
}

func TestViewGuiHasNoRenderer(t *testing.T) {
	env := withItems(t, twoIssues)

	err := runView(env, viewOptions{gui: true}, "board", []string{`{"columns":"status"}`})
	require.ErrorIs(t, err, ErrNoRenderer)
	require.Equal(t, "", env.Out.String())
}
