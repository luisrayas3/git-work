package cache

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/repository"
)

// Fetch and Push refuse a remote that is not configured, such as a URL,
// on go-git alone as through the git CLI (45f7009).
func TestFetchPushRemoteNotConfigured(t *testing.T) {
	for name, wrap := range map[string]func(repository.TestedRepo) repository.ClockedRepo{
		"go-git": func(r repository.TestedRepo) repository.ClockedRepo { return r },
		"gitcli": func(r repository.TestedRepo) repository.ClockedRepo {
			return gitcli.WrapRepo(r, r.GetLocalRemote())
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := repository.CreateGoGitTestRepo(t, false)
			remote := repository.CreateGoGitTestRepo(t, true)
			require.NoError(t, raw.AddRemote("origin", remote.GetLocalRemote()))

			c, err := NewRepoCacheNoEvents(wrap(raw))
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })
			me, err := c.Identities().New("René Descartes", "rene@descartes.fr")
			require.NoError(t, err)
			require.NoError(t, c.SetUserIdentity(me))

			for _, bad := range []string{remote.GetLocalRemote(), "nosuchremote"} {
				_, err := c.Push(bad)
				require.ErrorAs(t, err, &gitcli.ErrRemoteNotConfigured{})
				_, err = c.Fetch(bad)
				require.ErrorAs(t, err, &gitcli.ErrRemoteNotConfigured{})
				require.ErrorAs(t, c.Pull(bad), &gitcli.ErrRemoteNotConfigured{})
			}
			refs, err := raw.ListRefs("refs/remotes/")
			require.NoError(t, err)
			require.Empty(t, refs)

			require.NoError(t, c.CheckRemote("origin"))
			_, err = c.Push("origin")
			require.NoError(t, err)
			_, err = c.Fetch("origin")
			require.NoError(t, err)
		})
	}
}
