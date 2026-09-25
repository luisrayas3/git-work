package host

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/flow"
)

// The two attributes a flow entity carries (`3556569` E3).
const (
	AttrScript      = "script"
	AttrDescription = "description"
)

// FlowEntry is one flow in a listing.
//
// Archived is always false in a listing, which hides the archived,
// and is printed anyway so that a consumer reads one shape
// whatever the command asked for.
type FlowEntry struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Params      []FlowParam `json:"params"`
	Archived    bool        `json:"archived"`
}

// FlowParam is one argument of a flow.
//
// A parameter with no default is required,
// and its default is absent rather than null,
// because `None` is a default and JSON null is what it prints as.
type FlowParam struct {
	Name     string          `json:"name"`
	Default  json.RawMessage `json:"default,omitempty"`
	Required bool            `json:"required"`
}

// FlowList returns every unarchived flow, by name,
// and the warnings a reader should be shown about them.
//
// Parsing a handful of small scripts to list their arguments is cheap,
// and it is the only place the arguments can come from:
// the description is mirrored into an attribute so that a listing has one,
// but a signature is not (E2).
func FlowList(repo *cache.RepoCache) ([]FlowEntry, []string, error) {
	entries := []FlowEntry{}
	var warnings []string

	for _, name := range repo.Flows().Keys(config.ShapeFlow) {
		excerpt, err := repo.Flows().CurrentExcerpt(config.ShapeFlow, name)
		if err != nil {
			return nil, warnings, err
		}

		script, _ := excerpt.AttributeString(AttrScript)
		description, _ := excerpt.AttributeString(AttrDescription)

		params, warning := paramsOf(name, script)
		if warning != "" {
			warnings = append(warnings, warning)
		}

		entries = append(entries, FlowEntry{
			Name:        name,
			Description: description,
			Params:      params,
			Archived:    excerpt.Archived,
		})
	}

	return entries, warnings, nil
}

// FlowExport returns a flow's script, verbatim,
// which is `git work flow export NAME` and `work.flow.export(name)`:
// the file an import takes back unchanged.
func FlowExport(repo *cache.RepoCache, name string) (string, error) {
	excerpt, err := FlowExcerpt(repo, name)
	if err != nil {
		return "", err
	}
	return FlowScript(excerpt)
}

