package viewcmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/view"
)

// A test env's output is a buffer, which is never a terminal, so these tests
// are the no-surface path — which is the path an agent and a pipe take too.

func TestViewNeedsATerminal(t *testing.T) {
	env := execenv.NewTestEnv(t)

	err := runView(env, viewOptions{}, "list", nil)
	require.ErrorIs(t, err, view.ErrNoTerminal)
	require.Contains(t, err.Error(), "--gui")
	require.Equal(t, "", env.Out.String())
}

func TestViewGuiHasNoRenderer(t *testing.T) {
	env := execenv.NewTestEnv(t)

	err := runView(env, viewOptions{gui: true}, "board", []string{`{"columns":"status"}`})
	require.ErrorIs(t, err, view.ErrNoGui)
	require.Contains(t, err.Error(), "8b06191")
	require.Equal(t, "", env.Out.String())
}

// TestViewChecksTheCallBeforeTheSurface is why the missing terminal is not
// the first thing reported: a misspelled argument is the user's mistake, and
// hiding it behind "no terminal" would send them to fix the wrong thing.
func TestViewChecksTheCallBeforeTheSurface(t *testing.T) {
	env := execenv.NewTestEnv(t)

	err := runView(env, viewOptions{}, "board", []string{`{"colums":"status"}`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "colums")
	require.Contains(t, err.Error(), "columns (required)")

	// a required argument that is absent, likewise
	err = runView(env, viewOptions{}, "board", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	// and the arguments have to be an object at all
	err = runView(env, viewOptions{}, "list", []string{`["title"]`})
	require.Error(t, err)
	require.Contains(t, err.Error(), "JSON object")
}

func TestViewKwargsFromStdin(t *testing.T) {
	env := execenv.NewTestEnv(t)
	_, err := env.In.(*execenv.TestIn).WriteString(`{"columns":"status"}`)
	require.NoError(t, err)

	// the call parses, so what is left is the missing terminal
	err = runView(env, viewOptions{}, "board", []string{"-"})
	require.ErrorIs(t, err, view.ErrNoTerminal)
}

// TestEveryKindIsACommand is the contract between the table and the tree: a
// kind in one is a command in the other, with no second list to keep in step.
func TestEveryKindIsACommand(t *testing.T) {
	env := execenv.NewTestEnv(t)
	cmd := NewViewCommand(env)

	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, kind := range []string{"list", "board", "gantt", "show"} {
		require.True(t, names[kind], "%s is a view kind and a command", kind)
	}
}

// TestHelpCarriesTheTable is what makes the help trustworthy: it is generated
// from the same rows the check reads.
func TestHelpCarriesTheTable(t *testing.T) {
	long := kindLong("gantt")
	require.Contains(t, long, "start")
	require.Contains(t, long, "stop")
	require.Contains(t, long, "required")
	require.Contains(t, long, "day, week, month, quarter")
	require.Contains(t, long, "feature")
}
