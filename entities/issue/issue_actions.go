package issue

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// Fetching and pushing are the cache's, by namespace: RepoCache.Fetch and
// RepoCache.Push walk the subcaches, so an entity package needs no transport
// of its own.

// MergeAll will merge all the available remote issues
// Note: an author is necessary for the case where a merge commit is created, as this commit will
// have an author and may be signed if a signing key is available.
func MergeAll(repo repository.ClockedRepo, resolvers entity.Resolvers, remote string, mergeAuthor identity.Interface) <-chan entity.MergeResult {
	return dag.MergeAll(def, wrapper, repo, resolvers, remote, mergeAuthor)
}

// Remove will remove a local issue from its entity.Id
func Remove(repo repository.ClockedRepo, id entity.Id) error {
	return dag.Remove(def, repo, id)
}

// RemoveAll will remove all local issues.
// RemoveAll is idempotent.
func RemoveAll(repo repository.ClockedRepo) error {
	return dag.RemoveAll(def, repo)
}
