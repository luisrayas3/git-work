package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type rmOptions struct {
	id string
}

func newSchemaRmCommand(env *execenv.Env) *cobra.Command {
	options := rmOptions{}

	cmd := &cobra.Command{
		Use:   "rm KEY | --id ID",
		Short: "Remove a type or a field from the local repository",
		Long: `Remove a type's or a field's local ref. This is local: the entity comes back on
the next pull. The replicated removal is archive.

KEY is a type key or a field key, <type>/<field>. A KEY two entities hold is
refused, naming both ids: name one with --id ID, a full id or a unique prefix,
archived or not, which is never read as a key.`,
		Example: `git work schema rm task/estimate
git work schema rm --id db9cdb7`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaRm(env, options, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	addIdFlag(cmd, &options.id)

	return cmd
}

func runSchemaRm(env *execenv.Env, opts rmOptions, args []string) error {
	ref, err := schemaRef(args, opts.id, true)
	if err != nil {
		return err
	}

	warnDuplicates(env)

	return host.SchemaRm(env.Backend, ref)
}
