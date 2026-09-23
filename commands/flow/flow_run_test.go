package flowcmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	viewcmd "github.com/git-bug/git-bug/commands/view"
)

// runnableFlow creates two issues, edits one and returns a board of them.
const runnableFlow = `def kanban(status="open"):
    """A kanban of one status."""
    a = issue.new({"fields": {"title": "first", "status": "open"}})
    issue.new({"fields": {"title": "second", "status": "done"}})
    issue.set(a, estimate=3)
    return view.board(issue.list('map(select(.fields.status == "%s"))' % status), columns="status")
`

const quietFlow = `def quiet():
    """Write one issue and say nothing."""
    issue.new({"fields": {"title": "made by a flow"}})
`

// importScript puts a script in the store through the import command.
func importScript(t *testing.T, env *execenv.Env, name string, script string) {
	t.Helper()
	path := writeFlow(t, t.TempDir(), name, script)
	importFlows(t, env, flowImportOptions{}, path)
}

func TestFlowRunJSON(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban"}))

	var spec struct {
		View     string            `json:"view"`
		Bindings map[string]string `json:"bindings"`
		Items    []struct {
			Fields map[string]json.RawMessage `json:"fields"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &spec))
	require.Equal(t, "board", spec.View)
	require.Equal(t, map[string]string{"columns": "status"}, spec.Bindings)
	require.Len(t, spec.Items, 1)
	require.JSONEq(t, `"first"`, string(spec.Items[0].Fields["title"]))
	require.JSONEq(t, `3`, string(spec.Items[0].Fields["estimate"]))

	// the writes really happened, through the cache
	require.Len(t, env.Backend.Issues().AllIds(), 2)
}

// TestFlowRunPrintsASpecAsTheViewCommandDoes pins the one contract two
// printers share: a spec is a spec whichever half of the pipe built it.
func TestFlowRunPrintsASpecAsTheViewCommandDoes(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban"}))
	fromFlow := env.Out.String()
	require.True(t, strings.Index(fromFlow, `"view"`) < strings.Index(fromFlow, `"bindings"`),
		"a spec prints in the struct's order, not the map's")

	// the same items and bindings, through `git work view board`
	var spec struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(fromFlow), &spec))
	items, err := json.Marshal(spec.Items)
	require.NoError(t, err)

	viewEnv := execenv.NewTestEnv(t)
	_, err = viewEnv.In.(*execenv.TestIn).Write(items)
	require.NoError(t, err)
	viewEnv.Out.Reset()

	cmd := viewcmd.NewViewCommand(viewEnv)
	cmd.SetArgs([]string{"board", `{"columns":"status"}`})
	cmd.SetOut(viewEnv.Out)
	cmd.SetErr(viewEnv.Err)
	require.NoError(t, cmd.Execute())

	require.Equal(t, fromFlow, viewEnv.Out.String())
}

func TestFlowRunKwargsFromTheArgument(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban", `{"status":"done"}`}))

	var spec struct {
		Items []struct {
			Fields map[string]json.RawMessage `json:"fields"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &spec))
	require.Len(t, spec.Items, 1)
	require.JSONEq(t, `"second"`, string(spec.Items[0].Fields["title"]))
}

func TestFlowRunKwargsFromStdin(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	_, err := env.In.(*execenv.TestIn).WriteString(`{"status":"done"}`)
	require.NoError(t, err)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban", "-"}))
	require.Contains(t, env.Out.String(), "second")
	require.NotContains(t, env.Out.String(), "first")
}

func TestFlowRunText(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "text"}, []string{"kanban"}))

	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 2)
	// a placeholder until a renderer exists (84dfbde, 8b06191)
	require.Equal(t, "view board (1 items)", lines[0])
	require.Equal(t, 3, len(strings.Split(lines[1], "\t")))
	require.Contains(t, lines[1], "open")
	require.Contains(t, lines[1], "first")
}

func TestFlowRunTextOfSomethingThatIsNotASpec(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "count.star", `def count():
    """How many issues there are."""
    return len(issue.list("."))
`)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "text"}, []string{"count"}))
	require.Equal(t, "0\n", env.Out.String())
}

func TestFlowRunPrintsNothingForNone(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "quiet.star", quietFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"quiet"}))
	require.Equal(t, "", env.Out.String())
	require.Len(t, env.Backend.Issues().AllIds(), 1)
}

func TestFlowRunGuiHasNoRenderer(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	err := runFlowRun(env, flowRunOptions{format: "json", gui: true}, []string{"kanban"})
	require.ErrorIs(t, err, ErrNoRenderer)
	// and it ran nothing
	require.Empty(t, env.Backend.Issues().AllIds())
}

func TestFlowRunRefusals(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	// a flow nobody defined
	require.Error(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"absent"}))
	// arguments that are not a JSON object
	require.Error(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban", `["status"]`}))
	// an argument the signature does not have
	err := runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban", `{"statuss":"done"}`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "statuss")
	// an unknown format, once the flow has run
	require.Error(t, runFlowRun(env, flowRunOptions{format: "yaml"}, []string{"quiet"}))
}
