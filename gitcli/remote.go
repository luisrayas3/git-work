package gitcli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// upToDate is what FetchRefs and PushRefs report when git transferred nothing.
// repository.GoGitRepo returns this string for gogit.NoErrAlreadyUpToDate
// and commands print it verbatim, so it is kept identical.
const upToDate = "already up-to-date"

// FetchRefs fetch git refs matching a directory prefix to a remote
// Ex: prefix="foo" will fetch any remote refs matching "refs/foo/*" locally.
// The equivalent git refspec would be "refs/foo/*:refs/remotes/<remote>/foo/*"
func (r *repo) FetchRefs(remote string, prefixes ...string) (string, error) {
	args := []string{"fetch", remote}
	for _, prefix := range prefixes {
		// Not forced, matching repository.GoGitRepo: these namespaces only
		// ever grow, so a non-fast-forward update means the remote history
		// was rewritten and the caller should hear about it.
		args = append(args, fmt.Sprintf("refs/%s/*:refs/remotes/%s/%s/*", prefix, remote, prefix))
	}

	out, err := r.git.progress(args...)
	if err != nil {
		return "", err
	}
	if out == "" {
		return upToDate, nil
	}
	return out, nil
}

// PushRefs push git refs matching a directory prefix to a remote
// Ex: prefix="foo" will push any local refs matching "refs/foo/*" to the remote.
// The equivalent git refspec would be "refs/foo/*:refs/foo/*"
//
// Additionally, PushRefs will update the local references in refs/remotes/<remote>/foo to match
// the remote state.
//
// The push skips the pre-push hook. go-git runs no hooks, and the namespaces
// pushed here hold entity metadata, not code, so a hook that lints or tests
// the working tree has nothing to say about them.
//
// A remote may cap the refs one push can update (GitHub's "limit how many
// branches and tags can be updated in a single push" rule, which it applies
// to these namespaces too). The rejection names the cap and the refs it
// refused, which are the ones that differ; they are pushed again, that many
// per push.
func (r *repo) PushRefs(remote string, prefixes ...string) (string, error) {
	// git only updates a remote-tracking ref on push when a fetch refspec
	// maps the ref it pushed, and these namespaces are not part of a remote's
	// default fetch refspec. Add one per prefix for this invocation only:
	// `-c` appends to the multi-valued remote.<name>.fetch rather than
	// replacing it, so the remote's configured refspecs still apply. This is
	// the CLI equivalent of the in-memory remote-config edit
	// repository.GoGitRepo performs for the same reason.
	opts := make([]string, 0, len(prefixes))
	refspecs := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		opts = append(opts, fmt.Sprintf("remote.%s.fetch=refs/%s/*:refs/remotes/%s/%s/*",
			remote, prefix, remote, prefix))
		refspecs = append(refspecs, fmt.Sprintf("refs/%s/*:refs/%s/*", prefix, prefix))
	}
	git := r.git.with(opts...)
	push := func(refspecs []string) (string, error) {
		return git.progress(append([]string{"push", "--no-verify", remote}, refspecs...)...)
	}

	out, err := push(refspecs)
	if err != nil {
		limit, refused := refUpdateCap(err)
		if limit == 0 {
			return "", err
		}
		var reports []string
		for len(refused) > 0 {
			n := min(limit, len(refused))
			out, err := push(refused[:n])
			if err != nil {
				return "", err
			}
			reports = append(reports, out)
			refused = refused[n:]
		}
		return strings.Join(reports, "\n"), nil
	}
	if out == "" {
		return upToDate, nil
	}
	return out, nil
}

var (
	capPattern     = regexp.MustCompile(`can not update more than (\d+) branches or tags`)
	refusedPattern = regexp.MustCompile(`(?m)^ ! \[remote rejected\] +(\S+) -> (\S+)`)
)

// refUpdateCap reads a push rejection for the per-push ref cap it names
// and the refspecs the remote refused, or returns 0 when that is not what
// the rejection is about.
func refUpdateCap(err error) (limit int, refused []string) {
	m := capPattern.FindStringSubmatch(err.Error())
	if m == nil {
		return 0, nil
	}
	limit, _ = strconv.Atoi(m[1])
	for _, m := range refusedPattern.FindAllStringSubmatch(err.Error(), -1) {
		refused = append(refused, m[1]+":"+m[2])
	}
	if limit < 1 || len(refused) == 0 {
		return 0, nil
	}
	return limit, refused
}
