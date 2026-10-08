package cache

import (
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// SchemaEntries returns the unarchived winner of every (shape, key) in this
// namespace, which is what the schema is compiled from (E7, E8).
//
// It reads the excerpts alone: a config excerpt carries its whole document,
// so compiling the schema never loads an entity.
func (c *RepoCacheConfig) SchemaEntries() ([]schema.Entry, error) {
	var entries []schema.Entry

	for _, shape := range c.store.Shapes() {
		for _, key := range c.Keys(shape) {
			excerpt, err := c.CurrentExcerpt(shape, key)
			if err != nil {
				return nil, err
			}
			entries = append(entries, schema.Entry{
				Id:         excerpt.Id(),
				Shape:      excerpt.Shape,
				Key:        excerpt.Key,
				Attributes: excerpt.Attributes,
			})
		}
	}

	return entries, nil
}

// AllDuplicates returns every entity a key resolves away from: the losers of
// two clones defining one key before either pushed (E7).
//
// It is empty in the ordinary case, and every schema command prints what it
// returns on stderr, because a key silently resolving to one of two entities
// is how a team loses an edit without ever being told.
func (c *RepoCacheConfig) AllDuplicates() []*ConfigExcerpt {
	var duplicates []*ConfigExcerpt
	for _, shape := range c.store.Shapes() {
		for _, key := range c.Keys(shape) {
			duplicates = append(duplicates, c.Duplicates(shape, key)...)
		}
	}
	return duplicates
}

// LoadSchema compiles the built-ins plus every unarchived type and field
// entity into one schema (E8).
//
// It is not called Schema(): that name is the work-schema subcache's,
// and the two are different things —
// the subcache is the entities, this is what they mean.
//
// Nothing is memoised: the excerpts are in memory already and a schema is a
// few dozen small documents, so compiling on demand is cheaper than a cache
// that can go stale behind the ref watcher.
func (c *RepoCache) LoadSchema() (*schema.Schema, error) {
	return schema.Load(c.schema)
}

// Checker validates an issue write against the live schema.
//
// Validation belongs here, on the write path, and not in the entity:
// an operation's Validate is frozen and must accept everything ever written,
// while a schema says what may be written now (D6, E6).
func (c *RepoCache) Checker() (*schema.Checker, error) {
	s, err := c.LoadSchema()
	if err != nil {
		return nil, err
	}
	return &schema.Checker{Schema: s, Resolver: schemaResolver{repo: c}}, nil
}

// schemaResolver answers the two questions a value check can not answer
// from the schema alone: whether an identity exists, and what an issue is.
type schemaResolver struct {
	repo *RepoCache
}

func (r schemaResolver) IdentityExists(id string) error {
	_, err := r.repo.Identities().ResolveExcerptPrefix(id)
	return err
}

func (r schemaResolver) IssueType(id string) (string, error) {
	excerpt, err := r.repo.Issues().ResolveExcerptPrefix(id)
	if err != nil {
		// an alias is accepted wherever an id is (483dbe2)
		aliased, aliasErr := r.repo.Issues().ResolvePrefixOrAlias(id)
		if aliasErr != nil {
			return "", err
		}
		return issueTypeOf(aliased.Snapshot().Fields), nil
	}
	return issueTypeOf(excerpt.Fields), nil
}

// issueTypeOf reads an issue's type field, which is the key to the rest of
// the schema. An issue with no type has none: the empty string.
func issueTypeOf(fields map[string]issue.Value) string {
	raw, ok := fields[schema.TypeKey]
	if !ok {
		return ""
	}
	typeKey, ok := issue.String(raw)
	if !ok {
		return ""
	}
	return typeKey
}

// configShapeOfKey guesses which shape a key names,
// `type` first and `field` second, as `schema log KEY` resolves it (E10).
func configShapeOfKey(key string) []config.Shape {
	if _, _, ok := config.SplitFieldKey(key); ok {
		return []config.Shape{config.ShapeField, config.ShapeType}
	}
	return []config.Shape{config.ShapeType, config.ShapeField}
}

// ResolveSchemaKey finds the entity a schema command's KEY argument names,
// trying the type shape and then the field shape.
//
// A key two unarchived entities hold is refused, naming every holder,
// because the intent of a write on a duplicated key is to end the duplication,
// a write to the loser, and resolving it to the winner would lose that edit
// without a word (schema-archive-id.md, A2).
// Not-found is returned only when every shape found nothing.
func (c *RepoCacheConfig) ResolveSchemaKey(key string) (*ConfigCache, error) {
	var lastErr error
	for _, shape := range configShapeOfKey(key) {
		cached, err := c.ResolveKey(shape, key)
		if err == nil {
			return cached, nil
		}
		if multiple, ok := err.(*entity.ErrMultipleMatch); ok {
			return nil, duplicatedKeyError(key, multiple.Matching)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no type or field %s: %w", key, lastErr)
}

// duplicatedKeyError names every holder of a key, the winner first,
// and the flag that names one of them.
// The ids come in the order ResolveKey found them, which is Current's.
func duplicatedKeyError(key string, ids []entity.Id) error {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = id.Human()
	}
	names[0] += " (current)"

	times := "twice"
	if len(ids) > 2 {
		times = fmt.Sprintf("%d times", len(ids))
	}
	last := len(names) - 1
	listed := strings.Join(names[:last], ", ") + " and " + names[last]

	return fmt.Errorf("%s is defined %s: %s;\nname one with --id", key, times, listed)
}

// SchemaHolders returns every unarchived entity holding a key,
// the winner first and the rest by creation,
// which is what `schema log KEY` prints (schema-archive-id.md, A2).
func (c *RepoCacheConfig) SchemaHolders(key string) ([]*ConfigCache, error) {
	for _, shape := range configShapeOfKey(key) {
		candidates := c.candidates(shape, key)
		if len(candidates) == 0 {
			continue
		}
		holders := make([]*ConfigCache, 0, len(candidates))
		for _, excerpt := range candidates {
			cached, err := c.Resolve(excerpt.Id())
			if err != nil {
				return nil, err
			}
			holders = append(holders, cached)
		}
		return holders, nil
	}
	return nil, fmt.Errorf("no type or field %s: %w", key, entity.NewErrNotFound(c.Typename()))
}

// ResolveSchemaId finds the type or field entity whose id has the prefix:
// archived or not, winner or loser, and never a key (schema-archive-id.md, A3).
func (c *RepoCacheConfig) ResolveSchemaId(prefix string) (*ConfigCache, error) {
	if prefix == "" {
		return nil, fmt.Errorf("an empty id names no entity")
	}
	return c.ResolvePrefix(prefix)
}
