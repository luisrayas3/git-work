package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/repository"
)

// setDescription writes an issue's description, as another writer would.
func setDescription(t *testing.T, repo *cache.RepoCache, id, text string) {
	t.Helper()
	snapshot, err := host.IssueSnapshot(repo, id)
	require.NoError(t, err)
	require.NoError(t, host.IssueCommentEdit(repo, snapshot.Comments[0].CombinedId().String(), text, ""))
}

// description is an issue's description as the store has it.
func description(t *testing.T, repo *cache.RepoCache, id string) string {
	t.Helper()
	snapshot, err := host.IssueSnapshot(repo, id)
	require.NoError(t, err)
	return snapshot.Comments[0].Message
}

// fileHolds waits for a file to hold a text.
func fileHolds(t *testing.T, path, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, err := os.ReadFile(path)
		return err == nil && string(got) == want
	}, 5*time.Second, 10*time.Millisecond, "the file holds %q", want)
}

// TestARefreshIsAMerge: a file still holding the base becomes the tracker's
// text exactly, the newline an editor adds included; one with an edit of its
// own is merged, an overlap with markers that hold both sides
// (doc/design/views.md, "A refresh is a merge").
func TestARefreshIsAMerge(t *testing.T) {
	base := "one\ntwo\nthree"

	merged, err := mergeDescription(base, base, "one\ntwo\nthree\nfour")
	require.NoError(t, err)
	require.Equal(t, "one\ntwo\nthree\nfour", merged, "untouched: the tracker's text")
	merged, err = mergeDescription(base, base+"\n", "one\n2")
	require.NoError(t, err)
	require.Equal(t, "one\n2", merged, "the editor's newline is no edit")

	merged, err = mergeDescription(base, "ONE\ntwo\nthree\n", "one\ntwo\nthree\nfour")
	require.NoError(t, err)
	require.Equal(t, "ONE\ntwo\nthree\nfour\n", merged, "edits apart: both")

	merged, err = mergeDescription(base, "one\nmine\nthree", "one\ntheirs\nthree")
	require.NoError(t, err)
	require.Contains(t, merged, "<<<<<<< yours\nmine\n=======\ntheirs\n>>>>>>> tracker\n")
}

// TestAnUntouchedRefreshIsReverted: a file the editor left as git-work last
// wrote it, by a refresh or a merge, is the tracker's text; one the person
// saved over is theirs, and a file git-work never wrote over is the draft as
// ever (doc/design/views.md, "An untouched refresh is reverted").
func TestAnUntouchedRefreshIsReverted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "description.md")
	exit := func(file string, follow *following) externalTextMsg {
		require.NoError(t, os.WriteFile(path, []byte(file), 0o600))
		return readBack(targetDescription, "the draft", path, nil, follow)
	}

	msg := exit("a merge\n", &following{base: "tracker", written: "a merge", rewritten: true})
	require.True(t, msg.reverted)
	require.Equal(t, "tracker", msg.text)
	require.Equal(t, "tracker", msg.base)

	msg = exit("saved over it", &following{base: "tracker", written: "a merge", rewritten: true})
	require.False(t, msg.reverted)
	require.Equal(t, "saved over it", msg.text)
	require.Equal(t, "tracker", msg.base, "the base is the text last given")

	msg = exit("the draft", &following{base: "base", written: "the draft"})
	require.False(t, msg.reverted)
	require.Equal(t, "the draft", msg.text)
	require.False(t, msg.changed())
}

// twoProcesses is a store open twice, holding issues tasks: a writer, and a
// viewer watching the refs, as the TUI's process is while another writes.
// The follower reads the viewer, so the two never share a snapshot.
func twoProcesses(t *testing.T, issues int) (writer, viewer *cache.RepoCache, ids []string) {
	t.Helper()
	store := repository.CreateGoGitTestRepo(t, false)
	writer, err := cache.NewRepoCacheNoEvents(store)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	identity, err := writer.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, writer.SetUserIdentity(identity))
	_, _, err = host.SchemaInit(writer, "jira", false)
	require.NoError(t, err)
	// the issues exist before the viewer watches, as an open issue's do
	for range issues {
		ids = append(ids, newIssue(t, writer, map[string]any{"title": "one"}))
	}

	viewer, err = cache.NewRepoCacheNoEvents(store)
	require.NoError(t, err)
	t.Cleanup(func() { _ = viewer.Close() })
	stopWatching, err := viewer.Watch()
	require.NoError(t, err)
	t.Cleanup(stopWatching)
	return writer, viewer, ids
}

