package tui

import (
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// A relation field holds the other issue's whole id (AGENTS.md: cross-issue
// relationships are fields whose value is an entity.Id). Nobody reads a
// 64-character hash, so the renderer draws it as the issue it names — short
// id and title — and enter opens its picker on "go to" that issue
// (relationChoices), so enter, enter follows it, which is what makes it a
// link.

// isRelation says whether a kind's values are issue ids.
func isRelation(kind schema.Kind) bool {
	return kind == schema.KindRelation || kind == schema.KindMultiRelation
}

// linkIds are the ids a relation value holds: one, several, or none.
func linkIds(value any) []string {
	switch v := value.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if id, ok := item.(string); ok && id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	return nil
}

// linkLabel is one linked issue as a person reads it: its short id and its
// title. An id the store does not have — a link to an issue not pulled yet —
// is its short id alone, because the link is still true, just not followable.
func linkLabel(repo *cache.RepoCache, id string) string {
	short := id
	if len(short) > idWidth {
		short = short[:idWidth]
	}
	excerpt, err := repo.Issues().ResolveExcerpt(entity.Id(id))
	if err != nil {
		return short
	}
	return short + " " + excerpt.Title()
}

// linkText is a whole relation value, drawn.
func linkText(repo *cache.RepoCache, ids []string) string {
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		labels = append(labels, linkLabel(repo, id))
	}
	return strings.Join(labels, ", ")
}

// A people field (an assignee, a reporter) holds an identity's whole id, and
// is drawn as the person's name the same way (host.UserName). It is not a
// link: there is no page for a person, so enter on it edits it, and the
// picker lists names and writes the id (identityChoices).

// isPerson says whether a kind's value is an identity id.
func isPerson(kind schema.Kind) bool {
	return kind == schema.KindIdentity
}

// personText is a people value, drawn: the name, or the value as it is when
// it is not an id at all.
func personText(repo *cache.RepoCache, value any) string {
	if id, ok := value.(string); ok {
		return host.UserName(repo, id)
	}
	return plainValue(value)
}

// cellText is any value that is not a relation as a cell draws it: a person
// by name, everything else plain.
func (k *kinds) cellText(typeKey, fieldKey string, value any) string {
	if isPerson(k.of(typeKey, fieldKey)) {
		return personText(k.repo, value)
	}
	return plainValue(value)
}

// kinds remembers field kinds for one load, because a list asks for the same
// few (type, field) pairs once per row.
type kinds struct {
	repo  *cache.RepoCache
	known map[[2]string]schema.Kind
}

func newKinds(repo *cache.RepoCache) *kinds {
	return &kinds{repo: repo, known: map[[2]string]schema.Kind{}}
}

func (k *kinds) of(typeKey, fieldKey string) schema.Kind {
	pair := [2]string{typeKey, fieldKey}
	if kind, ok := k.known[pair]; ok {
		return kind
	}
	kind, _ := fieldKind(k.repo, typeKey, fieldKey)
	k.known[pair] = kind
	return kind
}

// relationHint is the status line on a relation cell: what enter does there,
// which on an empty one is change alone.
func relationHint(linked bool) string {
	if linked {
		return "enter: go to · change"
	}
	return "enter: change"
}
