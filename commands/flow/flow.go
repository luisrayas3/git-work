// Package flowcmd is the `git work flow` command tree, over the flow config
// entities of refs/work-flows.
//
// A flow is one Starlark function (`3556569` E2, `b511c63`):
// its name is the entity's key,
// its docstring the description,
// its parameters the arguments it takes.
// The refs are the runtime source of truth and the `.star` files in a tree are
// authoring files, so `import` is the only way from one to the other (E1).
//
// This tree is the entity's surface — list, run, import, export, log,
// archive and rm, to the map in doc/design/cli-convention.md,
// the bare noun being the human form of list —
// and it reaches the store through package `host`, as a script does;
// `flow/run` is the Starlark runtime `run` calls.
package flowcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
)

// The flow entity's attributes, its listing shape and the parsing behind them
// live in package host, because a script reaches them too (cli-convention.md).
const (
	attrScript      = host.AttrScript
	attrDescription = host.AttrDescription
)

type flowListOptions struct {
	format string
}

// NewFlowCommand is the bare `git work flow`: the human form of `flow list`,
// one line per flow, with --format json still there
// (decided 2026-10-08, doc/design/cli-convention.md).
// It has no Starlark name; work.flow.list() mirrors `flow list`.
func NewFlowCommand(env *execenv.Env) *cobra.Command {
	options := flowListOptions{}

	cmd := &cobra.Command{
		Use:   "flow",
		Short: "List the flows, one line each",
		Long: `List the flows, one line per flow: the name and the first line
of its description.

This is the human form of ` + "`git work flow list`" + `, which prints the same flows
as JSON, their arguments included, and is the form to script against;
--format json prints that here too.

` + flowListAbout,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowList(env, options)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	execenv.AddFormatFlag(cmd, &options.format, "text", "json")

	cmd.AddCommand(newFlowArchiveCommand(env))
	cmd.AddCommand(newFlowExportCommand(env))
	cmd.AddCommand(newFlowImportCommand(env))
	cmd.AddCommand(newFlowListCommand(env))
	cmd.AddCommand(newFlowLogCommand(env))
	cmd.AddCommand(newFlowRmCommand(env))
	cmd.AddCommand(newFlowRunCommand(env))

	return cmd
}

const flowListAbout = `A flow is one Starlark function stored in refs/work-flows. Its name is the
flow's name, its docstring the description and its parameters the arguments
` + "`git work flow run`" + ` takes.`

// newFlowListCommand is `git work flow list`, the plumbing:
// JSON by default, and what work.flow.list() returns.
func newFlowListCommand(env *execenv.Env) *cobra.Command {
	options := flowListOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the flows, JSON out",
		Long: `List the flows: their names, descriptions and arguments, as JSON.

` + flowListAbout + `

--format text prints one line per flow, the name and its summary line, which
is what the bare ` + "`git work flow`" + ` prints.`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowList(env, options)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runFlowList(env *execenv.Env, opts flowListOptions) error {
	warnDuplicates(env)

	entries, warnings, err := host.FlowList(env.Backend)
	if err != nil {
		return err
	}
	env.Warn(warnings)

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(entries)
	case "text":
		// One line per flow: a docstring's first line is its summary,
		// and the rest is for `flow list` or `flow export`.
		for _, entry := range entries {
			summary, _, _ := strings.Cut(entry.Description, "\n")
			env.Out.Printf("%s\t%s\n", entry.Name, strings.TrimSpace(summary))
		}
		return nil
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

// warnDuplicates prints the flows a key silently resolves to one of (E7).
//
// Every flow command calls it, because a team that loses an edit this way
// is never told otherwise.
func warnDuplicates(env *execenv.Env) {
	env.Warn(host.FlowWarnings(env.Backend))
}

// printIds prints the id of each flow that was created, one per line.
func printIds(env *execenv.Env, created []entity.Id) {
	for _, id := range created {
		env.Out.Println(id.String())
	}
}

// FlowCompletion completes a flow name.
func FlowCompletion(env *execenv.Env) completion.ValidArgsFunction {
	return func(cmd *cobra.Command, args []string, toComplete string) (completions []string, directives cobra.ShellCompDirective) {
		if err := execenv.LoadBackend(env)(cmd, args); err != nil {
			return completion.HandleError(err)
		}
		defer func() {
			_ = env.Backend.Close()
		}()

		for _, name := range env.Backend.Flows().Keys(config.ShapeFlow) {
			if !strings.HasPrefix(name, strings.TrimSpace(toComplete)) {
				continue
			}
			excerpt, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, name)
			if err != nil {
				return completion.HandleError(err)
			}
			description, _ := excerpt.AttributeString(attrDescription)
			completions = append(completions, name+"\t"+description)
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}