// TestTheEditorsFileFollowsTheTracker: while the editor has it, a change to
// the description lands in the file, as the tracker's text while the file is
// untouched and merged once the person has saved over it
// (doc/design/views.md, "The editor's file follows the tracker").
func TestTheEditorsFileFollowsTheTracker(t *testing.T) {
	writer, repo, ids := twoProcesses(t, 2)
	id, other := ids[0], ids[1]

	path := filepath.Join(t.TempDir(), "description.md")
	require.NoError(t, os.WriteFile(path, []byte("the body"), 0o600))
	follow := newFollowing(repo, entity.Id(id), "the body", false)
	follow.path, follow.written = path, "the body"
	follow.start()
	stopped := false
	defer func() {
		if !stopped {
			follow.stop()
		}
	}()

	// another issue's change leaves it alone
	setDescription(t, writer, other, "elsewhere")

	setDescription(t, writer, id, "one\ntwo\nthree\nfour")
	fileHolds(t, path, "one\ntwo\nthree\nfour")

	// the person saves over it; the next change is merged with what they saved
	require.NoError(t, os.WriteFile(path, []byte("ONE\ntwo\nthree\nfour\n"), 0o600))
	setDescription(t, writer, id, "one\ntwo\nthree\nfour\nfive")
	fileHolds(t, path, "ONE\ntwo\nthree\nfour\nfive\n")

	follow.stop()
	stopped = true
	msg := readBack(targetDescription, "one\ntwo\nthree", path, nil, follow)
	require.True(t, msg.followed)
	require.Equal(t, "one\ntwo\nthree\nfour\nfive", msg.base)
	require.True(t, msg.reverted, "left as the merge was written")
	require.Equal(t, "one\ntwo\nthree\nfour\nfive", msg.text)
}

// TestTheFileFollowsWhileTheEditorRuns: with the editor's process running
// and the ref watcher on, a write from another process lands in the file the
// editor has open, the editor seeing it before it exits.
func TestTheFileFollowsWhileTheEditorRuns(t *testing.T) {
	writer, viewer, ids := twoProcesses(t, 1)
	id := ids[0]

	path := filepath.Join(t.TempDir(), "description.md")
	require.NoError(t, os.WriteFile(path, []byte("the body"), 0o600))
	follow := newFollowing(viewer, entity.Id(id), "the body", false)
	follow.path, follow.written = path, "the body"

	// an editor that exits once its file holds the other process's text, and
	// fails if it never does
	editor := followedEditor{cmd: editorCommand(
		`i=0; while [ $i -lt 100 ]; do grep -q elsewhere "$1" && exit 0; i=$((i+1)); sleep 0.05; done; exit 1 #`,
		path), follow: follow}

	snapshot, err := host.IssueSnapshot(writer, id)
	require.NoError(t, err)
	written := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		written <- host.IssueCommentEdit(writer, snapshot.Comments[0].CombinedId().String(), "written elsewhere", "")
	}()
	require.NoError(t, editor.Run(), "the editor saw the change")
	require.NoError(t, <-written)

	msg := readBack(targetDescription, "the body", path, nil, follow)
	require.Equal(t, "written elsewhere", msg.base)
	require.True(t, msg.reverted)
	require.Equal(t, "written elsewhere", msg.text)
}

// TestTheDescriptionBoxNamesItsBase: enter on a draft made on a text the
// tracker has since changed is refused, the draft kept; esc e then opens on
// the merge, with the tracker's text as the base, and a draft saved from it
// is written (doc/design/views.md, "The description box names its base").
func TestTheDescriptionBoxNamesItsBase(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	repo := testRepo(t)
	id := newIssue(t, repo, map[string]any{"title": "one"})
	setDescription(t, repo, id, "one\ntwo\nthree")

	page := show(t, repo, id, nil)
	page = send(page, "tab", "tab", "space").(*showPage)
	page.desc.area.SetValue("ONE\ntwo\nthree")

	setDescription(t, repo, id, "one\ntwo\nthree\nfour")
	updated, cmd := page.Update(press("enter"))
	page = updated.(*showPage)
	require.Equal(t, bell()(), cmd())
	require.Equal(t, "the description changed since it was opened: esc e merges", page.status)
	require.True(t, page.editingDesc)
	require.Equal(t, "ONE\ntwo\nthree", page.desc.draft(), "the draft kept")
	require.Equal(t, "one\ntwo\nthree\nfour", description(t, repo, id), "nothing written")

	// esc e opens on the merge, the tracker's text its base
	text, follow, err := page.descForEditor()
	require.NoError(t, err)
	require.Equal(t, "ONE\ntwo\nthree\nfour\n", text)
	require.Equal(t, "one\ntwo\nthree\nfour", follow.base)
	require.True(t, follow.rewritten, "the merge is git-work's")
	opensTheEditor(t, page, "alt+e")

	// left as it opened, the merge is undone: the draft is the tracker's
	path := filepath.Join(t.TempDir(), "description.md")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	follow.written = text
	updated, _ = page.Update(readBack(targetDescription, text, path, nil, follow))
	page = updated.(*showPage)
	require.Equal(t, "one\ntwo\nthree\nfour", page.desc.draft())
	require.Equal(t, "the description changed in the tracker: the draft is its text", page.status)

	// saved, it is the draft, on the new base, and enter writes it
	_, follow, err = page.descForEditor()
	require.NoError(t, err)
	require.False(t, follow.rewritten, "the base is current")
	require.NoError(t, os.WriteFile(path, []byte("ONE\ntwo\nthree\nfour\n"), 0o600))
	follow.written = "one\ntwo\nthree\nfour"
	updated, _ = page.Update(readBack(targetDescription, "one\ntwo\nthree\nfour", path, nil, follow))
	page = updated.(*showPage)
	require.Equal(t, "ONE\ntwo\nthree\nfour", page.desc.draft())
	page = send(page, "enter").(*showPage)
	require.Equal(t, "description written", page.status)
	require.Equal(t, "ONE\ntwo\nthree\nfour", description(t, repo, id))
}
