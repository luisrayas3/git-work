package config

import (
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

var _ entity.Resolver = &SimpleResolver{}

// SimpleResolver is a Resolver loading config entities of one store directly from a Repo.
type SimpleResolver struct {
	store *Store
	repo  repository.ClockedRepo
}

func NewSimpleResolver(store *Store, repo repository.ClockedRepo) *SimpleResolver {
	return &SimpleResolver{store: store, repo: repo}
}

func (r *SimpleResolver) Resolve(id entity.Id) (entity.Resolved, error) {
	return r.store.Read(r.repo, id)
}