// FlowExcerpt resolves a flow name to the entity it names (E7).
func FlowExcerpt(repo *cache.RepoCache, name string) (*cache.ConfigExcerpt, error) {
	excerpt, err := repo.Flows().CurrentExcerpt(config.ShapeFlow, name)
	if err != nil {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	return excerpt, nil
}

// FlowScript returns a flow's source.
func FlowScript(excerpt *cache.ConfigExcerpt) (string, error) {
	script, ok := excerpt.AttributeString(AttrScript)
	if !ok {
		return "", fmt.Errorf("flow %s has no script", excerpt.Key)
	}
	return script, nil
}

// FlowWarnings returns the flows a key silently resolves to one of (E7).
//
// Every flow command asks for them, because a team that loses an edit this way
// is never told otherwise.
func FlowWarnings(repo *cache.RepoCache) []string {
	var warnings []string
	flows := repo.Flows()
	for _, name := range flows.Keys(config.ShapeFlow) {
		for _, duplicate := range flows.Duplicates(config.ShapeFlow, name) {
			warnings = append(warnings, fmt.Sprintf(
				"flow %s is defined by %s too, which is ignored; archive it to repair",
				name, duplicate.Id().Human()))
		}
	}
	return warnings
}

// FlowParams projects a parsed def's parameters.
func FlowParams(def *flow.Def) []FlowParam {
	params := make([]FlowParam, 0, len(def.Params))
	for _, param := range def.Params {
		params = append(params, FlowParam{
			Name:     param.Name,
			Default:  param.Default,
			Required: !param.HasDefault,
		})
	}
	return params
}

// paramsOf reads a script's parameters, tolerating a script that will not parse.
//
// Import refuses one, so this only happens to a flow written by another binary
// or edited by hand; a listing that failed whole because of one such flow
// would hide the ones that are fine.
func paramsOf(name string, script string) ([]FlowParam, string) {
	def, err := flow.Parse(script)
	if err != nil {
		return []FlowParam{}, fmt.Sprintf("flow %s does not parse: %v", name, err)
	}
	return FlowParams(def), ""
}

// The actions an import reports, per flow.
const (
	FlowActionCreate    = "create"
	FlowActionUpdate    = "update"
	FlowActionArchive   = "archive"
	FlowActionUnchanged = "unchanged"
)

// FlowSource is one script an import is given, and where it came from.
//
// The origin is only ever a diagnostic — a path on the command line,
// "standard input", or a position in a script's list —
// because a flow's identity is the function's name and nothing else (E2),
// so the file a script arrived in is never stored.
type FlowSource struct {
	Origin string
	Script string
}

// FlowChange is what an import does to one flow.
//
// Changes is the attributes it writes, in the shape the entity stores them,
// which is what --dry-run prints and what the write then applies.
type FlowChange struct {
	Name    string                  `json:"name"`
	Action  string                  `json:"action"`
	Changes map[string]config.Value `json:"changes"`
}

// FlowImport upserts every script it is given, keyed on the function's name.
//
// Both the changes and the ids created come back, because a dry run prints
// the changes and a write prints the ids (cli-convention.md), and the changes
// are the same list either way: what --dry-run showed is what the next call
// commits.
//
// Every source is parsed before anything is written, so one file with a typo
// in it aborts the whole import rather than half-applying it. Past that point
// an import that touches five flows is five commits — there is no atomic
// multi-entity commit in this store — and the ids created before a failure
// come back with the error, so that nothing written goes unreported (E9).
func FlowImport(repo *cache.RepoCache, sources []FlowSource, prune bool, dryRun bool) ([]FlowChange, []entity.Id, error) {
	parsed, err := parseFlowSources(sources)
	if err != nil {
		return nil, nil, err
	}

	changes, err := planFlowImport(repo, parsed, prune)
	if err != nil {
		return nil, nil, err
	}

	if dryRun {
		return changes, nil, nil
	}

	created, err := applyFlowChanges(repo, changes)
	return changes, created, err
}

// FlowArchive archives a flow, the replicated removal.
//
// The operation is appended and then committed: SetArchived alone leaves it
// on the cached entity, from where it reaches the excerpt and the on-disk
// cache file but never git, so it vanishes on the next cache rebuild
// (589ff1d).
func FlowArchive(repo *cache.RepoCache, name string) error {
	cached, err := flowCurrent(repo, name)
	if err != nil {
		return err
	}

	if _, err := cached.SetArchived(true); err != nil {
		return err
	}
	return cached.Commit()
}

// FlowRm deletes a flow's local ref; the flow comes back on the next pull.
func FlowRm(repo *cache.RepoCache, name string) error {
	excerpt, err := FlowExcerpt(repo, name)
	if err != nil {
		return err
	}

	return repo.Flows().Remove(excerpt.Id().String())
}

// FlowLog returns the operations one flow is made of, oldest first,
// or those of every flow when the name is empty.
//
// A flow is a config entity, so its history reads in the shape a schema's
// does, the entity named and all: one reader, one shape (E9).
func FlowLog(repo *cache.RepoCache, name string) ([]cmdjson.ConfigOperation, error) {
	names := []string{name}
	if name == "" {
		names = repo.Flows().Keys(config.ShapeFlow)
	}

	entries := []cmdjson.ConfigOperation{}
	for _, each := range names {
		cached, err := flowArchivedToo(repo, each)
		if err != nil {
			return nil, err
		}
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

// parsedFlow is one input, read.
type parsedFlow struct {
	def    *flow.Def
	script string
}

// parseFlowSources parses every script, and writes nothing.
func parseFlowSources(sources []FlowSource) ([]parsedFlow, error) {
	parsed := make([]parsedFlow, 0, len(sources))
	byName := make(map[string]string, len(sources))

	for _, source := range sources {
		def, err := flow.Parse(source.Script)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source.Origin, err)
		}
		if origin, ok := byName[def.Name]; ok {
			return nil, fmt.Errorf("%s: flow %s is already defined by %s",
				source.Origin, def.Name, origin)
		}
		byName[def.Name] = source.Origin
		parsed = append(parsed, parsedFlow{def: def, script: source.Script})
	}

	return parsed, nil
}

// planFlowImport computes what the import would do, per flow, by name.
func planFlowImport(repo *cache.RepoCache, inputs []parsedFlow, prune bool) ([]FlowChange, error) {
	current := make(map[string]*cache.ConfigExcerpt)
	for _, name := range repo.Flows().Keys(config.ShapeFlow) {
		excerpt, err := repo.Flows().CurrentExcerpt(config.ShapeFlow, name)
		if err != nil {
			return nil, err
		}
		current[name] = excerpt
	}

	var changes []FlowChange
	desired := make(map[string]struct{}, len(inputs))

	for _, input := range inputs {
		name := input.def.Name
		desired[name] = struct{}{}

		excerpt, exists := current[name]
		if !exists {
			attributes := map[string]config.Value{
				AttrScript: config.StringValue(input.script),
			}
			if input.def.Description != "" {
				attributes[AttrDescription] = config.StringValue(input.def.Description)
			}
			changes = append(changes, FlowChange{
				Name: name, Action: FlowActionCreate, Changes: attributes,
			})
			continue
		}

		// The comparison is on the decoded value, not on the stored bytes:
		// what matters is whether the flow differs,
		// not how a previous binary encoded the same string.
		set := map[string]config.Value{}
		if script, _ := excerpt.AttributeString(AttrScript); script != input.script {
			set[AttrScript] = config.StringValue(input.script)
		}
		if description, _ := excerpt.AttributeString(AttrDescription); description != input.def.Description {
			set[AttrDescription] = config.StringValue(input.def.Description)
		}

		action := FlowActionUpdate
		if len(set) == 0 {
			action = FlowActionUnchanged
		}
		changes = append(changes, FlowChange{Name: name, Action: action, Changes: set})
	}

	if prune {
		for name := range current {
			if _, ok := desired[name]; ok {
				continue
			}
			changes = append(changes, FlowChange{
				Name:    name,
				Action:  FlowActionArchive,
				Changes: map[string]config.Value{"archived": config.MustValue(true)},
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })

	return changes, nil
}

// applyFlowChanges writes the plan, one entity at a time through the cache,
// and returns the ids of the flows it created.
func applyFlowChanges(repo *cache.RepoCache, changes []FlowChange) ([]entity.Id, error) {
	var created []entity.Id

	for _, change := range changes {
		switch change.Action {
		case FlowActionCreate:
			cached, _, err := repo.Flows().New(config.ShapeFlow, change.Name, change.Changes)
			if err != nil {
				return created, fmt.Errorf("flow %s: %w", change.Name, err)
			}
			created = append(created, cached.Id())

		case FlowActionUpdate:
			cached, err := flowCurrent(repo, change.Name)
			if err != nil {
				return created, err
			}
			if err := cached.Update(change.Changes, nil); err != nil {
				return created, fmt.Errorf("flow %s: %w", change.Name, err)
			}

		case FlowActionArchive:
			if err := FlowArchive(repo, change.Name); err != nil {
				return created, fmt.Errorf("flow %s: %w", change.Name, err)
			}

		case FlowActionUnchanged:
			// nothing to write, and nothing to say about it
		}
	}

	return created, nil
}

// flowCurrent resolves a flow name to the cached entity, for a write.
func flowCurrent(repo *cache.RepoCache, name string) (*cache.ConfigCache, error) {
	cached, err := repo.Flows().Current(config.ShapeFlow, name)
	if err != nil {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	return cached, nil
}

// flowArchivedToo resolves a flow name to its entity, archived or not.
//
// A listing hides the archived and a writer refuses to touch one,
// but a log is history: an archived flow still has one,
// and reading why it was archived is the first thing anyone asks.
func flowArchivedToo(repo *cache.RepoCache, name string) (*cache.ConfigCache, error) {
	matching := repo.Flows().Query(cache.ConfigQuery{
		Shape:           config.ShapeFlow,
		Key:             name,
		IncludeArchived: true,
	})
	if len(matching) == 0 {
		return nil, fmt.Errorf("no flow named %s", name)
	}
	// ordered by (key, creation, id), so the first is the one E7 resolves to
	return repo.Flows().Resolve(matching[0].Id())
}
