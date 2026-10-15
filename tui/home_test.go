package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/view"
)

// homeFlows stores the flows these tests run: one that draws, one that
// prints, one that does neither, and one that fails.
func homeFlows(t *testing.T, repo *cache.RepoCache) {
	t.Helper()
	scripts := []string{
		`def overview():
    """The shape of the work.

    Every issue, last edited first.
    """
    work.view.list(fields=["title"])
`,
		`def report():
    """What changed."""
    print("# report")
    print("nothing changed")
`,
		`def quiet():
    """Does nothing."""
    pass
`,
		`def broken():
    """Fails."""
    fail("it broke")
`,
	}
	sources := make([]host.FlowSource, 0, len(scripts))
	for _, script := range scripts {
		sources = append(sources, host.FlowSource{Origin: "test", Script: script})
	}
	_, _, err := host.FlowImport(repo, sources, false, false)
	require.NoError(t, err)
}

// home builds the home page over the flows named.
func home(t *testing.T, repo *cache.RepoCache, flows ...string) *homePage {
	t.Helper()
	names, err := json.Marshal(flows)
	require.NoError(t, err)
	call, err := view.Parse(view.KindHome, map[string]json.RawMessage{"flows": names})
	require.NoError(t, err)
	p, err := newHomePage(repo, call)
	require.NoError(t, err)
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return p
}

// TestHomeDrawsTheFlowsItNames: in the call's order, each with its
// docstring's first line, and enter names the flow rather than running it.
func TestHomeDrawsTheFlowsItNames(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)

	p := home(t, repo, "report", "overview")
	drawn := plainView(p)
	require.True(t, strings.HasPrefix(drawn, `home  flows=["report","overview"]`), drawn)
	require.Less(t, strings.Index(drawn, "report"), strings.Index(drawn, "overview  The shape of the work."))
	require.NotContains(t, drawn, "Every issue", "the first line only")
	require.NotContains(t, drawn, "quiet", "only the flows named")

	_, cmd := p.Update(press("down"))
	require.Nil(t, cmd)
	_, cmd = p.Update(press("enter"))
	require.Equal(t, runFlowMsg{name: "overview"}, cmd())
}

// TestHomeRefusesAFlowThatIsNotStored: at the call, naming the flows there
// are, as an unknown field is refused naming the fields.
func TestHomeRefusesAFlowThatIsNotStored(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)
	ctx := context.Background()

	_, err := host.View(ctx, repo, nil, view.KindHome, map[string]json.RawMessage{"flows": json.RawMessage(`["overview","nope"]`)})
	require.EqualError(t, err, "view home: no flow named nope, the flows are broken, overview, quiet, report")

	_, err = host.View(ctx, repo, nil, view.KindHome, map[string]json.RawMessage{"flows": json.RawMessage(`[]`)})
	require.ErrorContains(t, err, "flows is empty")

	_, err = host.View(ctx, repo, nil, view.KindHome, nil)
	require.ErrorContains(t, err, "view home needs flows")

	_, err = host.View(ctx, repo, nil, view.KindHome, map[string]json.RawMessage{"flows": json.RawMessage(`["overview"]`)})
	require.ErrorIs(t, err, view.ErrNoTerminal, "a good call, and nowhere to draw it")
}

// TestTypingNarrowsTheHome: by name or by description; esc drops the text
// first, and is back after that.
func TestTypingNarrowsTheHome(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)

	p := home(t, repo, "overview", "report", "quiet")
	send(p, "c", "h", "a", "n")
	drawn := plainView(p)
	require.Contains(t, drawn, "report  What changed.")
	require.NotContains(t, drawn, "overview ")
	require.Contains(t, drawn, "/chan")
	require.False(t, atRest(p), "esc is the text's")

	send(p, "backspace", "backspace", "backspace", "backspace", "s", "h", "a", "p", "e")
	_, cmd := p.Update(press("enter"))
	require.Equal(t, runFlowMsg{name: "overview"}, cmd())

	send(p, "esc")
	require.Equal(t, "", p.picker.text())
	require.True(t, atRest(p))
	_, cmd = p.Update(press("esc"))
	require.Equal(t, popMsg{}, cmd())
}

