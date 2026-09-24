package flowcmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
)

// runnableFlow creates two issues, edits one and returns the ones it kept.
const runnableFlow = `def kanban(status="open"):
    """The issues of one status."""
    a = work.issue.new({"fields": {"title": "first", "status": "open"}})
    work.issue.new({"fields": {"title": "second", "status": "done"}})
    work.issue.set(a, estimate=3)
    return work.issue.list('map(select(.fields.status == "%s"))' % status)
`

// drawingFlow calls a view, which needs a surface to draw on.
const drawingFlow = `def kanban_view():
    """A board of everything."""
    return work.view.board(columns="status")
`

const quietFlow = `def quiet():
    """Write one issue and say nothing."""
    work.issue.new({"fields": {"title": "made by a flow"}})
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

	var items []struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &items))
	require.Len(t, items, 1)
	require.JSONEq(t, `"first"`, string(items[0].Fields["title"]))
	require.JSONEq(t, `3`, string(items[0].Fields["estimate"]))

	// the writes really happened, through the cache
	require.Len(t, env.Backend.Issues().AllIds(), 2)
}

// TestFlowRunDrawingNeedsASurface is the one thing `flow run` does not do for
// a flow that draws: a test env's output is a buffer, so there is no terminal
// and the view call is what says so.
func TestFlowRunDrawingNeedsASurface(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban_view.star", drawingFlow)

	env.Out.Reset()
	err := runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban_view"})
	require.ErrorContains(t, err, "no renderer here")
	require.Equal(t, "", env.Out.String())
}

func TestFlowRunKwargsFromTheArgument(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "kanban.star", runnableFlow)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "json"}, []string{"kanban", `{"status":"done"}`}))

	var items []struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &items))
	require.Len(t, items, 1)
	require.JSONEq(t, `"second"`, string(items[0].Fields["title"]))
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

// TestFlowRunText is what --format text is for now that drawing is a view's
// own business: a sentence is printed as a sentence, not in quotes.
func TestFlowRunText(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "report.star", `def report():
    """One line of prose."""
    return "two issues, one done"
`)

	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "text"}, []string{"report"}))
	require.Equal(t, "two issues, one done\n", env.Out.String())

	// a list of strings is one per line, which is what a pipe wants
	importScript(t, env, "titles.star", `def titles():
    """Every title."""
    return ["first", "second"]
`)
	env.Out.Reset()
	require.NoError(t, runFlowRun(env, flowRunOptions{format: "text"}, []string{"titles"}))
	require.Equal(t, "first\nsecond\n", env.Out.String())
}

func TestFlowRunTextOfSomethingElse(t *testing.T) {
	env := newTestEnv(t)
	importScript(t, env, "count.star", `def count():
    """How many issues there are."""
    return len(work.issue.list("."))
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
	require.ErrorIs(t, err, ErrNoGui)
	require.Contains(t, err.Error(), "8b06191")
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
