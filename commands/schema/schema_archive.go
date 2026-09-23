package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
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

	cached, err := resolveKey(env, args[0])
	if err != nil {
		return err
	}

	if _, err := cached.SetArchived(true); err != nil {
		return err
	}
	if err := cached.Commit(); err != nil {
		return err
	}

	warnOrphanedFields(env, cached.Shape(), cached.Key())

	return nil
}

// warnOrphanedFields says when archiving a type leaves its fields behind.
//
// Nothing is refused and nothing else is archived: a multi-entity change is
// not atomic here (AGENTS.md), so the honest thing is to name what is left.
func warnOrphanedFields(env *execenv.Env, shape config.Shape, key string) {
	if shape != config.ShapeType {
		return
	}

	for _, fieldKey := range env.Backend.Schema().Keys(config.ShapeField) {
		typeKey, _, ok := config.SplitFieldKey(fieldKey)
		if ok && typeKey == key {
			env.Err.Printf("warning: field %s is still live on the archived type %s\n", fieldKey, key)
		}
	}
}
