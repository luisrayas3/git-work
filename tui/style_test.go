package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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

// requireLink checks a link drawn on a line: every column of its id, the
// space after it and its title faint and underlined (under the cursor
// reversed and underlined, nothing faint), and the columns either side of
// it not underlined, so the link ends where its text does.
//
// It reads the line a column at a time (cells), so that a space drawn in a
// style of its own (lipgloss styles each space of an underlined run apart,
// which underlined avoids) is checked as closely as a letter.
func requireLink(t *testing.T, line, human, title string, cursor bool) {
	t.Helper()
	text := human
	if title != "" {
		text += " " + title
	}
	start, end := requireRun(t, line, text, func(attrs map[int]bool) bool {
		return attrs[4] && attrs[2] == !cursor && attrs[7] == cursor
	}, "the link is underlined, faint but never under the cursor, reversed under the cursor alone")
	requireEdges(t, line, start, end)
}

// requireOwnTitle checks an issue's own title drawn on a line: every
// column underlined, neither bold nor faint, and reversed under the cursor
// alone.
func requireOwnTitle(t *testing.T, line, title string, cursor bool) {
	t.Helper()
	start, end := requireRun(t, line, title, func(attrs map[int]bool) bool {
		return attrs[4] && !attrs[1] && !attrs[2] && attrs[7] == cursor
	}, "the title is underlined, in normal weight, not faint, reversed under the cursor alone")
	requireEdges(t, line, start, end)
}

// requireRun finds text on a line and checks every column of it, saying
// which columns it spans.
func requireRun(t *testing.T, line, text string, ok func(map[int]bool) bool, what string) (start, end int) {
	t.Helper()
	runes, attrs := cells(line)
	plain := string(runes)
	at := strings.Index(plain, text)
	require.GreaterOrEqual(t, at, 0, "%q is drawn: %q", text, line)
	start = len([]rune(plain[:at]))
	end = start + len([]rune(text))
	for c := start; c < end; c++ {
		require.True(t, ok(attrs[c]), "%s, column %d %v: %q", what, c, attrs[c], line)
	}
	return start, end
}

// requireEdges checks that the columns either side of [start, end) on a
// line, where there are any, are not underlined.
func requireEdges(t *testing.T, line string, start, end int) {
	t.Helper()
	_, attrs := cells(line)
	if start > 0 {
		require.False(t, attrs[start-1][4], "the column before is not underlined: %q", line)
	}
	if end < len(attrs) {
		require.False(t, attrs[end][4], "the column after is not underlined: %q", line)
	}
}

// cells reads a drawn line as runes, each with the SGR attributes it is
// drawn in (sgrBefore's reading of every escape since the last reset).
func cells(line string) ([]rune, []map[int]bool) {
	var text []rune
	var attrs []map[int]bool
	on := map[int]bool{}
	escape := regexp.MustCompile(`^\x1b\[([0-9;]*)m`)
	for line != "" {
		if m := escape.FindStringSubmatch(line); m != nil {
			if m[1] == "" || m[1] == "0" {
				on = map[int]bool{}
			} else {
				for n := range sgrBefore(m[0], "") {
					on[n] = true
				}
			}
			line = line[len(m[0]):]
			continue
		}
		r := []rune(line)[0]
		text = append(text, r)
		copied := map[int]bool{}
		for n := range on {
			copied[n] = true
		}
		attrs = append(attrs, copied)
		line = line[len(string(r)):]
	}
	return text, attrs
}

