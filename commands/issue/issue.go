// Package issuecmd is the `git work issue` command tree, over entities/issue.
//
// It is plumbing (e8d6426): explicit ids, no editor, no implicit selection,
// values as JSON.
// The `bug` tree keeps serving the old entity until the store is migrated (bf6f392).
package issuecmd

import (
	"fmt"
	"strings"

	text "github.com/MichaelMure/go-term-text"
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query"
	"github.com/git-bug/git-bug/util/colors"
)

type issueOptions struct {
	authorQuery         []string
	metadataQuery       []string
	participantQuery    []string
	actorQuery          []string
	labelQuery          []string
	titleQuery          []string
	noQuery             []string
	sortBy              string
	sortDirection       string
	outputFormat        string
	outputFormatChanged bool
}

func NewIssueCommand(env *execenv.Env) *cobra.Command {
	options := issueOptions{}

	cmd := &cobra.Command{
		Use:   "issue [QUERY]",
		Short: "List issues",
		Long: `Display a summary of each issue.

You can pass an additional query to filter and order the list. This query can be expressed either with a simple query language, flags, a text search over the fields, or a combination of the aforementioned.

Status filters are not available yet: open and closed are categories of the status field that the schema names.`,
		Example: `List issues sorted by last edition with a query:
git work issue sort:edit-desc

List issues with a label, sorted by creation:
git work issue --label area:core --by creation

Do a text search over the fields:
git work issue "foo bar" baz
`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			options.outputFormatChanged = cmd.Flags().Changed("format")
			return runIssue(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringSliceVarP(&options.authorQuery, "author", "a", nil,
		"Filter by author")
	cmd.RegisterFlagCompletionFunc("author", completion.UserForQuery(env))
	flags.StringSliceVarP(&options.metadataQuery, "metadata", "m", nil,
		"Filter by metadata. Example: jira-key=KEY")
	flags.StringSliceVarP(&options.participantQuery, "participant", "p", nil,
		"Filter by participant")
	cmd.RegisterFlagCompletionFunc("participant", completion.UserForQuery(env))
	flags.StringSliceVarP(&options.actorQuery, "actor", "A", nil,
		"Filter by actor")
	cmd.RegisterFlagCompletionFunc("actor", completion.UserForQuery(env))
	flags.StringSliceVarP(&options.labelQuery, "label", "l", nil,
		"Filter by a value of the labels field")
	flags.StringSliceVarP(&options.titleQuery, "title", "t", nil,
		"Filter by title")
	flags.StringSliceVarP(&options.noQuery, "no", "n", nil,
		"Filter by absence of something. Valid values are [label]")
	flags.StringVarP(&options.sortBy, "by", "b", "creation",
		"Sort the results by a characteristic. Valid values are [id,creation,edit]")
	cmd.RegisterFlagCompletionFunc("by", completion.From([]string{"id", "creation", "edit"}))
	flags.StringVarP(&options.sortDirection, "direction", "d", "asc",
		"Select the sorting direction. Valid values are [asc,desc]")
	cmd.RegisterFlagCompletionFunc("direction", completion.From([]string{"asc", "desc"}))
	flags.StringVarP(&options.outputFormat, "format", "f", "default",
		"Select the output formatting style. Valid values are [default,plain,id,json]")
	cmd.RegisterFlagCompletionFunc("format",
		completion.From([]string{"default", "plain", "id", "json"}))

	cmd.AddCommand(newIssueCommentCommand(env))
	cmd.AddCommand(newIssueNewCommand(env))
	cmd.AddCommand(newIssueRmCommand(env))
	cmd.AddCommand(newIssueSetCommand(env))
	cmd.AddCommand(newIssueShowCommand(env))

	return cmd
}

func runIssue(env *execenv.Env, opts issueOptions, args []string) error {
	var q *query.Query
	var err error

	if len(args) >= 1 {
		// either the shell or cobra remove the quotes, we need them back for the query parsing
		assembled := repairQuery(args)

		q, err = query.Parse(assembled)
		if err != nil {
			return err
		}
	} else {
		q = query.NewQuery()
	}

	err = completeQuery(q, opts)
	if err != nil {
		return err
	}

	allIds, err := env.Backend.Issues().Query(q)
	if err != nil {
		return err
	}

	excerpts := make([]*cache.IssueExcerpt, len(allIds))
	for i, id := range allIds {
		excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
		if err != nil {
			return err
		}
		excerpts[i] = excerpt
	}

	switch opts.outputFormat {
	case "default":
		if opts.outputFormatChanged || env.Out.IsTerminal() {
			return issuesDefaultFormatter(env, excerpts)
		}
		return issuesPlainFormatter(env, excerpts)
	case "id":
		return issuesIDFormatter(env, excerpts)
	case "plain":
		return issuesPlainFormatter(env, excerpts)
	case "json":
		return issuesJsonFormatter(env, excerpts)
	default:
		return fmt.Errorf("unknown format %s", opts.outputFormat)
	}
}

func repairQuery(args []string) string {
	for i, arg := range args {
		split := strings.Split(arg, ":")
		for j, s := range split {
			if strings.Contains(s, " ") {
				split[j] = fmt.Sprintf("\"%s\"", s)
			}
		}
		args[i] = strings.Join(split, ":")
	}
	return strings.Join(args, " ")
}

// statusOf is the status field as a string, or a dash when unset.
func statusOf(excerpt *cache.IssueExcerpt) string {
	if s, ok := excerpt.FieldString("status"); ok {
		return s
	}
	return "-"
}

func issuesJsonFormatter(env *execenv.Env, excerpts []*cache.IssueExcerpt) error {
	out := make([]cmdjson.IssueExcerpt, len(excerpts))
	for i, excerpt := range excerpts {
		j, err := cmdjson.NewIssueExcerpt(env.Backend, excerpt)
		if err != nil {
			return err
		}
		out[i] = j
	}
	return env.Out.PrintJSON(out)
}

func issuesIDFormatter(env *execenv.Env, excerpts []*cache.IssueExcerpt) error {
	for _, excerpt := range excerpts {
		env.Out.Println(excerpt.Id().String())
	}

	return nil
}

func issuesDefaultFormatter(env *execenv.Env, excerpts []*cache.IssueExcerpt) error {
	width := env.Out.Width()
	widthId := entity.HumanIdLength
	widthStatus := 10
	widthComment := 6

	widthRemaining := width -
		widthId - 1 -
		widthStatus - 1 -
		widthComment - 1

	widthTitle := int(float32(widthRemaining-3) * 0.7)
	if widthTitle < 0 {
		widthTitle = 0
	}

	widthRemaining = widthRemaining - widthTitle - 3 - 2
	widthAuthor := widthRemaining

	for _, excerpt := range excerpts {
		author, err := env.Backend.Identities().ResolveExcerpt(excerpt.AuthorId)
		if err != nil {
			return err
		}

		titleFmt := text.LeftPadMaxLine(strings.TrimSpace(excerpt.Title()), widthTitle, 0)
		authorFmt := text.LeftPadMaxLine(author.DisplayName(), widthAuthor, 0)
		statusFmt := text.LeftPadMaxLine(statusOf(excerpt), widthStatus, 0)

		comments := fmt.Sprintf("%3d 💬", excerpt.LenComments-1)
		if excerpt.LenComments-1 <= 0 {
			comments = ""
		}
		if excerpt.LenComments-1 > 999 {
			comments = "  ∞ 💬"
		}

		env.Out.Printf("%s\t%s\t%s   %s %s\n",
			colors.Cyan(excerpt.Id().Human()),
			colors.Yellow(statusFmt),
			titleFmt,
			colors.Magenta(authorFmt),
			comments,
		)
	}
	return nil
}

func issuesPlainFormatter(env *execenv.Env, excerpts []*cache.IssueExcerpt) error {
	for _, excerpt := range excerpts {
		env.Out.Printf("%s\t%s\t%s\n", excerpt.Id().Human(), statusOf(excerpt), strings.TrimSpace(excerpt.Title()))
	}
	return nil
}

// Finish the command flags transformation into the query.Query
func completeQuery(q *query.Query, opts issueOptions) error {
	q.Author = append(q.Author, opts.authorQuery...)
	for _, str := range opts.metadataQuery {
		tokens := strings.Split(str, "=")
		if len(tokens) < 2 {
			return fmt.Errorf("no \"=\" in key=value metadata markup")
		}
		var pair query.StringPair
		pair.Key = tokens[0]
		pair.Value = tokens[1]
		q.Metadata = append(q.Metadata, pair)
	}
	q.Participant = append(q.Participant, opts.participantQuery...)
	q.Actor = append(q.Actor, opts.actorQuery...)
	q.Label = append(q.Label, opts.labelQuery...)
	q.Title = append(q.Title, opts.titleQuery...)

	for _, no := range opts.noQuery {
		switch no {
		case "label":
			q.NoLabel = true
		default:
			return fmt.Errorf("unknown \"no\" filter %s", no)
		}
	}

	switch opts.sortBy {
	case "id":
		q.OrderBy = query.OrderById
	case "creation":
		q.OrderBy = query.OrderByCreation
	case "edit":
		q.OrderBy = query.OrderByEdit
	default:
		return fmt.Errorf("unknown sort flag %s", opts.sortBy)
	}

	switch opts.sortDirection {
	case "asc":
		q.OrderDirection = query.OrderAscending
	case "desc":
		q.OrderDirection = query.OrderDescending
	default:
		return fmt.Errorf("unknown sort direction %s", opts.sortDirection)
	}

	return nil
}
