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
// (doc/design/cli.md, "`git-work.display.id` draws an alias in place of the
// hash"; doc/design/cli.md, "The namespace, not Jira").
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
// this issue, and its short hash otherwise
// (doc/design/cli.md, "An alias is drawn only where it names that issue").
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

// drawnAliases is, for every issue an alias in the namespace may be drawn
// for, that alias: the issue ResolvePrefixOrAlias would answer for it.
//
// One pass over the excerpts, under the read lock, with the generation they
// were read at, so the index is current exactly as long as the generation is;
// an alias two issues carry loads them after it, as aliasId does.
func (c *RepoCacheIssue) drawnAliases(namespace string) (uint64, map[entity.Id]string) {
	key := AliasMetadataPrefix + namespace

	c.mu.RLock()
	gen := c.generation
	drawn := map[string]bool{}
	all := map[string][]carrier{}
	for id, excerpt := range c.excerpts {
		if alias := excerpt.CreateMetadata[key]; alias != "" {
			drawn[alias] = true
		}
		seen := map[string]bool{}
		for k, alias := range excerpt.CreateMetadata {
			if !strings.HasPrefix(k, AliasMetadataPrefix) || alias == "" || seen[alias] {
				continue
			}
			seen[alias] = true
			name, _ := excerpt.aliasNamespace(alias)
			all[alias] = append(all[alias], carrier{id, name})
		}
	}
	c.mu.RUnlock()

	names := make(map[entity.Id]string, len(drawn))
	for alias := range drawn {
		// the choice aliasId makes (doc/design/cli.md,
		// "A duplicate is drawn by its hash")
		named, err := c.aliasNamed(all[alias])
		if err != nil {
			continue // ambiguous
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
