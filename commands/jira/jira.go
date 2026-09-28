// Package jiracmd is the `git work jira` command tree: the sync of this
// store with one Jira Cloud project (doc/design/jira-sync.md, JS22).
//
// `schema` is a reader that prints the schema the sync would derive; `sync`
// is a writer that prints one JSON object per line, like `log`. Neither
// pushes: publishing the tracker stays `git work push`.
package jiracmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
)

func NewJiraCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jira",
		Short: "Sync with a Jira Cloud project",
		Long: `Keep this store and one Jira Cloud project converged, in both directions,
Jira winning a field both sides edited.

The clone is bound by git config: git-work.jira.url (https://<site>.atlassian.net),
git-work.jira.project (the project key) and git-work.jira.email. The API token
is JIRA_API_TOKEN, else git's credential helpers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	cmd.AddCommand(newJiraSchemaCommand(env))
	cmd.AddCommand(newJiraSyncCommand(env))

	return cmd
}
