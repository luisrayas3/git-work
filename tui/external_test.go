package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
)

// opensTheEditor sends one key and requires that it answers with the
// editor's command, not the bell; the command is never run, there being no
// terminal to hand over.
func opensTheEditor(t *testing.T, p page, spelling string) page {
	t.Helper()
	p, cmd := p.Update(press(spelling))
	require.NotNil(t, cmd, "the editor's command")
	require.NotEqual(t, bell()(), cmd(), "not the bell")
	return p
}

// TestTheEditorIsGitsShellCommand: the editor is a shell command, as git's
// is, so one with arguments runs, and the file is its one argument, quoted
// by the shell (doc/design/views.md, "The external editor").
func TestTheEditorIsGitsShellCommand(t *testing.T) {
	cmd := editorCommand("code --wait", "/tmp/a b.txt")
	require.Equal(t, []string{"sh", "-c", `code --wait "$1"`, "sh", "/tmp/a b.txt"}, cmd.Args)

	// a real run: an "editor" that rewrites the file
	path := filepath.Join(t.TempDir(), "a draft.txt")
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o600))
	require.NoError(t, editorCommand(`printf 'after\n' >`, path).Run())
	msg := readBack(targetComment, "before", path, nil)
	require.NoError(t, msg.err)
	require.Equal(t, "after\n", msg.text)
	require.True(t, msg.changed())
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "the file is removed")

	// the newline an editor ends a file with is no change
	require.False(t, externalTextMsg{before: "same", text: "same\n"}.changed())

	// an editor that failed is the error, the file removed all the same
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o600))
	msg = readBack(targetComment, "before", path, errors.New("exit status 1"))
	require.ErrorContains(t, msg.err, "editor")
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

// TestEscEWritesTheCommentInTheEditor: on the comment box or in its text,
// esc e opens the draft in the editor; what comes back is the draft, the
// cursor in the text, and enter still sends it.
func TestEscEWritesTheCommentInTheEditor(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page.focusStop(stopBox)
	opensTheEditor(t, page, "alt+e")

	// from inside the text, before the textarea sees the key
	page = send(page, "space", "a", " ", "d", "r", "a", "f", "t").(*showPage)
	opensTheEditor(t, page, "alt+e")
	require.Equal(t, "a draft", page.box.draft(), "the key is not typed")

	// unchanged, the draft is as it was
	page.focusStop(stopBox)
	updated, _ := page.Update(externalTextMsg{target: targetComment, before: "a draft", text: "a draft\n"})
	page = updated.(*showPage)
	require.Equal(t, "a draft", page.box.draft())
	require.True(t, page.inText(), "the cursor in the text")
	require.Equal(t, "unchanged", page.status)

	// the editor failing leaves it too, and says why
	updated, cmd := page.Update(externalTextMsg{target: targetComment, before: "a draft", err: errors.New("editor: exit status 1")})
	page = updated.(*showPage)
	require.Equal(t, bell()(), cmd())
	require.Equal(t, "a draft", page.box.draft())
	require.Contains(t, page.status, "exit status 1")

	// changed, it is the draft; nothing is sent until enter
	before, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	page.focusStop(stopFields)
	updated, _ = page.Update(externalTextMsg{target: targetComment, before: "a draft", text: "from the\neditor\n\n"})
	page = updated.(*showPage)
	require.Equal(t, stopBox, page.current().stop)
	require.True(t, page.inText())
	require.Equal(t, "from the\neditor", page.box.draft())
	after, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Len(t, after.Comments, len(before.Comments), "not saving sends")

	page = send(page, "enter").(*showPage)
	after, err = host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "from the\neditor", after.Comments[len(after.Comments)-1].Message)
}

