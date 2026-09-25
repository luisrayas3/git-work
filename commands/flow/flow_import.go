package flowcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type flowImportOptions struct {
	prune  bool
	dryRun bool
}

func newFlowImportCommand(env *execenv.Env) *cobra.Command {
	options := flowImportOptions{}

	cmd := &cobra.Command{
		Use:   "import FILE|DIR|-...",
		Short: "Import flows from Starlark files",
		Long: `Import flows from files, directories of *.star files, or standard input.

A file is one flow: exactly one top-level def, whose name is the flow's name,
whose docstring is the description and whose parameters are its arguments. The
file's name and location never matter, so a scratch file anywhere imports the
same as one under the repository.

Import is an upsert keyed on the def's name: a flow that is not there is
created and its id printed, one whose script or description differs is
updated, and one that is unchanged emits nothing. --prune additionally
archives every flow the inputs do not mention, which is the only way an import
removes anything.

Every input is parsed before anything is written, so a file that does not
parse aborts the whole import.`,
		Example: `git work flow import flows/
git work flow import flows/board.star flows/report.star
git work flow export board | git work flow import -`,
		Args:    cobra.MinimumNArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowImport(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.prune, "prune", false,
		"Archive every flow the inputs do not mention")
	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print what would be done, and write nothing")

	return cmd
}

// runFlowImport reads the files and lets the host do the rest.
//
// Reading argv, a directory and standard input is this command's own work;
// parsing, planning and writing is the host's, so that a script importing
// flows takes the same path as the shell does (cli-convention.md).
//
// The ids created before a failure are printed too, because they are in the
// store whether the rest of the import landed or not.
func runFlowImport(env *execenv.Env, opts flowImportOptions, args []string) error {
	warnDuplicates(env)

	sources, err := readSources(env, args)
	if err != nil {
		return err
	}

	changes, created, err := host.FlowImport(env.Backend, sources, opts.prune, opts.dryRun)
	if err != nil {
		printIds(env, created)
		return err
	}

	if opts.dryRun {
		return env.Out.PrintJSON(changes)
	}

	printIds(env, created)
	return nil
}

// readSources turns the arguments into the scripts they name:
// standard input, one file, or every *.star of a directory, not recursively.
func readSources(env *execenv.Env, args []string) ([]host.FlowSource, error) {
	var sources []host.FlowSource

	for _, arg := range args {
		read, err := readSource(env, arg)
		if err != nil {
			return nil, err
		}
		sources = append(sources, read...)
	}

	return sources, nil
}

func readSource(env *execenv.Env, arg string) ([]host.FlowSource, error) {
	if arg == "-" {
		data, err := io.ReadAll(env.In)
		if err != nil {
			return nil, fmt.Errorf("reading the standard input: %w", err)
		}
		return []host.FlowSource{{Origin: "standard input", Script: string(data)}}, nil
	}

	info, err := os.Stat(arg)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, err
		}
		return []host.FlowSource{{Origin: arg, Script: string(data)}}, nil
	}

	paths, err := filepath.Glob(filepath.Join(arg, "*.star"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	sources := make([]host.FlowSource, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, host.FlowSource{Origin: path, Script: string(data)})
	}
	return sources, nil
}
