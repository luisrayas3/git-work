package schemacmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// warnDuplicates prints, on stderr, every entity a key resolves away from.
//
// Two clones defining one key before either pushes is the one failure the
// entity boundary leaves (E7). The winner is deterministic, and saying so is
// the difference between a team repairing it and a team losing an edit.
func warnDuplicates(env *execenv.Env) {
	env.Warn(host.SchemaDuplicates(env.Backend))
}

// printImport is the end of import and of init: --dry-run prints the changes
// per entity, as JSON, in the same shape `issue set --dry-run` prints its
// operations, and a real write prints the ids it created and nothing else
// (cli-convention.md).
func printImport(env *execenv.Env, dryRun bool, changes []schema.Change, created []entity.Id) error {
	if dryRun {
		return env.Out.PrintJSON(changes)
	}

	printIds(env, created)
	return nil
}

// printIds prints the id of each entity that was created, one per line.
func printIds(env *execenv.Env, created []entity.Id) {
	for _, id := range created {
		env.Out.Println(id.String())
	}
}

// KeyCompletion completes a type or field key.
func KeyCompletion(env *execenv.Env) completion.ValidArgsFunction {
	return func(cmd *cobra.Command, args []string, toComplete string) (completions []string, directives cobra.ShellCompDirective) {
		if err := execenv.LoadBackend(env)(cmd, args); err != nil {
			return completion.HandleError(err)
		}
		defer func() {
			_ = env.Backend.Close()
		}()

		for _, shape := range []config.Shape{config.ShapeType, config.ShapeField} {
			for _, key := range env.Backend.Schema().Keys(shape) {
				if strings.Contains(key, strings.TrimSpace(toComplete)) {
					completions = append(completions, key+"\t"+string(shape))
				}
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}
