package cache

import (
	"sort"
	"time"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// ConfigCache is a wrapper around a config entity.
//
// Attribute values are accepted as the entity accepts them, by shape.
// What an attribute means for a shape — the field kinds, the categories,
// a flow's script being one Starlark function — is the schema layer's,
// on the write path above this one, never in the entity (E6).
type ConfigCache struct {
	CachedEntityBase[*config.Snapshot, config.Operation]
}

func NewConfigCache(c *config.Config, repo repository.ClockedRepo, getUserIdentity getUserIdentityFunc, entityUpdated func(id entity.Id) error, reload func() (*config.Config, error)) *ConfigCache {
	return &ConfigCache{
		CachedEntityBase: CachedEntityBase[*config.Snapshot, config.Operation]{
			repo:            repo,
			entityUpdated:   entityUpdated,
			getUserIdentity: getUserIdentity,
			entity:          &withSnapshot[*config.Snapshot, config.Operation]{Interface: c},
			reload: func() (dag.Interface[*config.Snapshot, config.Operation], error) {
				fresh, err := reload()
				if err != nil {
					return nil, err
				}
				return &withSnapshot[*config.Snapshot, config.Operation]{Interface: fresh}, nil
			},
		},
	}
}

// Shape returns what this entity configures.
func (c *ConfigCache) Shape() config.Shape {
	return c.Snapshot().Shape
}

// Key returns the entity's key, which is immutable.
func (c *ConfigCache) Key() string {
	return c.Snapshot().Key
}

// Set sets one attribute.
func (c *ConfigCache) Set(name string, value config.Value) (*config.SetOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	return c.SetRaw(author, time.Now().Unix(), name, value, nil)
}

func (c *ConfigCache) SetRaw(author identity.Interface, unixTime int64, name string, value config.Value, metadata map[string]string) (*config.SetOperation, error) {
	c.mu.Lock()
	op, err := config.Set(c.entity, author, unixTime, name, value, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

// Remove deletes one attribute.
func (c *ConfigCache) Remove(name string) (*config.RemoveOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	return c.RemoveRaw(author, time.Now().Unix(), name, nil)
}

func (c *ConfigCache) RemoveRaw(author identity.Interface, unixTime int64, name string, metadata map[string]string) (*config.RemoveOperation, error) {
	c.mu.Lock()
	op, err := config.Remove(c.entity, author, unixTime, name, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

// SetArchived archives or unarchives the entity, which is the replicated removal (E7).
func (c *ConfigCache) SetArchived(archived bool) (*config.SetArchivedOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	return c.SetArchivedRaw(author, time.Now().Unix(), archived, nil)
}

func (c *ConfigCache) SetArchivedRaw(author identity.Interface, unixTime int64, archived bool, metadata map[string]string) (*config.SetArchivedOperation, error) {
	c.mu.Lock()
	op, err := config.SetArchived(c.entity, author, unixTime, archived, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

// Update applies many attribute changes as one pack and one commit.
//
// This is the shape `schema import` and the Jira sync write through:
// a renumbered list of enum values, or a whole field reconciled,
// is many operations that must land together or not at all,
// and inside one entity the dag gives exactly that (E5).
// Commit takes the store's write lock and re-reads the entity first,
// so what is appended here is rebased onto the current tip
// instead of erasing whoever moved it (2a51f66).
func (c *ConfigCache) Update(set map[string]config.Value, remove []string) error {
	author, err := c.getUserIdentity()
	if err != nil {
		return err
	}
	return c.UpdateRaw(author, time.Now().Unix(), set, remove, nil)
}

func (c *ConfigCache) UpdateRaw(author identity.Interface, unixTime int64, set map[string]config.Value, remove []string, metadata map[string]string) error {
	if len(set) == 0 && len(remove) == 0 {
		return nil
	}

	// sorted, so that the same changes write the same pack
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)

	removals := append([]string(nil), remove...)
	sort.Strings(removals)

	// Build and validate every operation before appending any of them:
	// a rejected name half way through would otherwise leave the rest staged
	// on the cached entity, to be written by whoever commits next.
	ops := make([]config.Operation, 0, len(names)+len(removals))
	for _, name := range names {
		op := config.NewSetOp(author, unixTime, name, set[name])
		for k, v := range metadata {
			op.SetMetadata(k, v)
		}
		if err := op.Validate(); err != nil {
			return err
		}
		ops = append(ops, op)
	}
	for _, name := range removals {
		op := config.NewRemoveOp(author, unixTime, name)
		for k, v := range metadata {
			op.SetMetadata(k, v)
		}
		if err := op.Validate(); err != nil {
			return err
		}
		ops = append(ops, op)
	}

	c.mu.Lock()
	for _, op := range ops {
		c.entity.Append(op)
	}
	c.mu.Unlock()

	return c.Commit()
}

func (c *ConfigCache) SetMetadata(target entity.Id, newMetadata map[string]string) (*dag.SetMetadataOperation[*config.Snapshot], error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	return c.SetMetadataRaw(author, time.Now().Unix(), target, newMetadata)
}

func (c *ConfigCache) SetMetadataRaw(author identity.Interface, unixTime int64, target entity.Id, newMetadata map[string]string) (*dag.SetMetadataOperation[*config.Snapshot], error) {
	c.mu.Lock()
	op, err := config.SetMetadata(c.entity, author, unixTime, target, newMetadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}
