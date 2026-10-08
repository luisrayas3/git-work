package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/view"
)

// page is one screen of the renderer: the list, or one issue.
//
// It is not a tea.Model because the stack has to hand it a size and a refresh
// without those going through the message loop twice; everything else it gets
// is the message it would have got anyway.
type page interface {
	// Update handles one message and returns the page it became.
	Update(msg tea.Msg) (page, tea.Cmd)
	// View draws the page, already fitted to the size it was last given.
	View() string
	// Call is the call the page is drawing, its first line: the call, and
	// what follows the kind in place of the argument it names (callLine).
	Call() (call *view.Call, lead, leadArg string)
}

// The messages the pages send each other through the program.
type (
	// pushMsg opens a page on top of the stack: a list opening an issue.
	pushMsg struct{ page page }
	// popMsg closes the top page, revealing the one that opened it.
	popMsg struct{}
	// refreshMsg says the store changed and every page should re-read it.
	refreshMsg struct{}
	// blinkMsg drives the grabbed row's blink.
	blinkMsg struct{}
	// statusMsg is a line for the top page's status line, from the stack.
	statusMsg string
)

// root is the stack of pages, and the two keys that are the program's own.
type root struct {
	pages         []page
	width, height int

	// onCall says the cursor is on the call line, where back from the first
	// view puts it: the line unfolds into the whole command, back from there
	// quits, and any other key returns to the view
	// (doc/design/terminal-renderer.md, 2026-09-28).
	onCall bool

	// answer is what the view returns once the program ends: the id a
	// standalone `new` created, else nothing (doc/design/create.md, C4).
	answer json.RawMessage
}

// callHint is the status line while the cursor is on the call line.
const callHint = "esc: quit · enter: back · ctrl+c: copy the command"

func (r *root) Init() tea.Cmd {
	// the terminal's background decides the highlight colours, which have to
	// be light on a dark terminal and dark on a light one
	return tea.RequestBackgroundColor
}

func (r *root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A terminal that does not know its own size — a pty opened by a
		// script, a multiplexer mid-resize — reports zero. Drawing nothing at
		// all would be the one answer nobody can act on, so the default size
		// stands until a real one arrives.
		if msg.Width <= 0 || msg.Height <= 0 {
			return r, nil
		}
		r.width, r.height = msg.Width, msg.Height
		// every page in the stack, not only the top one: the one underneath
		// is drawn again the moment the top one closes.
		return r, r.broadcast(msg)

	case tea.KeyPressMsg:
		// ctrl+q is the program's, always: grabbed, in a text field, in a
		// picker. Anything that can swallow it can strand the user. It is not
		// ctrl+c, which copies (doc/design/terminal-renderer.md).
		if msg.String() == "ctrl+q" {
			return r, tea.Quit
		}
		if r.onCall {
			return r.callKey(msg)
		}

	case tea.BackgroundColorMsg:
		// an answer that does not parse carries no color, and IsDark calls
		// that dark: it is no answer, and the guess stands
		if msg.Color != nil {
			darkBackground = msg.IsDark()
		}
		return r, r.broadcast(msg)

	case pushMsg:
		r.pages = append(r.pages, msg.page)
		// the new page has never seen a size, so it is told at once
		_, cmd := r.top().Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		return r, cmd

	case popMsg:
		// Back from the first view is the one back that loses the view, so it
		// lands on the call line instead, and quits from there: a stray esc
		// must not end a session.
		if len(r.pages) <= 1 {
			r.onCall = true
			return r, r.say(callHint)
		}
		r.pages = r.pages[:len(r.pages)-1]
		return r, nil

	case refreshMsg:
		return r, r.broadcast(msg)

	case createdMsg:
		// Opened from a view, Create pops back to it and the view puts its
		// cursor on the new issue, which is the ghost's promise kept. With
		// nothing underneath, show on the new issue takes the draft's
		// place, so that the first view stays a view, and the id is the
		// program's answer (doc/design/create.md, C4).
		if len(r.pages) <= 1 {
			r.pages = []page{msg.shown}
			r.answer, _ = json.Marshal(msg.id)
			_, cmd := r.top().Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
			return r, tea.Batch(cmd, r.say("created "+human(msg.id)))
		}
		r.pages = r.pages[:len(r.pages)-1]
		top, cmd := r.top().Update(msg)
		r.pages[len(r.pages)-1] = top
		return r, cmd
	}

	top, cmd := r.top().Update(msg)
	r.pages[len(r.pages)-1] = top
	return r, cmd
}

