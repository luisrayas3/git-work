package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/config"
	flowrun "github.com/git-bug/git-bug/flow/run"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

// Home: the page `git work` opens on, and how a page runs a flow
// (doc/design/app.md, A8).
//
// Flows call views; views never call flows, and this one does not. Enter on
// a flow names it, as Enter on a row names an issue: the page answers a
// runFlowMsg, the stack hands the name to the renderer (root.run), and the
// renderer runs the flow on its worker — the goroutine Render was called on,
// which is the one a Starlark thread may run on — through flowrun.Flow, the
// function `git work flow run` calls. The flow's own `work.view.*` call
// reaches host.View and then this renderer's Render, which, finding its
// program already drawing, pushes the page onto the same stack and blocks
// the flow until that page leaves it (session.push). So the page a flow
// draws from home is a page over home, back is back to home, and the flow
// goes on, or returns, once it is closed, as it would from the shell.
//
// A flow that prints rather than draws gets its output on a page of its own
// (outputPage); one that does neither says so on the status line, and one
// that fails says why there, or on an output page where the reason is a
// Starlark backtrace, which a status line cannot hold.

// runFlowMsg asks the stack to run a flow by name.
type runFlowMsg struct {
	name string
}

// flowDoneMsg is a flow run from a page, finished: what it printed and
// returned, whether it drew any view, and how it failed.
type flowDoneMsg struct {
	name   string
	output string
	drew   bool
	err    error
}

// errQuit is what a flow's view call answers when the program it was drawn
// in has ended under it: the person quit, and the flow is not resumed.
var errQuit = errors.New("the view was quit")

// session is a program a Render started and is still drawing: a view call
// made while it runs is a page pushed onto its stack.
type session struct {
	ctx      context.Context
	repo     *cache.RepoCache
	program  *tea.Program
	jobs     chan func()
	finished chan struct{}
	// drawn counts the pages flows pushed, so that a flow run from a page
	// can tell whether it drew anything. It is the worker's alone.
	drawn int
}

// post hands a job to the worker, from a command's own goroutine, so that
// neither the program nor the worker waits on the other.
func (s *session) post(job func()) tea.Cmd {
	return func() tea.Msg {
		select {
		case s.jobs <- job:
		case <-s.finished:
		}
		return nil
	}
}

// send hands the program a message from the worker, unless it has ended.
func (s *session) send(msg tea.Msg) {
	select {
	case <-s.finished:
		return
	default:
	}
	s.program.Send(msg)
}

// serve runs the jobs posted until closed answers or the program ends, so
// that a flow blocked in a view call does not stop another being run from
// the page it drew.
func (s *session) serve(closed <-chan json.RawMessage) (json.RawMessage, error) {
	for {
		select {
		case job := <-s.jobs:
			job()
		case answer := <-closed:
			return answer, nil
		case <-s.finished:
			return nil, errQuit
		}
	}
}

// push is a view call made while the program draws: its page goes onto the
// stack, and the call returns once the page leaves it, with the page's
// answer, as a call from the shell returns once its program ends.
func (s *session) push(first page) (json.RawMessage, error) {
	select {
	case <-s.finished:
		return nil, errQuit
	default:
	}
	closed := make(chan json.RawMessage, 1)
	s.drawn++
	s.program.Send(pushMsg{page: first, closed: closed})
	return s.serve(closed)
}

// runFlow runs one flow on the worker, its output kept for a page, and
// tells the program how it went.
func (r *Renderer) runFlow(s *session, name string) {
	var out bytes.Buffer
	before := s.drawn
	raw, err := flowrun.Flow(s.ctx, s.repo, flowrun.Options{Stdout: &out, Stderr: &out, Renderer: r}, name, nil)
	if errors.Is(err, errQuit) {
		return
	}
	if err == nil && raw != nil {
		// a returned value prints as JSON after the output, as the command
		// prints it
		var indented bytes.Buffer
		if json.Indent(&indented, raw, "", "    ") == nil {
			raw = indented.Bytes()
		}
		out.Write(raw)
		out.WriteByte('\n')
	}
	s.send(flowDoneMsg{name: name, output: out.String(), drew: s.drawn != before, err: err})
}

// runFlow hands a flow's name to the renderer.
func (r *root) runFlow(msg runFlowMsg) tea.Cmd {
	if r.run == nil {
		return tea.Batch(r.say("no flow runs here"), bell())
	}
	return tea.Batch(r.say("running "+msg.name+"…"), r.run(msg.name))
}

