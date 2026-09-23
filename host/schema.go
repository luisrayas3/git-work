package host

import (
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// SchemaDuplicates returns every key that resolves away from an entity (E7).
//
// It is empty in the ordinary case, and every schema caller prints what it
// returns on stderr, because a key silently resolving to one of two entities
// is how a team loses an edit without ever being told.
func SchemaDuplicates(repo *cache.RepoCache) []string {
	var warnings []string
	for _, excerpt := range repo.Schema().AllDuplicates() {
		warnings = append(warnings, fmt.Sprintf(
			"%s %s is defined twice; %s is ignored, archive or import it",
			excerpt.Shape, excerpt.Key, excerpt.Id().Human()))
	}
	return warnings
}

// SchemaExport returns the live schema as the document a human edits,
// and the warnings a reader should be shown about it.
//
// The warnings are the duplicate keys and then whatever compiling the schema
// could not make sense of: a read never fails on what was written before (D6),
// so a dangling target type is said out loud and nothing more.
// They come back rather than reaching stderr here,
// because a script's diagnostics and a command's go to different places.
func SchemaExport(repo *cache.RepoCache) (*schema.Document, []string, error) {
	warnings := SchemaDuplicates(repo)

	s, err := repo.LoadSchema()
	if err != nil {
		return nil, warnings, err
	}
	warnings = append(warnings, s.Problems...)

	return schema.Export(s), warnings, nil
}

// SchemaImport writes what a document differs from the store by.
//
// The whole document is validated before anything is written, then reconcile
// says what differs, and dryRun stops there: the changes are what `--dry-run`
// prints and what the same call commits a moment later.
// Both are returned — the changes, and the ids of the entities created —
// because a writer prints ids and a dry run prints changes (cli-convention.md).
//
// An import that touches five entities is five commits: there is no atomic
// multi-entity commit in this store, and there does not need to be, because
// each entity is valid on its own at every step (E9).
// A failure part of the way through returns the ids already created with the
// error, so that nothing that was written goes unreported.
func SchemaImport(repo *cache.RepoCache, doc *schema.Document, prune bool, dryRun bool) ([]schema.Change, []entity.Id, error) {
	if err := doc.Validate(repo.Schema().Keys(config.ShapeType)); err != nil {
		return nil, nil, err
	}

	current, err := repo.Schema().SchemaEntries()
	if err != nil {
		return nil, nil, err
	}

	changes, err := schema.Reconcile(doc, current, prune)
	if err != nil {
		return nil, nil, err
	}

	if dryRun {
		return changes, nil, nil
	}

	created, err := applySchemaChanges(repo, changes)
	return changes, created, err
}

// SchemaInit instantiates an embedded preset as config entities.
//
// It refuses when a field entity already exists, archived or not, because a
// preset is a starting point and not a merge; to change a schema that is
// already there, export it, edit it and import it.
func SchemaInit(repo *cache.RepoCache, preset string, dryRun bool) ([]schema.Change, []entity.Id, error) {
	existing := repo.Schema().Query(cache.ConfigQuery{
		Shape:           config.ShapeField,
		IncludeArchived: true,
	})
	if len(existing) > 0 {
		keys := make([]string, 0, len(existing))
		for _, excerpt := range existing {
			keys = append(keys, excerpt.Key)
		}
		return nil, nil, fmt.Errorf("the schema already has %d fields (%s); export, edit and import it instead",
			len(existing), strings.Join(keys, ", "))
	}

	doc, err := schema.Preset(preset)
	if err != nil {
		return nil, nil, err
	}

	return SchemaImport(repo, doc, false, dryRun)
}

// SchemaLog returns the operations the schema is made of, oldest first within
// each entity. An empty key is every type and field entity, archived included.
func SchemaLog(repo *cache.RepoCache, key string) ([]cmdjson.ConfigOperation, error) {
	var entities []*cache.ConfigCache

	if key != "" {
		cached, err := resolveSchemaKey(repo, key)
		if err != nil {
			return nil, err
		}
		entities = append(entities, cached)
	} else {
		for _, excerpt := range repo.Schema().Query(cache.ConfigQuery{IncludeArchived: true}) {
			cached, err := repo.Schema().Resolve(excerpt.Id())
			if err != nil {
				return nil, err
			}
			entities = append(entities, cached)
		}
	}

	entries := []cmdjson.ConfigOperation{}
	for _, cached := range entities {
		snap := cached.Snapshot()
		for _, op := range snap.AllOperations() {
			entry, err := cmdjson.NewConfigOperation(snap, op)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// SchemaArchive archives a type or a field, the replicated removal,
// and returns what archiving it left behind.
func SchemaArchive(repo *cache.RepoCache, key string) ([]string, error) {
	cached, err := resolveSchemaKey(repo, key)
	if err != nil {
		return nil, err
	}

	if _, err := cached.SetArchived(true); err != nil {
		return nil, err
	}
	if err := cached.Commit(); err != nil {
		return nil, err
	}

	return orphanedFields(repo, cached.Shape(), cached.Key()), nil
}

// SchemaRm deletes a type's or a field's local ref;
// the entity comes back on the next pull.
func SchemaRm(repo *cache.RepoCache, key string) error {
	cached, err := resolveSchemaKey(repo, key)
	if err != nil {
		return err
	}

	return repo.Schema().Remove(cached.Id().String())
}

// applySchemaChanges writes what reconcile computed, one entity at a time,
// and returns the ids of the entities it created.
func applySchemaChanges(repo *cache.RepoCache, changes []schema.Change) ([]entity.Id, error) {
	var created []entity.Id

	for _, change := range changes {
		id, err := applySchemaChange(repo, change)
		if err != nil {
			return created, fmt.Errorf("%s %s: %w", change.Shape, change.Key, err)
		}
		if id != entity.UnsetId {
			created = append(created, id)
		}
	}

	return created, nil
}

func applySchemaChange(repo *cache.RepoCache, change schema.Change) (entity.Id, error) {
	schemaCache := repo.Schema()

	switch change.Action {
	case schema.ActionCreate:
		cached, _, err := schemaCache.New(change.Shape, change.Key, change.Set)
		if err != nil {
			return entity.UnsetId, err
		}
		return cached.Id(), nil

	case schema.ActionUpdate:
		cached, err := schemaCache.Resolve(change.Id)
		if err != nil {
			return entity.UnsetId, err
		}
		// one pack, one commit: a renumbered list of values lands together
		return entity.UnsetId, cached.Update(change.Set, change.Remove)

	case schema.ActionArchive:
		cached, err := schemaCache.Resolve(change.Id)
		if err != nil {
			return entity.UnsetId, err
		}
		if _, err := cached.SetArchived(true); err != nil {
			return entity.UnsetId, err
		}
		return entity.UnsetId, cached.Commit()

	default:
		return entity.UnsetId, fmt.Errorf("unknown action %s", change.Action)
	}
}

// orphanedFields names the fields an archived type leaves behind.
//
// Nothing is refused and nothing else is archived: a multi-entity change is
// not atomic here (AGENTS.md), so the honest thing is to name what is left.
func orphanedFields(repo *cache.RepoCache, shape config.Shape, key string) []string {
	if shape != config.ShapeType {
		return nil
	}

	var warnings []string
	for _, fieldKey := range repo.Schema().Keys(config.ShapeField) {
		typeKey, _, ok := config.SplitFieldKey(fieldKey)
		if ok && typeKey == key {
			warnings = append(warnings, fmt.Sprintf(
				"field %s is still live on the archived type %s", fieldKey, key))
		}
	}
	return warnings
}

// resolveSchemaKey finds the entity a KEY argument names, type first,
// field second, the same way from the shell and from a script.
func resolveSchemaKey(repo *cache.RepoCache, key string) (*cache.ConfigCache, error) {
	return repo.Schema().ResolveSchemaKey(key)
}
