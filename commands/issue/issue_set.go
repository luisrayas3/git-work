package issuecmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
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
	fields, err := readFields(env, args[1])
	if err != nil {
		return err
	}

	ops, err := host.IssueSet(env.Backend, args[0], fields, opts.dryRun)
	if err != nil {
		return err
	}

	return printOperations(env, opts, ops)
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
	items, err := readItems(env, args[1])
	if err != nil {
		return err
	}

	ops, err := host.IssueAdd(env.Backend, args[0], items, opts.dryRun)
	if err != nil {
		return err
	}

	return printOperations(env, opts, ops)
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
	items, err := readItems(env, args[1])
	if err != nil {
		return err
	}

	ops, err := host.IssueRemove(env.Backend, args[0], items, opts.dryRun)
	if err != nil {
		return err
	}

	return printOperations(env, opts, ops)
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
	ops, err := host.IssueArchive(env.Backend, args[0], opts.dryRun)
	if err != nil {
		return err
	}

	return printOperations(env, opts, ops)
}

// printOperations is the end of every writer: --dry-run prints the operations
// in their wire shape, and a real write prints nothing at all.
//
// The operations reaching here are already checked against the schema, at
// planning time, so a dry run reports what a commit would refuse (bb9e89e).
func printOperations(env *execenv.Env, opts writeOptions, ops []issue.Operation) error {
	if opts.dryRun {
		warnUnvalidated(env)
		return env.Out.PrintJSON(ops)
	}
	return nil
}

// warnUnvalidated says, on stderr, that a dry run checked nothing.
//
// With no type entity in the store there is no schema to measure a write
// against (E4), and silence would read as approval.
func warnUnvalidated(env *execenv.Env) {
	s, err := env.Backend.LoadSchema()
	if err != nil {
		env.Err.Printf("the schema could not be read: %v\n", err)
		return
	}
	if s.Empty() {
		env.Err.Println("no type is defined, so nothing was checked against the schema; git work schema init")
	}
}