// flowDone is a flow run from a page, finished.
func (r *root) flowDone(msg flowDoneMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		text := msg.err.Error()
		if !strings.Contains(text, "\n") && msg.output == "" {
			return tea.Batch(r.say(text), bell())
		}
		return tea.Batch(r.push(newOutputPage(repoOf(r.top()), msg.name, msg.output+text)),
			r.say(msg.name+" failed"), bell())
	case msg.output != "":
		return r.push(newOutputPage(repoOf(r.top()), msg.name, msg.output))
	case !msg.drew:
		return r.say(msg.name + " ran, and drew and printed nothing")
	}
	return r.say("")
}

// push opens a page on the stack from inside Update.
func (r *root) push(p page) tea.Cmd {
	_, cmd := r.Update(pushMsg{page: p})
	return cmd
}

// goHome is `h`: back down to the home page where the stack has one, or the
// home flow run, its page over this one, where it does not.
func (r *root) goHome() tea.Cmd {
	for at, p := range r.pages {
		if _, ok := p.(*homePage); ok {
			if at == len(r.pages)-1 {
				return tea.Batch(r.say("already home"), bell())
			}
			r.popTo(at+1, nil)
			return nil
		}
	}
	return r.runFlow(runFlowMsg{name: "home"})
}

// openFlowPicker is `r`: every stored flow, not only home's, in the picker
// the search is drawn in (finder.go), over the page; the flow chosen runs,
// its page pushed over this one.
func (r *root) openFlowPicker() tea.Cmd {
	repo := repoOf(r.top())
	if repo == nil {
		return bell()
	}
	flows := flowChoices(repo, repo.Flows().Keys(config.ShapeFlow))
	if len(flows) == 0 {
		return tea.Batch(r.say("no flow is stored; git work flow import stores them"), bell())
	}
	r.search = &searchPanel{
		repo:   repo,
		picker: newPicker(flows, ""),
		title:  "run a flow",
		verb:   "run",
		choose: chooseFlow,
	}
	return nil
}

// chooseFlow is enter on a flow in a picker: its name handed to the stack,
// never the flow called.
func chooseFlow(chosen choice) tea.Cmd {
	if chosen.value == "" {
		return tea.Batch(func() tea.Msg { return statusMsg("no flow named " + chosen.label) }, bell())
	}
	return func() tea.Msg { return runFlowMsg{name: chosen.value} }
}

// flowChoices are flows as a picker lists them: the name, its docstring's
// first line dim beside it. A name the store no longer has — archived since
// the call was checked — is drawn as such and writes nothing.
func flowChoices(repo *cache.RepoCache, names []string) []choice {
	out := make([]choice, 0, len(names))
	for _, name := range names {
		excerpt, err := host.FlowExcerpt(repo, name)
		if err != nil {
			out = append(out, choice{label: name, dim: "(no such flow)"})
			continue
		}
		description, _ := excerpt.AttributeString(host.AttrDescription)
		out = append(out, choice{label: name, dim: firstLine(description), value: name})
	}
	return out
}

// commandOf is the shell command that draws a page, which the palette
// copies.
func commandOf(p page) string {
	if out, ok := p.(*outputPage); ok {
		return "git work flow run " + out.name
	}
	call, _, _ := p.Call()
	return command(call)
}

// homePage draws flows by name; enter runs one. Its rows are a picker's
// (edit.go), so it is typed into as every picker is: typing narrows, by
// name or first line, and esc clears the text before it is anything else.
type homePage struct {
	repo   *cache.RepoCache
	call   *view.Call
	picker *picker
	top    int

	width, height int
	status        string
	help          *help
}

func newHomePage(repo *cache.RepoCache, call *view.Call) (*homePage, error) {
	p := &homePage{repo: repo, call: call, width: 80, height: 24}
	p.picker = newPicker(flowChoices(repo, call.Strings("flows")), "")
	return p, nil
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func (p *homePage) Call() (*view.Call, string, string) {
	return p.call, "", ""
}

func (p *homePage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case statusMsg:
		p.status = string(msg)
	case refreshMsg:
		cursor := p.picker.cursor
		p.picker.fixed = flowChoices(p.repo, p.call.Strings("flows"))
		p.picker.refresh()
		p.picker.cursor = min(cursor, max(len(p.picker.items)-1, 0))
	case tea.PasteMsg:
		p.picker.setText(msg.Content)
	case tea.KeyPressMsg:
		return p.key(msg)
	}
	return p, nil
}

func (p *homePage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	if p.help != nil {
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
	}

	switch {
	case keys.quit.matches(press):
		return p, tea.Quit
	case keys.help.matches(press):
		p.help = &help{}
		return p, nil
	case keys.filter.matches(press):
		// typing narrows already; `/` is the empty filter it always was
		return p, nil
	case keys.pageUp.matches(press):
		p.picker.move(-p.room())
		return p, nil
	case keys.pageDn.matches(press):
		p.picker.move(p.room())
		return p, nil
	case keys.top.matches(press):
		p.picker.move(-len(p.picker.items))
		return p, nil
	case keys.bottom.matches(press):
		p.picker.move(len(p.picker.items))
		return p, nil
	}

	chose, cancelled, cmd := p.picker.Update(press)
	switch {
	case cancelled:
		return p, func() tea.Msg { return popMsg{} }
	case chose:
		chosen, _ := p.picker.chosen()
		return p, chooseFlow(chosen)
	}
	return p, cmd
}

