package tests

import (
	"os/exec"
	"testing"

	"github.com/git-bug/git-bug/repository"
)

// TestOpenWorktreeConfig opens a repository that enables extensions.worktreeConfig
// and holds a config.worktree,
// which go-git v5 refused as an unsupported extension.
func TestOpenWorktreeConfig(t *testing.T) {
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git("init")
	git("config", "extensions.worktreeConfig", "true")
	git("config", "--worktree", "some.key", "val")

	repo, err := repository.OpenGoGitRepo(dir, "git-bug", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
}
