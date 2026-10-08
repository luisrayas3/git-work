package viewcmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
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

// TestViewRefusesRank: rank is internal, the order every view draws and every
// drag writes, so the command refuses it as an argument and as a layer key,
// before the missing terminal (Luis, 2026-10-08).
func TestViewRefusesRank(t *testing.T) {
	env := execenv.NewTestEnv(t)

	err := runView(env, viewOptions{}, "list", []string{`{"rank":"rank"}`})
	require.Error(t, err)
	require.NotErrorIs(t, err, view.ErrNoTerminal)
	require.Contains(t, err.Error(), "rank")

	err = runView(env, viewOptions{}, "gantt", []string{`{"start":"a","stop":"b","expand":{"relation":"children","rank":"rank"}}`})
	require.Error(t, err)
	require.NotErrorIs(t, err, view.ErrNoTerminal)
	require.Contains(t, err.Error(), "takes no key rank")
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
	// the third tier is `optional`, and the footnote says what that means
	// rather than warning that it is not drawn yet (f9c991e)
	require.Contains(t, long, "optional")
	require.Contains(t, long, "has no default")
	require.NotContains(t, long, "not drawn yet")
}

// TestViewChecksShowEntries: each entry of `show` is checked as show's own
// call is, before the missing terminal, naming the type
// (doc/design/show-from-a-view.md, V2).
func TestViewChecksShowEntries(t *testing.T) {
	env := execenv.NewTestEnv(t)
	me, err := env.Backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(me))
	_, _, err = host.SchemaInit(env.Backend, "jira", false)
	require.NoError(t, err)

	for _, c := range []struct{ kind, kwargs, want string }{
		{"list", `{"show":{"saga":{}}}`, "show names type saga, which the schema does not have; the types are initiative, epic"},
		{"board", `{"columns":"status","show":{"epic":{"id":"abc"}}}`, "show epic takes no id"},
		{"gantt", `{"start":"due","stop":"due","show":{"epic":{"colour":"red"}}}`, "show epic takes no argument colour, an entry takes fields (defaulted), expand (optional)"},
		{"matrix", `{"rows":"type","columns":"status","show":{"epic":{"expand":"nephews"}}}`, "show epic: view show: expand"},
		{"show", `{"id":"abc","show":{"epic":{"expand":{"relation":"children","details":["status"]}}}}`, "takes no details"},
		{"list", `{"show":{"epic":{"expand":{"relation":"children","fields":["colour"]}}}}`, "colour"},
	} {
		err := runView(env, viewOptions{}, c.kind, []string{c.kwargs})
		require.Error(t, err, c.kwargs)
		require.NotErrorIs(t, err, view.ErrNoTerminal, c.kwargs)
		require.Contains(t, err.Error(), c.want)
	}

	// a good map passes the check and meets the missing terminal
	err = runView(env, viewOptions{}, "list", []string{`{"show":{"epic":{"expand":"children"}}}`})
	require.ErrorIs(t, err, view.ErrNoTerminal)
}
