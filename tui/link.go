package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// A relation field holds the other issue's whole id (AGENTS.md: cross-issue
// relationships are fields whose value is an entity.Id). Nobody reads a
// 64-character hash, so the renderer draws it as the issue it names — short
// id and title — and enter on it opens that issue, which is what makes it a
// link, while space opens its picker (relationChoices) like any other edit.

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

// linkLabel is one linked issue as a person reads it: the id it is drawn by
// (humanOf, an alias where git-work.display.id asks for one) and its title.
// An id the store does not have — a link to an issue not pulled yet —
// is its short id alone, because the link is still true, just not followable.
func linkLabel(repo *cache.RepoCache, id string) string {
	human, title, ok := linkParts(repo, id)
	if !ok {
		return human
	}
	return human + " " + title
}

// linkParts are linkLabel's two pieces: the id the link is drawn by, and
// the title, with ok false for an id the store does not have, which is the
// short id alone.
func linkParts(repo *cache.RepoCache, id string) (human, title string, ok bool) {
	excerpt, err := repo.Issues().ResolveExcerpt(entity.Id(id))
	if err != nil {
		if len(id) > idWidth {
			id = id[:idWidth]
		}
		return id, "", false
	}
	return humanOf(repo, id), excerpt.Title(), true
}

// linkStyled is linkText drawn, the one way every view draws a link:
// list, board, gantt, matrix, show and new alike. The whole link, id and
// title and the space between them, is faint and underlined: underlined
// as the cue that enter follows it, and faint so it never outweighs the
// row's own title (ownTitle), which is underlined in full strength. An id
// the store does not have is drawn the same, the id alone. The commas
// between links are plain, so each link reads as one.
//
// Every piece is drawn over base, which is the wash of the row under the
// cursor or the reversed cell under the column cursor; on a reversed base
// nothing is faint, the link reversed and underlined, so the cursor reads
// as plainly on a link as anywhere.
// A width above zero cuts the whole to it as truncate cuts the plain text,
// so the text drawn is linkText's cut to the column, the styles kept.
func linkStyled(repo *cache.RepoCache, ids []string, base lipgloss.Style, width int) string {
	style := base.Underline(true)
	if !base.GetReverse() {
		style = style.Faint(true)
	}
	pieces := make([]styledPiece, 0, 3*len(ids))
	for at, link := range ids {
		if at > 0 {
			pieces = append(pieces, styledPiece{", ", base})
		}
		human, title, ok := linkParts(repo, link)
		pieces = append(pieces, styledPiece{human, style})
		if ok {
			pieces = append(pieces, styledPiece{" " + title, style})
		}
	}
	return renderPieces(pieces, width)
}

// styledPiece is a run of text in one style, for a line drawn in several.
type styledPiece struct {
	text  string
	style lipgloss.Style
}

// draw is text in the piece's style, an underlined one drawn as one run
// (underlined), so a link's underline runs unbroken through its spaces.
func (p styledPiece) draw(text string) string {
	if p.style.GetUnderline() {
		return underlined(p.style, text)
	}
	return p.style.Render(text)
}

// renderPieces draws pieces in their styles, cut to a width (none when it
// is not above zero) exactly as truncate cuts their text joined: what fits
// in all but the last column, then the ellipsis, in the style of the piece
// the cut falls in.
func renderPieces(pieces []styledPiece, width int) string {
	total := 0
	for _, piece := range pieces {
		total += ansi.StringWidth(piece.text)
	}
	cut := width > 0 && total > width
	room := width - 1

	var out strings.Builder
	for _, piece := range pieces {
		if piece.text == "" {
			continue
		}
		if !cut {
			out.WriteString(piece.draw(piece.text))
			continue
		}
		if w := ansi.StringWidth(piece.text); w <= room {
			out.WriteString(piece.draw(piece.text))
			room -= w
			continue
		}
		out.WriteString(piece.draw(ansi.Truncate(piece.text, room, "") + "…"))
		break
	}
	return out.String()
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
// link: there is no page for a person, so space on it edits it like any
// field, and the picker lists names and writes the id (identityChoices).

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

// cellText is any value as a cell draws it: a relation as the issues it
// names, a person by name, everything else plain.
func (k *kinds) cellText(typeKey, fieldKey string, value any) string {
	switch kind := k.of(typeKey, fieldKey); {
	case isRelation(kind):
		return linkText(k.repo, linkIds(value))
	case isPerson(kind):
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