// TestEscInATextOpensThePalette: in a text, esc opens the palette over it,
// the text focused underneath and the draft as it was; from there e is the
// editor, g goes back to the text, any other letter rings, and a second esc
// leaves the text, the draft kept (doc/design/tui.md, "The palette over a
// text").
func TestEscInATextOpensThePalette(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page.focusStop(stopBox)
	stack := stackOf(page, 100, 30)
	for _, key := range []string{"space", "a", "b"} {
		run(stack, key)
	}
	inBox := func() {
		t.Helper()
		shown := stack.top().(*showPage)
		require.True(t, shown.inText(), "the text focused")
		require.Equal(t, "ab", shown.box.draft(), "the draft as it was")
	}
	inBox()

	// esc opens the palette over the text, which lists the text's commands
	run(stack, "esc")
	require.True(t, stack.palette)
	inBox()
	drawn := drawnStack(stack)
	require.Contains(t, drawn, "this text")
	require.Contains(t, drawn, "e editor")
	require.NotContains(t, drawn, "expand", "no page command")
	require.Contains(t, drawn, "e editor   g close   esc leave text", "the box lists its own esc")
	require.NotContains(t, drawn, "ctrl+c", "no ctrl+c over a text")
	require.NotContains(t, drawn, "? keys", "no hints under the palette")

	// ctrl+c is no text command: it rings, the palette open, nothing copied
	_, cmd := stack.Update(press("ctrl+c"))
	require.Equal(t, bell()(), cmd())
	require.True(t, stack.palette)
	inBox()

	// a letter that is no text command rings, the palette open
	for _, letter := range []string{"x", "k"} {
		_, cmd := stack.Update(press(letter))
		require.Equal(t, bell()(), cmd(), letter)
		require.True(t, stack.palette)
		inBox()
	}

	// g goes back to the text
	require.Nil(t, run(stack, "g"))
	require.False(t, stack.palette)
	inBox()

	// e opens the editor, from the text
	run(stack, "esc")
	_, cmd = stack.Update(press("e"))
	require.NotNil(t, cmd, "the editor's command")
	require.NotEqual(t, bell()(), cmd(), "not the bell")
	require.False(t, stack.palette)
	inBox()

	// alt+e, which esc e typed quickly is, is the editor too
	_, cmd = stack.Update(press("alt+e"))
	require.NotNil(t, cmd)
	require.NotEqual(t, bell()(), cmd())
	inBox()

	// esc esc leaves the text, the draft kept, the page still there
	run(stack, "esc")
	run(stack, "esc")
	require.False(t, stack.palette)
	require.Len(t, stack.pages, 1)
	shown := stack.top().(*showPage)
	require.False(t, shown.inText())
	require.Equal(t, "ab", shown.box.draft())
	require.Equal(t, "draft kept", shown.status)

	// esc esc typed quickly, one key, too
	run(stack, "space")
	inBox()
	run(stack, "alt+esc")
	require.False(t, stack.top().(*showPage).inText())
	require.Len(t, stack.pages, 1)

	// the description's editor: esc esc leaves it, an untouched one gone
	run(stack, "tab")
	run(stack, "space")
	require.True(t, stack.top().(*showPage).editingDesc)
	run(stack, "esc")
	require.True(t, stack.palette)
	require.True(t, stack.top().(*showPage).editingDesc, "the text focused")
	_, cmd = stack.Update(press("e"))
	require.NotEqual(t, bell()(), cmd(), "the editor")
	run(stack, "esc")
	run(stack, "esc")
	require.False(t, stack.top().(*showPage).editingDesc)
	require.Nil(t, stack.top().(*showPage).desc, "nothing kept")
	require.Len(t, stack.pages, 1)

	// new's description box
	draft := newView(t, repo, `{"draft":{"fields":{"type":"task","title":"one"}}}`)
	draft.editor = nil
	draft.focusStop(stopBox)
	stack = stackOf(draft, 100, 30)
	run(stack, "space")
	run(stack, "x")
	run(stack, "esc")
	require.True(t, stack.palette)
	require.True(t, stack.top().(*newPage).inText())
	_, cmd = stack.Update(press("e"))
	require.NotEqual(t, bell()(), cmd(), "the editor")
	run(stack, "esc")
	run(stack, "esc")
	require.False(t, stack.top().(*newPage).inText())
	require.Equal(t, "x", stack.top().(*newPage).body.draft())
	require.Len(t, stack.pages, 1)
}

