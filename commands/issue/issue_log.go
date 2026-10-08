package issuecmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/util/colors"
)

type issueLogOptions struct {
	from, to       string
	includeArchive bool
	format         string
}

func newIssueLogCommand(env *execenv.Env) *cobra.Command {
	options := issueLogOptions{}

	cmd := &cobra.Command{
		Use:   "log [ID|PROGRAM]",
		Short: "Print the history of one issue or of many",
		Long: `Print operations, oldest first, in the shape the store holds it. Each entry
names the issue it belongs to. This is what a status report is generated from.

The argument is one issue — an id prefix or an alias — or a jq program over
the same array the list runs on, the unarchived issues, in which case the
selected issues' operations all come back, ordered by time. With no argument
the selection is the list's default: every unarchived issue.
--include-archive brings the archived back into the program's input; an id
names its issue archived or not, with or without it.

An id is tried first, because no id prefix is a valid jq program; what does not
resolve is compiled as one, and if that fails too the error names both.

--from TIME and --to TIME select the operations written in the half-open
window [from, to), so back-to-back windows neither drop an operation nor count
it twice. ` + TimeFormsHelp + `. The cut is each operation's own wall-clock
time, so an operation pulled late still lands in the window it was written in.`,
		Example: `What one issue has been through:
git work issue log 6a1b2c3

Everything that happened this past week:
git work issue log --from 7d --format text
`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueLog(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false
	flags.StringVar(&options.from, "from", "", "only operations at or after TIME")
	flags.StringVar(&options.to, "to", "", "only operations before TIME")
	flags.BoolVar(&options.includeArchive, "include-archive", false, "include the archived issues in the program's input")

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runIssueLog(env *execenv.Env, opts issueLogOptions, args []string) error {
	from, err := parseTimeFlag("--from", opts.from)
	if err != nil {
		return err
	}
	to, err := parseTimeFlag("--to", opts.to)
	if err != nil {
		return err
	}

	var idOrProgram string
	if len(args) == 1 {
		idOrProgram = args[0]
	}

	entries, err := host.IssueLogBetween(env.Backend, idOrProgram, from, to, opts.includeArchive)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(entries)
	case "text":
		for _, entry := range entries {
			env.Out.Printf("%s\t%s\t%s\t%s\t%s\n",
				colors.Cyan(shortId(entry.Issue)),
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

// shortId is the issue column of the text form: one log may span many issues,
// so every line says which one it is about.
func shortId(id string) string {
	return entity.Id(id).Human()
}
