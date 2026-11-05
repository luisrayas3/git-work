package commands

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
)

func newPullCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull [REMOTE]",
		Short: "Pull updates from a git remote",
		Long: `Fetch the tracker's work-* namespaces from a remote into
refs/remotes/<REMOTE>/work-*, then merge them into the local tracker.

The remote is the argument, else the git-work.remote config, else origin. It
must be a configured remote's name: the remote-tracking refs are named after
it, so a URL is refused before anything is fetched. git remote add NAME URL
gives a URL a name.`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runPull(env, args)
		}),
		ValidArgsFunction: completion.GitRemote(env),
	}

	return cmd
}

func runPull(env *execenv.Env, args []string) error {
	if len(args) > 1 {
		return errors.New("Only pulling from one remote at a time is supported")
	}
	remote, err := remoteArg(env, args)
	if err != nil {
		return err
	}
	// refused here, before the hint and the progress lines, though Fetch
	// would refuse it too
	if err := env.Backend.CheckRemote(remote); err != nil {
		return err
	}

	hintRefspecs(env, remote)

	env.Out.Println("Fetching remote ...")

	stdout, err := env.Backend.Fetch(remote)
	if err != nil {
		return err
	}

	env.Out.Println(stdout)

	env.Out.Println("Merging data ...")

	for result := range env.Backend.MergeAll(remote) {
		if result.Err != nil {
			env.Err.Println(result.Err)
		}

		if result.Status != entity.MergeStatusNothing {
			env.Out.Printf("%s: %s\n", result.Id.Human(), result)
		}
	}

	return nil
}
