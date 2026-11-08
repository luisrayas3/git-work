package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
)

// `esc e` writes a box's text in an external editor (doc/design/views.md,
// "The comment box"): the draft goes to a temporary file, the screen is
// suspended while the editor runs, and what the file holds when it exits
// comes back into the box as the draft, the cursor in the text. Nothing is
// sent or written on the way back: enter still does that, so an editor's
// exit means one thing on every box.

// textTarget is the box a text written in the editor goes back to.
type textTarget int

const (
	// targetComment is show's comment box, and new's description box,
	// which is the one box new has.
	targetComment textTarget = iota
	// targetDescription is show's description, in its editor.
	targetDescription
)

// externalTextMsg is the editor's exit: the text written out, and what the
// file held after, or the error that kept it from being read.
type externalTextMsg struct {
	target       textTarget
	before, text string
	err          error
}

// changed says the editor left a text the box does not already hold. An
// editor that adds the newline a file ends with has changed nothing.
func (m externalTextMsg) changed() bool {
	return trimEnd(m.text) != trimEnd(m.before)
}

// trimEnd is a text as it goes back into a box: the trailing whitespace an
// editor leaves at the end of a file is no part of it, the indent of its
// first line is.
func trimEnd(text string) string {
	return strings.TrimRightFunc(text, unicode.IsSpace)
}

// editorCommand runs the editor git would on a file. The editor is a shell
// command, as git's is, which may carry arguments ("code --wait"), so it is
// run by the shell with the file as its one argument, quoted by the shell
// and not by us.
func editorCommand(editor, path string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
}

// editExternally writes text to a temporary `.md` file, the suffix being
// what tells an editor the text is markdown, and runs the editor on it with
// the screen suspended; the file is read and removed on the editor's exit.
// The editor is git's: GIT_EDITOR, core.editor, VISUAL, EDITOR, then the
// first common one found (gitcli's GetCoreEditor).
func editExternally(repo *cache.RepoCache, target textTarget, text string) (tea.Cmd, error) {
	editor, err := repo.GetCoreEditor()
	if err != nil {
		return nil, fmt.Errorf("no editor: %w", err)
	}
	file, err := os.CreateTemp("", "git-work-*.md")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	_, err = file.WriteString(text)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return tea.ExecProcess(editorCommand(editor, path), func(err error) tea.Msg {
		return readBack(target, text, path, err)
	}), nil
}

// readBack is the editor's exit as the box hears of it; the file goes
// whatever happened.
func readBack(target textTarget, before, path string, ran error) externalTextMsg {
	defer os.Remove(path)
	msg := externalTextMsg{target: target, before: before}
	if ran != nil {
		msg.err = fmt.Errorf("editor: %w", ran)
		return msg
	}
	out, err := os.ReadFile(path)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.text = string(out)
	return msg
}

// keepText puts what the editor left into a box as its draft, unless it left
// nothing new; either way the cursor is in the text after.
func keepText(box *commentBox, msg externalTextMsg) {
	if msg.changed() {
		box.area.SetValue(trimEnd(msg.text))
	}
}
