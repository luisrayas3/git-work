package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
)

// A grabbed row carried past the edge of its group enters the next one, and
// the drop writes the grouping field with the rank, in one commit
// (2026-10-02, doc/design/terminal-renderer.md, Rank).

// ops is how many operations an issue carries, which is how a test sees that
// a drop was one call: a `set` of two keys is two operations in one commit,
// and two calls would be four.
func ops(t *testing.T, repo *cache.RepoCache, id string) int {
	t.Helper()

	log, err := host.IssueLog(repo, id)
	require.NoError(t, err)
	return len(log)
}

func TestListDragIntoTheGroupBelow(t *testing.T) {
	repo := testRepo(t)
	first := newIssue(t, repo, map[string]any{"title": "a first", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "b second", "status": "in-progress"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status","query":"sort_by(.fields.title)"}`)
	require.Equal(t, first, page.currentId())
	before := ops(t, repo, first)

	page = send(page, "space", "down").(*listPage)
	require.Contains(t, plainView(page), "in-progress")
	page = send(page, "space").(*listPage)

	require.Equal(t, "in-progress", fieldOf(t, repo, first, "status"))
	require.NotEmpty(t, fieldOf(t, repo, first, "rank"), "the rank goes with it")
	require.Equal(t, before+2, ops(t, repo, first), "one set of two keys, which is one commit")
	require.Contains(t, plainView(page), "moved to in-progress")
	require.Equal(t, first, page.currentId(), "the cursor follows the row")
}

func TestListDragIntoTheGroupAbove(t *testing.T) {
	repo := testRepo(t)
	newIssue(t, repo, map[string]any{"title": "a first", "status": "to-do"})
	second := newIssue(t, repo, map[string]any{"title": "b second", "status": "in-progress"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status","query":"sort_by(.fields.title)"}`)
	// over to-do's ghost (ghost.go)
	page = send(page, "down", "down").(*listPage)
	require.Equal(t, second, page.currentId())

	page = send(page, "space", "up", "space").(*listPage)
	require.Equal(t, "to-do", fieldOf(t, repo, second, "status"))
	require.Equal(t, second, page.currentId())
}

// TestListDragIntoNone: `(none)` is the group of the rows with no value at
// all, so a drop there clears the field.
func TestListDragIntoNone(t *testing.T) {
	repo := testRepo(t)
	filed := newIssue(t, repo, map[string]any{"title": "a filed", "status": "to-do"})
	newIssue(t, repo, map[string]any{"title": "b loose"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status","query":"sort_by(.fields.title)"}`)
	require.Equal(t, filed, page.currentId())

	page = send(page, "space", "down", "space").(*listPage)
	require.Equal(t, "", fieldOf(t, repo, filed, "status"))
	require.Contains(t, plainView(page), "moved to "+noGroup)
}

// TestListDragWillNotChangeTheType: the type decides which fields an issue
// has, so a move does not get to change it.
func TestListDragWillNotChangeTheType(t *testing.T) {
	repo := testRepo(t)
	task := newIssue(t, repo, map[string]any{"title": "a task"})
	newIssue(t, repo, map[string]any{"title": "b story", "type": "story"})

	page := list(t, repo, `{"fields":["title"],"group_by":"type","query":"sort_by(.fields.title)"}`)
	require.Equal(t, task, page.currentId())
	before := ops(t, repo, task)

	page = send(page, "space").(*listPage)
	updated, cmd := page.Update(press("down"))
	require.NotNil(t, cmd, "the bell")
	page = updated.(*listPage)
	require.Contains(t, plainView(page), "type cannot be changed")

	page = send(page, "space").(*listPage)
	require.Equal(t, "task", fieldOf(t, repo, task, "type"))
	require.Equal(t, before+1, ops(t, repo, task), "the rank alone")
}

// TestListDragWillNotChangeASet: a row grouped by a set-valued field is in
// a group per set, and a drag cannot say which item it meant.
func TestListDragWillNotChangeASet(t *testing.T) {
	repo := testRepo(t)
	one := newIssue(t, repo, map[string]any{"title": "a one", "labels": []any{"red"}})
	newIssue(t, repo, map[string]any{"title": "b two", "labels": []any{"blue"}})

	page := list(t, repo, `{"fields":["title","labels"],"group_by":"labels","query":"sort_by(.fields.title)"}`)
	require.Equal(t, one, page.currentId())

	page = send(page, "space").(*listPage)
	updated, cmd := page.Update(press("down"))
	require.NotNil(t, cmd, "the bell")
	require.Contains(t, plainView(updated.(*listPage)), "issue add/remove")
	require.Equal(t, "red", fieldOf(t, repo, one, "labels"))
}

// TestListDragKeepsAChildAmongItsSiblings: only a root crosses groups; a
// nested row belongs to its parent, wherever the parent is.
func TestListDragKeepsAChildAmongItsSiblings(t *testing.T) {
	repo := testRepo(t)
	story := newIssue(t, repo, map[string]any{"title": "a story", "type": "story", "status": "to-do"})
	child := newIssue(t, repo, map[string]any{"title": "b child", "status": "in-progress", "parent": story})
	newIssue(t, repo, map[string]any{"title": "c other", "status": "done"})

	page := list(t, repo, `{"fields":["title","status"],"group_by":"status","expand":"children",`+
		`"query":"map(select(.fields.type != \"task\" or .fields.parent == null)) | sort_by(.fields.title)"}`)
	page = send(page, "tab").(*listPage)
	require.Equal(t, child, page.currentId(), "the cursor is on the child")
	before := ops(t, repo, child)

	page = send(page, "space", "down", "space").(*listPage)
	require.Equal(t, "in-progress", fieldOf(t, repo, child, "status"), "its group did not change")
	require.Equal(t, before+1, ops(t, repo, child), "the rank alone")
}

func TestGanttDragCrossesGroups(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	first := newIssue(t, repo, map[string]any{
		"title": "a first", "start": "2026-09-07", "stop": "2026-09-13", "status": "to-do"})
	newIssue(t, repo, map[string]any{
		"title": "b second", "start": "2026-09-07", "stop": "2026-09-13", "status": "in-progress"})

	page := gantt(t, repo, `{"start":"start","stop":"stop","from":"2026-09-07","group_by":"status",`+
		`"query":"sort_by(.fields.title)"}`)
	require.Equal(t, first, page.current().id)
	before := ops(t, repo, first)

	send(page, "space", "down", "space")
	require.Equal(t, `"in-progress"`, field(t, repo, first, "status"))
	require.Equal(t, before+2, ops(t, repo, first), "the status and the rank, in one commit")
	require.Equal(t, "moved to in-progress", page.status)
	require.Equal(t, first, page.current().id, "the cursor follows the row")
}

// TestBoardDragCrossesSwimlanes: up and down past the end of a stack carry
// the card into the lane above or below, and the drop writes the lane's
// field exactly as a column drop writes the column's.
func TestBoardDragCrossesSwimlanes(t *testing.T) {
	repo := testRepo(t)
	high := newIssue(t, repo, map[string]any{"title": "high", "status": "to-do", "priority": "high"})
	low := newIssue(t, repo, map[string]any{"title": "low", "status": "to-do", "priority": "low"})

	page := board(t, repo, `{"columns":"status","group_by":"priority","query":"sort_by(.fields.title)"}`)
	require.Equal(t, high, page.currentId())
	before := ops(t, repo, high)

	// carried down and back up within one drag, into the lane it emptied on
	// its way out, the card is where it was picked up and nothing is written
	page = send(page, "space", "j", "k", "space").(*boardPage)
	require.Equal(t, before, ops(t, repo, high))
	require.Equal(t, "high", fieldOf(t, repo, high, "priority"))

	page = send(page, "space", "j", "space").(*boardPage)
	require.Equal(t, "low", fieldOf(t, repo, high, "priority"))
	require.Equal(t, before+2, ops(t, repo, high), "the priority and the rank, in one commit")
	require.Contains(t, plainView(page), "moved to low")
	require.Equal(t, high, page.currentId(), "the cursor follows the card")

	require.Equal(t, "low", fieldOf(t, repo, low, "priority"), "the other card did not move")
}
