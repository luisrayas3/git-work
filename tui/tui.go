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
// Only `list` and `show` are drawn today. A `board` or a `gantt` fails naming
// this renderer, so a view is never silently a different view than it says.
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
// The answer is nil for every kind today; the signature carries one because a
// view is a question as much as a picture, and the deferred questions to the
// user (choose, confirm, ask) will return what the user answered.
func (r *Renderer) Render(ctx context.Context, repo *cache.RepoCache, call *view.Call) (json.RawMessage, error) {
	// Nesting is in the argument table, and parsed, so that a script written
	// against it fails on the renderer rather than on the spelling.
	for _, arg := range []string{"expand", "depth"} {
		if call.Has(arg) {
			return nil, fmt.Errorf("the terminal renderer does not nest rows yet (84dfbde): %s", arg)
		}
	}

	first, err := r.page(repo, call)
	if err != nil {
		return nil, err
	}

	in, closeIn, err := openInput()
	if err != nil {
		return nil, err
	}
	defer closeIn()

	program := tea.NewProgram(&root{pages: []page{first}, width: 80, height: 24},
		tea.WithContext(ctx),
		tea.WithInput(in),
		tea.WithOutput(r.out),
	)

	stopWatching := watch(repo, program)
	defer stopWatching()

	// The program runs on its own goroutine and the caller's stays here, as a
	// worker: a view blocks the script that called it, and the script's thread
	// is the only one Starlark may be re-entered on. Nothing posts a job yet:
	// actions injected into views are deferred, not decided
	// (doc/design/terminal-renderer.md), and when they come, this loop is
	// where they run, so navigation and redraw never wait on a script.
	jobs := make(chan func())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	for {
		select {
		case job := <-jobs:
			job()
		case err := <-done:
			return nil, err
		}
	}
}

// page builds the first screen of a call, or says which renderer is missing.
func (r *Renderer) page(repo *cache.RepoCache, call *view.Call) (page, error) {
	switch call.Kind {
	case view.KindList:
		return newListPage(repo, call)
	case view.KindShow:
		return newShowPage(repo, call.String("id"), call.Strings("fields"))
	default:
		return nil, fmt.Errorf("the terminal renderer does not draw a %s yet (84dfbde)", call.Kind)
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
