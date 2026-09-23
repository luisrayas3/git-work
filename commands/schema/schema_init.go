package schemacmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/schema"
)

type initOptions struct {
	dryRun bool
}

func newSchemaInitCommand(env *execenv.Env) *cobra.Command {
	options := initOptions{}

	cmd := &cobra.Command{
		Use:   "init [PRESET]",
		Short: "Create a preset's types and fields",
		Long: `Instantiate an embedded preset as config entities: one entity per type and
one per (type, field). With no argument, the jira preset.

It refuses when a field entity already exists, archived or not, because a
preset is a starting point and not a merge; to change a schema that is already
there, export it, edit it and import it.

The ids of the entities it creates are printed, one per line, and nothing else.`,
		Example: `git work schema init
git work schema init jira`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaInit(env, options, args)
		}),
		ValidArgsFunction: completion.From(schema.PresetNames()),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print the changes that would be committed, and write nothing")

	return cmd
}

func runSchemaInit(env *execenv.Env, opts initOptions, args []string) error {
	name := schema.DefaultPreset
	if len(args) == 1 {
		name = args[0]
	}

	existing := env.Backend.Schema().Query(cache.ConfigQuery{
		Shape:           config.ShapeField,
		IncludeArchived: true,
	})
	if len(existing) > 0 {
		keys := make([]string, 0, len(existing))
		for _, excerpt := range existing {
			keys = append(keys, excerpt.Key)
		}
		return fmt.Errorf("the schema already has %d fields (%s); export, edit and import it instead",
			len(existing), strings.Join(keys, ", "))
	}

	doc, err := schema.Preset(name)
	if err != nil {
		return err
	}

	return importDocument(env, doc, importOptions{dryRun: opts.dryRun})
}
