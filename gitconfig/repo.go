package gitconfig

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/git-bug/git-bug/repository"
)

var _ repository.ClockedRepo = &repo{}

// repo decorates a repository.ClockedRepo
// so that every configuration read is evaluated by the git CLI.
// Go embedding does not virtualize method calls,
// so every inherited method that reads config internally
// (GetUserName, GetUserEmail, GetCoreEditor, GetRemotes)
// is overridden here as well;
// otherwise they would silently keep using go-git's view.
type repo struct {
	repository.ClockedRepo
	dir string
}

// WrapRepo returns r with configuration reads routed through `git config`,
// run from dir (any path inside the repository).
// If no git binary is on PATH,
// r is returned unchanged and go-git's include-less reads remain.
func WrapRepo(r repository.ClockedRepo, dir string) repository.ClockedRepo {
	if _, err := exec.LookPath("git"); err != nil {
		return r
	}
	return &repo{ClockedRepo: r, dir: dir}
}

// LocalConfig give access to the repository scoped configuration
func (r *repo) LocalConfig() repository.Config {
	return &config{
		ConfigRead:  &reader{dir: r.dir, scope: scopeLocal},
		ConfigWrite: r.ClockedRepo.LocalConfig(),
	}
}

// GlobalConfig give access to the global scoped configuration
func (r *repo) GlobalConfig() repository.Config {
	return &config{
		ConfigRead:  &reader{dir: r.dir, scope: scopeGlobal},
		ConfigWrite: r.ClockedRepo.GlobalConfig(),
	}
}

// AnyConfig give access to a merged local/global configuration
func (r *repo) AnyConfig() repository.ConfigRead {
	return &reader{dir: r.dir, scope: scopeAny}
}

// GetUserName returns the name the user has used to configure git
func (r *repo) GetUserName() (string, error) {
	return r.AnyConfig().ReadString("user.name")
}

// GetUserEmail returns the email address that the user has used to configure git.
func (r *repo) GetUserEmail() (string, error) {
	return r.AnyConfig().ReadString("user.email")
}

// GetCoreEditor returns the name of the editor that the user has used to configure git.
// The resolution order mirrors repository.GoGitRepo:
// $GIT_EDITOR, core.editor, $VISUAL, $EDITOR,
// then a list of common editors.
func (r *repo) GetCoreEditor() (string, error) {
	if val, ok := os.LookupEnv("GIT_EDITOR"); ok {
		return val, nil
	}

	val, err := r.AnyConfig().ReadString("core.editor")
	if err == nil && val != "" {
		return val, nil
	}
	if err != nil && !errors.Is(err, repository.ErrNoConfigEntry) {
		return "", err
	}

	if val, ok := os.LookupEnv("VISUAL"); ok {
		return val, nil
	}

	if val, ok := os.LookupEnv("EDITOR"); ok {
		return val, nil
	}

	for _, cmd := range []string{"editor", "nano", "vim", "vi", "emacs"} {
		if _, err := exec.LookPath(cmd); err == nil {
			return cmd, nil
		}
	}

	return "ed", nil
}

// GetRemotes returns the configured remotes repositories.
// Like repository.GoGitRepo, the first URL of each remote is returned.
func (r *repo) GetRemotes() (map[string]string, error) {
	rd := &reader{dir: r.dir, scope: scopeAny}
	out, code, err := rd.git("-z", "--get-regexp", `^remote\..*\.url$`)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	if code == 1 {
		return result, nil
	}
	for _, entry := range splitNul(out) {
		key, value := splitEntry(entry)
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if _, seen := result[name]; !seen {
			result[name] = value
		}
	}
	return result, nil
}
