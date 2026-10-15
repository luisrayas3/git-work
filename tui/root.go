package tui

import (
	"encoding/json"

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
	// closed, where there is one, is told when the page leaves the stack,
	// with its answer: it is a flow's view call waiting to return (home.go).
	pushMsg struct {
		page   page
		closed chan<- json.RawMessage
	}
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

	// palette says the palette is open over the top page (palette.go,
	// doc/design/app.md, A1).
	palette bool
	// search is the palette's search, open over the top page (finder.go,
	// A5), or nil
	search *searchPanel

	// answer is what the view returns once the program ends: the id a
	// standalone `new` created, else nothing (doc/design/create.md, C4).
	answer json.RawMessage

	// run hands a flow's name to the renderer, which runs it off the
	// program's goroutine (home.go); nil where nothing can, as in a test.
	run func(name string) tea.Cmd
	// waiting are the pages a flow's view call pushed, by their place in
	// the stack, each told its answer when it leaves.
	waiting map[int]chan<- json.RawMessage
}

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
		if r.palette {
			return r.paletteKey(msg)
		}
		if r.search != nil {
			return r.searchKey(msg)
		}
		if atRest(r.top()) {
			if cmd, done := r.restKey(msg); done {
				return r, cmd
			}
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
		if msg.closed != nil {
			if r.waiting == nil {
				r.waiting = map[int]chan<- json.RawMessage{}
			}
			r.waiting[len(r.pages)-1] = msg.closed
		}
		// the new page has never seen a size, so it is told at once
		_, cmd := r.top().Update(tea.WindowSizeMsg{Width: r.width, Height: r.height})
		return r, cmd

	case popMsg:
		// Back from the first page is quit: it is the second esc, the first
		// having opened the palette, so a stray esc still cannot end a
		// session (doc/design/app.md, A1).
		if len(r.pages) <= 1 {
			return r, tea.Quit
		}
		r.popTo(len(r.pages)-1, nil)
		return r, nil

	case tea.PasteMsg:
		if r.search != nil {
			r.search.picker.setText(msg.Content)
			return r, nil
		}

	case runFlowMsg:
		return r, r.runFlow(msg)

	case flowDoneMsg:
		return r, r.flowDone(msg)

	case refreshMsg:
		if r.search != nil {
			r.search.picker.refresh()
		}
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
		// a flow's `new` answers the id it created, as a standalone one does
		answer, _ := json.Marshal(msg.id)
		r.popTo(len(r.pages)-1, answer)
		top, cmd := r.top().Update(msg)
		r.pages[len(r.pages)-1] = top
		return r, cmd
	}

	top, cmd := r.top().Update(msg)
	r.pages[len(r.pages)-1] = top
	return r, cmd
}

// popTo leaves the first n pages, telling every flow whose page left that
// its view call is over; the top one is given answer.
func (r *root) popTo(n int, answer json.RawMessage) {
	for at := len(r.pages) - 1; at >= n; at-- {
		if closed, ok := r.waiting[at]; ok {
			closed <- answer
			delete(r.waiting, at)
		}
		answer = nil
	}
	r.pages = r.pages[:n]
}

// say puts a line in the top page's status line.
func (r *root) say(status string) tea.Cmd {
	top, cmd := r.top().Update(statusMsg(status))
	r.pages[len(r.pages)-1] = top
	return cmd
}

func (r *root) View() tea.View {
	content := r.top().View()
	switch {
	case r.palette:
		content = r.withPalette(content)
	case r.search != nil:
		content = r.withSearch(content)
	}
	view := tea.NewView(content)
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
