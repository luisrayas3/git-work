package issuecmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/util/colors"
)

type issueShowOptions struct {
	format string
}

func newIssueShowCommand(env *execenv.Env) *cobra.Command {
	options := issueShowOptions{}

	cmd := &cobra.Command{
		Use:     "show ISSUE_ID",
		Short:   "Display the details of an issue",
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueShow(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.format, "format", "f", "default",
		"Select the output formatting style. Valid values are [default,json]")
	cmd.RegisterFlagCompletionFunc("format", completion.From([]string{"default", "json"}))

	return cmd
}

func runIssueShow(env *execenv.Env, opts issueShowOptions, args []string) error {
	i, _, err := resolveIssue(env.Backend, args)
	if err != nil {
		return err
	}

	snap := i.Snapshot()

	if len(snap.Comments) == 0 {
		return errors.New("invalid issue: no comment")
	}

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(cmdjson.NewIssueSnapshot(snap))
	case "default":
		return showDefaultFormatter(env, snap)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

func showDefaultFormatter(env *execenv.Env, snapshot *issue.Snapshot) error {
	status, ok := snapshot.FieldString("status")
	if !ok {
		status = "-"
	}

	// Header
	env.Out.Printf("%s [%s] %s\n\n",
		colors.Cyan(snapshot.Id().Human()),
		colors.Yellow(status),
		snapshot.Title(),
	)

	env.Out.Printf("%s opened this issue %s\n",
		colors.Magenta(snapshot.Author.DisplayName()),
		snapshot.CreateTime.String(),
	)

	env.Out.Printf("This was last edited at %s\n\n",
		snapshot.EditTime().String(),
	)

	// Fields, verbatim JSON, title excluded since it is the header
	for _, key := range snapshot.FieldKeys() {
		if key == issue.TitleKey {
			continue
		}
		env.Out.Printf("%s: %s\n", key, string(snapshot.Fields[key]))
	}

	if len(snapshot.Fields) > 1 {
		env.Out.Println()
	}

	// Actors
	var actors = make([]string, len(snapshot.Actors))
	for i := range snapshot.Actors {
		actors[i] = snapshot.Actors[i].DisplayName()
	}

	env.Out.Printf("actors: %s\n",
		strings.Join(actors, ", "),
	)

	// Participants
	var participants = make([]string, len(snapshot.Participants))
	for i := range snapshot.Participants {
		participants[i] = snapshot.Participants[i].DisplayName()
	}

	env.Out.Printf("participants: %s\n\n",
		strings.Join(participants, ", "),
	)

	// Comments
	indent := "  "

	for i, comment := range snapshot.Comments {
		var message string
		env.Out.Printf("%s%s #%d %s <%s>\n\n",
			indent,
			comment.CombinedId().Human(),
			i,
			comment.Author.DisplayName(),
			comment.Author.Email(),
		)

		if comment.Message == "" {
			message = colors.BlackBold(colors.WhiteBg("No description provided."))
		} else {
			message = comment.Message
		}

		env.Out.Printf("%s%s\n\n\n",
			indent,
			message,
		)
	}

	return nil
}
