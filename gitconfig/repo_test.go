package gitconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/repository"
)

// setup creates a test repo with user.* removed from its local config
// and isolates the process from the developer's own global/system git config,
// so that only what the test writes is visible.
func setup(t *testing.T) (raw repository.TestedRepo, wrapped repository.ClockedRepo, dir, home string) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	raw = repository.CreateGoGitTestRepo(t, false)
	dir = raw.GetLocalRemote()
	// go-git mirrors user.* into a typed struct
	// and re-marshals it on every write,
	// so its RemoveAll("user.name") is a no-op;
	// unset via git instead.
	gitConfig(t, dir, "--local", "--unset", "user.name")
	gitConfig(t, dir, "--local", "--unset", "user.email")

	wrapped = WrapRepo(raw, dir)
	require.IsType(t, &repo{}, wrapped, "git must be on PATH for these tests")

	return raw, wrapped, dir, home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func gitConfig(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "config"}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestInclude(t *testing.T) {
	raw, wrapped, _, home := setup(t)

	inc := filepath.Join(home, "identity.gitconfig")
	writeFile(t, inc, "[user]\n\tname = Included Name\n")
	// Write the include through go-git to prove writes and reads interoperate.
	require.NoError(t, raw.LocalConfig().StoreString("include.path", inc))

	// go-git alone cannot see through the include: this is upstream #1475.
	_, err := raw.GetUserName()
	require.ErrorIs(t, err, repository.ErrNoConfigEntry)

	name, err := wrapped.GetUserName()
	require.NoError(t, err)
	require.Equal(t, "Included Name", name)

	name, err = wrapped.LocalConfig().ReadString("user.name")
	require.NoError(t, err)
	require.Equal(t, "Included Name", name)

	name, err = wrapped.AnyConfig().ReadString("user.name")
	require.NoError(t, err)
	require.Equal(t, "Included Name", name)

	// The global scope does not see a local include.
	_, err = wrapped.GlobalConfig().ReadString("user.name")
	require.ErrorIs(t, err, repository.ErrNoConfigEntry)
}

func TestIncludeIfGitdir(t *testing.T) {
	_, wrapped, dir, home := setup(t)

	// git compares gitdir: patterns against the real path of the git dir.
	root, err := filepath.EvalSymlinks(filepath.Dir(dir))
	require.NoError(t, err)

	match := filepath.Join(home, "match.gitconfig")
	writeFile(t, match, "[user]\n\tname = Conditional Name\n\temail = conditional@example.com\n")
	miss := filepath.Join(home, "miss.gitconfig")
	writeFile(t, miss, "[user]\n\tname = Wrong Name\n")

	// Global config with two conditional includes:
	// one whose gitdir: condition matches this repo,
	// one that must not apply.
	writeFile(t, filepath.Join(home, ".gitconfig"),
		"[includeIf \"gitdir:"+root+"/\"]\n\tpath = "+match+"\n"+
			"[includeIf \"gitdir:"+filepath.Join(home, "elsewhere")+"/\"]\n\tpath = "+miss+"\n")

	name, err := wrapped.GetUserName()
	require.NoError(t, err)
	require.Equal(t, "Conditional Name", name)

	email, err := wrapped.GetUserEmail()
	require.NoError(t, err)
	require.Equal(t, "conditional@example.com", email)

	name, err = wrapped.GlobalConfig().ReadString("user.name")
	require.NoError(t, err)
	require.Equal(t, "Conditional Name", name)
}