// room is how many rows fit between the call line and the bottom.
func (p *homePage) room() int {
	return max(p.height-1-len(p.bottom()), 1)
}

func (p *homePage) bottom() []string {
	var lines []string
	if p.picker.text() != "" {
		lines = append(lines, fit("/"+p.picker.input.View(), p.width))
	}
	pairs := []hint{{"enter", "run"}, {"type", "narrow"}}
	if p.picker.text() != "" {
		pairs = append(pairs, hint{"esc", "clear"})
	}
	count := fmt.Sprintf("%d flows", len(p.picker.items))
	return append(lines, bottomLine(hints(pairs...), lastAction(p.status, count), p.width))
}

func (p *homePage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}

	items := p.picker.items
	nameWidth := 0
	for _, item := range items {
		nameWidth = max(nameWidth, len([]rune(item.label)))
	}
	nameWidth = min(nameWidth, max(p.width/3, 8))

	rows := make([]string, 0, len(items))
	for at, item := range items {
		name := pad(item.label, nameWidth)
		marker := "  "
		if at == p.picker.cursor {
			marker = "› "
			name = styleCell.Render(name)
		} else {
			name = styleHeader.Render(name)
		}
		rows = append(rows, fit(marker+name+"  "+styleDim.Render(item.dim), p.width))
	}
	if len(rows) == 0 {
		rows = append(rows, styleDim.Render("  (no flow matches)"))
	}

	bottom := p.bottom()
	room := p.room()
	call, lead, leadArg := p.Call()
	lines := []string{callLine(call, lead, leadArg, p.width)}
	lines = append(lines, window(&p.top, rows, nil, p.picker.cursor, room)...)
	for len(lines) < p.height-len(bottom) {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, bottom...), "\n")
}

// outputPage is what a flow run from a page printed, when it printed
// rather than drew: a report's markdown, read where it was asked for.
type outputPage struct {
	repo *cache.RepoCache
	name string
	text string

	top           int
	width, height int
	status        string
	help          *help
}

func newOutputPage(repo *cache.RepoCache, name, text string) *outputPage {
	return &outputPage{repo: repo, name: name, text: strings.TrimRight(text, "\n"), width: 80, height: 24}
}

// Call is the command that printed it, which no view kind is: the first
// line reads `flow run NAME`.
func (p *outputPage) Call() (*view.Call, string, string) {
	return &view.Call{Kind: "flow run"}, p.name, ""
}

func (p *outputPage) Update(msg tea.Msg) (page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case statusMsg:
		p.status = string(msg)
	case tea.KeyPressMsg:
		return p.key(msg)
	}
	return p, nil
}

func (p *outputPage) key(press tea.KeyPressMsg) (page, tea.Cmd) {
	if p.help != nil {
		if p.help.Update(press) {
			p.help = nil
		}
		return p, nil
	}
	room := p.room()
	switch {
	case keys.quit.matches(press):
		return p, tea.Quit
	case keys.up.matches(press):
		p.top--
	case keys.down.matches(press):
		p.top++
	case keys.pageUp.matches(press):
		p.top -= room
	case keys.pageDn.matches(press), press.Code == tea.KeySpace:
		p.top += room
	case keys.top.matches(press):
		p.top = 0
	case keys.bottom.matches(press):
		p.top = len(p.lines())
	case keys.copy.matches(press):
		p.status = "copied the output"
		return p, setClipboard(p.text)
	case keys.help.matches(press):
		p.help = &help{}
	case keys.back.matches(press):
		return p, func() tea.Msg { return popMsg{} }
	}
	return p, nil
}

func (p *outputPage) lines() []string {
	return prose(p.text, p.width)
}

func (p *outputPage) room() int {
	return max(p.height-2, 1)
}

func (p *outputPage) View() string {
	if p.help != nil {
		return p.help.View(p.width)
	}
	body := p.lines()
	room := p.room()
	p.top = min(max(p.top, 0), max(len(body)-room, 0))

	call, lead, leadArg := p.Call()
	lines := []string{callLine(call, lead, leadArg, p.width)}
	lines = append(lines, body[p.top:min(p.top+room, len(body))]...)
	for len(lines) < p.height-1 {
		lines = append(lines, "")
	}
	position := fmt.Sprintf("%d lines", len(body))
	bottom := bottomLine(hints(hint{"pgdn", "more"}, hint{"esc", "back"}), lastAction(p.status, position), p.width)
	return strings.Join(append(lines, bottom), "\n")
}
