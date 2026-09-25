package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

func newSchemaRmCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm KEY",
		Short: "Remove a type or a field from the local repository",
		Long: `Remove a type's or a field's local ref. This is local: the entity comes back on
the next pull. The replicated removal is archive.

KEY is a type key or a field key, <type>/<field>.`,
		Example: `git work schema rm task/estimate`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaRm(env, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	return cmd
}

func runSchemaRm(env *execenv.Env, args []string) error {
	warnDuplicates(env)

	return host.SchemaRm(env.Backend, args[0])
}
