package commands

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/gitcli"
)

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	require.NoError(t, err)
	return string(out)
}

// A REMOTE that is not a configured remote's name, a URL above all, is
// refused before anything is fetched, pushed or printed (45f7009): the
// remote-tracking refs are named after it.
func TestRemoteNotConfigured(t *testing.T) {
	env, dir := newInstallEnv(t)

	url, err := env.Repo.AnyConfig().ReadString("remote.origin.url")
	require.NoError(t, err)

	run := map[string]func(remote string) error{
		"pull": func(remote string) error { return runPull(env, []string{remote}) },
		"push": func(remote string) error { return runPush(env, []string{remote}) },
		"sync": func(remote string) error {
			return runSync(env, syncOptions{format: "json"}, []string{remote})
		},
		"migrate --from": func(remote string) error {
			return runMigrate(env, migrateOptions{from: remote, dryRun: true})
		},
		"install": func(remote string) error { return runInstall(env, []string{remote}) },
	}

	refs := gitOutput(t, dir, "for-each-ref")
	config := gitOutput(t, dir, "config", "--list", "--local")

	for name, run := range run {
		for _, remote := range []string{url, "nosuchremote"} {
			t.Run(name+" "+remote, func(t *testing.T) {
				env.Out.(*execenv.TestOut).Reset()
				env.Err.(*execenv.TestOut).Reset()

				err := run(remote)
				require.ErrorAs(t, err, &gitcli.ErrRemoteNotConfigured{})
				require.ErrorContains(t, err, "git remote add NAME URL")

				require.Empty(t, env.Out.String())
				require.Empty(t, env.Err.String())
				require.Equal(t, refs, gitOutput(t, dir, "for-each-ref"))
				require.Equal(t, config, gitOutput(t, dir, "config", "--list", "--local"))
			})
		}
	}

	// the configured remote still works, both ways
	require.NoError(t, runPull(env, []string{"origin"}))
	require.NotEmpty(t, gitOutput(t, dir, "for-each-ref", "refs/remotes/origin/work-users/"))
	require.NoError(t, runPush(env, []string{"origin"}))
	require.NoError(t, runSync(env, syncOptions{format: "json"}, []string{"origin"}))
}
