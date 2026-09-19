package bugcmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	_select "github.com/git-bug/git-bug/commands/select"
	"github.com/git-bug/git-bug/entities/bug"
)

func newBugDeselectCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deselect",
		Short: "Clear the implicitly selected issue",
		Example: `git work issue select 2f15
git work issue comment
git work issue status
git work issue deselect
`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runBugDeselect(env)
		}),
	}

	return cmd
}

func runBugDeselect(env *execenv.Env) error {
	err := _select.Clear(env.Backend, bug.Namespace)
	if err != nil {
		return err
	}

	return nil
}
