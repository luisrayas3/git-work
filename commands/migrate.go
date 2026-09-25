package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/migrate"
)

type migrateOptions struct {
	dryRun bool
}

func newMigrateCommand(env *execenv.Env) *cobra.Command {
	options := migrateOptions{}

	cmd := &cobra.Command{
		Use:   "migrate [--dry-run]",
		Short: "Migrate the store once from git-bug's format to the owned model",
		Long: `Migrate the store once from git-bug's format to the owned model (bf6f392).

Every issue under refs/issues/* is replayed under refs/work-issues/*,
one commit per original commit, with its id, its comment ids and its
lamport times unchanged, and the label taxonomy becomes fields;
the identities are copied to refs/work-users/*. The old refs are kept.

The schema has to be in place first: git work schema import schema.yaml.
The command refuses when refs/work-issues/* holds anything.
doc/design/store-migration.md is the design.`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(env, options)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print what would be written and write nothing but the identity copy")

	return cmd
}

func runMigrate(env *execenv.Env, options migrateOptions) error {
	copied, err := migrate.CopyIdentities(env.Repo)
	if err != nil {
		return err
	}
	if copied > 0 {
		env.Out.Printf("copied %d identities to refs/work-users/*\n", copied)
	}

	s, err := env.Backend.LoadSchema()
	if err != nil {
		return err
	}
	plan, err := migrate.Prepare(env.Repo, s)
	if err != nil {
		return err
	}

	problems := 0
	packs := 0
	for _, i := range plan.Issues {
		packs += i.Packs
		env.Out.Printf("%s %-8s %s\n", i.Id.Human(), i.Type, i.Title)
		for _, problem := range i.Problems {
			problems++
			env.Out.Printf("  the schema would refuse: %s\n", strings.ReplaceAll(problem, "\n", "\n  "))
		}
	}
	env.Out.Printf("%d issues, %d commits, %d with fields the schema would refuse\n", len(plan.Issues), packs, problems)

	if options.dryRun {
		env.Out.Println("dry run: nothing written")
		return nil
	}

	unlock, err := cache.LockWrite(env.Repo)
	if err != nil {
		return err
	}
	defer unlock()

	if err := plan.Apply(env.Repo); err != nil {
		return fmt.Errorf("migration aborted, nothing written: %w", err)
	}
	env.Out.Printf("migrated %d issues to refs/work-issues/*; refs/issues/* is kept and frozen\n", len(plan.Issues))
	return nil
}
