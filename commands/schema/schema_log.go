package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type logOptions struct {
	format string
}

func newSchemaLogCommand(env *execenv.Env) *cobra.Command {
	options := logOptions{}

	cmd := &cobra.Command{
		Use:   "log [KEY]",
		Short: "Print the schema's history",
		Long: `Print the schema's history: every operation of every type and field entity,
one JSON object per line, oldest first within each entity. With a KEY, only
that entity's.

This is what says who added a status and when. KEY is a type key or a field
key, <type>/<field>.`,
		Example: `git work schema log
git work schema log task/status`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaLog(env, options, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.format, "format", "f", "json",
		"Select the output formatting style. Valid values are [json,text]")

	return cmd
}

func runSchemaLog(env *execenv.Env, opts logOptions, args []string) error {
	warnDuplicates(env)

	key := ""
	if len(args) == 1 {
		key = args[0]
	}

	entries, err := host.SchemaLog(env.Backend, key)
	if err != nil {
		return err
	}

	return cmdjson.WriteConfigOperations(env.Out.Raw(), opts.format, entries)
}
