package issuecmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/util/colors"
)

type issueGetOptions struct {
	format string
}

func newIssueGetCommand(env *execenv.Env) *cobra.Command {
	options := issueGetOptions{}

	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Print one issue whole",
		Long: `Print the whole issue: its fields, its people and its comments.

get pairs with set at the document level, so there is no per-field getter:
a field is ` + "`git work issue get ID | jq .fields.status`" + `.
ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueGet(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runIssueGet(env *execenv.Env, opts issueGetOptions, args []string) error {
	i, err := resolveIssue(env, args[0])
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
	case "text":
		return issueTextFormatter(env, snap)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

func issueTextFormatter(env *execenv.Env, snapshot *issue.Snapshot) error {
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
