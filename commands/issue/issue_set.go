package issuecmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
)

// writeOptions are the flags every writer that changes an issue has.
type writeOptions struct {
	dryRun bool
}

func addWriteFlags(cmd *cobra.Command, options *writeOptions) {
	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print the operations that would be committed, and write nothing")
}

func newIssueSetCommand(env *execenv.Env) *cobra.Command {
	options := writeOptions{}

	cmd := &cobra.Command{
		Use:   "set ID FIELDS|-",
		Short: "Set fields of an issue",
		Long: `Set fields of an issue from a JSON object, given as the argument or on
standard input: one SetField operation per key, all of them in one commit.

  git work issue set 2f15 '{"status":"done","estimate":3}'

A null clears a field. Setting a field replaces it whole, last writer wins,
which is what a cardinality-one relation wants too: {"parent":"6a1b2c3"}.
For the items of a list-valued field, use add and remove instead.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueSet(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	addWriteFlags(cmd, &options)

	return cmd
}

func runIssueSet(env *execenv.Env, opts writeOptions, args []string) error {
	i, err := resolveIssue(env, args[0])
	if err != nil {
		return err
	}

	fields, err := readFields(env, args[1])
	if err != nil {
		return err
	}

	ops, err := i.PlanSetFields(fields)
	if err != nil {
		return err
	}

	return commitOrPrint(env, i, opts, ops)
}

func newIssueAddCommand(env *execenv.Env) *cobra.Command {
	options := writeOptions{}

	cmd := &cobra.Command{
		Use:   "add ID ITEMS|-",
		Short: "Add items to list-valued fields of an issue",
		Long: `Add items to list-valued fields from a JSON object of lists, given as the
argument or on standard input: one AddValue operation per item, one commit.

  git work issue add 2f15 '{"labels":["area:core","prio:high"]}'

Items have set semantics, so two people adding two items concurrently both
win. A many-cardinality relation is a list field too, of the other issues'
ids; an item that is the unambiguous prefix of one issue is taken as its id.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueAdd(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	addWriteFlags(cmd, &options)

	return cmd
}

func runIssueAdd(env *execenv.Env, opts writeOptions, args []string) error {
	i, items, err := resolveIssueAndItems(env, args)
	if err != nil {
		return err
	}

	ops, err := i.PlanAddValues(items)
	if err != nil {
		return err
	}

	return commitOrPrint(env, i, opts, ops)
}

func newIssueRemoveCommand(env *execenv.Env) *cobra.Command {
	options := writeOptions{}

	cmd := &cobra.Command{
		Use:   "remove ID ITEMS|-",
		Short: "Remove items from list-valued fields of an issue",
		Long: `Remove items from list-valued fields from a JSON object of lists, given as
the argument or on standard input: one RemoveValue operation per item, one commit.

  git work issue remove 2f15 '{"labels":["prio:high"]}'

Removing an item that is not there changes nothing.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueRemove(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	addWriteFlags(cmd, &options)

	return cmd
}

func runIssueRemove(env *execenv.Env, opts writeOptions, args []string) error {
	i, items, err := resolveIssueAndItems(env, args)
	if err != nil {
		return err
	}

	ops, err := i.PlanRemoveValues(items)
	if err != nil {
		return err
	}

	return commitOrPrint(env, i, opts, ops)
}

func newIssueArchiveCommand(env *execenv.Env) *cobra.Command {
	options := writeOptions{}

	cmd := &cobra.Command{
		Use:   "archive ID",
		Short: "Archive an issue",
		Long: `Archive an issue: set its archived field, which is one of the three fields
every issue has. This is the replicated removal, the one that reaches every
clone; rm only deletes the local ref.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueArchive(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	addWriteFlags(cmd, &options)

	return cmd
}

func runIssueArchive(env *execenv.Env, opts writeOptions, args []string) error {
	i, err := resolveIssue(env, args[0])
	if err != nil {
		return err
	}

	ops, err := i.PlanSetFields(map[string]issue.Value{
		issue.ArchivedKey: issue.MustValue(true),
	})
	if err != nil {
		return err
	}

	return commitOrPrint(env, i, opts, ops)
}

// resolveIssueAndItems reads the two arguments add and remove share,
// resolving each item that names another issue by a prefix.
func resolveIssueAndItems(env *execenv.Env, args []string) (*cache.IssueCache, map[string][]issue.Value, error) {
	i, err := resolveIssue(env, args[0])
	if err != nil {
		return nil, nil, err
	}

	items, err := readItems(env, args[1])
	if err != nil {
		return nil, nil, err
	}

	for key, list := range items {
		for at, item := range list {
			list[at] = resolveItem(env, key, item)
		}
	}

	return i, items, nil
}

// commitOrPrint is the end of every writer: --dry-run prints the operations in
// their wire shape and writes nothing, otherwise they land as one commit and
// nothing is printed.
func commitOrPrint(env *execenv.Env, i *cache.IssueCache, opts writeOptions, ops []issue.Operation) error {
	if opts.dryRun {
		return env.Out.PrintJSON(ops)
	}
	return i.CommitOperations(ops)
}
