package issuecmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/util/colors"
)

type issueLogOptions struct {
	format string
}

func newIssueLogCommand(env *execenv.Env) *cobra.Command {
	options := issueLogOptions{}

	cmd := &cobra.Command{
		Use:   "log ID",
		Short: "Print the operations an issue is made of",
		Long: `Print the issue's history: every operation, oldest first, in the shape the
store holds it. This is what a status report is generated from.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueLog(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runIssueLog(env *execenv.Env, opts issueLogOptions, args []string) error {
	i, err := resolveIssue(env, args[0])
	if err != nil {
		return err
	}

	ops := i.Snapshot().AllOperations()
	entries := make([]cmdjson.IssueOperation, len(ops))
	for at, op := range ops {
		entry, err := cmdjson.NewIssueOperation(op)
		if err != nil {
			return err
		}
		entries[at] = entry
	}

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(entries)
	case "text":
		for _, entry := range entries {
			env.Out.Printf("%s\t%s\t%s\t%s\n",
				colors.Cyan(entry.HumanId),
				colors.Yellow(entry.Type),
				time.Unix(entry.UnixTime, 0).Format(time.RFC3339),
				colors.Magenta(entry.Author.Name),
			)
		}
		return nil
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}
