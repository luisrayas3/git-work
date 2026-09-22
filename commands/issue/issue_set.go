package issuecmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
)

type issueSetOptions struct {
	add    []string
	remove []string
}

func newIssueSetCommand(env *execenv.Env) *cobra.Command {
	options := issueSetOptions{}

	cmd := &cobra.Command{
		Use:   "set ISSUE_ID KEY [VALUE]",
		Short: "Set a field of an issue, or add and remove items of a list field",
		Long: `Set one field of an issue to a value, replacing it whole,
or add and remove items of a list-valued field with set semantics,
so that two people adding two items concurrently both win.

A value or item is JSON when it parses as JSON and a string otherwise:
  git work issue set 2f15 status closed
  git work issue set 2f15 estimate 3
  git work issue set 2f15 assignee null              # clears the field
  git work issue set 2f15 labels --add area:core      # one item
  git work issue set 2f15 blocks --add 9a3c --remove 1e77
  git work issue set 2f15 labels '["area:core"]'      # replaces the list

Relations are fields too: a cardinality-one relation is set with a value,
a cardinality-many one with --add and --remove of the other issue's id.`,
		Args:    cobra.RangeArgs(2, 3),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueSet(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringArrayVar(&options.add, "add", nil,
		"Add an item to the list field; repeatable")
	flags.StringArrayVar(&options.remove, "remove", nil,
		"Remove an item from the list field; repeatable")

	return cmd
}

func runIssueSet(env *execenv.Env, opts issueSetOptions, args []string) error {
	i, rest, err := resolveIssue(env.Backend, args)
	if err != nil {
		return err
	}
	key := rest[0]

	hasValue := len(rest) == 2
	hasItems := len(opts.add)+len(opts.remove) > 0

	switch {
	case hasValue && hasItems:
		return errors.New("give a value or --add/--remove items, not both")
	case !hasValue && !hasItems:
		return errors.New("a value or at least one --add/--remove item is required")
	case hasValue:
		if _, err := i.SetField(key, parseValue(rest[1])); err != nil {
			return err
		}
	default:
		for _, item := range opts.add {
			if _, err := i.AddValue(key, resolveItem(env, key, item)); err != nil {
				return err
			}
		}
		for _, item := range opts.remove {
			if _, err := i.RemoveValue(key, resolveItem(env, key, item)); err != nil {
				return err
			}
		}
	}

	return i.Commit()
}
