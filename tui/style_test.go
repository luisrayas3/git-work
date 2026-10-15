package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sgrBefore is the set of SGR attributes the escape right before text
// switches on, its colour arguments skipped so that a 236 is not read as a
// 2; nil when text is not right after an escape on the line.
func sgrBefore(line, text string) map[int]bool {
	match := regexp.MustCompile(`\x1b\[([0-9;]*)m` + regexp.QuoteMeta(text)).FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	params := strings.Split(match[1], ";")
	on := map[int]bool{}
	for at := 0; at < len(params); at++ {
		n, _ := strconv.Atoi(params[at])
		switch n {
		case 38, 48, 58:
			// an extended colour: 5;n or 2;r;g;b
			if at+1 < len(params) && params[at+1] == "5" {
				at += 2
			} else {
				at += 4
			}
			continue
		}
		on[n] = true
	}
	return on
}

// rawLine is the drawn line whose plain text holds needle, styling kept.
func rawLine(t *testing.T, drawn, needle string) string {
	t.Helper()
	for _, line := range strings.Split(drawn, "\n") {
		if strings.Contains(ansiPattern.ReplaceAllString(line, ""), needle) {
			return line
		}
	}
	require.Failf(t, "no line holds it", "%q", needle)
	return ""
}

// requireHeader checks one group header: the label, a space and a rule to
// the window's edge, the rule faint and outside the label's bold.
func requireHeader(t *testing.T, drawn, label string, width int) {
	t.Helper()
	plain := strings.Split(ansiPattern.ReplaceAllString(drawn, ""), "\n")
	require.Contains(t, plain, groupRule(label, width), "the header is the label and a rule to the edge")
	raw := rawLine(t, drawn, groupRule(label, width))
	rule := sgrBefore(raw, " ─")
	require.True(t, rule[2], "the rule is faint: %q", raw)
	require.False(t, rule[1], "and not bold, as the label is: %q", raw)
}

func TestEveryGroupHeaderCarriesARule(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "start": "2026-09-07", "stop": "2026-09-13"})
	newTyped(t, repo, "task", map[string]any{"title": "a", "parent": story, "status": "to-do", "priority": "high",
		"start": "2026-09-07", "stop": "2026-09-13", "estimate": 1})

	t.Run("list", func(t *testing.T) {
		page := list(t, repo, `{"fields":["title"],"group_by":"type","expand":{"relation":"children","group_by":"status"},`+
			`"query":"map(select(.fields.type == \"story\"))"}`)
		send(page, "shift+z")
		drawn := page.View()
		requireHeader(t, drawn, "story", page.width)
		// a nested group's header is at its table's indent, the rule
		// running from after its label to the window's edge
		requireHeader(t, drawn, indent(1)+"to-do", page.width)
	})
	t.Run("board", func(t *testing.T) {
		page := board(t, repo, `{"columns":"status","group_by":"priority"}`)
		requireHeader(t, page.View(), "high", page.width)
	})
	t.Run("gantt", func(t *testing.T) {
		page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"type"}`)
		requireHeader(t, page.View(), "task", page.width)
	})
	t.Run("matrix", func(t *testing.T) {
		page := matrix(t, repo, `{"rows":"parent","columns":"status","value":"estimate","group_by":"priority","query":`+
			`"map(select(.fields.type == \"task\"))"}`)
		requireHeader(t, page.View(), "high", page.width)
	})
}

func TestTitlesAreBoldAndLinksFaint(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story, "start": "2026-09-07", "stop": "2026-09-13"})
	link := humanOf(repo, story) + " the story"

	t.Run("list", func(t *testing.T) {
		page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
		line := rawLine(t, page.View(), "the task")
		require.True(t, sgrBefore(line, "the task")[1], "the title is bold: %q", line)
		attrs := sgrBefore(line, link)
		require.True(t, attrs[2], "the link is faint: %q", line)
		require.False(t, attrs[4], "and not underlined: %q", line)

		// under the column cursor the title is reversed and still bold,
		// and the link is reversed and not faint
		send(page, "l")
		line = rawLine(t, page.View(), "the task")
		attrs = sgrBefore(line, "the task")
		require.True(t, attrs[1] && attrs[7], "the title cell: %q", line)
		send(page, "l")
		line = rawLine(t, page.View(), "the task")
		attrs = sgrBefore(line, link)
		require.True(t, attrs[7], "the link cell is reversed: %q", line)
		require.False(t, attrs[2] || attrs[4], "and neither faint nor underlined: %q", line)
	})
	t.Run("board", func(t *testing.T) {
		page := board(t, repo, `{"columns":"status","card":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
		drawn := page.View()
		require.True(t, sgrBefore(rawLine(t, drawn, "the task"), "the task")[1], "the title is bold")
		line := rawLine(t, drawn, "parent:")
		attrs := sgrBefore(line, truncate(link, page.colWidth-len(" parent: ")))
		require.True(t, attrs[2], "the link is faint: %q", line)
		require.False(t, attrs[4], "and not underlined: %q", line)
	})
	t.Run("gantt", func(t *testing.T) {
		page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"map(select(.fields.type == \"task\"))"}`)
		line := rawLine(t, page.View(), "the task")
		require.True(t, sgrBefore(line, "the task")[1], "the title is bold: %q", line)

		page = gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","label":"parent","query":"map(select(.fields.type == \"task\"))"}`)
		line = rawLine(t, page.View(), "the story")
		require.False(t, sgrBefore(line, link)[1], "another label is not: %q", line)
	})
}
