package config

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// Fetching and pushing are the cache's, by namespace: RepoCache.Fetch and
// RepoCache.Push walk the subcaches, so an entity package needs no transport
// of its own.

// MergeAll merges all the available remote entities of this namespace.
func (s *Store) MergeAll(repo repository.ClockedRepo, resolvers entity.Resolvers, remote string, mergeAuthor identity.Interface) <-chan entity.MergeResult {
	return dag.MergeAll(s.def, s.wrapper, repo, resolvers, remote, mergeAuthor)
}

// Remove deletes a local entity of this namespace from its id.
//
// This is the local removal: the entity comes back on the next pull.
// The replicated removal is SetArchived (E7).
func (s *Store) Remove(repo repository.ClockedRepo, id entity.Id) error {
	return dag.Remove(s.def, repo, id)
}

// RemoveAll removes all local entities of this namespace. It is idempotent.
func (s *Store) RemoveAll(repo repository.ClockedRepo) error {
	return dag.RemoveAll(s.def, repo)
}
