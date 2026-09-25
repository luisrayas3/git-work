package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

func newSchemaArchiveCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive KEY",
		Short: "Archive a type or a field",
		Long: `Archive a type or a field: the replicated removal, the one that reaches every
clone. A ref cannot be deleted across clones — it comes back on the next pull —
so removing a config entity is an operation like any other, and setting the
flag back undoes it.

An archived field stops being settable; it does not vanish from the issues that
have it, because a schema says what may be written now, never what was written
before. KEY is a type key or a field key, <type>/<field>.`,
		Example: `git work schema archive task/estimate`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaArchive(env, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	return cmd
}

func runSchemaArchive(env *execenv.Env, args []string) error {
	warnDuplicates(env)

	// Archiving a type leaves its fields behind, and the host names them:
	// a multi-entity change is not atomic here (AGENTS.md), so nothing else
	// is archived and the honest thing is to say what is left.
	warnings, err := host.SchemaArchive(env.Backend, args[0])
	env.Warn(warnings)

	return err
}
