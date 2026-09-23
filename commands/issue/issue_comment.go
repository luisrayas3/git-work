package issuecmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/util/text"
)

// newIssueCommentCommand groups the two comment verbs.
// There is no comment list: `git work issue get ID` prints the comments,
// and get is the only reader of an issue's content (cli-convention.md).
func newIssueCommentCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Write an issue's comments",
	}

	cmd.AddCommand(newIssueCommentNewCommand(env))
	cmd.AddCommand(newIssueCommentEditCommand(env))

	return cmd
}

func newIssueCommentNewCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new ISSUE_ID BODY|-",
		Short: "Add a new comment to an issue",
		Long: `Add a comment to an issue, its body given as the argument or on standard
input. The new comment's id is printed, and nothing else.
ISSUE_ID is an id prefix or an alias.`,
		Args:    cobra.ExactArgs(2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueCommentNew(env, args)
		}),
		ValidArgsFunction: IssueCompletion(env),
	}

	return cmd
}

func runIssueCommentNew(env *execenv.Env, args []string) error {
	i, err := resolveIssue(env, args[0])
	if err != nil {
		return err
	}

	body, err := readBody(env, args[1])
	if err != nil {
		return err
	}
	body = text.Cleanup(body)
	if body == "" {
		return errors.New("a comment body is required")
	}

	commentId, _, err := i.AddComment(body)
	if err != nil {
		return err
	}

	if err := i.Commit(); err != nil {
		return err
	}

	env.Out.Println(commentId.String())
	return nil
}

func newIssueCommentEditCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit COMMENT_ID BODY|-",
		Short: "Edit an existing comment on an issue",
		Long: `Replace the body of one comment, given as the argument or on standard input.
COMMENT_ID is the comment's own id, as get and comment new print it.`,
		Args:    cobra.ExactArgs(2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueCommentEdit(env, args)
		}),
	}

	return cmd
}

func runIssueCommentEdit(env *execenv.Env, args []string) error {
	i, commentId, err := env.Backend.Issues().ResolveComment(args[0])
	if err != nil {
		return err
	}

	body, err := readBody(env, args[1])
	if err != nil {
		return err
	}
	body = text.Cleanup(body)
	if body == "" {
		return errors.New("a comment body is required")
	}

	if _, err := i.EditComment(commentId, body); err != nil {
		return err
	}

	return i.Commit()
}
