package flowcmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type flowLogOptions struct {
	format string
}

func newFlowLogCommand(env *execenv.Env) *cobra.Command {
	options := flowLogOptions{}

	cmd := &cobra.Command{
		Use:   "log [NAME]",
		Short: "Print the flows' history",
		Long: `Print the history of one flow, or of every flow: every operation, oldest
first and one JSON object per line, so it says who changed a flow, when, and
to what.

--format text prints one line per operation instead.`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowLog(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runFlowLog(env *execenv.Env, opts flowLogOptions, args []string) error {
	warnDuplicates(env)

	name := ""
	if len(args) == 1 {
		name = args[0]
	}

	entries, err := host.FlowLog(env.Backend, name)
	if err != nil {
		return err
	}

	return cmdjson.WriteConfigOperations(env.Out.Raw(), opts.format, entries)
}
