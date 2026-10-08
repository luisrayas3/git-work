package cache

import (
	"errors"
	"strings"
	"sync"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

// DisplayIdKey is the git config key that names the alias namespace an
// issue's id is drawn in: `jira` draws `PROJ-12` where an issue has that
// alias, and `hash`, the default, draws the short hash
// (doc/design/alias-ids.md A1, A2).
const DisplayIdKey = "git-work.display.id"

// DisplayHash is the value of DisplayIdKey that draws every id as its hash.
const DisplayHash = "hash"

// display is the state behind IssueHumanId: the namespace, read once per
// process, and the index of the aliases that may be drawn, rebuilt when the
// issue excerpts change.
type display struct {
	once      sync.Once
	namespace string

	mu    sync.Mutex
	built bool
	gen   uint64
	names map[entity.Id]string
}

// DisplayNamespace is the alias namespace ids are drawn in, or "" when they
// are drawn as hashes.
//
// It is read through the repository's config, which in a command is the git
// CLI (package gitcli), so an `[include]`, the global config and a
// `git -c git-work.display.id=… work …` override all reach it.
func (c *RepoCache) DisplayNamespace() string {
	c.display.once.Do(func() {
		value, err := c.repo.AnyConfig().ReadString(DisplayIdKey)
		if err != nil && !errors.Is(err, repository.ErrNoConfigEntry) {
			return
		}
		value = strings.TrimSpace(value)
		if value != DisplayHash {
			c.display.namespace = value
		}
	})
	return c.display.namespace
}

// IssueHumanId is the id an issue is drawn by: its alias in the display
// namespace when that alias, typed back into any id position, resolves to
// this issue, and its short hash otherwise (alias-ids.md A3).
//
// It is what `human_id` carries in every issue document, so that every
// renderer that draws `human_id` follows the setting with no rule of its own.
func (c *RepoCache) IssueHumanId(id entity.Id) string {
	namespace := c.DisplayNamespace()
	if namespace == "" {
		return id.Human()
	}

	c.display.mu.Lock()
	defer c.display.mu.Unlock()
	if gen := c.issues.Generation(); !c.display.built || c.display.gen != gen {
		c.display.gen, c.display.names = c.issues.drawnAliases(namespace)
		c.display.built = true
	}
	if name, ok := c.display.names[id]; ok {
		return name
	}
	return id.Human()
}

// IsFallbackId says whether a drawn id is a hash standing in for an alias:
// a namespace is set and the drawn id is a prefix of the issue's id, which an
// alias never is, because A3 does not draw an alias a prefix could be read as.
// A renderer with styling draws such an id dim (alias-ids.md A3).
func (c *RepoCache) IsFallbackId(id, human string) bool {
	return c.DisplayNamespace() != "" && human != "" && strings.HasPrefix(id, human)
}

// drawnAliases is, for every issue an alias in the namespace may be drawn
// for, that alias: the issue ResolvePrefixOrAlias would answer for it.
//
// One pass over the excerpts, under the read lock, with the generation they
// were read at, so the index is current exactly as long as the generation is.
func (c *RepoCacheIssue) drawnAliases(namespace string) (uint64, map[entity.Id]string) {
	key := AliasMetadataPrefix + namespace

	c.mu.RLock()
	gen := c.generation
	all := map[string][]entity.Id{}
	live := map[string][]entity.Id{}
	for id, excerpt := range c.excerpts {
		alias := excerpt.CreateMetadata[key]
		if alias == "" {
			continue
		}
		all[alias] = append(all[alias], id)
		if !excerpt.Consolidated() {
			live[alias] = append(live[alias], id)
		}
	}
	c.mu.RUnlock()

	names := make(map[entity.Id]string, len(all))
	for alias, carriers := range all {
		// the same choice aliasId makes (A6)
		var named entity.Id
		switch {
		case len(live[alias]) == 1:
			named = live[alias][0]
		case len(live[alias]) == 0 && len(carriers) == 1:
			named = carriers[0]
		default:
			continue // ambiguous until a sync consolidates them
		}
		// an id prefix is tried before an alias, so an all-hex alias that is
		// the prefix of exactly one other issue names that issue instead
		if isHex(alias) {
			if other, err := c.ResolveExcerptPrefix(alias); err == nil && other.Id() != named {
				continue
			}
		}
		names[named] = alias
	}
	return gen, names
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return s != ""
}