// callKey is a key with the cursor on the call line: back quits, copy copies
// the command, and anything else returns to the view — a direction, enter or
// tab does only that, and every other key then means what it means there.
func (r *root) callKey(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case keys.back.matches(press):
		return r, tea.Quit
	case keys.copy.matches(press):
		call, _, _ := r.top().Call()
		return r, tea.Batch(tea.SetClipboard(command(call)), r.say("copied the command"))
	}
	r.onCall = false
	cmd := r.say("")
	switch {
	case keys.act.matches(press), keys.next.matches(press), keys.previous.matches(press),
		keys.up.matches(press), keys.down.matches(press), keys.left.matches(press), keys.right.matches(press):
		return r, cmd
	}
	top, more := r.top().Update(press)
	r.pages[len(r.pages)-1] = top
	return r, tea.Batch(cmd, more)
}

// say puts a line in the top page's status line.
func (r *root) say(status string) tea.Cmd {
	top, cmd := r.top().Update(statusMsg(status))
	r.pages[len(r.pages)-1] = top
	return cmd
}

func (r *root) View() tea.View {
	content := r.top().View()
	if r.onCall {
		content = r.withCallUnfolded(content)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

// withCallUnfolded replaces the page's first line, the call, with the call
// unfolded — the query laid out as a pipeline under it — keeping the status
// line and giving up what is under the call to make the room.
func (r *root) withCallUnfolded(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return content
	}
	call, lead, leadArg := r.top().Call()
	unfolded := callLines(call, lead, leadArg, r.width)
	status := lines[len(lines)-1]
	rest := lines[1 : len(lines)-1]
	room := max(r.height-len(unfolded)-1, 0)
	if len(rest) > room {
		rest = rest[:room]
	}
	out := append(unfolded, rest...)
	return strings.Join(append(out, status), "\n")
}

func (r *root) top() page {
	return r.pages[len(r.pages)-1]
}

// broadcast hands a message to every page and batches what they answer.
func (r *root) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(r.pages))
	for at, p := range r.pages {
		updated, cmd := p.Update(msg)
		r.pages[at] = updated
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// watch turns another process's writes into a refresh.
//
// The ref watcher reconciles the subcaches and the subcache tells its
// observers; this forwards that to the program. The forwarding goroutine
// exists because an observer runs on the watcher's own goroutine and
// Program.Send blocks: a slow redraw must not stall the watcher, and a burst
// of events must collapse into one refresh, which a buffer of one does.
func watch(repo *cache.RepoCache, program *tea.Program) func() {
	changed := make(chan struct{}, 1)
	obs := &observer{changed: changed}

	repo.Issues().RegisterObserver(repo.Name(), obs)
	repo.Schema().RegisterObserver(repo.Name(), obs)

	// A watcher that could not start is a view that does not update itself;
	// it is not a view that fails to open.
	stopWatcher, _ := repo.Watch()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-changed:
				program.Send(refreshMsg{})
			}
		}
	}()

	return func() {
		close(done)
		if stopWatcher != nil {
			stopWatcher()
		}
		repo.Issues().UnregisterObserver(obs)
		repo.Schema().UnregisterObserver(obs)
	}
}

// observer coalesces every entity event into one "something changed".
//
// What changed is deliberately ignored: the pages re-read the store, which is
// the same reconciliation the cache does, so a spurious event costs a query
// and a missed one is covered by the watcher's poll (d591cb3).
type observer struct {
	changed chan struct{}
}

func (o *observer) EntityEvent(cache.EntityEventType, string, string, entity.Id) {
	select {
	case o.changed <- struct{}{}:
	default:
	}
}