// TestTextCommandsAreNoTextareaKey: no text command's letter is one of the
// textarea's alt keys, so alt+<letter> and esc <letter> are one key in the
// text too (doc/design/tui.md, "The palette over a text").
func TestTextCommandsAreNoTextareaKey(t *testing.T) {
	bound := map[string]bool{}
	keyMap := reflect.ValueOf(newCommentBox(80).area.KeyMap)
	for i := range keyMap.NumField() {
		if b, ok := keyMap.Field(i).Interface().(key.Binding); ok {
			for _, k := range b.Keys() {
				bound[k] = true
			}
		}
	}
	require.True(t, bound["alt+f"], "the textarea's word keys are read")
	for _, c := range textCommands() {
		require.False(t, bound["alt+"+c.key], "alt+%s is the textarea's", c.key)
	}
}

// TestEscEWritesTheDescriptionInTheEditor: on the description tab, or in its
// editor, esc e opens the editor's draft, or the body the store has; what
// comes back is the editor's draft, and enter writes it.
func TestEscEWritesTheDescriptionInTheEditor(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	page = send(page, "tab", "tab").(*showPage)
	require.Equal(t, tabDescription, page.tab)
	opensTheEditor(t, page, "alt+e")
	require.Nil(t, page.desc, "nothing opened until the editor exits")

	updated, _ := page.Update(externalTextMsg{target: targetDescription, before: "the body", text: "a new body\n"})
	page = updated.(*showPage)
	require.True(t, page.editingDesc, "the cursor in the description's text")
	require.Equal(t, "a new body", page.desc.draft())
	document, err := host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "the body", document.Comments[0].Message, "nothing written yet")

	opensTheEditor(t, page, "alt+e")
	require.Equal(t, "a new body", page.desc.draft(), "the key is not typed")

	page = send(page, "enter").(*showPage)
	document, err = host.IssueGet(repo, id)
	require.NoError(t, err)
	require.Equal(t, "a new body", document.Comments[0].Message)
}

// TestEscEElsewhereRings: on show and on new, esc e anywhere but a text
// rings and says so.
func TestEscEElsewhereRings(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})

	page := show(t, repo, id, nil)
	require.Equal(t, stopFields, page.current().stop)
	rings(t, page, "alt+e")
	require.Equal(t, "no text here", page.status)
	page = send(page, "tab", "tab", "right").(*showPage)
	require.Equal(t, tabComments, page.tab)
	rings(t, page, "alt+e", "the comments tab has no text to write")

	draft := newView(t, repo, `{"draft":{"fields":{"type":"task","title":"one"}}}`)
	draft.editor = nil
	draft.focusStop(stopFields)
	rings(t, draft, "alt+e")
}

// TestEscEWritesNewsDescription: new's description box takes esc e as
// show's comment box does; what comes back is the draft's body, and nothing
// is created.
func TestEscEWritesNewsDescription(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)

	page := newView(t, repo, `{"draft":{"fields":{"type":"task","title":"one"}}}`)
	page.editor = nil
	page.focusStop(stopBox)
	opensTheEditor(t, page, "alt+e")
	page = send(page, "space", "x").(*newPage)
	opensTheEditor(t, page, "alt+e")
	require.Equal(t, "x", page.body.draft())

	page.focusStop(stopFields)
	updated, _ := page.Update(externalTextMsg{target: targetComment, before: "x", text: "the body\n"})
	page = updated.(*newPage)
	require.True(t, page.inText())
	require.Equal(t, "the body", page.body.draft())

	// enter accepts it, as it accepts what is typed
	page = send(page, "enter").(*newPage)
	require.False(t, page.inText())
	require.Equal(t, "the body", page.body.draft())
}
