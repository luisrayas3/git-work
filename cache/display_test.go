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
// (doc/design/cli.md, "An alias is drawn only where it names that issue").
func TestIssueHumanId(t *testing.T) {
	c := newDisplayTestCache(t, "jira")
	require.Equal(t, "jira", c.DisplayNamespace())

	keyed := newAliased(t, c, "PROJ-1")
	local, err := newTask(t, c, nil)
	require.NoError(t, err)
	require.Equal(t, "PROJ-1", c.IssueHumanId(keyed.Id()))
	require.Equal(t, local.Id().Human(), c.IssueHumanId(local.Id()))

	// an alias in another namespace is not drawn
	other, _, err := c.Issues().NewWithMetadata("linear", "", map[string]issue.Value{
		"type": issue.StringValue("task"),
	}, map[string]string{AliasMetadataPrefix + "linear": "ENG-1"})
	require.NoError(t, err)
	require.Equal(t, other.Id().Human(), c.IssueHumanId(other.Id()))

	// two copies of one key the rule cannot order: both hashes, the key is
	// ambiguous
	a := newAliased(t, c, "PROJ-2")
	b := newAliased(t, c, "PROJ-2")
	require.Equal(t, a.Id().Human(), c.IssueHumanId(a.Id()))
	require.Equal(t, b.Id().Human(), c.IssueHumanId(b.Id()))

	// two copies it can order: the one that reached the system first is the
	// key, the other its hash, before consolidation and after it
	// (doc/design/cli.md, "A duplicate is drawn by its hash"); the index
	// follows each write without anything being told
	c.Issues().SetReach("jira", testReach)
	later := newCopy(t, c, "PROJ-3", "10003", 200)
	require.Equal(t, "PROJ-3", c.IssueHumanId(later.Id()))
	first := newCopy(t, c, "PROJ-3", "10003", 100)
	require.Equal(t, "PROJ-3", c.IssueHumanId(first.Id()))
	require.Equal(t, later.Id().Human(), c.IssueHumanId(later.Id()))
	archive(t, c, later)
	require.Equal(t, "PROJ-3", c.IssueHumanId(first.Id()))
	require.Equal(t, later.Id().Human(), c.IssueHumanId(later.Id()))

	// every drawn alias resolves back to its issue
	for _, i := range []*IssueCache{keyed, first} {
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
	}
}

// TestDisplayNamespaceThroughGit: the key is read through the git CLI, so
// `git -c git-work.display.id=hash work …`, which git hands its subcommand in
// GIT_CONFIG_PARAMETERS, overrides the repository's value for one command
// (doc/design/cli.md, "`git-work.display.id` draws an alias in place of the
// hash").
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

// TestIssueHumanIdBuildsOnce: drawing every issue builds the alias index once.
// Building it loads the copies of a shared alias, and loading an issue
// changes no excerpt, so the generation the index was built at stays current
// (doc/design/cli.md, "An alias is drawn only where it names that issue").
func TestIssueHumanIdBuildsOnce(t *testing.T) {
	c := newDisplayTestCache(t, "jira")
	c.Issues().SetReach("jira", testReach)

	var all []*IssueCache
	for i := int64(0); i < 4; i++ {
		all = append(all, newCopy(t, c, "PROJ-1", "10001", 100+i))
		all = append(all, newCopy(t, c, "PROJ-2", "10002", 200+i))
	}
	for i := 0; i < 3; i++ {
		plain, err := newTask(t, c, nil)
		require.NoError(t, err)
		all = append(all, plain)
	}
	// a cache smaller than the copies of one alias, so building the index
	// reads them from disk and evicts as it goes
	c.Issues().SetCacheSize(1)

	require.Equal(t, "PROJ-1", c.IssueHumanId(all[0].Id()))
	gen := c.Issues().Generation()
	for _, i := range all {
		c.IssueHumanId(i.Id())
	}
	require.Equal(t, gen, c.Issues().Generation())
	require.Equal(t, gen, c.display.gen)
	require.Equal(t, "PROJ-2", c.IssueHumanId(all[1].Id()))
	require.Equal(t, all[2].Id().Human(), c.IssueHumanId(all[2].Id()))
	require.Equal(t, all[8].Id().Human(), c.IssueHumanId(all[8].Id()))
}

// TestResolveLeavesGeneration: loading an entity into memory, and evicting
// one, changes no excerpt, so the generation does not move.
func TestResolveLeavesGeneration(t *testing.T) {
	c := newDisplayTestCache(t, "")
	a, err := newTask(t, c, nil)
	require.NoError(t, err)
	b, err := newTask(t, c, nil)
	require.NoError(t, err)
	c.Issues().SetCacheSize(0)
	_, loaded := c.Issues().cached[a.Id()]
	require.False(t, loaded)

	gen := c.Issues().Generation()
	_, err = c.Issues().Resolve(a.Id())
	require.NoError(t, err)
	_, err = c.Issues().Resolve(b.Id())
	require.NoError(t, err)
	require.Equal(t, gen, c.Issues().Generation())
}
