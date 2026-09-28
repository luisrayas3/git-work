package cache

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/repository"
)

func TestCredential(t *testing.T) {
	// the developer's own git configuration and helpers stay out of it
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")

	raw := repository.CreateGoGitTestRepo(t, false)
	dir := raw.GetLocalRemote()

	git := func(stdin string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("", "config", "credential.helper", "store --file="+filepath.Join(home, "credentials"))
	git("protocol=https\nhost=site.atlassian.net\nusername=me@example.com\npassword=tok3n\n\n",
		"credential", "approve")

	// go-git alone has no credential helpers
	bare, err := NewRepoCacheNoEvents(raw)
	require.NoError(t, err)
	_, err = bare.Credential("https://site.atlassian.net", "me@example.com")
	require.Error(t, err)

	c, err := NewRepoCacheNoEvents(gitcli.WrapRepo(raw, dir))
	require.NoError(t, err)
	secret, err := c.Credential("https://site.atlassian.net", "me@example.com")
	require.NoError(t, err)
	require.Equal(t, "tok3n", secret)
}
