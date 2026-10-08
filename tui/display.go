package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
)

// An issue is drawn by its `human_id`: the short hash, or its alias when
// git-work.display.id names a namespace and the alias resolves back to it
// (doc/design/alias-ids.md). The rows a query makes carry it already; what
// the renderer draws from an id alone (a link, a picker, a message) asks the
// cache through humanOf.

// humanOf is the id an issue is drawn by, from its id.
func humanOf(repo *cache.RepoCache, id string) string {
	if repo == nil {
		return entity.Id(id).Human()
	}
	return repo.IssueHumanId(entity.Id(id))
}

// isAlias says whether a drawn id is an alias rather than a hash: a hash is
// always a prefix of the id it stands for, and an alias is never drawn when
// it could be read as one (A3).
func isAlias(id, human string) bool {
	return human != "" && !strings.HasPrefix(id, human)
}

// copyOf is what copying an issue's id puts on the clipboard: what is shown
// (A7). An alias is copied as it is drawn, since it is accepted back at every
// id position; a hash is copied whole, as it always was, since a prefix may
// stop being unique as the store grows.
func copyOf(id, human string) string {
	if isAlias(id, human) {
		return human
	}
	return id
}

// linkCopy is what copying a relation cell puts on the clipboard: the ids it
// holds, each as copyOf has it, so a link copies what it shows.
func linkCopy(repo *cache.RepoCache, ids []string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, copyOf(id, humanOf(repo, id)))
	}
	return strings.Join(out, ", ")
}

// idStyle is the style an id column draws an id in. Ids are drawn dim; with a
// namespace set, an alias is drawn at full strength and the hash an issue fell
// back to stays dim, so the issues that have no name in that namespace stand
// out at no width cost (A3).
func idStyle(repo *cache.RepoCache, base lipgloss.Style, id, human string) lipgloss.Style {
	if repo != nil && repo.DisplayNamespace() != "" && isAlias(id, human) {
		return base
	}
	return base.Faint(true)
}
