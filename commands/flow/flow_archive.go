package flowcmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
)

func newFlowArchiveCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive NAME",
		Short: "Archive a flow",
		Long: `Archive a flow: it leaves every listing and stops being importable over.

Archiving is an operation, so it reaches every clone, which is what makes it
the removal a team can rely on. A ref cannot be deleted across clones; rm
deletes the local one and the flow comes back on the next pull.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowArchive(env, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	return cmd
}

func runFlowArchive(env *execenv.Env, args []string) error {
	warnDuplicates(env)

	cached, err := current(env, args[0])
	if err != nil {
		return err
	}

	_, err = cached.SetArchived(true)

	return err
}

func newFlowRmCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm NAME",
		Short: "Remove a flow from the local repository",
		Long: `Remove a flow's local ref. This is local: the flow comes back on the next
pull. The replicated removal is archive.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowRm(env, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	return cmd
}

func runFlowRm(env *execenv.Env, args []string) error {
	warnDuplicates(env)

	excerpt, err := currentExcerpt(env, args[0])
	if err != nil {
		return err
	}

	return env.Backend.Flows().Remove(excerpt.Id().String())
}
