// Package gitcli provides a repository.ClockedRepo decorator
// whose operations run the git CLI instead of go-git.
//
// go-git is an excellent object database
// but an incomplete reimplementation of git's environment,
// and the two places it falls short are the two it owns here:
//
//   - Configuration. go-git (as of v5.19) does not evaluate `[include]` or
//     `[includeIf]`, so any value behind one — commonly user.name and
//     user.email in a per-machine or per-directory include — is invisible
//     (upstream git-bug #1475). `git config` is the only implementation that
//     evaluates gitdir:/onbranch:/hasconfig: conditions exactly as git does.
//
//   - Remote transport. go-git's SSH transport ignores ~/.ssh/config and the
//     default identity files; its DefaultAuthBuilder only offers keys held by
//     ssh-agent, so an empty agent fails to authenticate against a remote the
//     git CLI reaches without trouble. url.insteadOf, credential helpers,
//     proxies and the rest of git's transport configuration are in the same
//     position.
//
// Everything else — objects, trees, refs, clocks, browsing — is local object
// access, where go-git is both fast and correct, and is left to it. Config
// writes also stay on go-git: nothing is lost by writing through it.
package gitcli

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runner invokes the git CLI inside one repository.
type runner struct {
	// dir is a path inside the repository (working tree or git dir);
	// git resolves the actual repository from it,
	// so worktrees and bare repositories behave the way git expects.
	dir string
	// opts are `key=value` overrides passed as `-c`,
	// applying to a single invocation without touching config on disk.
	opts []string
}

// with returns a copy of r carrying additional `-c key=value` overrides.
func (r runner) with(opts ...string) runner {
	combined := make([]string, 0, len(r.opts)+len(opts))
	combined = append(combined, r.opts...)
	combined = append(combined, opts...)
	return runner{dir: r.dir, opts: combined}
}

// run executes `git -C <dir> [-c opt…] <args…>`,
// returning stdout, stderr and the exit code.
// The returned error is non-nil only when the process could not be run at all:
// a non-zero exit is reported through code,
// because git uses exit status as data
// (`git config` exits 1 to mean "no such key").
// args[0] must be the git subcommand; it names the command in error messages.
func (r runner) run(args ...string) (stdout, stderr []byte, code int, err error) {
	full := make([]string, 0, 2+2*len(r.opts)+len(args))
	full = append(full, "-C", r.dir)
	for _, opt := range r.opts {
		full = append(full, "-c", opt)
	}
	full = append(full, args...)

	cmd := exec.Command("git", full...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	stdout, stderr = outBuf.Bytes(), errBuf.Bytes()

	if err == nil {
		return stdout, stderr, 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, exitErr.ExitCode(), nil
	}
	return nil, nil, -1, fmt.Errorf("git %s: %w", args[0], err)
}

// output runs git and returns stdout with the exit code.
// A non-zero exit that came with a diagnostic on stderr becomes an error;
// a silent non-zero exit is left for the caller to interpret.
func (r runner) output(args ...string) ([]byte, int, error) {
	stdout, stderr, code, err := r.run(args...)
	if err != nil {
		return nil, code, err
	}
	if code != 0 {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return stdout, code, fmt.Errorf("git %s: %s", args[0], msg)
		}
	}
	return stdout, code, nil
}

// progress runs a command whose result is the human-readable report
// git writes to stderr as it works, and returns that report.
func (r runner) progress(args ...string) (string, error) {
	_, stderr, code, err := r.run(args...)
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(string(stderr))
	if code != 0 {
		if msg == "" {
			return "", fmt.Errorf("git %s: exit status %d", args[0], code)
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return msg, nil
}
