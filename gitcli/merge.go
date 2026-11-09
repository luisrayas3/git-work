package gitcli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MergeLabels name the three sides of a merge in its conflict markers:
// ours, the base, and theirs, in `git merge-file`'s order.
type MergeLabels [3]string

// MergeFile is `git merge-file -p`: the changes from base to theirs
// applied to ours, an overlap of the two written with conflict markers
// labelled by labels. It answers the merged text and how many conflicts it
// holds; a conflict is a result, not an error.
//
// go-git has no text merge, and git's is the one a person reading the
// markers already knows.
func MergeFile(ours, base, theirs string, labels MergeLabels) (string, int, error) {
	dir, err := os.MkdirTemp("", "git-work-merge-*")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)

	paths := make([]string, 3)
	for i, text := range []string{ours, base, theirs} {
		paths[i] = filepath.Join(dir, fmt.Sprint(i))
		if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
			return "", 0, err
		}
	}

	args := []string{"merge-file", "-p"}
	for _, label := range labels {
		args = append(args, "-L", label)
	}
	args = append(args, paths...)

	cmd := exec.Command("git", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return out.String(), 0, nil
	}
	// the exit status is the number of conflicts, up to 127; above that
	// (a negative status, as git writes an error) it failed
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code > 0 && code <= 127 {
			return out.String(), code, nil
		}
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			return "", 0, fmt.Errorf("git merge-file: %s", msg)
		}
	}
	return "", 0, fmt.Errorf("git merge-file: %w", err)
}
