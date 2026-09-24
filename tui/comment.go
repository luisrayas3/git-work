package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// commentBox writes a comment on one issue.
//
// Enter is a newline here and not a submit: a comment is prose, and the one
// thing worse than a second key to send it is sending half of it.
type commentBox struct {
	issueId string
	area    textarea.Model
}

func newCommentBox(issueId string, width, height int) *commentBox {
	area := textarea.New()
	area.SetWidth(max(width-2, 20))
	area.SetHeight(max(height, 3))
	area.Focus()
	return &commentBox{issueId: issueId, area: area}
}

// Update returns the body once the user is done, empty when they gave up.
func (c *commentBox) Update(msg tea.Msg) (done bool, body string, cmd tea.Cmd) {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, keys.cancel):
			return true, "", nil
		case key.Matches(press, keys.submit):
			return true, strings.TrimSpace(c.area.Value()), nil
		}
	}

	updated, cmd := c.area.Update(msg)
	c.area = updated
	return false, "", cmd
}

func (c *commentBox) View(width int) []string {
	lines := []string{styleHeader.Render("comment on " + c.issueId[:7])}
	for _, line := range strings.Split(c.area.View(), "\n") {
		lines = append(lines, fit(line, width))
	}
	return append(lines, styleDim.Render("ctrl+s or ctrl+d sends it, esc cancels"))
}
