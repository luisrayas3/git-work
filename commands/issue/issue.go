// Package issuecmd is the `git work issue` command tree, over entities/issue.
//
// It is plumbing (e8d6426): explicit ids, no editor, no implicit selection,
// JSON in and JSON out, and no sugar flag anywhere.
// The command map it implements is doc/design/cli-convention.md.
// The `bug` tree keeps serving the old entity until the store is migrated (bf6f392).
//
// Every verb is a call into package `host`, which a flow's script reaches too:
// this tree parses argv and formats the result, and nothing else.
package issuecmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/util/colors"
)

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

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

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

func runIssueList(env *execenv.Env, opts issueListOptions, args []string) error {
	var program string
	if len(args) == 1 {
		program = args[0]
	}

	values, err := host.IssueList(env.Backend, program)
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
// the values have to be issue-shaped, which is host.IssueItems' question and
// the same one the terminal renderer asks of the same JSON.
func printIssueLines(env *execenv.Env, values []any) (bool, error) {
	objects, ok := host.IssueItems(values)
	if !ok {
		return false, nil
	}

	for _, object := range objects {
		fields, _ := object["fields"].(map[string]any)
		env.Out.Printf("%s\t%s\t%s\n",
			colors.Cyan(humanIdOf(object)),
			colors.Yellow(host.StringOr(fields["status"], "-")),
			host.StringOr(fields["title"], ""),
		)
	}
	return true, nil
}

func humanIdOf(object map[string]any) string {
	if human, ok := object["human_id"].(string); ok {
		return human
	}
	id := host.StringOr(object["id"], "")
	if len(id) > entity.HumanIdLength {
		return id[:entity.HumanIdLength]
	}
	return id
}
