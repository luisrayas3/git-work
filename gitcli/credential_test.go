package gitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/repository"
)

func wrappedTestRepo(t *testing.T) (repository.TestedRepo, *repo) {
	t.Helper()
	// no graphical prompt either: an empty askpass is no askpass to git
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "")
	raw := repository.CreateGoGitTestRepo(t, false)
	wrapped := WrapRepo(raw, raw.GetLocalRemote())
	require.IsType(t, &repo{}, wrapped, "git must be on PATH for these tests")
	return raw, wrapped.(*repo)
}

func TestCredentialFillAsksTheHelper(t *testing.T) {
	home := isolateGit(t)
	raw, r := wrappedTestRepo(t)

	// a helper that records what it was asked and answers one host only
	seen := filepath.Join(home, "seen")
	helper := filepath.Join(home, "helper.sh")
	writeFile(t, helper, `#!/bin/sh
test "$1" = get || exit 0
input=$(cat)
{ printf '%s\n' "$input"; echo "prompt=$GIT_TERMINAL_PROMPT"; } > `+seen+`
case "$input" in *host=jira.example.com*) echo password=s3cret ;; esac
`)
	require.NoError(t, os.Chmod(helper, 0o700))
	gitConfig(t, raw.GetLocalRemote(), "credential.helper", helper)

	secret, err := r.CredentialFill("https://jira.example.com", "me@example.com")
	require.NoError(t, err)
	require.Equal(t, "s3cret", secret)

	asked, err := os.ReadFile(seen)
	require.NoError(t, err)
	require.Contains(t, string(asked), "protocol=https\n")
	require.Contains(t, string(asked), "host=jira.example.com\n")
	require.Contains(t, string(asked), "username=me@example.com\n")
	require.Contains(t, string(asked), "prompt=0")

	// a helper with nothing to say, and no terminal to ask: an error, not a hang
	_, err = r.CredentialFill("https://other.example.com/ex/jira/1", "me@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git credential approve")

	_, err = r.CredentialFill("jira.example.com", "me@example.com")
	require.Error(t, err)
	_, err = r.CredentialFill("https://jira.example.com", "me\n@example.com")
	require.Error(t, err)
}

// TestCredentialFillReadsWhatApproveStored is the recipe the error names:
// `git credential approve` stores a token, and the next fill finds it.
func TestCredentialFillReadsWhatApproveStored(t *testing.T) {
	home := isolateGit(t)
	raw, r := wrappedTestRepo(t)
	dir := raw.GetLocalRemote()

	store := filepath.Join(home, "credentials")
	gitConfig(t, dir, "credential.helper", "store --file="+store)

	_, err := r.CredentialFill("https://site.atlassian.net", "me@example.com")
	require.Error(t, err, "nothing stored yet")

	approve := exec.Command("git", "-C", dir, "credential", "approve")
	approve.Stdin = strings.NewReader("protocol=https\nhost=site.atlassian.net\nusername=me@example.com\npassword=tok3n\n\n")
	out, err := approve.CombinedOutput()
	require.NoError(t, err, string(out))

	secret, err := r.CredentialFill("https://site.atlassian.net", "me@example.com")
	require.NoError(t, err)
	require.Equal(t, "tok3n", secret)

	// another user on the same site has none
	_, err = r.CredentialFill("https://site.atlassian.net", "you@example.com")
	require.Error(t, err)
}
