package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// commentBox is where a comment is written, on `show`, under the title.
//
// The box is typed in only once space has put the cursor in it; then enter
// sends, as every chat box does, and a newline is alt+enter, or shift+enter
// where the terminal tells it apart (keys.newline; 2026-10-02). The box holds
// the text and nothing else; which keys enter, leave and send it are the
// page's.
type commentBox struct {
	area textarea.Model
	// bare draws the box without its gutter: the palette's query editor,
	// which is typed in from the moment it opens and is never on without
	// being in
	bare bool
}

// The box opens two lines tall, which is a sentence and the start of a
// second, and grows with what is typed up to a paragraph; past that it
// scrolls, so the fields under it stay on the screen.
const (
	commentMinHeight = 2
	commentMaxHeight = 8
)

func newCommentBox(width int) *commentBox {
	area := textarea.New()
	area.Placeholder = "add a comment"
	area.ShowLineNumbers = false
	area.DynamicHeight = true
	area.MinHeight = commentMinHeight
	area.MaxHeight = commentMaxHeight
	area.SetHeight(commentMinHeight)
	area.SetWidth(max(width-commentGutter-1, 20))
	area.KeyMap.InsertNewline = keys.newline.binding
	c := &commentBox{area: area}
	c.restyle()
	return c
}

// restyle takes the textarea's styles for the terminal's background, less
// the cursor line's own background: on the wrong kind of terminal that shade
// is the colour of the text, and what is typed disappears into it.
func (c *commentBox) restyle() {
	styles := textarea.DefaultStyles(darkBackground)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Text = lipgloss.NewStyle()
	styles.Focused.EndOfBuffer = lipgloss.NewStyle()
	styles.Blurred.CursorLine = styles.Blurred.Text
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle()
	c.area.SetStyles(styles)
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
	c.area.SetWidth(max(width-commentGutter-1, 20))
}

// commentGutter is the column left of the box's text, where `›` marks the
// box while the cursor is on it.
const commentGutter = 1

// View is the box's lines. On says the cursor is on the box as a whole, not
// in its text: then every line has the light wash a row under the cursor
// has, and the first is marked `›` in the gutter, as a row is, so that the
// box reads as one cell; in the text it is the textarea's own, with its
// cursor, and no wash. The gutter is there in every state, so the text keeps
// its column whether the cursor is elsewhere, on the box or in it; one mark,
// not one a line, because a margin of markers reads as a list, not as one
// place (doc/design/views.md, "The comment box").
func (c *commentBox) View(width int, on bool) []string {
	lines := make([]string, 0, commentMaxHeight)
	washed := on && !c.area.Focused()
	for at, line := range strings.Split(c.area.View(), "\n") {
		switch {
		case c.bare:
		case washed:
			wash := styleRow()
			if c.area.Value() == "" {
				wash = wash.Faint(true)
			}
			mark := " "
			if at == 0 {
				mark = "›"
			}
			line = styleRow().Render(mark) + wash.Render(pad(ansi.Strip(line), max(width-commentGutter, 0)))
		default:
			line = strings.Repeat(" ", commentGutter) + line
		}
		lines = append(lines, fit(line, width))
	}
	return lines
}
