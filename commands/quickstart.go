package commands

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

// newQuickstartCommand is `git work quickstart`, the page an agent reads first.
//
// It is deliberately not everything: the model, the issue commands, where the
// rest is, and then the types and fields this repository has, which is what
// turns the rest into a document `issue new` accepts. Anything longer is a
// page an agent skims instead of reads.
//
// It takes no argument and no flag: there is nothing to narrow, and a verb
// whose whole job is "tell me where I am" that first needs to be told what to
// say would have missed the point.
func newQuickstartCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Print how to use git-work, and this repository's types",
		Long: `Print a short guide to git-work, as markdown, for an agent.

The guide is the model — how issues are stored, what the commands are, where
everything this leaves out can be found — followed by the types and fields
this repository actually has, read from the store. One call is enough to write
a ` + "`git work issue new`" + ` document the schema check accepts.

It is ` + "`work.quickstart()`" + ` in a flow's script, the same text.`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runQuickstart(env)
		}),
	}

	return cmd
}

func runQuickstart(env *execenv.Env) error {
	text, err := host.Quickstart(env.Backend)
	if err != nil {
		return err
	}

	env.Out.Println(strings.TrimRight(text, "\n"))

	return nil
}
