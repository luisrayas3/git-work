package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/view"
)

// The query is edited from the palette (doc/design/app.md, A10, 2026-10-30):
// `j` opens the query section as a text box in the palette's box, and enter
// draws the page again from the call with the query typed in its place.
// The page is rebuilt from the call, as the renderer built it: the call is
// the page, so a page with another query is another page, at the same place
// in the stack. `J` puts back the query the page was called with.
//
// The call the page was first drawn from is kept by the stack, not by the
// page, so every kind with a query has the reset without knowing of it, and
// a flow waiting on the page still gets its answer when it leaves.

// queryEditor is the query being typed, over the page it will redraw.
type queryEditor struct {
	box *commentBox
	// opened is the text the box opened with, which enter leaves alone
	opened string
	// failed is why the last enter drew nothing: the query does not parse,
	// or the page refused it
	failed string
}

// queryEditorHeight is the tallest the box grows, past which it scrolls.
const queryEditorHeight = 16

func newQueryEditor(query string, width int) *queryEditor {
	box := newCommentBox(width)
	box.area.Placeholder = "every issue, last edited first"
	box.area.MinHeight = 3
	box.area.MaxHeight = queryEditorHeight
	if formatted, err := jq.Format(query, width-2); err == nil {
		query = formatted
	}
	box.area.SetValue(query)
	box.area.MoveToBegin()
	box.focus(true)
	return &queryEditor{box: box, opened: box.draft()}
}

// hasQuery says the page's kind takes a query, so `j` and `J` are on its
// palette.
func hasQuery(p page) bool {
	call, _, _ := p.Call()
	for _, arg := range view.Kinds[call.Kind] {
		if arg.Name == "query" {
			return true
		}
	}
	return false
}

// openQueryEditor is `j`: the query section of the palette made a text box.
func (r *root) openQueryEditor() tea.Cmd {
	top := r.top()
	if !hasQuery(top) || repoOf(top) == nil {
		return tea.Batch(r.say("this page has no query"), bell())
	}
	call, _, _ := top.Call()
	width, _ := r.boxRoom("")
	r.query = newQueryEditor(call.String("query"), width-r.labelWidth())
	return nil
}

// queryKey is a key with the query editor open: esc leaves it as it was,
// enter draws the page from the query typed, and every other key is typing.
func (r *root) queryKey(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keys.cancel.matches(press):
		r.query = nil
		return r, nil
	case keys.act.matches(press):
		return r, r.applyQuery()
	}
	return r, r.query.box.Update(press)
}

// applyQuery draws the top page again from its call with the typed query,
// an empty one being the default. What does not draw keeps the editor open,
// saying why, so nothing typed is lost to a typo.
func (r *root) applyQuery() tea.Cmd {
	text := r.query.box.draft()
	top := r.top()
	call, _, _ := top.Call()
	if text == r.query.opened {
		r.query = nil
		return nil
	}
	redrawn, err := r.withQuery(call, text)
	if err != nil {
		r.query.failed = err.Error()
		return bell()
	}
	r.query = nil
	at := len(r.pages) - 1
	if r.called == nil {
		r.called = map[int]*view.Call{}
	}
	if _, kept := r.called[at]; !kept {
		r.called[at] = call
	}
	cmd := r.replaceTop(redrawn)
	if sameQuery(r.called[at], redrawn) {
		delete(r.called, at)
	}
	return tea.Batch(cmd, r.say("query changed; esc J resets it"))
}

// resetQuery is `J`: the top page drawn from the call it was opened with.
func (r *root) resetQuery() tea.Cmd {
	at := len(r.pages) - 1
	called, ok := r.called[at]
	if !ok {
		return tea.Batch(r.say("the query is the one the page was called with"), bell())
	}
	redrawn, err := pageFor(repoOf(r.top()), called)
	if err != nil {
		return tea.Batch(r.say(err.Error()), bell())
	}
	delete(r.called, at)
	return tea.Batch(r.replaceTop(redrawn), r.say("query reset"))
}

// withQuery is the page call draws with query in place of its own.
func (r *root) withQuery(call *view.Call, query string) (page, error) {
	kwargs := make(map[string]json.RawMessage, len(call.Args))
	for name, raw := range call.Args {
		kwargs[name] = raw
	}
	delete(kwargs, "query")
	if query != "" {
		raw, err := json.Marshal(query)
		if err != nil {
			return nil, err
		}
		kwargs["query"] = raw
	}
	parsed, err := view.Parse(call.Kind, kwargs)
	if err != nil {
		return nil, err
	}
	return pageFor(repoOf(r.top()), parsed)
}

// replaceTop puts p where the top page is, told the window's size, as a
// page pushed is.
func (r *root) replaceTop(p page) tea.Cmd {
	r.pages[len(r.pages)-1] = p
	_, cmd := p.Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
	return cmd
}

// sameQuery says a page draws the query a call names, however either is
// laid out.
func sameQuery(call *view.Call, p page) bool {
	drawn, _, _ := p.Call()
	return canonical(call.String("query")) == canonical(drawn.String("query"))
}

// canonical is a query laid out on one line, so two layouts of one program
// compare equal.
func canonical(query string) string {
	if formatted, err := jq.Format(query, 1<<20); err == nil {
		return formatted
	}
	return strings.Join(strings.Fields(query), " ")
}

// pageFor builds the page a call draws, or says which renderer is missing.
func pageFor(repo *cache.RepoCache, call *view.Call) (page, error) {
	switch call.Kind {
	case view.KindList:
		return newListPage(repo, call)
	case view.KindShow:
		return newShowView(repo, call)
	case view.KindBoard:
		return newBoardPage(repo, call)
	case view.KindGantt:
		return newGanttPage(repo, call)
	case view.KindMatrix:
		return newMatrixPage(repo, call)
	case view.KindNew:
		return newNewPage(repo, call)
	case view.KindHome:
		return newHomePage(repo, call)
	default:
		return nil, fmt.Errorf("the terminal renderer does not draw a %s", call.Kind)
	}
}

// queryEditorLines is the palette's inside with the query being typed: the
// call's head, the box under the label query, and why the last enter drew
// nothing.
func (r *root) queryEditorLines(width int) []string {
	col := r.labelWidth()
	lines := append(r.callHead(col, width), "")
	label := pad("query", col)
	for _, line := range r.query.box.View(width-col, false) {
		lines = append(lines, fit(label+line, width))
		label = strings.Repeat(" ", col)
	}
	if r.query.failed != "" {
		lines = append(lines, "")
		lines = append(lines, prose(r.query.failed, width)...)
	}
	return lines
}

// queryHints is the status line while the query is typed.
func queryHints() string {
	return hints(hint{"enter", "draw"}, hint{"alt+enter", "newline"}, hint{"esc", "cancel"})
}

// withQueryEditor draws the query editor as the palette is drawn.
func (r *root) withQueryEditor(content string) string {
	width, _ := r.boxRoom(content)
	return r.withBox(content, r.queryEditorLines(width), queryHints())
}
