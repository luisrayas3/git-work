package commands

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
)

// TestBareWorkWithNoTerminalIsTheHelp: the home flow draws, so with nowhere
// to draw it bare `git work` prints the help, as it always did, home flow
// or not (doc/design/app.md, A8).
func TestBareWorkWithNoTerminalIsTheHelp(t *testing.T) {
	env := newQuickstartTestEnv(t)
	_, _, err := host.FlowImport(env.Backend, []host.FlowSource{{Origin: "test", Script: `def home():
    """Home."""
    work.view.home(flows=["home"])
`}}, false, false)
	require.NoError(t, err)

	ran, err := runHome(env, &cobra.Command{}, nil)
	require.NoError(t, err)
	require.False(t, ran)
}
