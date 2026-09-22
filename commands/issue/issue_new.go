package issuecmd

import (
	"errors"

	"github.com/spf13/cobra"

	buginput "github.com/git-bug/git-bug/commands/bug/input"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/util/text"
)

type issueNewOptions struct {
	title       string
	message     string
	messageFile string
	fields      []string
}

func newIssueNewCommand(env *execenv.Env) *cobra.Command {
	options := issueNewOptions{}

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new issue",
		Long: `Create a new issue with a title, a body and initial fields.

Values are JSON when they parse as JSON and strings otherwise:
  --set status=open --set estimate=3 --set 'labels=["area:core"]'`,
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueNew(env, options)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.title, "title", "t", "",
		"Provide a title to describe the issue")
	flags.StringVarP(&options.message, "message", "m", "",
		"Provide a message to describe the issue")
	flags.StringVarP(&options.messageFile, "file", "F", "",
		"Take the message from the given file. Use - to read the message from the standard input")
	flags.StringArrayVarP(&options.fields, "set", "s", nil,
		"Set an initial field, as key=value; repeatable")

	return cmd
}

func runIssueNew(env *execenv.Env, opts issueNewOptions) error {
	var err error
	if opts.messageFile != "" && opts.message == "" {
		var title string
		title, opts.message, err = buginput.BugCreateFileInput(opts.messageFile)
		if err != nil {
			return err
		}
		if opts.title == "" {
			opts.title = title
		}
	}

	if opts.title == "" {
		return errors.New("a title is required (-t)")
	}

	fields, err := parseAssignments(opts.fields)
	if err != nil {
		return err
	}

	i, _, err := env.Backend.Issues().New(
		text.CleanupOneLine(opts.title),
		text.Cleanup(opts.message),
		fields,
	)
	if err != nil {
		return err
	}

	env.Out.Printf("%s created\n", i.Id().Human())

	return nil
}
