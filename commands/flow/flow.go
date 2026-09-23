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
// This tree is the entity's surface — list, get, run, import, export, log,
// archive and rm, to the map in doc/design/cli-convention.md —
// and it reaches the store through package `host`, as a script does;
// `flow/run` is the Starlark runtime `run` calls.
package flowcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/host"
)

// The flow entity's attributes, its listing shape and the parsing behind them
// live in package host, because a script reaches them too (cli-convention.md).
const (
	attrScript      = host.AttrScript
	attrDescription = host.AttrDescription
)

// flowEntry is one flow in a listing, paramEntry one of its arguments,
// and flowDetail one flow whole.
type (
	flowEntry  = host.FlowEntry
	paramEntry = host.FlowParam
	flowDetail = host.FlowDetail
)

type flowListOptions struct {
	format string
}

func NewFlowCommand(env *execenv.Env) *cobra.Command {
	options := flowListOptions{}

	cmd := &cobra.Command{
		Use:   "flow",
		Short: "List the flows",
		Long: `List the flows: their names, descriptions and arguments.

A flow is one Starlark function stored in refs/work-flows. Its name is the
flow's name, its docstring the description and its parameters the arguments
` + "`git work flow run`" + ` takes.

--format text prints one line per flow, the name and its description.`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowList(env, options)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	cmd.AddCommand(newFlowArchiveCommand(env))
	cmd.AddCommand(newFlowExportCommand(env))
	cmd.AddCommand(newFlowGetCommand(env))
	cmd.AddCommand(newFlowImportCommand(env))
	cmd.AddCommand(newFlowLogCommand(env))
	cmd.AddCommand(newFlowRmCommand(env))

	return cmd
}

// addFormatFlag adds the one output flag every reader has.
func addFormatFlag(cmd *cobra.Command, format *string) {
	cmd.Flags().StringVarP(format, "format", "f", "json",
		"Select the output formatting style. Valid values are [json,text]")
	cmd.RegisterFlagCompletionFunc("format", completion.From([]string{"json", "text"}))
}

func runFlowList(env *execenv.Env, opts flowListOptions) error {
	warnDuplicates(env)

	entries, warnings, err := host.FlowList(env.Backend)
	if err != nil {
		return err
	}
	warn(env, warnings)

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(entries)
	case "text":
		for _, entry := range entries {
			env.Out.Printf("%s\t%s\n", entry.Name, entry.Description)
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
	warn(env, host.FlowWarnings(env.Backend))
}

// warn prints what a reader should be told about the flows it just read.
func warn(env *execenv.Env, warnings []string) {
	for _, warning := range warnings {
		env.Err.Printf("warning: %s\n", warning)
	}
}

// currentExcerpt resolves a flow name to the entity it names (E7).
func currentExcerpt(env *execenv.Env, name string) (*cache.ConfigExcerpt, error) {
	return host.FlowExcerpt(env.Backend, name)
}

// archivedToo resolves a flow name to its entity, archived or not.
//
// A listing hides the archived and a writer refuses to touch one,
// but a log is history: an archived flow still has one,
// and reading why it was archived is the first thing anyone asks.
func archivedToo(env *execenv.Env, name string) (*cache.ConfigCache, error) {
	matching := env.Backend.Flows().Query(cache.ConfigQuery{
		Shape:           config.ShapeFlow,
		Key:             name,
		IncludeArchived: true,
	})
	if len(matching) == 0 {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	// ordered by (key, creation, id), so the first is the one E7 resolves to
	return env.Backend.Flows().Resolve(matching[0].Id())
}

// current resolves a flow name to the cached entity, for a write.
func current(env *execenv.Env, name string) (*cache.ConfigCache, error) {
	cached, err := env.Backend.Flows().Current(config.ShapeFlow, name)
	if err != nil {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	return cached, nil
}

// scriptOf returns a flow's source.
func scriptOf(excerpt *cache.ConfigExcerpt) (string, error) {
	return host.FlowScript(excerpt)
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
