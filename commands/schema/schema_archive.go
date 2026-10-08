package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type archiveOptions struct {
	id string
}

func newSchemaArchiveCommand(env *execenv.Env) *cobra.Command {
	options := archiveOptions{}

	cmd := &cobra.Command{
		Use:   "archive KEY | --id ID",
		Short: "Archive a type or a field",
		Long: `Archive a type or a field: the replicated removal, the one that reaches every
clone. A ref cannot be deleted across clones — it comes back on the next pull —
so removing a config entity is an operation like any other, and setting the
flag back undoes it.

An archived field stops being settable; it does not vanish from the issues that
have it, because a schema says what may be written now, never what was written
before. KEY is a type key or a field key, <type>/<field>.

A KEY two entities hold, two clones having defined it before exchanging, is
refused, naming both ids: name the one to archive with --id ID, a full id or a
unique prefix, archived or not, which is never read as a key.`,
		Example: `git work schema archive task/estimate
git work schema archive --id db9cdb7`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaArchive(env, options, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	addIdFlag(cmd, &options.id)

	return cmd
}

func runSchemaArchive(env *execenv.Env, opts archiveOptions, args []string) error {
	ref, err := schemaRef(args, opts.id, true)
	if err != nil {
		return err
	}

	warnDuplicates(env)

	// Archiving a type leaves its fields behind, and the host names them:
	// a multi-entity change is not atomic here (AGENTS.md), so nothing else
	// is archived and the honest thing is to say what is left.
	warnings, err := host.SchemaArchive(env.Backend, ref)
	env.Warn(warnings)

	return err
}