func TestTitlesAreUnderlinedAndLinksAreOneStyle(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story, "status": "to-do",
		"start": "2026-09-07", "stop": "2026-09-13"})
	human := humanOf(repo, story)

	t.Run("list", func(t *testing.T) {
		page := list(t, repo, `{"fields":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
		line := rawLine(t, page.View(), "the task")
		requireOwnTitle(t, line, "the task", false)
		requireLink(t, line, human, "the story", false)

		// under the column cursor the title is reversed and still
		// underlined, and so is the link, nothing faint
		send(page, "l")
		requireOwnTitle(t, rawLine(t, page.View(), "the task"), "the task", true)
		send(page, "l")
		line = rawLine(t, page.View(), "the task")
		requireLink(t, line, human, "the story", true)
		requireOwnTitle(t, line, "the task", false)
	})
	t.Run("board", func(t *testing.T) {
		page := board(t, repo, `{"columns":"status","card":["title","parent"],"query":"map(select(.fields.type == \"task\"))"}`)
		drawn := page.View()
		requireOwnTitle(t, rawLine(t, drawn, "the task"), "the task", false)
		requireLink(t, rawLine(t, drawn, "parent:"), human, "the story", false)
		require.Contains(t, ansiPattern.ReplaceAllString(drawn, ""), "parent: "+human+" the")
	})
	t.Run("show", func(t *testing.T) {
		// the cursor opens on the first row of the fields
		page := show(t, repo, task, []string{"status", "parent"})
		drawn := page.View()
		requireLink(t, rawLine(t, drawn, "parent"), human, "the story", false)
		// the page's heading is bold and not underlined: it is the
		// page's title, not a row's
		heading := sgrBefore(rawLine(t, drawn, "the task"), "the task")
		require.True(t, heading[1] && !heading[4], "the heading: %v", heading)
		page = show(t, repo, task, []string{"parent", "status"})
		requireLink(t, rawLine(t, page.View(), "parent"), human, "the story", true)
	})
	t.Run("gantt", func(t *testing.T) {
		page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"map(select(.fields.type == \"task\"))"}`)
		requireOwnTitle(t, rawLine(t, page.View(), "the task"), "the task", false)

		page = gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","label":"parent","query":"map(select(.fields.type == \"task\"))"}`)
		requireLink(t, rawLine(t, page.View(), "the story"), human, "the story", false)
	})
	t.Run("matrix", func(t *testing.T) {
		page := matrix(t, repo, `{"rows":"parent","columns":"status","query":"map(select(.fields.type == \"task\"))"}`)
		requireLink(t, rawLine(t, page.View(), "the story"), human, "the story", false)
	})
	t.Run("an id that does not resolve", func(t *testing.T) {
		missing := strings.Repeat("ab", 32)
		drawn := linkStyled(repo, []string{missing, story}, lipgloss.NewStyle(), 0)
		require.Equal(t, missing[:idWidth]+", "+human+" the story", ansiPattern.ReplaceAllString(drawn, ""))
		// the comma is drawn plain, outside any style, and either side of
		// it is a link of its own, the unresolved one styled the same
		require.Contains(t, drawn, "\x1b[m, \x1b[", "%q", drawn)
		requireLink(t, drawn, missing[:idWidth], "", false)
		requireLink(t, drawn, human, "the story", false)
		_, attrs := cells(drawn)
		sep := len([]rune(missing[:idWidth]))
		for c := sep; c < sep+2; c++ {
			require.Empty(t, attrs[c], "the separator is plain: %q", drawn)
		}
	})
	t.Run("cut to a width", func(t *testing.T) {
		full := linkText(repo, []string{story, story})
		for width := 1; width <= len(full)+1; width++ {
			drawn := linkStyled(repo, []string{story, story}, lipgloss.NewStyle(), width)
			require.Equal(t, truncate(full, width), ansiPattern.ReplaceAllString(drawn, ""), "width %d", width)
			// every column drawn is the link's, faint and underlined, the
			// ellipsis included, but for the comma and the space after
			// it, and an ellipsis cutting into them, which are plain
			sep := len([]rune(linkLabel(repo, story)))
			text, attrs := cells(drawn)
			for c := range text {
				inSep := c >= sep && c < sep+2
				require.Equal(t, !inSep, attrs[c][2] && attrs[c][4], "width %d column %d: %q", width, c, drawn)
				require.Equal(t, inSep, len(attrs[c]) == 0, "width %d column %d: %q", width, c, drawn)
			}
		}
	})
}
