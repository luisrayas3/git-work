package issuecmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
)

func newIssueRmCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rm ISSUE_ID",
		Short:   "Remove an existing issue",
		Long:    "Remove an existing issue from the local repository. Removing an issue that came from a bridge does not remove it on the remote; only the local copy goes.",
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueRm(env, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	return cmd
}

func runIssueRm(env *execenv.Env, args []string) error {
	err := env.Backend.Issues().Remove(args[0])
	if err != nil {
		return err
	}

	env.Out.Printf("issue %s removed\n", args[0])

	return nil
}
