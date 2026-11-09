package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/gitcli"
	"github.com/git-bug/git-bug/host"
)

// `esc e` writes a box's text in an external editor (doc/design/views.md,
// "The external editor"): the draft goes to a temporary file, the screen is
// suspended while the editor runs, and what the file holds when it exits
// comes back into the box as the draft, the cursor in the text. Nothing is
// sent or written on the way back: enter does that, so an editor's exit
// means one thing on every box. The description's file follows the tracker
// while the editor has it (following, below).

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

	// followed says the file followed the tracker while the editor had it
	// (the description's), and base is then the tracker text it was last
	// given, the base the box's next write names (doc/design/views.md,
	// "The description box names its base").
	followed bool
	base     string
	// rewritten says git-work wrote over the file while it was open, or
	// opened it on a merge; the file is the draft either way
	// (doc/design/views.md, "A merge is the draft").
	rewritten bool
	// followErr is what kept the file from following, if anything did.
	followErr error
}

// changed says the editor left a text the box does not already hold. An
// editor that adds the newline a file ends with has changed nothing; a file
// git-work wrote over, or opened on a merge, holds a text the box does not,
// touched or not (doc/design/views.md, "A merge is the draft").
func (m externalTextMsg) changed() bool {
	return m.rewritten || trimEnd(m.text) != trimEnd(m.before)
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
//
// follow, where there is one, keeps the file following the tracker while
// the editor runs (the description's); it is nil for a text that writes a
// new comment, which has nothing in the tracker to follow.
func editExternally(repo *cache.RepoCache, target textTarget, text string, follow *following) (tea.Cmd, error) {
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
	cmd := editorCommand(editor, path)
	if follow == nil {
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return readBack(target, text, path, err, nil)
		}), nil
	}
	follow.path = path
	return tea.Exec(followedEditor{cmd: cmd, follow: follow}, func(err error) tea.Msg {
		return readBack(target, text, path, err, follow)
	}), nil
}

// readBack is the editor's exit as the box hears of it; the file goes
// whatever happened. What the file holds is the draft, git-work's merge
// included, touched or not (doc/design/views.md, "A merge is the draft").
func readBack(target textTarget, before, path string, ran error, follow *following) externalTextMsg {
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
	if follow != nil {
		follow.mu.Lock()
		defer follow.mu.Unlock()
		msg.followed, msg.base = true, follow.base
		msg.rewritten, msg.followErr = follow.rewritten, follow.err
	}
	return msg
}

// keepText puts what the editor left into a box as its draft, unless it left
// nothing new; either way the cursor is in the text after.
func keepText(box *commentBox, msg externalTextMsg) {
	if msg.changed() {
		box.area.SetValue(trimEnd(msg.text))
	}
}

// following is the description's file while the editor has it open
// (doc/design/views.md, "The editor's file follows the tracker"): on each
// change to the issue, a description that differs from the text last put in
// the file is merged into it.
//
// It is its own goroutine and observer, not the page's: the program's loop
// is blocked while the editor runs, and the ref watcher, on its own
// goroutine, goes on reconciling the cache and telling its observers.
type following struct {
	repo  *cache.RepoCache
	issue entity.Id
	path  string

	mu sync.Mutex
	// base is the tracker text last put in the file.
	base string
	// rewritten says git-work has written over the text the file opened
	// with, or opened it on a merge (doc/design/views.md, "A merge is the
	// draft").
	rewritten bool
	// err is the first thing that kept the file from following.
	err error

	changed chan struct{}
	done    chan struct{}
	stopped chan struct{}
}

// newFollowing follows an issue's description from base, the tracker text
// the draft was made on; merged says the draft is git-work's merge, made as
// the editor opens (doc/design/views.md, "The description box names its
// base").
func newFollowing(repo *cache.RepoCache, issue entity.Id, base string, merged bool) *following {
	return &following{repo: repo, issue: issue, base: base, rewritten: merged}
}

