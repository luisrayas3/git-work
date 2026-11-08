package tui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

// yellowBold is the escape a bold yellow would draw, which no grab draws.
var yellowBold = regexp.MustCompile(`\x1b\[(\d+;)*(33|93|38;5;(3|11))(;\d+)*m`)

// TestGrabStyleBlinksItsOwnColour: a colour swaps with its bright or normal
// pair, both ways, and no colour swaps plain for bold
// (doc/design/tui.md, "A grab blinks its own colour").
func TestGrabStyleBlinksItsOwnColour(t *testing.T) {
	pairs := map[string]string{"1": "9", "2": "10", "3": "11", "4": "12", "5": "13", "6": "14"}
	for normal, bright := range pairs {
		require.Equal(t, lipgloss.Color(bright), brightPair(lipgloss.Color(normal)))
		require.Equal(t, lipgloss.Color(normal), brightPair(lipgloss.Color(bright)))

		on, off := grabStyle(lipgloss.Color(normal), true), grabStyle(lipgloss.Color(normal), false)
		require.Equal(t, lipgloss.Color(normal), on.GetForeground())
		require.Equal(t, lipgloss.Color(bright), off.GetForeground())
		require.False(t, on.GetBold() || off.GetBold())
		require.Equal(t, lipgloss.Color(normal), grabStyle(lipgloss.Color(bright), false).GetForeground())
	}

	on, off := grabStyle(nil, true), grabStyle(nil, false)
	require.True(t, isNoColor(on.GetForeground()) && isNoColor(off.GetForeground()), "no colour of its own, none drawn")
	require.False(t, on.GetBold())
	require.True(t, off.GetBold())
}

// TestGanttGrabBlinksTheBarsColour: a grabbed bar in a colored group keeps
// that colour and blinks its bright pair; an ungrouped one blinks bold.
func TestGanttGrabBlinksTheBarsColour(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	switchColorOn(t, repo, "story")
	head := newTyped(t, repo, "story", map[string]any{"title": "head", "color": "red"})
	id := newTyped(t, repo, "task", map[string]any{"title": "bar", "start": "2026-09-07", "stop": "2026-09-20", "parent": head})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"parent","query":"map(select(.fields.type == \"task\"))"}`)
	barLine := func() string {
		for _, l := range strings.Split(page.View(), "\n") {
			if strings.Contains(ansiPattern.ReplaceAllString(l, ""), id[:idWidth]) {
				return l
			}
		}
		return ""
	}
	send(page, "right") // the cursor off the first cell, so that it draws in the bar's colour
	require.Regexp(t, `\x1b\[(\d+;)*31(;\d+)*m▓`, barLine(), "the group's red")

	send(page, "space")
	require.GreaterOrEqual(t, page.grabbed, 0)
	require.True(t, page.blink)
	require.Regexp(t, `\x1b\[(\d+;)*31(;\d+)*m▓`, barLine(), "its own colour on the blink")
	page.Update(blinkMsg{})
	require.False(t, page.blink)
	require.Regexp(t, `\x1b\[(\d+;)*91(;\d+)*m▓`, barLine(), "its bright pair off it")
	require.NotRegexp(t, yellowBold, barLine())
	send(page, "esc")

	// ungrouped and uncolored: plain, then bold
	page = gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","query":"map(select(.fields.type == \"task\"))"}`)
	send(page, "right", "space")
	require.NotRegexp(t, `\x1b\[(\d+;)*1(;\d+)*m▓`, barLine(), "plain on the blink")
	page.Update(blinkMsg{})
	require.Regexp(t, `\x1b\[(\d+;)*1(;\d+)*m[^\x1b]*▓`, barLine(), "bold off it")
	require.NotRegexp(t, yellowBold, barLine())
}

// TestListAndBoardGrabsAreNotYellow: a list row and a board card have no
// colour, so their grab markers blink plain and bold, never yellow.
func TestListAndBoardGrabsAreNotYellow(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "one", "status": "to-do"})

	l := list(t, repo, "")
	send(l, "space")
	require.GreaterOrEqual(t, l.grabbed, 0)
	for range 2 {
		require.NotRegexp(t, yellowBold, l.View())
		l.Update(blinkMsg{})
	}

	b := board(t, repo, `{"columns":"status"}`)
	send(b, "space")
	for range 2 {
		require.NotRegexp(t, yellowBold, b.View())
		b.Update(blinkMsg{})
	}
}

// TestShowSideGrabBlinks: a side table's grabbed row blinks too, its tick
// running while the grab is held and stopping after the drop.
func TestShowSideGrabBlinks(t *testing.T) {
	repo := testRepo(t)
	story, _, _, _ := storyWithTasks(t, repo)
	page, err := specCall(t, repo, story, `{"expand":{"relation":"children","fields":["status"]}}`)
	require.NoError(t, err)
	send(page, "tab")

	_, cmd := page.Update(press("space"))
	require.NotNil(t, page.grab)
	require.NotNil(t, cmd, "the grab starts the blink")
	require.True(t, page.grab.blink)
	require.NotRegexp(t, yellowBold, page.View())
	_, cmd = page.Update(blinkMsg{})
	require.NotNil(t, cmd, "and keeps it running")
	require.False(t, page.grab.blink)
	require.Regexp(t, `≡\x1b\[1m`, page.View(), "bold off the blink")

	send(page, "esc")
	_, cmd = page.Update(blinkMsg{})
	require.Nil(t, cmd, "no grab, no tick")

}
