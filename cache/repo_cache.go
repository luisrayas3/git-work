package cache

import (
	"fmt"
	"strings"
	"sync"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/multierr"
)

// 1: original format
// 2: added cache for identities with a reference in the bug cache
// 3: no more legacy identity
// 4: entities make their IDs from data, not git commit
const formatVersion = 4

// The maximum number of bugs loaded in memory. After that, eviction will be done.
const defaultMaxLoadedBugs = 1000

var _ repository.RepoCommon = &RepoCache{}
var _ repository.RepoConfig = &RepoCache{}
var _ repository.RepoKeyring = &RepoCache{}

// cacheMgmt is the expected interface for a sub-cache.
type cacheMgmt interface {
	Typename() string
	Load() error
	Refresh() error
	Build() <-chan BuildEvent
	SetCacheSize(size int)
	RemoveAll() error
	MergeAll(remote string) <-chan entity.MergeResult
	GetNamespace() string
	RegisterObserver(repoName string, observer Observer)
	UnregisterObserver(observer Observer)
	Close() error
}

// RepoCache is a cache for a Repository. This cache has multiple functions:
//
//  1. After being loaded, a Bug is kept in memory in the cache, allowing for fast
//     access later.
//  2. The cache maintains in memory and on disk a pre-digested excerpt for each bug,
//     allowing for fast querying the whole set of bugs without having to load
//     them individually.
//  3. The cache guarantees that a single instance of a Bug is loaded at once, avoiding
//     loss of data that we could have with multiple copies in the same process.
//  4. The same way, the cache maintains in memory a single copy of the loaded identities.
//
// The cache also protects the on-disk data by locking the git repository for its
// own usage, by writing a lock file. Of course, normal git operations are not
// affected, only git-bug related one.
type RepoCache struct {
	// the underlying repo
	repo repository.ClockedRepo

	// the name of the repository, as defined in the MultiRepoCache
	name string

	// resolvers for all known entities and excerpts
	resolvers entity.Resolvers

	bugs       *RepoCacheBug
	issues     *RepoCacheIssue
	identities *RepoCacheIdentity

	subcaches []cacheMgmt

	// the user identity's id, if known
	muUserIdentity sync.RWMutex
	userIdentityId entity.Id
}

// NewRepoCache create or open a cache on top of a raw repository.
// The caller is expected to read all returned events before the cache is considered
// ready to use.
func NewRepoCache(r repository.ClockedRepo) (*RepoCache, chan BuildEvent) {
	return NewNamedRepoCache(r, defaultRepoName)
}

// NewNamedRepoCache create or open a named cache on top of a raw repository.
// The caller is expected to read all returned events before the cache is considered
// ready to use.
func NewNamedRepoCache(r repository.ClockedRepo, name string) (*RepoCache, chan BuildEvent) {
	c := &RepoCache{
		repo: r,
		name: name,
	}

	c.identities = NewRepoCacheIdentity(r, c.getResolvers, c.GetUserIdentity)
	c.subcaches = append(c.subcaches, c.identities)

	c.bugs = NewRepoCacheBug(r, c.getResolvers, c.GetUserIdentity)
	c.subcaches = append(c.subcaches, c.bugs)

	// bugs and issues are peers while the store migrates from one to the
	// other (f4bac00, bf6f392); entities/bug leaves once it has.
	c.issues = NewRepoCacheIssue(r, c.getResolvers, c.GetUserIdentity)
	c.subcaches = append(c.subcaches, c.issues)

	c.resolvers = entity.Resolvers{
		&IdentityCache{}:   entity.ResolverFunc[*IdentityCache](c.identities.Resolve),
		&IdentityExcerpt{}: entity.ResolverFunc[*IdentityExcerpt](c.identities.ResolveExcerpt),
		&BugCache{}:        entity.ResolverFunc[*BugCache](c.bugs.Resolve),
		&BugExcerpt{}:      entity.ResolverFunc[*BugExcerpt](c.bugs.ResolveExcerpt),
		&IssueCache{}:      entity.ResolverFunc[*IssueCache](c.issues.Resolve),
		&IssueExcerpt{}:    entity.ResolverFunc[*IssueExcerpt](c.issues.ResolveExcerpt),
	}

	// small buffer so that the functions below can emit an event without blocking
	events := make(chan BuildEvent)

	go func() {
		defer close(events)

		// No lock is taken here. Readers never lock, and a writer takes the
		// short write lock around its own commit instead (see lock.go).
		err := c.load()
		if err == nil {
			return
		}

		// Cache is either missing, broken or outdated. Rebuilding.
		c.buildCache(events)
	}()

	return c, events
}

func NewRepoCacheNoEvents(r repository.ClockedRepo) (*RepoCache, error) {
	cache, events := NewRepoCache(r)
	for event := range events {
		if event.Err != nil {
			for range events {
			}
			return nil, event.Err
		}
	}
	return cache, nil
}

// Bugs gives access to the Bug entities
func (c *RepoCache) Bugs() *RepoCacheBug {
	return c.bugs
}

// Issues gives access to the Issue entities
func (c *RepoCache) Issues() *RepoCacheIssue {
	return c.issues
}

// Identities gives access to the Identity entities
func (c *RepoCache) Identities() *RepoCacheIdentity {
	return c.identities
}

func (c *RepoCache) getResolvers() entity.Resolvers {
	return c.resolvers
}

// setCacheSize change the maximum number of loaded bugs
func (c *RepoCache) setCacheSize(size int) {
	for _, subcache := range c.subcaches {
		subcache.SetCacheSize(size)
	}
}

// load will try to read from the disk all the cache files
func (c *RepoCache) load() error {
	var errWait multierr.ErrWaitGroup
	for _, mgmt := range c.subcaches {
		errWait.Go(mgmt.Load)
	}
	return errWait.Wait()
}

func (c *RepoCache) Close() error {
	var errWait multierr.ErrWaitGroup
	for _, mgmt := range c.subcaches {
		errWait.Go(mgmt.Close)
	}
	err := errWait.Wait()
	if err != nil {
		return err
	}

	return c.repo.Close()
}

func (c *RepoCache) buildCache(events chan BuildEvent) {
	events <- BuildEvent{Event: BuildEventCacheIsBuilt}

	var wg sync.WaitGroup
	for _, subcache := range c.subcaches {
		wg.Add(1)
		go func(subcache cacheMgmt) {
			defer wg.Done()

			buildEvents := subcache.Build()
			for buildEvent := range buildEvents {
				events <- buildEvent
				if buildEvent.Err != nil {
					return
				}
			}
		}(subcache)
	}
	wg.Wait()
}

func (c *RepoCache) registerObserver(repoName string, typename string, observer Observer) error {
	switch typename {
	case bug.Typename:
		c.bugs.RegisterObserver(repoName, observer)
	case issue.Typename:
		c.issues.RegisterObserver(repoName, observer)
	case identity.Typename:
		c.identities.RegisterObserver(repoName, observer)
	default:
		var allTypenames []string
		for _, subcache := range c.subcaches {
			allTypenames = append(allTypenames, subcache.Typename())
		}
		return fmt.Errorf("unknown typename `%s`, available types are [%s]", typename, strings.Join(allTypenames, ", "))
	}
	return nil
}

func (c *RepoCache) registerAllObservers(repoName string, observer Observer) {
	for _, subcache := range c.subcaches {
		subcache.RegisterObserver(repoName, observer)
	}
}

func (c *RepoCache) unregisterAllObservers(observer Observer) {
	for _, subcache := range c.subcaches {
		subcache.UnregisterObserver(observer)
	}
}
