package cache

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// reachHook is a registered ReachFunc and the version of what it answers.
type reachHook struct {
	version int
	f       ReachFunc
}

// reachHooks is the ordering hook of each alias namespace that has one,
// registered before any store opens, because every issue excerpt carries
// what each of them answers (doc/design/store.md, "The excerpt carries the
// reach").
var reachHooks = struct {
	mu    sync.RWMutex
	hooks map[string]reachHook
}{hooks: map[string]reachHook{}}

// RegisterReach registers the ordering hook of an alias namespace, the <name>
// of alias:<name>: what alias resolution and FirstReached order that
// namespace's copies by. A namespace with no hook orders nothing, so two of
// its copies of one alias are ambiguous.
//
// It is called from an init function, before any store opens: every issue
// excerpt carries each registered hook's answer, and a cache file built under
// other hooks, or another version of one, is rebuilt. A change to what a hook
// answers for some history changes its version.
func RegisterReach(namespace string, version int, f ReachFunc) {
	reachHooks.mu.Lock()
	defer reachHooks.mu.Unlock()
	if _, ok := reachHooks.hooks[namespace]; ok {
		panic("cache: a second reach hook for alias namespace " + namespace)
	}
	reachHooks.hooks[namespace] = reachHook{version, f}
}

func hasReach(namespace string) bool {
	reachHooks.mu.RLock()
	defer reachHooks.mu.RUnlock()
	_, ok := reachHooks.hooks[namespace]
	return ok
}

// reachesOf is what an excerpt carries of a copy's reach: its Reach in every
// namespace whose hook says it reached that system, the time in UTC.
func reachesOf(snap *issue.Snapshot) map[string]Reach {
	reachHooks.mu.RLock()
	defer reachHooks.mu.RUnlock()
	var out map[string]Reach
	for namespace, hook := range reachHooks.hooks {
		r, ok := hook.f(snap)
		if !ok || r.At.IsZero() {
			continue
		}
		r.At = r.At.UTC()
		if out == nil {
			out = map[string]Reach{}
		}
		out[namespace] = r
	}
	return out
}

// reachStamp names the registered hooks and their versions, which the issue
// excerpts are built with besides this code: the issue cache file records
// it, and one recorded under other hooks is rebuilt.
func reachStamp() string {
	reachHooks.mu.RLock()
	defer reachHooks.mu.RUnlock()
	names := make([]string, 0, len(reachHooks.hooks))
	for namespace, hook := range reachHooks.hooks {
		names = append(names, fmt.Sprintf("%s@%d", namespace, hook.version))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// FirstReached is the canonical one of several local copies of one external
// issue: the copy that reached the system first, by the namespace's hook,
// ties to the lower entity id; a copy that never reached it is never first
// (doc/design/cli.md, "The copy that reached the system first is canonical").
// When none has reached it, the one copy not archived is first
// (doc/design/cli.md, "Archiving breaks a tie nothing else orders");
// without exactly one, the copies are ambiguous, an ErrMultipleMatch
// naming every one.
//
// The caller knows the copies are of one issue: the Jira sync's consolidation
// calls it with the copies of one Jira id, and alias resolution with the
// carriers of one alias. It reads the excerpts and loads no entity.
func (c *RepoCacheIssue) FirstReached(namespace string, ids []entity.Id) (entity.Id, error) {
	copies, err := c.reachAll(namespace, ids)
	if err != nil {
		return "", err
	}
	return firstOf(copies, ids)
}

type reached struct {
	id       entity.Id
	reach    Reach
	ok       bool
	archived bool
}

// reachAll reads each copy's reach in the namespace off its excerpt.
func (c *RepoCacheIssue) reachAll(namespace string, ids []entity.Id) ([]reached, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	copies := make([]reached, 0, len(ids))
	for _, id := range ids {
		excerpt, ok := c.excerpts[id]
		if !ok {
			return nil, entity.NewErrNotFound(c.typename)
		}
		r, ok := excerpt.Reaches[namespace]
		var archived bool
		_ = json.Unmarshal(excerpt.Fields[issue.ArchivedKey], &archived)
		copies = append(copies, reached{id: id, reach: r, ok: ok, archived: archived})
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
	if first != nil {
		return first.id, nil
	}
	// nothing reached the system: a person's archive of every copy but one
	// is the only order left
	// (doc/design/cli.md, "Archiving breaks a tie nothing else orders")
	for i := range copies {
		if copies[i].archived {
			continue
		}
		if first != nil {
			return "", ambiguous(ids)
		}
		first = &copies[i]
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
// It reads the excerpts and loads no entity.
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
	if !hasReach(namespace) {
		return "", ambiguous(ids)
	}
	copies, err := c.reachAll(namespace, ids)
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
