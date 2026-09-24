package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
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
}

// The messages the pages send each other through the program.
type (
	// pushMsg opens a page on top of the stack: a list opening an issue.
	pushMsg struct{ page page }
	// popMsg closes the top page, revealing the one that opened it.
	popMsg struct{}
	// refreshMsg says the store changed and every page should re-read it.
	refreshMsg struct{}
)

// root is the stack of pages, and the two keys that are the program's own.
type root struct {
	pages         []page
	width, height int
}

func (r *root) Init() tea.Cmd {
	return nil
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
		// ctrl+c is the program's, always: grabbed, in a text field, in a
		// picker. Anything that can swallow it can strand the user.
		if msg.String() == "ctrl+c" {
			return r, tea.Quit
		}

	case pushMsg:
		r.pages = append(r.pages, msg.page)
		// the new page has never seen a size, so it is told at once
		_, cmd := r.top().Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		return r, cmd

	case popMsg:
		if len(r.pages) <= 1 {
			return r, tea.Quit
		}
		r.pages = r.pages[:len(r.pages)-1]
		return r, nil

	case refreshMsg:
		return r, r.broadcast(msg)
	}

	top, cmd := r.top().Update(msg)
	r.pages[len(r.pages)-1] = top
	return r, cmd
}

func (r *root) View() tea.View {
	view := tea.NewView(r.top().View())
	view.AltScreen = true
	return view
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
