package schemacmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/schema"
)

// warnDuplicates prints, on stderr, every entity a key resolves away from.
//
// Two clones defining one key before either pushes is the one failure the
// entity boundary leaves (E7). The winner is deterministic, and saying so is
// the difference between a team repairing it and a team losing an edit.
func warnDuplicates(env *execenv.Env) {
	for _, excerpt := range env.Backend.Schema().AllDuplicates() {
		env.Err.Printf("warning: %s %s is defined twice; %s is ignored, archive or import it\n",
			excerpt.Shape, excerpt.Key, excerpt.Id().Human())
	}
}

// warnProblems prints what compiling the schema could not make sense of.
//
// A read never fails on what was written before (D6), so a dangling target
// type or an unreadable attribute is said out loud and nothing more.
func warnProblems(env *execenv.Env, s *schema.Schema) {
	for _, problem := range s.Problems {
		env.Err.Printf("warning: %s\n", problem)
	}
}

// loadSchema is the reader's first step: the live schema, with whatever it
// could not read and whichever keys are defined twice reported on stderr.
func loadSchema(env *execenv.Env) (*schema.Schema, error) {
	warnDuplicates(env)

	s, err := env.Backend.LoadSchema()
	if err != nil {
		return nil, err
	}
	warnProblems(env, s)

	return s, nil
}

// readArg returns an argument's file, or standard input when it is "-".
func readArg(env *execenv.Env, arg string) ([]byte, error) {
	if arg == "-" {
		data, err := io.ReadAll(env.In)
		if err != nil {
			return nil, fmt.Errorf("reading the standard input: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(arg)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// applyChanges writes what reconcile computed, one entity at a time.
//
// An import that touches five entities is five commits: there is no atomic
// multi-entity commit in this store, and there does not need to be, because
// each entity is valid on its own at every step (E9).
// Only the ids of created entities reach stdout (cli-convention.md).
func applyChanges(env *execenv.Env, changes []schema.Change) error {
	for _, change := range changes {
		if err := applyChange(env, change); err != nil {
			return fmt.Errorf("%s %s: %w", change.Shape, change.Key, err)
		}
	}
	return nil
}

func applyChange(env *execenv.Env, change schema.Change) error {
	schemaCache := env.Backend.Schema()

	switch change.Action {
	case schema.ActionCreate:
		created, _, err := schemaCache.New(change.Shape, change.Key, change.Set)
		if err != nil {
			return err
		}
		env.Out.Println(created.Id().String())
		return nil

	case schema.ActionUpdate:
		cached, err := schemaCache.Resolve(change.Id)
		if err != nil {
			return err
		}
		// one pack, one commit: a renumbered list of values lands together
		return cached.Update(change.Set, change.Remove)

	case schema.ActionArchive:
		cached, err := schemaCache.Resolve(change.Id)
		if err != nil {
			return err
		}
		if _, err := cached.SetArchived(true); err != nil {
			return err
		}
		return cached.Commit()

	default:
		return fmt.Errorf("unknown action %s", change.Action)
	}
}

// printChanges is what --dry-run prints: the changes per entity, as JSON,
// in the same shape `issue set --dry-run` prints its operations.
func printChanges(env *execenv.Env, changes []schema.Change) error {
	return env.Out.PrintJSON(changes)
}

// storeTypeKeys are the types already in the store, which a partial document
// may name without defining them.
func storeTypeKeys(env *execenv.Env) []string {
	return env.Backend.Schema().Keys(config.ShapeType)
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

// resolveKey finds the entity a KEY argument names, type first, field second.
func resolveKey(env *execenv.Env, key string) (*cache.ConfigCache, error) {
	return env.Backend.Schema().ResolveSchemaKey(key)
}
