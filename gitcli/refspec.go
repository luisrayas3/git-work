package gitcli

import (
	"fmt"
	"slices"
	"strings"
)

// FetchRefspecs is the tracker's lines in a remote's fetch configuration.
//
// The tracker's remote-tracking refs live under refs/remotes/<remote>/<ns>/.
// The default refspec, +refs/heads/*:refs/remotes/<remote>/*, reads them as
// branches, so `git remote prune` and `git fetch --prune` map each back to a
// refs/heads/<ns>/... that does not exist and delete it. A refspec per
// namespace maps them back to their real source instead, and prune then
// drops only what the remote actually deleted (e66709b).
type FetchRefspecs interface {
	// MissingFetchRefspecs returns the tracker refspecs, one per namespace,
	// that remote.<remote>.fetch lacks, in the namespaces' order.
	// A remote that is not configured is ErrRemoteNotConfigured.
	MissingFetchRefspecs(remote string, namespaces ...string) ([]string, error)
	// AddFetchRefspecs adds the missing ones to the repository's own config
	// with `git config --add`, leaving every other line as it is.
	AddFetchRefspecs(remote string, namespaces ...string) error
}

var _ FetchRefspecs = &repo{}

// ErrRemoteNotConfigured is a remote name with no remote.<name> section,
// such as a URL given where a remote's name could go.
type ErrRemoteNotConfigured struct{ Remote string }

func (e ErrRemoteNotConfigured) Error() string {
	return fmt.Sprintf("remote %q is not configured", e.Remote)
}

// TrackerRefspec is the fetch refspec that maps one tracker namespace
// to its remote-tracking refs.
func TrackerRefspec(remote, namespace string) string {
	return fmt.Sprintf("+refs/%s/*:refs/remotes/%s/%s/*", namespace, remote, namespace)
}

func (r *repo) MissingFetchRefspecs(remote string, namespaces ...string) ([]string, error) {
	out, code, err := r.git.output("remote")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("git remote: exit status %d", code)
	}
	if !slices.Contains(strings.Fields(string(out)), remote) {
		return nil, ErrRemoteNotConfigured{Remote: remote}
	}

	// remote.<name>.fetch is multi-valued and git uses the lines of every
	// scope together, so all of them count.
	out, code, err = r.git.output("config", "-z", "--get-all", "remote."+remote+".fetch")
	if err != nil {
		return nil, err
	}
	var have []string
	switch code {
	case 0:
		have = splitNul(out)
	case 1:
		// no fetch line at all
	default:
		return nil, fmt.Errorf("git config: exit status %d", code)
	}

	var missing []string
	for _, ns := range namespaces {
		spec := TrackerRefspec(remote, ns)
		if !slices.Contains(have, spec) {
			missing = append(missing, spec)
		}
	}
	return missing, nil
}

func (r *repo) AddFetchRefspecs(remote string, namespaces ...string) error {
	missing, err := r.MissingFetchRefspecs(remote, namespaces...)
	if err != nil {
		return err
	}
	w := &writer{git: r.git}
	for _, spec := range missing {
		if err := w.config("--add", "--", "remote."+remote+".fetch", spec); err != nil {
			return err
		}
	}
	return nil
}
