package host

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/view"
)

// View parses a view call and hands it to a renderer, which draws it and
// blocks until the user is done.
//
// It is the one code path: `git work view KIND KWARGS` and
// `work.view.KIND(**kwargs)` are this function twice over,
// so the two surfaces can not disagree about what a view takes
// or about what happens when nothing can draw it.
//
// The renderer is nil where nothing can draw: a pipe, an agent, a flow run
// without a terminal. That is not a failure of the call, it is the absence of
// a surface, and it is said that way.
func View(ctx context.Context, repo *cache.RepoCache, renderer view.Renderer, kind string, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	call, err := view.Parse(kind, kwargs)
	if err != nil {
		return nil, err
	}
	// what the table cannot check alone is checked against the live schema,
	// here, so that every surface refuses a bad call the same way, and
	// before it is drawn
	if call.Has("expand") || call.Has("shows") {
		s, err := repo.LoadSchema()
		if err != nil {
			return nil, err
		}
		if err := view.CheckSchema(call, s); err != nil {
			return nil, err
		}
	}

	// `draft`, on new and on every kind that draws a ghost, is checked as
	// `issue new` would check it, so a flow learns at the call and not
	// after the person has typed (doc/design/create.md, C1 and C9)
	if call.Has("draft") {
		if _, err := IssueDraft(repo, call.Raw("draft")); err != nil {
			return nil, fmt.Errorf("view %s: draft %w", call.Kind, err)
		}
	}

	if call.Kind == view.KindHome {
		if err := checkHomeFlows(repo, call.Strings("flows")); err != nil {
			return nil, err
		}
	}

	if renderer == nil {
		return nil, view.ErrNoTerminal
	}

	return renderer.Render(ctx, repo, call)
}

// checkHomeFlows refuses a home page that names a flow the store does not
// have, naming the ones it does, as an unknown field is refused naming the
// type's fields: a home whose entry fails only when it is chosen is a home
// that has rotted unseen (doc/design/app.md, A8).
func checkHomeFlows(repo *cache.RepoCache, names []string) error {
	stored := repo.Flows().Keys(config.ShapeFlow)
	if len(names) == 0 {
		return fmt.Errorf("view home: flows is empty, it names the flows drawn: %s", flowNames(stored))
	}
	var missing []string
	for _, name := range names {
		if !slices.Contains(stored, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("view home: no flow named %s, the flows are %s",
			strings.Join(missing, ", "), flowNames(stored))
	}
	return nil
}

func flowNames(names []string) string {
	if len(names) == 0 {
		return "none (git work flow import stores them)"
	}
	return strings.Join(names, ", ")
}
