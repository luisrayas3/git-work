package jiracmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
)

func newJiraSchemaCommand(env *execenv.Env) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the schema the sync derives from the Jira project",
		Long: `Print the live schema with the Jira project's issue types, fields and values
adopted or added, each carrying its Jira id as an alias. Nothing is written;
notes on what does not map go to stderr.

The first mapping is reviewed as this file, then imported; after that every
sync derives and imports it itself.`,
		Example: `git work jira schema > jira.yaml
$EDITOR jira.yaml
git work schema import jira.yaml --dry-run
git work schema import jira.yaml`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runJiraSchema(env, format)
		}),
	}

	execenv.AddFormatFlag(cmd, &format, "yaml", "json")

	return cmd
}

func runJiraSchema(env *execenv.Env, format string) error {
	doc, notes, err := host.JiraSchema(env.Ctx, env.Backend)
	warnNotes(env, notes)
	if err != nil {
		return err
	}
	raw, err := doc.Marshal(format)
	if err != nil {
		return err
	}
	env.Out.Println(strings.TrimRight(string(raw), "\n"))
	return nil
}

func warnNotes(env *execenv.Env, notes []jira.Note) {
	for _, n := range notes {
		env.Err.Println(fmt.Sprintf("%s: %s: %s", n.Level, n.Key, n.Message))
	}
}