// start registers the observer and runs the goroutine that follows. The
// tracker is read once at the start, for a change made as the file was
// being written.
func (f *following) start() {
	f.changed = make(chan struct{}, 1)
	f.done = make(chan struct{})
	f.stopped = make(chan struct{})
	f.changed <- struct{}{}
	f.repo.Issues().RegisterObserver(f.repo.Name(), f)
	go func() {
		defer close(f.stopped)
		for {
			select {
			case <-f.done:
				return
			case <-f.changed:
				f.refresh()
			}
		}
	}()
}

// stop ends the following once any write to the file is done: what the
// editor left is read after this.
func (f *following) stop() {
	f.repo.Issues().UnregisterObserver(f)
	close(f.done)
	<-f.stopped
}

// EntityEvent is the cache's: an observer runs on the watcher's goroutine,
// so it only flags the change, and a burst of them is one refresh.
func (f *following) EntityEvent(_ cache.EntityEventType, _ string, _ string, id entity.Id) {
	if id != f.issue {
		return
	}
	select {
	case f.changed <- struct{}{}:
	default:
	}
}

// refresh merges the tracker's description into the file when it differs
// from the text last put there (doc/design/views.md, "A refresh is a
// merge"). The file is replaced by a rename, so an editor never reads half
// of it.
func (f *following) refresh() {
	snapshot, err := host.IssueSnapshot(f.repo, f.issue.String())
	if err != nil || len(snapshot.Comments) == 0 {
		f.fail(err)
		return
	}
	theirs := snapshot.Comments[0].Message

	f.mu.Lock()
	defer f.mu.Unlock()
	if cache.MessageHash(theirs) == cache.MessageHash(f.base) {
		return
	}
	ours, err := os.ReadFile(f.path)
	if err != nil {
		f.failLocked(err)
		return
	}
	merged, err := mergeDescription(f.base, string(ours), theirs)
	if err == nil {
		err = writeOver(f.path, merged)
	}
	if err != nil {
		f.failLocked(err)
		return
	}
	f.base, f.rewritten = theirs, true
}

func (f *following) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failLocked(err)
}

func (f *following) failLocked(err error) {
	if err != nil && f.err == nil {
		f.err = err
	}
}

// mergeDescription is the text a description's file is given when the
// tracker's moves from base to theirs while the file holds ours
// (doc/design/views.md, "A refresh is a merge"): a file still holding the
// base is theirs exactly; one with an edit of its own is `git merge-file`'s
// merge, an overlap written with conflict markers. Each side ends in one
// newline, so the newline an editor ends a file with is no conflict.
func mergeDescription(base, ours, theirs string) (string, error) {
	if trimEnd(ours) == trimEnd(base) {
		return theirs, nil
	}
	line := func(text string) string { return trimEnd(text) + "\n" }
	merged, _, err := gitcli.MergeFile(line(ours), line(base), line(theirs),
		gitcli.MergeLabels{"yours", "base", "tracker"})
	return merged, err
}

// writeOver replaces a file's content by renaming a written temporary file
// over it, in the same directory so the rename is one step.
func writeOver(path, text string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".git-work-*.md")
	if err != nil {
		return err
	}
	_, err = file.WriteString(text)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		_ = os.Remove(file.Name())
	}
	return err
}

// followedEditor is the editor's process with the file following the
// tracker for exactly as long as it runs.
type followedEditor struct {
	cmd    *exec.Cmd
	follow *following
}

func (e followedEditor) Run() error {
	e.follow.start()
	defer e.follow.stop()
	return e.cmd.Run()
}

// The standard streams are the terminal's unless already set, as
// tea.ExecProcess has them.
func (e followedEditor) SetStdin(r io.Reader) {
	if e.cmd.Stdin == nil {
		e.cmd.Stdin = r
	}
}

func (e followedEditor) SetStdout(w io.Writer) {
	if e.cmd.Stdout == nil {
		e.cmd.Stdout = w
	}
}

func (e followedEditor) SetStderr(w io.Writer) {
	if e.cmd.Stderr == nil {
		e.cmd.Stderr = w
	}
}
