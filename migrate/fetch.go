package migrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

// oldIdentityNamespace is oldIdentityPrefix as a fetch prefix.
const oldIdentityNamespace = "identities"

// Fetched is what Fetch brought into the old store.
type Fetched struct {
	// Identities are the refs/identities/* created or fast-forwarded.
	Identities int
	// Copied are the identities then copied to refs/work-users/*.
	Copied int
	// Issues are the refs/issues/* created or updated.
	Issues int
}

// Fetch fetches git-bug's store from a remote
// and merges it into the local refs the migration reads (6274f33):
// refs/identities/* and refs/issues/*.
// git work pull no longer carries either namespace,
// so this is the one way to bring them in.
//
// The identities are fast-forwarded,
// since identity.MergeAll is bound to the new namespace,
// and then copied as CopyIdentities does,
// because the old issues resolve their authors there;
// the issues are merged by entities/bug.
// Neither is ever given a merge commit:
// the old store is frozen,
// so a history that diverged from the remote's is an error.
//
// It writes refs, so the caller holds the write lock.
func Fetch(repo repository.ClockedRepo, remote string) (Fetched, error) {
	var fetched Fetched

	if _, err := repo.FetchRefs(remote, oldIdentityNamespace, bug.Namespace); err != nil {
		return fetched, fmt.Errorf("fetching git-bug's store from %s: %w", remote, err)
	}

	remoteIdentities := fmt.Sprintf("refs/remotes/%s/%s/", remote, oldIdentityNamespace)
	refs, err := repo.ListRefs(remoteIdentities)
	if err != nil {
		return fetched, err
	}
	for _, remoteRef := range refs {
		changed, err := fastForward(repo, remoteRef, oldIdentityPrefix+strings.TrimPrefix(remoteRef, remoteIdentities))
		if err != nil {
			return fetched, err
		}
		if changed {
			fetched.Identities++
		}
	}

	fetched.Copied, err = CopyIdentities(repo)
	if err != nil {
		return fetched, err
	}

	// the authors resolve as bug.Read resolves them, from refs/work-users/*;
	// no author: a merge commit is refused, as the old store is frozen
	resolvers := entity.Resolvers{&identity.Identity{}: identity.NewSimpleResolver(repo)}
	// the channel is drained whatever happens, so its sender never blocks
	var first error
	for result := range bug.MergeAll(repo, resolvers, remote, nil) {
		switch {
		case first != nil:
		case result.Err != nil:
			first = result.Err
		case result.Status == entity.MergeStatusInvalid:
			first = fmt.Errorf("issue %s from %s: %s", result.Id.Human(), remote, result.Reason)
		case result.Status == entity.MergeStatusNew, result.Status == entity.MergeStatusUpdated:
			fetched.Issues++
		}
	}
	return fetched, first
}

// fastForward moves local to remote when local is missing or behind it,
// and reports whether it moved.
func fastForward(repo repository.RepoData, remote, local string) (bool, error) {
	remoteHash, err := repo.ResolveRef(remote)
	if err != nil {
		return false, err
	}
	exists, err := repo.RefExist(local)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, repo.UpdateRef(local, remoteHash)
	}
	localHash, err := repo.ResolveRef(local)
	if err != nil {
		return false, err
	}
	if localHash == remoteHash {
		return false, nil
	}
	localCommits, err := repo.ListCommits(local)
	if err != nil {
		return false, err
	}
	if slices.Contains(localCommits, remoteHash) {
		return false, nil
	}
	remoteCommits, err := repo.ListCommits(remote)
	if err != nil {
		return false, err
	}
	if !slices.Contains(remoteCommits, localHash) {
		return false, fmt.Errorf("%s has diverged from %s", local, remote)
	}
	return true, repo.UpdateRef(local, remoteHash)
}
