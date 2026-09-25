package issuecmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

func newIssueNewCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new DOC|-",
		Short: "Create a new issue from a JSON document",
		Long: `Create an issue from a JSON document, given as the argument or on standard input.

  {"fields": {"title": "…", "type": "task", "status": "open"},
   "body": "the first comment",
   "aliases": {"jira": "PROJ-12"}}

A title is required and lives in fields, like every other property of an issue.
An alias is an external id, immutable, accepted wherever an id is.
The new issue's id is printed, and nothing else.`,
		Example: `git work issue new '{"fields":{"title":"Task: rework the CLI","type":"task"}}'
echo "$doc" | git work issue new -`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueNew(env, args)
		}),
	}

	return cmd
}

func runIssueNew(env *execenv.Env, args []string) error {
	data, err := execenv.ReadLiteralOrStdin(env, args[0])
	if err != nil {
		return err
	}

	var doc host.IssueDocument
	if err := host.DecodeStrict(data, &doc); err != nil {
		return err
	}

	id, err := host.IssueNew(env.Backend, doc)
	if err != nil {
		return err
	}

	env.Out.Println(id.String())

	return nil
}
