package flowcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type flowGetOptions struct {
	format string
}

func newFlowGetCommand(env *execenv.Env) *cobra.Command {
	options := flowGetOptions{}

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Print one flow whole",
		Long: `Print a flow: its description, its arguments, its script and its entity id.

--format text prints the script verbatim, which is what export gives.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowGet(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runFlowGet(env *execenv.Env, opts flowGetOptions, args []string) error {
	warnDuplicates(env)

	detail, warnings, err := host.FlowGet(env.Backend, args[0])
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		warn(env, warnings)
		return env.Out.PrintJSON(detail)
	case "text":
		// verbatim: what comes out has to import back unchanged
		env.Out.Print(detail.Script)
		return nil
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}
