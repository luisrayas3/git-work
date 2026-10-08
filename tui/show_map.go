package tui

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// A view's `show` is what Enter opens per type (doc/design/show-from-a-view.md):
// the stored issue's type picks the entry, never a row's shaped `fields.type`,
// an unlisted type opens a bare show, and the page opened carries the map as
// its own `show`, so every page it opens in turn — a side table's row, a
// relation cell, the creator and the page after Create — opens by it too.
// The map travels as the argument's JSON, checked once in host.View.

// storedType is the type an issue holds in the store, "" where it cannot be
// read, which opens a bare show and lets the page report what is wrong.
func storedType(repo *cache.RepoCache, id string) string {
	excerpt, err := repo.Issues().ResolveExcerpt(entity.Id(id))
	if err != nil {
		return ""
	}
	typeKey, _ := issue.String(excerpt.Fields[schema.TypeKey])
	return typeKey
}

// showOf is the show page Enter opens on an issue, by a view's map.
func showOf(repo *cache.RepoCache, shows json.RawMessage, id string) (*showPage, error) {
	call, err := view.ShowFor(shows, storedType(repo, id), id)
	if err != nil {
		return nil, err
	}
	return newShowView(repo, call)
}

// pushShow opens an issue over the page by a view's map, or says why not in
// the status line.
func pushShow(repo *cache.RepoCache, shows json.RawMessage, id string, status *string) tea.Cmd {
	shown, err := showOf(repo, shows, id)
	if err != nil {
		*status = err.Error()
		return bell()
	}
	return func() tea.Msg { return pushMsg{page: shown} }
}
