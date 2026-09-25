package host

import (
	"encoding/json"
	"fmt"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/config"
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
