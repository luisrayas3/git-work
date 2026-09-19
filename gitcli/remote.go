package gitcli

import "fmt"

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
func (r *repo) PushRefs(remote string, prefixes ...string) (string, error) {
	// git only updates a remote-tracking ref on push when a fetch refspec
	// maps the ref it pushed, and these namespaces are not part of a remote's
	// default fetch refspec. Add one per prefix for this invocation only:
	// `-c` appends to the multi-valued remote.<name>.fetch rather than
	// replacing it, so the remote's configured refspecs still apply. This is
	// the CLI equivalent of the in-memory remote-config edit
	// repository.GoGitRepo performs for the same reason.
	opts := make([]string, 0, len(prefixes))
	args := []string{"push", remote}
	for _, prefix := range prefixes {
		opts = append(opts, fmt.Sprintf("remote.%s.fetch=refs/%s/*:refs/remotes/%s/%s/*",
			remote, prefix, remote, prefix))
		args = append(args, fmt.Sprintf("refs/%s/*:refs/%s/*", prefix, prefix))
	}

	out, err := r.git.with(opts...).progress(args...)
	if err != nil {
		return "", err
	}
	if out == "" {
		return upToDate, nil
	}
	return out, nil
}
