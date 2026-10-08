package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type logOptions struct {
	format string
	id     string
}

func newSchemaLogCommand(env *execenv.Env) *cobra.Command {
	options := logOptions{}

	cmd := &cobra.Command{
		Use:   "log [KEY | --id ID]",
		Short: "Print the schema's history",
		Long: `Print the schema's history: every operation of every type and field entity,
one JSON object per line, oldest first within each entity. With a KEY, only
the operations of the entities holding it: one in the ordinary case, and every
one, the current first, when two clones defined the key before exchanging.
With --id ID, a full id or a unique prefix, only that entity's, archived or
not; an ID is never read as a key.

This is what says who added a status and when. KEY is a type key or a field
key, <type>/<field>.`,
		Example: `git work schema log
git work schema log task/status
git work schema log --id db9cdb7`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaLog(env, options, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addIdFlag(cmd, &options.id)
	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runSchemaLog(env *execenv.Env, opts logOptions, args []string) error {
	ref, err := schemaRef(args, opts.id, false)
	if err != nil {
		return err
	}

	warnDuplicates(env)

	entries, err := host.SchemaLog(env.Backend, ref)
	if err != nil {
		return err
	}

	return cmdjson.WriteConfigOperations(env.Out.Raw(), opts.format, entries)
}
