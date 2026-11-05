package commands

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
)

func newPushCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [REMOTE]",
		Short: "Push updates to a git remote",
		Long: `Push the tracker's work-* namespaces to a remote, and update
refs/remotes/<REMOTE>/work-* to match.

The remote is the argument, else the git-work.remote config, else origin. It
must be a configured remote's name: the remote-tracking refs are named after
it, so a URL is refused before anything is pushed. git remote add NAME URL
gives a URL a name.`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runPush(env, args)
		}),
		ValidArgsFunction: completion.GitRemote(env),
	}

	return cmd
}

func runPush(env *execenv.Env, args []string) error {
	if len(args) > 1 {
		return errors.New("Only pushing to one remote at a time is supported")
	}
	remote, err := remoteArg(env, args)
	if err != nil {
		return err
	}

	// Push refuses a remote that is not configured before it pushes anything
	stdout, err := env.Backend.Push(remote)
	if err != nil {
		return err
	}

	env.Out.Println(stdout)

	return nil
}
