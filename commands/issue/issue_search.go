package issuecmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

type issueSearchOptions struct {
	includeArchive bool
	format         string
}

func newIssueSearchCommand(env *execenv.Env) *cobra.Command {
	options := issueSearchOptions{}

	cmd := &cobra.Command{
		Use:   "search TEXT",
		Short: "Find the issues whose id, alias, title or description contain TEXT",
		Long: `Print the issues whose id, alias, title or description contain TEXT, as one
array of the excerpts ` + "`git work issue list`" + ` prints.

The description is the issue's first comment; the other comments are not
searched. The match ignores case. An id matches by prefix; an alias, the
title and the description match anywhere in them.

The issues an id or an alias names come first, then those whose title
matches, then those that match in the description alone; within each, the
last edited comes first. Empty TEXT matches every issue.

The input is the unarchived issues; --include-archive brings the archived
back. --format text prints one line per issue (id, status, title).`,
		Example: `The issues that mention a word:
git work issue search 'rate limit'

By Jira key, for a human:
git work issue search PROJ-12 --format text
`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueSearch(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false
	flags.BoolVar(&options.includeArchive, "include-archive", false, "search the archived issues too")

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runIssueSearch(env *execenv.Env, opts issueSearchOptions, args []string) error {
	found, err := host.IssueSearch(env.Backend, args[0], opts.includeArchive)
	if err != nil {
		return err
	}
	values := []any{found}

	switch opts.format {
	case "json":
		return printValues(env, values)
	case "text":
		if printed, err := printIssueLines(env, values); printed || err != nil {
			return err
		}
		// nothing found: an empty array, which has no line to draw
		return printValues(env, values)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}
