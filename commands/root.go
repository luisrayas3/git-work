package commands

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/bridge"
	"github.com/git-bug/git-bug/commands/bug"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/commands/flow"
	"github.com/git-bug/git-bug/commands/issue"
	schemacmd "github.com/git-bug/git-bug/commands/schema"
	"github.com/git-bug/git-bug/commands/user"
)

func NewRootCommand(ctx context.Context, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   execenv.RootCommandName,
		Short: "A project tracker embedded in Git",
		Long: `git-work is a project tracker embedded in git, invoked as ` + "`git work`" + `.

git-work stores issues as git objects, separate from the files history. As
issues are regular git objects, they can be pushed and pulled from/to the same
git remote you are already using to collaborate with other people.

`,

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			root := cmd.Root()
			root.Version = version
		},

		// For the root command, force the execution of the PreRun
		// even if we just display the help. This is to make sure that we check
		// the repository and give the user early feedback.
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				os.Exit(1)
			}
		},

		SilenceUsage:      true,
		DisableAutoGenTag: true,
	}

	const entityGroup = "entity"
	const uiGroup = "ui"
	const remoteGroup = "remote"

	cmd.AddGroup(&cobra.Group{ID: entityGroup, Title: "Entities"})
	cmd.AddGroup(&cobra.Group{ID: uiGroup, Title: "Interactive interfaces"})
	cmd.AddGroup(&cobra.Group{ID: remoteGroup, Title: "Interaction with the outside world"})

	addCmdWithGroup := func(child *cobra.Command, groupID string) {
		cmd.AddCommand(child)
		child.GroupID = groupID
	}

	env := execenv.NewEnv(ctx)

	addCmdWithGroup(issuecmd.NewIssueCommand(env), entityGroup)
	addCmdWithGroup(schemacmd.NewSchemaCommand(env), entityGroup)
	addCmdWithGroup(bugcmd.NewBugCommand(env), entityGroup)
	addCmdWithGroup(flowcmd.NewFlowCommand(env), entityGroup)
	addCmdWithGroup(usercmd.NewUserCommand(env), entityGroup)
	addCmdWithGroup(newLabelCommand(env), entityGroup)

	addCmdWithGroup(newTermUICommand(env), uiGroup)
	addCmdWithGroup(newWebUICommand(env), uiGroup)

	addCmdWithGroup(newPullCommand(env), remoteGroup)
	addCmdWithGroup(newPushCommand(env), remoteGroup)
	addCmdWithGroup(bridgecmd.NewBridgeCommand(env), remoteGroup)

	cmd.AddCommand(newVersionCommand(env))
	cmd.AddCommand(newWipeCommand(env))

	return cmd
}