func TestScopePrecedence(t *testing.T) {
	_, wrapped, dir, _ := setup(t)

	gitConfig(t, dir, "--global", "user.name", "Global Name")
	gitConfig(t, dir, "--local", "user.name", "Local Name")

	// Local shadows global: no ErrMultipleConfigEntry across scopes.
	name, err := wrapped.AnyConfig().ReadString("user.name")
	require.NoError(t, err)
	require.Equal(t, "Local Name", name)

	name, err = wrapped.GlobalConfig().ReadString("user.name")
	require.NoError(t, err)
	require.Equal(t, "Global Name", name)

	// Duplicates within one scope are still an error.
	gitConfig(t, dir, "--local", "--add", "user.name", "Another Local")
	_, err = wrapped.AnyConfig().ReadString("user.name")
	require.ErrorIs(t, err, repository.ErrMultipleConfigEntry)
	_, err = wrapped.LocalConfig().ReadString("user.name")
	require.ErrorIs(t, err, repository.ErrMultipleConfigEntry)

	_, err = wrapped.AnyConfig().ReadString("nodot")
	require.Error(t, err)
}

func TestReadAllBoolTimestamp(t *testing.T) {
	_, wrapped, dir, _ := setup(t)

	local := wrapped.LocalConfig()
	require.NoError(t, local.StoreString("section.key", "value"))
	require.NoError(t, local.StoreBool("section.flag", true))
	require.NoError(t, local.StoreString("section.sub.key", "sub value"))
	// A git-native boolean spelling go-git's strconv-based reader rejects.
	gitConfig(t, dir, "--local", "section.yes", "yes")
	gitConfig(t, dir, "--local", "section.empty", "")

	all, err := local.ReadAll("section")
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"section.key":     "value",
		"section.flag":    "true",
		"section.sub.key": "sub value",
		"section.yes":     "yes",
		"section.empty":   "",
	}, all)

	all, err = local.ReadAll("section.sub")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"section.sub.key": "sub value"}, all)

	all, err = local.ReadAll("nosuchsection")
	require.NoError(t, err)
	require.Empty(t, all)

	flag, err := local.ReadBool("section.flag")
	require.NoError(t, err)
	require.True(t, flag)

	yes, err := local.ReadBool("section.yes")
	require.NoError(t, err)
	require.True(t, yes)

	_, err = local.ReadBool("section.key")
	require.Error(t, err)

	empty, err := local.ReadString("section.empty")
	require.NoError(t, err)
	require.Equal(t, "", empty)

	gitConfig(t, dir, "--local", "section.time", "1234")
	ts, err := wrapped.AnyConfig().ReadTimestamp("section.time")
	require.NoError(t, err)
	require.Equal(t, int64(1234), ts.Unix())

	_, err = local.ReadString("section.missing")
	require.ErrorIs(t, err, repository.ErrNoConfigEntry)
}

func TestGetRemotes(t *testing.T) {
	raw, wrapped, dir, home := setup(t)

	remotes, err := wrapped.GetRemotes()
	require.NoError(t, err)
	require.Empty(t, remotes)

	require.NoError(t, raw.AddRemote("origin", "https://example.com/a.git"))
	gitConfig(t, dir, "--local", "--add", "remote.origin.url", "https://example.com/second.git")

	// A remote that only exists behind an include is visible too.
	inc := filepath.Join(home, "remotes.gitconfig")
	writeFile(t, inc, "[remote \"mirror\"]\n\turl = https://example.com/mirror.git\n")
	gitConfig(t, dir, "--local", "include.path", inc)

	remotes, err = wrapped.GetRemotes()
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"origin": "https://example.com/a.git",
		"mirror": "https://example.com/mirror.git",
	}, remotes)
}

func TestGetCoreEditor(t *testing.T) {
	_, wrapped, dir, _ := setup(t)

	t.Setenv("GIT_EDITOR", "from-env")
	ed, err := wrapped.GetCoreEditor()
	require.NoError(t, err)
	require.Equal(t, "from-env", ed)

	require.NoError(t, os.Unsetenv("GIT_EDITOR"))
	gitConfig(t, dir, "--local", "core.editor", "from-config")
	ed, err = wrapped.GetCoreEditor()
	require.NoError(t, err)
	require.Equal(t, "from-config", ed)
}
