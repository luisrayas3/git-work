package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The status line names the keys that act under the cursor, on every kind
// and in every state (doc/design/terminal-renderer.md, Navigation and
// editing). These say what it reads where, which is the whole of the
// feature: a key the line promises is a key the page answers.

// hintCase is one cursor position and the line it should draw.
type hintCase struct {
	what string
	// at moves the cursor there, from a page freshly opened.
	at   func(p page) page
	line string
}

// runHints walks the cases, each from a page of its own, and checks both the
// line the page computes and that it reaches the screen.
func runHints(t *testing.T, open func() page, cases []hintCase) {
	t.Helper()

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			p := open()
			if c.at != nil {
				p = c.at(p)
			}
			require.Equal(t, c.line, hintOf(t, p))
			require.Contains(t, plainView(p), c.line)
		})
	}
}

// hintOf is the line the page would draw, whatever kind it is.
func hintOf(t *testing.T, p page) string {
	t.Helper()

	switch page := p.(type) {
	case *listPage:
		return page.hintLine()
	case *boardPage:
		return page.hintLine()
	case *ganttPage:
		return page.hintLine()
	case *matrixPage:
		return page.hintLine()
	case *showPage:
		return page.hintLine()
	}
	t.Fatalf("no hints on %T", p)
	return ""
}

func TestListHintsNameWhatIsUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"type": "story", "title": "the story"})
	other := newIssue(t, repo, map[string]any{"title": "blocked"})
	newIssue(t, repo, map[string]any{
		"title": "the task", "parent": story, "blocks": []any{other}, "rank": "a",
	})

	const fields = `"fields":["title","archived","parent","blocks","children"]`
	onlyTask := `"query":"map(select(.fields.title == \"the task\"))"`

	open := func(kwargs string) func() page {
		return func() page { return list(t, repo, kwargs) }
	}
	right := func(n int) func(page) page {
		return func(p page) page {
			for range n {
				p = send(p, "right")
			}
			return p
		}
	}

	runHints(t, open(`{`+fields+`,`+onlyTask+`}`), []hintCase{
		{what: "the id, with no rank bound", line: "enter: open · ? keys"},
		{what: "a text cell", at: right(1), line: "enter: open · space: edit · ? keys"},
		{what: "a bool cell", at: right(2), line: "enter: open · space: flip · ? keys"},
		{what: "a relation", at: right(3), line: "enter: go to · space: change · ? keys"},
		// the set is issue add/remove, so space rings and is not named
		{what: "a multi-relation", at: right(4), line: "enter: go to · ? keys"},
		// drawn, never written: space rings there too
		{what: "a column that is not a field", at: right(5), line: "enter: open · ? keys"},
		{what: "filtering", at: func(p page) page { return send(p, "/") },
			line: "enter: keep · esc: clear · ? keys"},
		{what: "an editor open", at: func(p page) page { return send(p, "right", "space") },
			line: "enter: write · esc: cancel · ? keys"},
		{what: "a picker open", at: func(p page) page { return send(p, "right", "right", "right", "space") },
			line: "enter: write · esc: cancel · /: narrow · ? keys"},
	})

	runHints(t, open(`{`+fields+`,`+onlyTask+`,"rank":"rank"}`), []hintCase{
		{what: "the id, with a rank bound", line: "enter: open · space: grab · ? keys"},
		{what: "grabbed", at: func(p page) page { return send(p, "space") },
			line: "↑↓: move · space: drop · esc: put back · ? keys"},
	})

	// a nested row with children adds the tree's keys
	runHints(t, open(`{"fields":["title"],"expand":"children","query":"map(select(.fields.type == \"story\"))"}`),
		[]hintCase{
			{what: "a row with children", line: "enter: open · z: fold · tab: into children · ? keys"},
			{what: "folded", at: func(p page) page { return send(p, "z") },
				line: "enter: open · z: unfold · tab: into children · ? keys"},
		})
}

func TestBoardHintsNameWhatIsUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do", "rank": "a"})

	open := func(kwargs string) func() page {
		return func() page { return board(t, repo, kwargs) }
	}
	grab := func(p page) page { return send(p, "space") }

	runHints(t, open(`{"columns":"status"}`), []hintCase{
		// a card has no cell: it is opened, grabbed, and edited on show
		{what: "a card", line: "enter: open · space: grab · ? keys"},
		{what: "grabbed, with no rank bound", at: grab,
			line: "←→: column · space: drop · esc: put back · ? keys"},
		{what: "filtering", at: func(p page) page { return send(p, "/") },
			line: "enter: keep · esc: clear · ? keys"},
	})

	runHints(t, open(`{"columns":"status","rank":"rank"}`), []hintCase{
		{what: "grabbed, with a rank bound", at: grab,
			line: "←→: column · ↑↓: reorder · space: drop · esc: put back · ? keys"},
	})
}

func TestGanttHintsNameWhatIsUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	newIssue(t, repo, map[string]any{
		"title": "dated", "start": "2026-09-07", "stop": "2026-09-13", "rank": "a",
	})

	open := func(kwargs string) func() page {
		return func() page { return gantt(t, repo, kwargs) }
	}
	grab := func(p page) page { return send(p, "space") }

	runHints(t, open(`{"start":"start","stop":"stop","from":"2026-09-07"}`), []hintCase{
		{what: "a bar", line: "enter: open · space: grab bar · ? keys"},
		{what: "grabbed, with no rank bound", at: grab,
			line: "←→: shift · space: drop · esc: put back · ? keys"},
		{what: "filtering", at: func(p page) page { return send(p, "/") },
			line: "enter: keep · esc: clear · ? keys"},
	})

	runHints(t, open(`{"start":"start","stop":"stop","from":"2026-09-07","rank":"rank"}`), []hintCase{
		{what: "grabbed, with a rank bound", at: grab,
			line: "←→: shift · ↑↓: reorder · space: drop · esc: put back · ? keys"},
	})

	// a row with no dates has no bar to shift: dragAlong refuses it, so the
	// grab is named only where a bound rank still gives it something to do
	newIssue(t, repo, map[string]any{"title": "dateless", "rank": "b"})
	// the dateless row, wherever the order put it
	dateless := func(p page) page {
		page := p.(*ganttPage)
		for at, index := range page.order {
			if b := &page.bars[index]; !b.hasStart && !b.hasStop {
				page.cursor = at
			}
		}
		return page
	}
	runHints(t, open(`{"start":"start","stop":"stop","from":"2026-09-07"}`), []hintCase{
		{what: "a dateless row", at: dateless, line: "enter: open · ? keys"},
	})
	runHints(t, open(`{"start":"start","stop":"stop","from":"2026-09-07","rank":"rank"}`), []hintCase{
		{what: "a dateless row, with a rank bound", at: dateless,
			line: "enter: open · space: grab · ? keys"},
		{what: "a dateless row, grabbed", at: func(p page) page { return grab(dateless(p)) },
			line: "↑↓: reorder · space: drop · esc: put back · ? keys"},
	})
}

func TestMatrixHintsNameWhatEnterOpens(t *testing.T) {
	repo := testRepo(t)
	twoByTwo(t, repo)

	open := func() page {
		return matrix(t, repo, `{"rows":"parent","columns":"iteration","value":"estimate","query":"`+tasksOnly+`"}`)
	}
	onCell := func(row, column string) func(page) page {
		return func(p page) page {
			put(t, p.(*matrixPage), row, column)
			return p
		}
	}
	totalsColumn := func(p page) page {
		page := p.(*matrixPage)
		page.col = len(page.cols)
		return page
	}
	grandRow := func(p page) page {
		page := p.(*matrixPage)
		page.row = len(page.drawn) - 1
		return page
	}

	// space rings on a sum, so it is never named; what enter opens is the
	// cell, or the total's row, column or lot
	runHints(t, open, []hintCase{
		{what: "a cell", at: onCell("alpha", "sprint 1"),
			line: "enter: open issues · /: narrow axes · ? keys"},
		{what: "the totals column", at: func(p page) page { return totalsColumn(onCell("alpha", "sprint 1")(p)) },
			line: "enter: open row · /: narrow axes · ? keys"},
		{what: "a totals row", at: func(p page) page { return grandRow(onCell("alpha", "sprint 1")(p)) },
			line: "enter: open column · /: narrow axes · ? keys"},
		{what: "the grand total", at: func(p page) page { return totalsColumn(grandRow(p)) },
			line: "enter: open all · /: narrow axes · ? keys"},
		{what: "filtering", at: func(p page) page { return send(p, "/") },
			line: "enter: keep · esc: clear · ? keys"},
	})
}

func TestShowHintsNameWhatIsUnderTheCursor(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	other := newIssue(t, repo, map[string]any{"title": "blocked"})
	id := newIssue(t, repo, map[string]any{
		"title": "the task", "status": "to-do", "parent": story, "blocks": []any{other},
	})

	open := func(fields ...string) func() page {
		return func() page { return show(t, repo, id, fields) }
	}
	// the page opens on the box; the stops wrap round to the header
	toHeader := func(p page) page { return send(p, "tab", "tab") }
	toFields := func(p page) page { return send(p, "shift+tab") }
	// a child's rows come after the fields, so the cursor is put on the
	// first of them rather than walked there
	toChildren := func(p page) page {
		page := toFields(p).(*showPage)
		for at, row := range page.rows {
			if row.derived {
				page.row = at
				break
			}
		}
		return page
	}

	runHints(t, open("status"), []hintCase{
		{what: "the comment box", line: "space: type · tab: skip · ? keys"},
		{what: "typing in it", at: func(p page) page { return send(p, "space") },
			line: "enter: send · alt+enter: newline · esc: leave · ? keys"},
		{what: "the tabs", at: func(p page) page { return send(p, "tab") },
			line: "←→: tab · ? keys"},
		{what: "the title", at: toHeader, line: "space: edit · ? keys"},
		{what: "the type", at: func(p page) page { return send(toHeader(p), "left") },
			line: "space: edit · ? keys"},
		{what: "archived", at: func(p page) page { return send(toHeader(p), "right") },
			line: "space: flip · ? keys"},
		{what: "a field row", at: toFields, line: "space: edit · ? keys"},
	})

	runHints(t, open("parent", "blocks"), []hintCase{
		{what: "a relation row", at: toFields, line: "enter: go to · space: change · ? keys"},
		{what: "a multi-relation row", at: func(p page) page { return send(toFields(p), "down") },
			line: "enter: go to · ? keys"},
	})

	// an empty relation has nothing to go to: enter rings there
	empty := newIssue(t, repo, map[string]any{"title": "no parent"})
	runHints(t, func() page { return show(t, repo, empty, []string{"parent"}) }, []hintCase{
		{what: "an empty relation row", at: toFields, line: "space: change · ? keys"},
	})

	// a child's row is the other side of a relation: followed, never edited
	children, err := showCall(t, repo, `{"id":"`+story+`","children":[{"type":"task","relation":"parent"}]}`)
	require.NoError(t, err)
	runHints(t, func() page { return children }, []hintCase{
		{what: "a child's row", at: toChildren, line: "enter: follow · ? keys"},
	})

	none, err := showCall(t, repo, `{"id":"`+id+`","children":[{"type":"task","relation":"parent"}]}`)
	require.NoError(t, err)
	runHints(t, func() page { return none }, []hintCase{
		{what: "an empty section", at: toChildren, line: helpHint},
	})
}
