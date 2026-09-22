package issuecmd

import (
	"errors"

	termtext "github.com/MichaelMure/go-term-text"
	"github.com/spf13/cobra"

	buginput "github.com/git-bug/git-bug/commands/bug/input"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/util/text"
)

func newIssueCommentCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "comment ISSUE_ID",
		Short:   "List an issue's comments",
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueComment(env, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	cmd.AddCommand(newIssueCommentNewCommand(env))
	cmd.AddCommand(newIssueCommentEditCommand(env))

	return cmd
}

func runIssueComment(env *execenv.Env, args []string) error {
	i, _, err := resolveIssue(env.Backend, args)
	if err != nil {
		return err
	}

	snap := i.Snapshot()

	for i, comment := range snap.Comments {
		if i != 0 {
			env.Out.Println()
		}

		env.Out.Printf("Author: %s\n", comment.Author.DisplayName())
		env.Out.Printf("Id: %s\n", comment.CombinedId().Human())
		env.Out.Printf("Date: %s\n\n", comment.FormatTime())
		env.Out.Println(termtext.LeftPadLines(comment.Message, 4))
	}

	return nil
}

type issueCommentMessageOptions struct {
	messageFile string
	message     string
}

func addMessageFlags(cmd *cobra.Command, options *issueCommentMessageOptions) {
	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.messageFile, "file", "F", "",
		"Take the message from the given file. Use - to read the message from the standard input")
	flags.StringVarP(&options.message, "message", "m", "",
		"Provide the message from the command line")
}

func (o *issueCommentMessageOptions) resolve() (string, error) {
	if o.messageFile != "" && o.message == "" {
		message, err := buginput.BugCommentFileInput(o.messageFile)
		if err != nil {
			return "", err
		}
		o.message = message
	}
	if o.message == "" {
		return "", errors.New("a message is required (-m or -F)")
	}
	return text.Cleanup(o.message), nil
}

func newIssueCommentNewCommand(env *execenv.Env) *cobra.Command {
	options := issueCommentMessageOptions{}

	cmd := &cobra.Command{
		Use:     "new ISSUE_ID",
		Short:   "Add a new comment to an issue",
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueCommentNew(env, options, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	addMessageFlags(cmd, &options)

	return cmd
}

func runIssueCommentNew(env *execenv.Env, opts issueCommentMessageOptions, args []string) error {
	i, _, err := resolveIssue(env.Backend, args)
	if err != nil {
		return err
	}

	message, err := opts.resolve()
	if err != nil {
		return err
	}

	commentId, _, err := i.AddComment(message)
	if err != nil {
		return err
	}

	if err := i.Commit(); err != nil {
		return err
	}

	env.Out.Printf("%s created\n", commentId.Human())
	return nil
}

func newIssueCommentEditCommand(env *execenv.Env) *cobra.Command {
	options := issueCommentMessageOptions{}

	cmd := &cobra.Command{
		Use:     "edit COMMENT_ID",
		Short:   "Edit an existing comment on an issue",
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueCommentEdit(env, options, args)
		}),
	}

	addMessageFlags(cmd, &options)

	return cmd
}

func runIssueCommentEdit(env *execenv.Env, opts issueCommentMessageOptions, args []string) error {
	i, commentId, err := env.Backend.Issues().ResolveComment(args[0])
	if err != nil {
		return err
	}

	message, err := opts.resolve()
	if err != nil {
		return err
	}

	_, err = i.EditComment(commentId, message)
	if err != nil {
		return err
	}

	return i.Commit()
}
