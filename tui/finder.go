package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// The finder is the issue picker (doc/design/app.md, A5): one widget in
// three places — a relation field's picker, the target of a link (A6), and
// the palette's search, where choosing an issue opens its show.
//
// It is a picker whose choices are host.SearchExcerpts's answer to the text,
// asked again each time the text changes, so it matches as
// `git work issue search` does: id prefix, alias, title, description, case
// ignored, the surest match first. The search is a scan over the excerpts the
// cache holds, milliseconds for thousands of issues; what is costly is
// drawing each one, so the finder hands back the first finderLimit and
// counts the rest.

// finderLimit is as many issues as the finder hands back at once: enough to
// walk, few enough to draw on every key.
const finderLimit = 200

// finderScope narrows what the finder offers.
type finderScope struct {
	// types are the types it offers, every type when empty: a relation's
	// target_types
	types []string
	// exclude is an issue never offered: the one a relation is on
	exclude string
	// current is offered even where the scope or the archive would leave it
	// out, since a picker opens on what the cell showed
	current string
}

// newFinder is the finder over the store, with the fixed choices after the
// issues, on the current value.
func newFinder(repo *cache.RepoCache, scope finderScope, fixed []choice, current string) *picker {
	p := &picker{input: pickerInput(), fixed: fixed, current: current, search: findIssues(repo, scope)}
	p.refresh()
	return p
}

// findIssues is the finder's search: the matching issues in the scope, each
// drawn as a link is — the id it is drawn by and the title — with its type
// dim beside it. The archived are searched too, and only the current value
// kept among them.
func findIssues(repo *cache.RepoCache, scope finderScope) func(text string) ([]choice, int) {
	allowed := map[string]bool{}
	for _, t := range scope.types {
		allowed[t] = true
	}
	return func(text string) ([]choice, int) {
		excerpts, err := host.SearchExcerpts(repo, strings.TrimSpace(text), scope.current != "")
		if err != nil {
			return nil, 0
		}
		var out []choice
		more := 0
		for _, excerpt := range excerpts {
			id := excerpt.Id().String()
			if id == scope.exclude {
				continue
			}
			typeKey, _ := issue.String(excerpt.Fields[schema.TypeKey])
			if id != scope.current {
				if archived(excerpt) || (len(allowed) > 0 && !allowed[typeKey]) {
					continue
				}
				if len(out) >= finderLimit {
					more++
					continue
				}
			}
			out = append(out, choice{
				label:   humanOf(repo, id) + " " + excerpt.Title(),
				dim:     typeKey,
				value:   id,
				current: id == scope.current,
			})
		}
		return out, more
	}
}

// archived says whether an excerpt is archived.
func archived(excerpt *cache.IssueExcerpt) bool {
	var is bool
	_ = json.Unmarshal(excerpt.Fields[issue.ArchivedKey], &is)
	return is
}

// searchPanel is the palette's search (`s`): the finder over every issue,
// drawn at the foot of the page as the palette is. Choosing an issue pushes
// its show, opened by the page's own shows, as enter on a row of it would
// (show_map.go).
type searchPanel struct {
	repo   *cache.RepoCache
	shows  json.RawMessage
	picker *picker

	// title, verb and choose make the panel another picker over the page:
	// `r`'s, over the stored flows (home.go). Empty, it is the search.
	title, verb string
	choose      func(chosen choice) tea.Cmd
}

// openSearch is `s`: the finder, over the top page.
func (r *root) openSearch() tea.Cmd {
	top := r.top()
	repo := repoOf(top)
	if repo == nil {
		return bell()
	}
	r.search = &searchPanel{
		repo:   repo,
		shows:  showsOf(top),
		picker: newFinder(repo, finderScope{}, nil, ""),
	}
	return nil
}

// searchKey is a key with the search open: the picker's, and on a choice
// the issue's show pushed over the page.
func (r *root) searchKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	chose, cancelled, cmd := r.search.picker.Update(msg)
	if !chose {
		return r, cmd
	}
	panel := r.search
	r.search = nil
	if cancelled {
		return r, nil
	}
	chosen, _ := panel.picker.chosen()
	if panel.choose != nil {
		return r, panel.choose(chosen)
	}
	shown, err := showOf(panel.repo, panel.shows, chosen.value)
	if err != nil {
		return r, tea.Batch(bell(), r.say(err.Error()))
	}
	return r, func() tea.Msg { return pushMsg{page: shown} }
}

// searchHints is the status line while the search is open.
func (s *searchPanel) hints() string {
	verb := "open"
	if s.verb != "" {
		verb = s.verb
	}
	if s.picker.text() != "" {
		return hints(hint{"enter", verb}, hint{"esc", "clear"})
	}
	return hints(hint{"enter", verb}, hint{"esc", "back"})
}

// heading is the panel's name over its input.
func (s *searchPanel) heading() string {
	if s.title != "" {
		return s.title
	}
	return "search"
}

// withSearch draws the search over the foot of the page, as the palette is
// drawn: the page keeps its first lines.
func (r *root) withSearch(content string) string {
	lines := strings.Split(content, "\n")
	height := max(r.height, len(lines))
	panel := append([]string{
		styleRule.Render(strings.Repeat("─", max(r.width, 1))),
		styleHeader.Render(r.search.heading()),
	}, r.search.picker.lines(r.width)...)
	keep := min(max(height-1-len(panel), 0), len(lines)-1)
	out := append([]string{}, lines[:max(keep, 0)]...)
	for len(out) < height-1-len(panel) {
		out = append(out, "")
	}
	out = append(out, panel...)
	return strings.Join(append(out, bottomLine(r.search.hints(), "", r.width)), "\n")
}
