package issuecmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/colors"
)

type issueGetOptions struct {
	at     string
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
ID is an id prefix or an alias.

--at TIME prints the issue as it stood at that moment, replayed from its
operations: ` + TimeFormsHelp + `.
An issue created after TIME did not exist yet, and that is an error.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueGet(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false
	flags.StringVar(&options.at, "at", "", "the issue as it stood at TIME")

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runIssueGet(env *execenv.Env, opts issueGetOptions, args []string) error {
	at, err := parseTimeFlag("--at", opts.at)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		document, err := host.IssueGetAt(env.Backend, args[0], at)
		if err != nil {
			return err
		}
		return env.Out.PrintJSON(document)
	case "text":
		// The text form prints what the JSON projection drops, an author's
		// email among it, so it reads the snapshot rather than the document.
		snap, err := host.IssueSnapshotAt(env.Backend, args[0], at)
		if err != nil {
			return err
		}
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

	// Header: the drawn id, and the hash beside it when that is an alias, the
	// one page where a key is mapped to the hash an agent printed (alias-ids.md A5)
	name := env.Backend.IssueHumanId(snapshot.Id())
	if name != snapshot.Id().Human() {
		name += " (" + snapshot.Id().Human() + ")"
	}
	env.Out.Printf("%s [%s] %s\n\n",
		colors.Cyan(name),
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

	// Fields, verbatim JSON, title excluded since it is the header; a people
	// field is the person's name, as the views draw it (host.UserName)
	typeKey, _ := snapshot.FieldString(schema.TypeKey)
	s, _ := env.Backend.LoadSchema()
	for _, key := range snapshot.FieldKeys() {
		if key == issue.TitleKey {
			continue
		}
		if s != nil {
			if field, ok := s.Field(typeKey, key); ok && field.Kind == schema.KindIdentity {
				if id, ok := issue.String(snapshot.Fields[key]); ok && id != "" {
					env.Out.Printf("%s: %s\n", key, host.UserName(env.Backend, id))
					continue
				}
			}
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
