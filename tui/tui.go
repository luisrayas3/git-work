// Package tui is the terminal renderer: it draws a view call and blocks until
// the user quits (`84dfbde`).
//
// It is reached two ways, and they are the same way:
// `git work view KIND KWARGS` and a flow's `work.view.KIND(**kwargs)`
// both call `host.View`, which parses the call against the table in package
// `view` and hands the result to whatever renderer the surface could build.
// There is no `tui` command: a view is the command (decided 2026-09-24).
//
// This is the seam. No kind is drawn yet, and a kind that is not draws an
// error naming this renderer, so a view is never silently a different view
// than it says.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
// view is a question as much as a picture — `work.view.list(pick=True)` will
// return the issue that was chosen.
func (r *Renderer) Render(ctx context.Context, repo *cache.RepoCache, call *view.Call) (json.RawMessage, error) {
	return nil, fmt.Errorf("the terminal renderer does not draw a %s yet (84dfbde)", call.Kind)
}
