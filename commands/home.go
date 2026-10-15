package commands

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	flowcmd "github.com/git-bug/git-bug/commands/flow"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/tui"
)

// homeFlow is the flow bare `git work` runs (doc/design/app.md, A8).
const homeFlow = "home"

// runHome is bare `git work`: `git work flow run home`, where there is a
// terminal to draw on, a repository to read and a home flow stored in it.
// It answers false, having run nothing, where any of the three is missing,
// and the root prints its help as it always did.
//
// The identity is settled only once the flow is known to run, so that
// `git work` in a repository with no home flow writes nothing.
func runHome(env *execenv.Env, cmd *cobra.Command, args []string) (bool, error) {
	if _, ok := tui.New(env.Out.Raw()); !ok {
		return false, nil
	}
	if env.Backend == nil {
		if err := execenv.LoadBackend(env)(cmd, args); err != nil {
			return false, nil
		}
	}

	ran := false
	err := execenv.CloseBackend(env, func(*cobra.Command, []string) error {
		if _, err := host.FlowExcerpt(env.Backend, homeFlow); err != nil {
			return nil
		}
		ran = true
		if err := execenv.EnsureUser(env); err != nil {
			return err
		}
		return flowcmd.Run(env, homeFlow, nil)
	})(cmd, args)
	return ran, err
}
