package commands

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/repository"
)

func newInstallCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [REMOTE]",
		Short: "Add the tracker's fetch refspecs to a remote's configuration",
		Long: `Add one fetch refspec per tracker namespace to remote.<REMOTE>.fetch:
+refs/<ns>/*:refs/remotes/<REMOTE>/<ns>/* for each of the work-* namespaces.

The tracker's remote-tracking refs live under refs/remotes/<REMOTE>/work-*/.
Under the default refspec alone git reads them as branches, so git remote
prune and git fetch --prune delete them all, and the next pull fetches the
whole tracker again. With these refspecs a prune drops only what the remote
deleted.

Run it once per clone. The remote is the argument, else the git-work.remote
config, else origin, and it must be a configured remote. Only the missing lines
are added, with git config --add; the others are left as they are, so running
it again changes nothing. It prints nothing.`,
		Example: `git work install
git work install upstream`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runInstall(env, args)
		}),
		ValidArgsFunction: completion.GitRemote(env),
	}

	return cmd
}

func runInstall(env *execenv.Env, args []string) error {
	remote, err := remoteArg(env, args)
	if err != nil {
		return err
	}
	return env.Backend.InstallRefspecs(remote)
}

// remoteArg is the remote install, pull and push act on: the argument, else
// the git-work.remote config, else origin. Whether it is configured is
// checked by the cache (cache.RepoCache.CheckRemote).
func remoteArg(env *execenv.Env, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return repository.GetDefaultString("git-work.remote", env.Repo.AnyConfig(), "origin")
}

// hintRefspecs tells, on stderr, that remote's fetch configuration lacks the
// tracker's refspecs, and how to add them. It never edits the config. Pull
// refuses a remote that is not a configured name before it gets here; should
// one reach it, it says nothing, since the pull is about to fail with
// gitcli.ErrRemoteNotConfigured. A hint is advisory, so a failure to read the
// config is no reason to stop a pull: it says nothing then too.
func hintRefspecs(env *execenv.Env, remote string) {
	missing, err := env.Backend.MissingRefspecs(remote)
	if err != nil || len(missing) == 0 {
		return
	}
	env.Err.Printf("remote.%s.fetch lacks the tracker's refspecs; git work install adds them\n", remote)
}
