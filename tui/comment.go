package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// commentBox is where a comment is written, on `show`, under the title.
//
// Enter is a newline here and not a submit: a comment is prose, and the one
// thing worse than a second key to send it is sending half of it. The box
// holds the text and nothing else; which keys leave it and which send it are
// the page's, because the page owns the buttons they press.
type commentBox struct {
	area textarea.Model
}

// commentHeight is the box's lines: enough to see a sentence being written,
// few enough that the fields under it stay on the screen.
const commentHeight = 3

func newCommentBox(width int) *commentBox {
	area := textarea.New()
	area.Placeholder = "add a comment"
	area.ShowLineNumbers = false
	area.SetHeight(commentHeight)
	area.SetWidth(max(width-2, 20))
	area.Focus()
	return &commentBox{area: area}
}

func (c *commentBox) Update(msg tea.Msg) tea.Cmd {
	updated, cmd := c.area.Update(msg)
	c.area = updated
	return cmd
}

// draft is what has been typed, as it would be sent.
func (c *commentBox) draft() string {
	return strings.TrimSpace(c.area.Value())
}

func (c *commentBox) focus(on bool) {
	if on {
		c.area.Focus()
	} else {
		c.area.Blur()
	}
}

func (c *commentBox) resize(width int) {
	c.area.SetWidth(max(width-2, 20))
}

func (c *commentBox) View(width int) []string {
	lines := make([]string, 0, commentHeight)
	for _, line := range strings.Split(c.area.View(), "\n") {
		lines = append(lines, fit(line, width))
	}
	return lines
}
