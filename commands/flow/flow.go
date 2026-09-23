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
// Running a flow is the runtime's business and lives elsewhere;
// this tree is the entity's surface: list, get, import, export, log,
// archive and rm, to the map in doc/design/cli-convention.md.
package flowcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/flow"
)

// The two attributes a flow entity carries (E3).
const (
	attrScript      = "script"
	attrDescription = "description"
)

// flowEntry is one flow in a listing.
//
// Archived is always false in a listing, which hides the archived,
// and is printed anyway so that a consumer reads one shape
// whatever the command asked for.
type flowEntry struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Params      []paramEntry `json:"params"`
	Archived    bool         `json:"archived"`
}

// paramEntry is one argument of a flow.
//
// A parameter with no default is required,
// and its default is absent rather than null,
// because `None` is a default and JSON null is what it prints as.
type paramEntry struct {
	Name     string          `json:"name"`
	Default  json.RawMessage `json:"default,omitempty"`
	Required bool            `json:"required"`
}

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

	entries, err := listFlows(env)
	if err != nil {
		return err
	}

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

// listFlows returns every unarchived flow, by name.
//
// Parsing a handful of small scripts to list their arguments is cheap,
// and it is the only place the arguments can come from:
// the description is mirrored into an attribute so that a listing has one,
// but a signature is not (E2).
func listFlows(env *execenv.Env) ([]flowEntry, error) {
	entries := []flowEntry{}

	for _, name := range env.Backend.Flows().Keys(config.ShapeFlow) {
		excerpt, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, name)
		if err != nil {
			return nil, err
		}
		entries = append(entries, flowEntryOf(env, name, excerpt))
	}

	return entries, nil
}

func flowEntryOf(env *execenv.Env, name string, excerpt *cache.ConfigExcerpt) flowEntry {
	script, _ := excerpt.AttributeString(attrScript)
	description, _ := excerpt.AttributeString(attrDescription)

	return flowEntry{
		Name:        name,
		Description: description,
		Params:      paramsOf(env, name, script),
		Archived:    excerpt.Archived,
	}
}

// paramsOf reads a script's parameters, tolerating a script that will not parse.
//
// Import refuses one, so this only happens to a flow written by another binary
// or edited by hand; a listing that failed whole because of one such flow
// would hide the ones that are fine.
func paramsOf(env *execenv.Env, name string, script string) []paramEntry {
	def, err := flow.Parse(script)
	if err != nil {
		env.Err.Printf("warning: flow %s does not parse: %v\n", name, err)
		return []paramEntry{}
	}
	return paramEntries(def)
}

func paramEntries(def *flow.Def) []paramEntry {
	params := make([]paramEntry, 0, len(def.Params))
	for _, param := range def.Params {
		params = append(params, paramEntry{
			Name:     param.Name,
			Default:  param.Default,
			Required: !param.HasDefault,
		})
	}
	return params
}

// warnDuplicates prints the flows a key silently resolves to one of (E7).
//
// Every flow command calls it, because a team that loses an edit this way
// is never told otherwise.
func warnDuplicates(env *execenv.Env) {
	flows := env.Backend.Flows()
	for _, name := range flows.Keys(config.ShapeFlow) {
		for _, duplicate := range flows.Duplicates(config.ShapeFlow, name) {
			env.Err.Printf("warning: flow %s is defined by %s too, which is ignored; archive it to repair\n",
				name, duplicate.Id().Human())
		}
	}
}

// currentExcerpt resolves a flow name to the entity it names (E7).
func currentExcerpt(env *execenv.Env, name string) (*cache.ConfigExcerpt, error) {
	excerpt, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, name)
	if err != nil {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	return excerpt, nil
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
	script, ok := excerpt.AttributeString(attrScript)
	if !ok {
		return "", fmt.Errorf("flow %s has no script", excerpt.Key)
	}
	return script, nil
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
