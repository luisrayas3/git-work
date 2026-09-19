package bugcmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
)

func newBugRmCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rm ISSUE_ID",
		Short:   "Remove an existing issue",
		Long:    "Remove an existing issue in the local repository. Note removing issues that were imported from bridges will not remove the issue on the remote, and will only remove the local copy of the issue.",
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runBugRm(env, args)
		}),
		ValidArgsFunction: BugCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	return cmd
}

func runBugRm(env *execenv.Env, args []string) (err error) {
	if len(args) == 0 {
		return errors.New("you must provide an issue prefix to remove")
	}

	err = env.Backend.Bugs().Remove(args[0])

	if err != nil {
		return
	}

	env.Out.Printf("issue %s removed\n", args[0])

	return
}
