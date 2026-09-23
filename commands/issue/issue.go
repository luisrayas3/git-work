// Package issuecmd is the `git work issue` command tree, over entities/issue.
//
// It is plumbing (e8d6426): explicit ids, no editor, no implicit selection,
// JSON in and JSON out, and no sugar flag anywhere.
// The command map it implements is doc/design/cli-convention.md.
// The `bug` tree keeps serving the old entity until the store is migrated (bf6f392).
package issuecmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/util/colors"
)

// defaultProgram is the list you get when you name no program:
// everything that is not archived, most recently edited first.
//
// It is written as a jq program rather than special-cased in Go
// so that `.` means the whole array and nothing is hidden from it.
// "mine" would be the better default, but it needs the schema's assignee
// field to know which one it is, so it waits for the schema (e8d6426).
const defaultProgram = `map(select(.fields.archived != true))
	| sort_by(.edit_time.lamport, .edit_time.timestamp)
	| reverse`

type issueListOptions struct {
	format string
}

func NewIssueCommand(env *execenv.Env) *cobra.Command {
	options := issueListOptions{}

	cmd := &cobra.Command{
		Use:   "issue [PROGRAM]",
		Short: "List issues",
		Long: `Run a jq program over the issues and print what it emits.

The program's input is the array of issue excerpts, the same JSON this command
prints: one object per issue, with an id, times, an author and a fields map.
With no program, the list is every unarchived issue, last edited first.

Each emitted value is printed as JSON, one per line when there are several.
--format text prints one line per issue when the program returned issues, and
falls back to JSON when it returned anything else.`,
		Example: `Every issue, in the input's own order:
git work issue .

The titles of the issues of one epic:
git work issue 'map(select(.fields.parent == "6a1b2c3")) | map(.fields.title)'

A kanban of what is not done:
git work issue 'map(select(.fields.status != "done"))' | git work view board '{"columns":"status"}'
`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueList(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	cmd.AddCommand(newIssueAddCommand(env))
	cmd.AddCommand(newIssueArchiveCommand(env))
	cmd.AddCommand(newIssueCommentCommand(env))
	cmd.AddCommand(newIssueGetCommand(env))
	cmd.AddCommand(newIssueLogCommand(env))
	cmd.AddCommand(newIssueNewCommand(env))
	cmd.AddCommand(newIssueRemoveCommand(env))
	cmd.AddCommand(newIssueRmCommand(env))
	cmd.AddCommand(newIssueSetCommand(env))

	return cmd
}

// addFormatFlag adds the one output flag every reader has.
func addFormatFlag(cmd *cobra.Command, format *string) {
	cmd.Flags().StringVarP(format, "format", "f", "json",
		"Select the output formatting style. Valid values are [json,text]")
	cmd.RegisterFlagCompletionFunc("format", completion.From([]string{"json", "text"}))
}

func runIssueList(env *execenv.Env, opts issueListOptions, args []string) error {
	program := defaultProgram
	if len(args) == 1 {
		program = args[0]
	}

	input, err := issueListInput(env)
	if err != nil {
		return err
	}

	values, err := jq.Run(program, input)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		return printValues(env, values)
	case "text":
		if printed, err := printIssueLines(env, values); printed || err != nil {
			return err
		}
		// Not a list of issues: JSON is the honest answer.
		return printValues(env, values)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

// issueListInput is the array a program runs over: every issue as an excerpt,
// oldest first, so that a program that does not sort still reads the same twice.
func issueListInput(env *execenv.Env) (any, error) {
	ids := env.Backend.Issues().AllIds()

	excerpts := make([]*cache.IssueExcerpt, len(ids))
	for i, id := range ids {
		excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		excerpts[i] = excerpt
	}
	sort.Sort(cache.IssuesByCreationTime(excerpts))

	out := make([]cmdjson.IssueExcerpt, len(excerpts))
	for i, excerpt := range excerpts {
		j, err := cmdjson.NewIssueExcerpt(env.Backend, excerpt)
		if err != nil {
			return nil, err
		}
		out[i] = j
	}

	return jq.Input(out)
}

// printValues prints what a program emitted.
// One value keeps the indented shape of every other JSON this tool prints;
// several are one compact value per line, which is what a stream is.
func printValues(env *execenv.Env, values []any) error {
	if len(values) == 1 {
		return env.Out.PrintJSON(values[0])
	}
	for _, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		env.Out.Println(string(raw))
	}
	return nil
}

// printIssueLines prints one line per issue and reports whether it could:
// the values have to be issue-shaped, objects with an id and a fields map,
// either as one array or as a stream of them.
func printIssueLines(env *execenv.Env, values []any) (bool, error) {
	items := values
	if len(values) == 1 {
		if array, ok := values[0].([]any); ok {
			items = array
		}
	}
	if len(items) == 0 {
		return false, nil
	}

	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return false, nil
		}
		if _, ok := object["id"].(string); !ok {
			return false, nil
		}
		if _, ok := object["fields"].(map[string]any); !ok {
			return false, nil
		}
		objects = append(objects, object)
	}

	for _, object := range objects {
		fields, _ := object["fields"].(map[string]any)
		env.Out.Printf("%s\t%s\t%s\n",
			colors.Cyan(humanIdOf(object)),
			colors.Yellow(stringOr(fields["status"], "-")),
			stringOr(fields["title"], ""),
		)
	}
	return true, nil
}

func humanIdOf(object map[string]any) string {
	if human, ok := object["human_id"].(string); ok {
		return human
	}
	id, _ := object["id"].(string)
	if len(id) > entity.HumanIdLength {
		return id[:entity.HumanIdLength]
	}
	return id
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}
