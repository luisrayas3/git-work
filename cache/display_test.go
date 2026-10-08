package cache

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/repository"
)

func newDisplayTestCache(t *testing.T, namespace string) *RepoCache {
	t.Helper()
	c := newSchemaTestCache(t)
	if namespace != "" {
		require.NoError(t, c.LocalConfig().StoreString(DisplayIdKey, namespace))
	}
	return c
}

// TestIssueHumanId: an issue is drawn by its alias exactly when the alias,
// typed back, resolves to it, and by its short hash otherwise
// (doc/design/alias-ids.md A3).
func TestIssueHumanId(t *testing.T) {
	c := newDisplayTestCache(t, "jira")
	require.Equal(t, "jira", c.DisplayNamespace())

	keyed := newAliased(t, c, "PROJ-1")
	local, err := newTask(t, c, nil)
	require.NoError(t, err)
	require.Equal(t, "PROJ-1", c.IssueHumanId(keyed.Id()))
	require.Equal(t, local.Id().Human(), c.IssueHumanId(local.Id()))
	require.False(t, c.IsFallbackId(keyed.Id().String(), "PROJ-1"))
	require.True(t, c.IsFallbackId(local.Id().String(), local.Id().Human()))

	// an alias in another namespace is not drawn
	other, _, err := c.Issues().NewWithMetadata("linear", "", map[string]issue.Value{
		"type": issue.StringValue("task"),
	}, map[string]string{AliasMetadataPrefix + "linear": "ENG-1"})
	require.NoError(t, err)
	require.Equal(t, other.Id().Human(), c.IssueHumanId(other.Id()))

	// two unconsolidated copies of one key: both hashes, the key is ambiguous
	winner := newAliased(t, c, "PROJ-2")
	loser := newAliased(t, c, "PROJ-2")
	require.Equal(t, winner.Id().Human(), c.IssueHumanId(winner.Id()))
	require.Equal(t, loser.Id().Human(), c.IssueHumanId(loser.Id()))

	// consolidated: the survivor is the key, the loser its hash (A6); the
	// index follows the write without anything being told
	consolidate(t, c, loser, winner)
	require.Equal(t, "PROJ-2", c.IssueHumanId(winner.Id()))
	require.Equal(t, loser.Id().Human(), c.IssueHumanId(loser.Id()))

	// every drawn alias resolves back to its issue
	for _, i := range []*IssueCache{keyed, winner} {
		back, err := c.Issues().ResolvePrefixOrAlias(c.IssueHumanId(i.Id()))
		require.NoError(t, err)
		require.Equal(t, i.Id(), back.Id())
	}
}

// TestIssueHumanIdHexAlias: an all-hex alias that is the prefix of exactly one
// other issue would resolve to that issue, so it is not drawn.
func TestIssueHumanIdHexAlias(t *testing.T) {
	c := newDisplayTestCache(t, "jira")
	target, err := newTask(t, c, nil)
	require.NoError(t, err)
	prefix := target.Id().String()[:5]

	shadowed := newAliased(t, c, prefix)
	require.Equal(t, shadowed.Id().Human(), c.IssueHumanId(shadowed.Id()))
	require.Equal(t, target.Id().Human(), c.IssueHumanId(target.Id()))
}

// TestIssueHumanIdHash: with no setting, or `hash`, every id is its hash.
func TestIssueHumanIdHash(t *testing.T) {
	for _, value := range []string{"", DisplayHash} {
		c := newDisplayTestCache(t, value)
		require.Equal(t, "", c.DisplayNamespace())
		keyed := newAliased(t, c, "PROJ-1")
		require.Equal(t, keyed.Id().Human(), c.IssueHumanId(keyed.Id()))
		require.False(t, c.IsFallbackId(keyed.Id().String(), keyed.Id().Human()))
	}
}

// TestDisplayNamespaceThroughGit: the key is read through the git CLI, so
// `git -c git-work.display.id=hash work …`, which git hands its subcommand in
// GIT_CONFIG_PARAMETERS, overrides the repository's value for one command
// (alias-ids.md A1).
func TestDisplayNamespaceThroughGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	open := func(t *testing.T, raw repository.TestedRepo) *RepoCache {
		c, err := NewRepoCacheNoEvents(gitcli.WrapRepo(raw, raw.GetLocalRemote()))
		require.NoError(t, err)
		return c
	}

	raw := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, raw.LocalConfig().StoreString(DisplayIdKey, "jira"))
	require.Equal(t, "jira", open(t, raw).DisplayNamespace())

	t.Setenv("GIT_CONFIG_PARAMETERS", "'"+DisplayIdKey+"'='hash'")
	require.Equal(t, "", open(t, raw).DisplayNamespace())

	t.Setenv("GIT_CONFIG_PARAMETERS", "'"+DisplayIdKey+"'='linear'")
	require.Equal(t, "linear", open(t, raw).DisplayNamespace())
}