// TestThePaletteRunsFlows: h and r are everywhere's, after search and new;
// r is the search's picker over every stored flow, and h on a stack with no
// home page runs the home flow.
func TestThePaletteRunsFlows(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)
	newIssue(t, repo, map[string]any{"title": "one"})

	stack := stackOf(list(t, repo, ""), 100, 30)
	var ran []string
	stack.run = func(name string) tea.Cmd {
		ran = append(ran, name)
		return nil
	}

	run(stack, "esc")
	require.Regexp(t, `s search.*n new.*h home.*r run a flow.*\? help.*q quit`, drawnStack(stack), "A1's order")

	run(stack, "r")
	require.NotNil(t, stack.search, "the picker is open over the page")
	require.Len(t, stack.search.picker.items, 4, "every stored flow")
	drawn := drawnStack(stack)
	require.Contains(t, drawn, "run a flow\n")
	require.Contains(t, drawn, "What changed.")
	require.Len(t, stack.pages, 1)

	// typed into at once, and its esc its own: the text, then the picker
	run(stack, "q")
	require.Equal(t, "q", stack.search.picker.text())
	run(stack, "esc")
	require.Equal(t, "", stack.search.picker.text())
	run(stack, "esc")
	require.Nil(t, stack.search, "the second esc cancels")
	require.Len(t, stack.pages, 1)

	// the flow chosen runs over the page
	run(stack, "alt+r")
	run(stack, "r")
	run(stack, "e")
	run(stack, "p")
	run(stack, "enter")
	require.Equal(t, []string{"report"}, ran)
	require.Nil(t, stack.search)
	require.IsType(t, &listPage{}, stack.top())

	// no home page on the stack: h runs the home flow
	run(stack, "alt+h")
	require.Equal(t, []string{"report", "home"}, ran)

	// with one, h is back down to it
	stack = stackOf(home(t, repo, "overview"), 100, 20)
	stack.Update(pushMsg{page: list(t, repo, "")})
	stack.Update(pushMsg{page: list(t, repo, "")})
	require.Len(t, stack.pages, 3)
	run(stack, "alt+h")
	require.Len(t, stack.pages, 1)
	require.IsType(t, &homePage{}, stack.top())
}

// TestAFlowsOutputIsAPage: a flow that prints is read on a page; one that
// neither prints nor draws says so; one that fails says why.
func TestAFlowsOutputIsAPage(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)

	stack := stackOf(home(t, repo, "report"), 100, 20)
	stack.Update(flowDoneMsg{name: "report", output: "# report\nnothing changed\n"})
	require.Len(t, stack.pages, 2)
	drawn := drawnStack(stack)
	require.True(t, strings.HasPrefix(drawn, "flow run  report"), drawn)
	require.Contains(t, drawn, "nothing changed")
	require.Equal(t, "git work flow run report", commandOf(stack.top()))
	run(stack, "esc")
	require.Contains(t, drawnStack(stack), "home › flow run report")
	run(stack, "esc")
	require.Len(t, stack.pages, 1)

	stack.Update(flowDoneMsg{name: "quiet"})
	require.Contains(t, drawnStack(stack), "quiet ran, and drew and printed nothing")

	stack.Update(flowDoneMsg{name: "x", err: errors.New("no flow named x")})
	require.Len(t, stack.pages, 1)
	require.Contains(t, drawnStack(stack), "no flow named x")
}

// probe asks the running program what its stack is, on the program's own
// goroutine, so that the test reads it without a race.
type probe struct{ reply chan []string }

// TestEnterRunsTheFlowOnTheSameStack: the flow runs on the renderer's
// worker, its view call pushes onto the stack the program is drawing and
// returns when that page leaves it; a flow that prints opens its output.
func TestEnterRunsTheFlowOnTheSameStack(t *testing.T) {
	repo := testRepo(t)
	homeFlows(t, repo)
	newIssue(t, repo, map[string]any{"title": "one"})

	stack := &root{pages: []page{home(t, repo, "overview", "report", "broken")}, width: 100, height: 20}
	program := tea.NewProgram(stack,
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
		tea.WithWindowSize(100, 20),
		tea.WithFilter(func(m tea.Model, msg tea.Msg) tea.Msg {
			if p, ok := msg.(probe); ok {
				var kinds []string
				for _, each := range m.(*root).pages {
					call, lead, _ := each.Call()
					kinds = append(kinds, strings.TrimSpace(call.Kind+" "+lead))
				}
				p.reply <- kinds
				return nil
			}
			return msg
		}),
	)
	renderer := &Renderer{}
	done := make(chan error, 1)
	go func() {
		_, err := renderer.drive(context.Background(), repo, stack, program)
		done <- err
	}()

	pages := func() []string {
		reply := make(chan []string, 1)
		program.Send(probe{reply: reply})
		return <-reply
	}
	until := func(want ...string) {
		t.Helper()
		require.Eventually(t, func() bool {
			return strings.Join(pages(), " › ") == strings.Join(want, " › ")
		}, 5*time.Second, 5*time.Millisecond, "the stack is %v", want)
	}

	program.Send(press("enter"))
	until("home", "list")
	program.Send(popMsg{})
	until("home")

	program.Send(press("down"))
	program.Send(press("enter"))
	until("home", "flow run report")
	program.Send(popMsg{})
	until("home")

	// a failure carries its backtrace, so it is a page too
	program.Send(press("down"))
	program.Send(press("enter"))
	until("home", "flow run broken")
	program.Send(popMsg{})

	// quitting under a flow blocked in its view ends the flow, and the
	// renderer
	program.Send(press("home"))
	program.Send(press("enter"))
	until("home", "list")
	program.Quit()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the renderer did not end with its program")
	}
}
