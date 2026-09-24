package host

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/git-bug/git-bug/cache"
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

	if renderer == nil {
		return nil, errors.New("no renderer here: a view needs a terminal, or --gui")
	}

	return renderer.Render(ctx, repo, call)
}
