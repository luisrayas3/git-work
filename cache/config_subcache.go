package cache

import (
	"math"
	"sort"
	"time"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

// configMaxLoaded is unbounded on purpose (E8):
// a schema is a few dozen small entities,
// and evicting one only to read it back on the next lookup buys nothing.
const configMaxLoaded = math.MaxInt32

// RepoCacheConfig is the subcache of one config namespace.
//
// It is instantiated twice, once per store,
// because two subcaches cannot share a Typename
// and the cache file is named after the namespace.
type RepoCacheConfig struct {
	*SubCache[*config.Config, *ConfigExcerpt, *ConfigCache]

	store *config.Store
}

func NewRepoCacheConfig(repo repository.ClockedRepo,
	resolvers func() entity.Resolvers,
	getUserIdentity getUserIdentityFunc,
	store *config.Store) *RepoCacheConfig {

	makeCached := func(c *config.Config, entityUpdated func(id entity.Id) error) *ConfigCache {
		reload := func() (*config.Config, error) {
			return store.ReadWithResolver(repo, resolvers(), c.Id())
		}
		return NewConfigCache(c, repo, getUserIdentity, entityUpdated, reload)
	}

	actions := Actions[*config.Config]{
		ReadWithResolver:    store.ReadWithResolver,
		ReadAllWithResolver: store.ReadAllWithResolver,
		Remove:              store.Remove,
		RemoveAll:           store.RemoveAll,
		MergeAll:            store.MergeAll,
	}

	sc := NewSubCache[*config.Config, *ConfigExcerpt, *ConfigCache](
		repo, resolvers, getUserIdentity,
		makeCached, NewConfigExcerpt, actions,
		store.Typename(), store.Namespace(),
		formatVersion, configMaxLoaded,
	)

	return &RepoCacheConfig{SubCache: sc, store: store}
}

// Store returns the entity store this subcache reads and writes.
func (c *RepoCacheConfig) Store() *config.Store {
	return c.store
}

// ConfigQuery selects config excerpts.
//
// There is no filter language here and there does not need to be:
// what a caller wants from a config namespace
// is the entities of one shape, or the entities under one key.
// Everything richer is jq over the JSON a command prints (483dbe2).
type ConfigQuery struct {
	// Shape, when set, keeps only entities of that shape.
	Shape config.Shape
	// Key, when set, keeps only entities with that exact key.
	Key string
	// IncludeArchived keeps the archived entities, which are hidden by default.
	IncludeArchived bool
}

// Query returns the matching excerpts, ordered by (shape, key, creation, id).
func (c *RepoCacheConfig) Query(q ConfigQuery) []*ConfigExcerpt {
	c.mu.RLock()

	var matching []*ConfigExcerpt
	for _, excerpt := range c.excerpts {
		if q.Shape != "" && excerpt.Shape != q.Shape {
			continue
		}
		if q.Key != "" && excerpt.Key != q.Key {
			continue
		}
		if excerpt.Archived && !q.IncludeArchived {
			continue
		}
		matching = append(matching, excerpt)
	}

	c.mu.RUnlock()

	sort.Sort(ConfigsByKey(matching))
	return matching
}

// candidates returns the unarchived entities of a shape and key,
// oldest first, which is the order E7 resolves a duplicate by.
func (c *RepoCacheConfig) candidates(shape config.Shape, key string) []*ConfigExcerpt {
	matching := c.Query(ConfigQuery{Shape: shape, Key: key})
	sort.Sort(ConfigsByCreation(matching))
	return matching
}

// CurrentExcerpt returns the excerpt of the entity that a shape and key resolve to.
//
// Two clones creating the same key before either pushes
// produce two entities with the same shape and key (E7).
// The one with the lowest creation lamport time wins,
// ties by lowest id, from the excerpts alone —
// deterministic on every clone, and repairable,
// because archiving the loser is an operation that replicates.
func (c *RepoCacheConfig) CurrentExcerpt(shape config.Shape, key string) (*ConfigExcerpt, error) {
	candidates := c.candidates(shape, key)
	if len(candidates) == 0 {
		return nil, entity.NewErrNotFound(c.Typename())
	}
	return candidates[0], nil
}

// Current returns the entity that a shape and key resolve to (E7).
func (c *RepoCacheConfig) Current(shape config.Shape, key string) (*ConfigCache, error) {
	excerpt, err := c.CurrentExcerpt(shape, key)
	if err != nil {
		return nil, err
	}
	return c.Resolve(excerpt.Id())
}

// Duplicates returns the losers of a shape and key: everything Current ignored.
//
// It is empty in the ordinary case.
// The schema commands print what it returns on stderr,
// because a key silently resolving to one of two entities
// is how a team loses an edit without ever being told.
func (c *RepoCacheConfig) Duplicates(shape config.Shape, key string) []*ConfigExcerpt {
	candidates := c.candidates(shape, key)
	if len(candidates) <= 1 {
		return nil
	}
	return candidates[1:]
}

// Keys returns the distinct keys of a shape, in order.
func (c *RepoCacheConfig) Keys(shape config.Shape) []string {
	seen := make(map[string]struct{})
	var keys []string
	for _, excerpt := range c.Query(ConfigQuery{Shape: shape}) {
		if _, ok := seen[excerpt.Key]; ok {
			continue
		}
		seen[excerpt.Key] = struct{}{}
		keys = append(keys, excerpt.Key)
	}
	return keys
}

// ResolveKey resolves a shape and key to an entity, refusing an ambiguity.
//
// Unlike Current it does not pick a winner:
// a caller that means to edit one entity
// should not silently edit one of two.
func (c *RepoCacheConfig) ResolveKey(shape config.Shape, key string) (*ConfigCache, error) {
	candidates := c.candidates(shape, key)
	switch len(candidates) {
	case 0:
		return nil, entity.NewErrNotFound(c.Typename())
	case 1:
		return c.Resolve(candidates[0].Id())
	default:
		ids := make([]entity.Id, len(candidates))
		for i, excerpt := range candidates {
			ids[i] = excerpt.Id()
		}
		return nil, entity.NewErrMultipleMatch(c.Typename(), ids)
	}
}

// ResolveConfigCreateMetadata retrieves an entity that has the exact given
// metadata on its Create operation. It fails if multiple entities match.
func (c *RepoCacheConfig) ResolveConfigCreateMetadata(key string, value string) (*ConfigCache, error) {
	return c.ResolveMatcher(func(excerpt *ConfigExcerpt) bool {
		return excerpt.CreateMetadata[key] == value
	})
}

// New creates a config entity in this namespace and writes it to the repository.
func (c *RepoCacheConfig) New(shape config.Shape, key string, attributes map[string]config.Value) (*ConfigCache, *config.CreateOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, nil, err
	}
	return c.NewRaw(author, time.Now().Unix(), shape, key, attributes, nil)
}

// NewRaw creates a config entity with an explicit author, time and create metadata.
func (c *RepoCacheConfig) NewRaw(author identity.Interface, unixTime int64, shape config.Shape, key string, attributes map[string]config.Value, metadata map[string]string) (*ConfigCache, *config.CreateOperation, error) {
	e, op, err := c.store.Create(author, unixTime, shape, key, attributes, metadata)
	if err != nil {
		return nil, nil, err
	}

	// A new entity has no ref yet, so there is nothing to rebase onto — but
	// Commit increments the lamport clocks, which is a read-modify-write on a
	// file two processes must not interleave.
	unlock, err := lockWrite(c.repo)
	if err != nil {
		return nil, nil, err
	}
	err = e.Commit(c.repo)
	unlock()
	if err != nil {
		return nil, nil, err
	}

	cached, err := c.add(e)
	if err != nil {
		return nil, nil, err
	}

	return cached, op, nil
}
