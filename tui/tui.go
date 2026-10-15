// Package tui is the terminal renderer: it draws a view call and blocks until
// the user quits (`84dfbde`).
//
// It is reached two ways, and they are the same way:
// `git work view KIND KWARGS` and a flow's `work.view.KIND(**kwargs)`
// both call `host.View`, which parses the call against the table in package
// `view` and hands the result to whatever renderer the surface could build.
// There is no `tui` command: a view is the command (decided 2026-09-24).
//
// The renderer writes through package `host`, like every other caller, so a
// title edited here and a title set from the shell are the same operation
// against the same write lock. Nothing here touches a ref.
//
// Every kind in the table is drawn: `list`, `show`, `board`, `gantt`,
// `matrix`, `new` and `home`, and `expand` nests the list and the gantt. A kind the table grows
// that this renderer does not draw fails naming it, so a view is never
// silently a different view than it says.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/view"
)

// Renderer draws view calls on a terminal.
type Renderer struct {
	// out is the terminal, held as a file because the program needs the fd.
	out *os.File

	// session is the program a Render started and is still drawing; a view
	// call made while it draws — a flow run from a page — is pushed onto its
	// stack rather than starting a second program (home.go). It is touched
	// only on the goroutine Render was first called on.
	session *session
}

var _ view.Renderer = &Renderer{}

// New returns the terminal renderer for a writer, and whether that writer is
// a terminal at all.
//
// A caller that must draw — `git work view` — turns a false into the error
// that says so. A caller that may not need to draw — `flow run` — passes no
// renderer instead, and `host.View` complains only if the flow calls a view.
func New(out io.Writer) (*Renderer, bool) {
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, false
	}
	return &Renderer{out: file}, true
}

// Render draws a call and blocks until the user quits.
//
// The answer is nil for every kind but `new`, which answers the id it
// created, or nil when it was left without creating (doc/design/create.md,
// C4); the signature carries one because a view is a question as much as a
// picture, and the deferred questions to the user (choose, confirm, ask)
// will return what the user answered.
func (r *Renderer) Render(ctx context.Context, repo *cache.RepoCache, call *view.Call) (json.RawMessage, error) {
	first, err := r.page(repo, call)
	if err != nil {
		return nil, err
	}
	if r.session != nil {
		return r.session.push(first)
	}

	in, closeIn, err := openInput()
	if err != nil {
		return nil, err
	}
	defer closeIn()

	stack := &root{pages: []page{first}, width: 80, height: 24}
	program := tea.NewProgram(stack,
		tea.WithContext(ctx),
		tea.WithInput(in),
		tea.WithOutput(r.out),
	)

	stopWatching := watch(repo, program)
	defer stopWatching()

	return r.drive(ctx, repo, stack, program)
}

// drive runs the program, and is the worker until it ends.
//
// The program runs on its own goroutine and the caller's stays here, as a
// worker: a view blocks the script that called it, and the script's thread
// is the only one Starlark may be re-entered on. A flow run from a page is a
// job here (home.go), so navigation and redraw never wait on a script;
// actions injected into views, still deferred
// (doc/design/terminal-renderer.md), would run here too.
func (r *Renderer) drive(ctx context.Context, repo *cache.RepoCache, stack *root, program *tea.Program) (json.RawMessage, error) {
	s := &session{ctx: ctx, repo: repo, program: program,
		jobs: make(chan func()), finished: make(chan struct{})}
	stack.run = func(name string) tea.Cmd {
		return s.post(func() { r.runFlow(s, name) })
	}
	r.session = s
	defer func() { r.session = nil }()

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		close(s.finished)
		done <- err
	}()

	for {
		select {
		case job := <-s.jobs:
			job()
		case err := <-done:
			if err != nil {
				return nil, err
			}
			return stack.answer, nil
		}
	}
}

// page builds the first screen of a call, or says which renderer is missing.
func (r *Renderer) page(repo *cache.RepoCache, call *view.Call) (page, error) {
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

// openInput returns the terminal to read keys from.
//
// Standard input is not it when the kwargs came in on a pipe
// (`git work flow run NAME -`), so the controlling terminal is opened
// directly: the view is on the screen either way, and it has to be typeable.
func openInput() (io.Reader, func(), error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return os.Stdin, func() {}, nil
	}

	tty, err := os.Open("/dev/tty")
	if err != nil {
		return nil, nil, fmt.Errorf("a view needs a terminal to type into: %w", err)
	}
	return tty, func() { _ = tty.Close() }, nil
}
