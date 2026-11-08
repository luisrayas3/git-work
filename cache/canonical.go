package cache

import (
	"sort"
	"sync"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
)

// A Reach is a local copy's first contact with an external system
// (doc/design/cli.md, "The copy that reached the system first is canonical").
type Reach struct {
	// Issue is the external issue the copy is linked to, by the system's own
	// id, or "" when it is not linked yet: an alias names one copy among
	// several only when every copy that reached the system is linked to one
	// issue.
	Issue string
	// At is when the copy first reached the system, on the system's clock.
	At time.Time
	// Rank orders two copies at one At, the lower first.
	Rank int
}

// A ReachFunc is a system's ordering hook: when the copy whose snapshot it
// reads first reached the system, and false when it never has.
// Every input must be an immutable fact of the copy's own history, so that
// every clone holding the copies orders them alike.
type ReachFunc func(snap *issue.Snapshot) (Reach, bool)

// reaches is the ordering hook of each alias namespace that has one.
type reaches struct {
	mu    sync.RWMutex
	hooks map[string]ReachFunc
}

// SetReach registers the ordering hook of an alias namespace, the <name> of
// alias:<name>: what alias resolution orders that namespace's copies by.
// A namespace with no hook orders nothing, so two of its copies of one alias
// are ambiguous.
func (c *RepoCacheIssue) SetReach(namespace string, f ReachFunc) {
	c.reaches.mu.Lock()
	defer c.reaches.mu.Unlock()
	if c.reaches.hooks == nil {
		c.reaches.hooks = map[string]ReachFunc{}
	}
	c.reaches.hooks[namespace] = f
}

func (c *RepoCacheIssue) reachOf(namespace string) ReachFunc {
	c.reaches.mu.RLock()
	defer c.reaches.mu.RUnlock()
	return c.reaches.hooks[namespace]
}

// FirstReached is the canonical one of several local copies of one external
// issue: the copy that reached the system first, by reach, ties to the lower
// entity id; a copy that never reached it is never first
// (doc/design/cli.md, "The copy that reached the system first is canonical").
// When none has reached it the copies are ambiguous, an ErrMultipleMatch
// naming every one.
//
// The caller knows the copies are of one issue: the Jira sync's consolidation
// calls it with the copies of one Jira id, and alias resolution with the
// carriers of one alias.
func (c *RepoCacheIssue) FirstReached(reach ReachFunc, ids []entity.Id) (entity.Id, error) {
	copies, err := c.reachAll(reach, ids)
	if err != nil {
		return "", err
	}
	return firstOf(copies, ids)
}

type reached struct {
	id    entity.Id
	reach Reach
	ok    bool
}

// reachAll loads each copy and reads its reach.
func (c *RepoCacheIssue) reachAll(reach ReachFunc, ids []entity.Id) ([]reached, error) {
	copies := make([]reached, 0, len(ids))
	for _, id := range ids {
		ic, err := c.Resolve(id)
		if err != nil {
			return nil, err
		}
		r, ok := reach(ic.Snapshot())
		r.At = r.At.UTC()
		copies = append(copies, reached{id: id, reach: r, ok: ok && !r.At.IsZero()})
	}
	return copies, nil
}

func firstOf(copies []reached, ids []entity.Id) (entity.Id, error) {
	if len(ids) == 1 {
		return ids[0], nil
	}
	var first *reached
	for i := range copies {
		cp := &copies[i]
		if !cp.ok {
			continue
		}
		if first == nil || before(cp, first) {
			first = cp
		}
	}
	if first == nil {
		return "", ambiguous(ids)
	}
	return first.id, nil
}

func before(a, b *reached) bool {
	switch {
	case !a.reach.At.Equal(b.reach.At):
		return a.reach.At.Before(b.reach.At)
	case a.reach.Rank != b.reach.Rank:
		return a.reach.Rank < b.reach.Rank
	}
	return a.id < b.id
}

func ambiguous(ids []entity.Id) error {
	sorted := append([]entity.Id(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return entity.NewErrMultipleMatch(issue.Typename, sorted)
}

// carrier is an issue carrying an alias, and the namespace it carries it in.
type carrier struct {
	id        entity.Id
	namespace string
}

// aliasNamed is the issue an alias names among the issues carrying it
// (doc/design/cli.md, "A duplicate is drawn by its hash"):
// the only carrier, or, among copies carrying it in one namespace,
// the copy that reached that namespace's system first, by its hook,
// when every copy that reached it is linked to one external issue.
// Anything else is ambiguous, an ErrMultipleMatch naming every carrier.
//
// It loads entities only for an alias two issues carry.
func (c *RepoCacheIssue) aliasNamed(carriers []carrier) (entity.Id, error) {
	ids := make([]entity.Id, len(carriers))
	for i, cr := range carriers {
		ids[i] = cr.id
	}
	switch len(carriers) {
	case 0:
		return "", entity.NewErrNotFound(issue.Typename)
	case 1:
		return ids[0], nil
	}
	namespace := carriers[0].namespace
	for _, cr := range carriers[1:] {
		if cr.namespace != namespace {
			return "", ambiguous(ids)
		}
	}
	reach := c.reachOf(namespace)
	if reach == nil {
		return "", ambiguous(ids)
	}
	copies, err := c.reachAll(reach, ids)
	if err != nil {
		return "", err
	}
	// copies of one alias are copies of one issue only if what reached the
	// system is linked to one issue: a key moved or reused names two
	linked := ""
	for _, cp := range copies {
		if !cp.ok {
			continue
		}
		if cp.reach.Issue == "" || linked != "" && cp.reach.Issue != linked {
			return "", ambiguous(ids)
		}
		linked = cp.reach.Issue
	}
	return firstOf(copies, ids)
}
