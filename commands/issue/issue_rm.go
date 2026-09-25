package issuecmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

func newIssueRmCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm ID",
		Short: "Remove an issue from the local repository",
		Long: `Remove an issue's local ref. This is local: the issue comes back on the next
pull, and removing one that came from a bridge does not remove it on the remote.
The replicated removal is archive.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueRm(env, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	return cmd
}

func runIssueRm(env *execenv.Env, args []string) error {
	return host.IssueRm(env.Backend, args[0])
}
