package config

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// Fetch retrieves updates of this namespace from a remote.
// This does not change the local state.
func (s *Store) Fetch(repo repository.Repo, remote string) (string, error) {
	return dag.Fetch(s.def, repo, remote)
}

// Push updates a remote with the local changes of this namespace.
func (s *Store) Push(repo repository.Repo, remote string) (string, error) {
	return dag.Push(s.def, repo, remote)
}

// Pull does a Fetch + MergeAll and returns an error if a merge fails.
// An author is necessary for the case where a merge commit is created.
func (s *Store) Pull(repo repository.ClockedRepo, resolvers entity.Resolvers, remote string, mergeAuthor identity.Interface) error {
	return dag.Pull(s.def, s.wrapper, repo, resolvers, remote, mergeAuthor)
}

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
