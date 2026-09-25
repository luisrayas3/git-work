package flowcmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/host"
)

type flowExportOptions struct {
	all bool
}

func newFlowExportCommand(env *execenv.Env) *cobra.Command {
	options := flowExportOptions{}

	cmd := &cobra.Command{
		Use:   "export NAME|--all DIR",
		Short: "Print a flow's script, or write every flow to a directory",
		Long: `Print a flow's script on standard output, verbatim, so that a redirection
writes the file an import takes back unchanged.

--all writes one <name>.star per unarchived flow into the directory, which is
created if it is missing, and prints nothing.`,
		Example: `git work flow export board > flows/board.star
git work flow export --all flows/`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowExport(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.all, "all", false,
		"Write every flow into the directory given as the argument")

	return cmd
}

func runFlowExport(env *execenv.Env, opts flowExportOptions, args []string) error {
	warnDuplicates(env)

	if opts.all {
		return exportAll(env, args[0])
	}

	script, err := host.FlowExport(env.Backend, args[0])
	if err != nil {
		return err
	}

	env.Out.Print(script)

	return nil
}

// exportAll writes one file per unarchived flow, named after the flow.
//
// The name is where a flow's identity lives, so it is the file name too;
// import does not care, and a human reading the directory does.
func exportAll(env *execenv.Env, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	for _, name := range env.Backend.Flows().Keys(config.ShapeFlow) {
		excerpt, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, name)
		if err != nil {
			return err
		}
		script, err := host.FlowScript(excerpt)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name+".star")
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			return fmt.Errorf("flow %s: %w", name, err)
		}
	}

	return nil
}
