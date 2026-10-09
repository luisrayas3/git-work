package host

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/repository"
)

// atTestRepo is a store with an identity set, which is what a writer needs.
func atTestRepo(t *testing.T) (*cache.RepoCache, identity.Interface) {
	t.Helper()

	repo := repository.CreateGoGitTestRepo(t, false)
	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	i, err := backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, backend.SetUserIdentity(i))

	return backend, i
}

// newIssueAt creates an issue whose create operation carries a chosen time,
// so that a replay can be measured against times a test names.
func newIssueAt(t *testing.T, repo *cache.RepoCache, author identity.Interface, when time.Time, title string, fields map[string]issue.Value) *cache.IssueCache {
	t.Helper()

	i, _, err := repo.Issues().NewRaw(author, when.Unix(), title, "body", nil, fields, nil)
	require.NoError(t, err)
	return i
}

// setAt commits one SetField operation stamped with a chosen time.
func setAt(t *testing.T, i *cache.IssueCache, author identity.Interface, when time.Time, key string, value issue.Value) {
	t.Helper()

	op := issue.NewSetFieldOp(author, when.Unix(), key, value)
	require.NoError(t, op.Validate())
	require.NoError(t, i.CommitOperations([]issue.Operation{op}))
}

func str(s string) issue.Value {
	raw, _ := json.Marshal(s)
	return issue.Value(raw)
}

func field(t *testing.T, snap *issue.Snapshot, key string) string {
	t.Helper()
	v, ok := snap.FieldString(key)
	require.True(t, ok, "field %s", key)
	return v
}

// TestIssueSnapshotAtReplays: the value a field had at a moment is the value
// the operations at or before that moment left it at.
func TestIssueSnapshotAtReplays(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	t2 := t1.Add(24 * time.Hour)

	i := newIssueAt(t, repo, author, t0, "a title", map[string]issue.Value{"status": str("v0")})
	setAt(t, i, author, t1, "status", str("v1"))
	setAt(t, i, author, t2, "status", str("v2"))

	// At the creation, before any edit.
	snap, err := IssueSnapshotAt(repo, i.Id().String(), t0)
	require.NoError(t, err)
	require.Equal(t, "v0", field(t, snap, "status"))
	require.Equal(t, "a title", snap.Title())

	// At t1 and between t1 and t2: the first edit, and only it.
	snap, err = IssueSnapshotAt(repo, i.Id().String(), t1)
	require.NoError(t, err)
	require.Equal(t, "v1", field(t, snap, "status"))

	snap, err = IssueSnapshotAt(repo, i.Id().String(), t1.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "v1", field(t, snap, "status"))

	// At t2, and after everything.
	snap, err = IssueSnapshotAt(repo, i.Id().String(), t2)
	require.NoError(t, err)
	require.Equal(t, "v2", field(t, snap, "status"))

	snap, err = IssueSnapshotAt(repo, i.Id().String(), t2.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "v2", field(t, snap, "status"))

	// The operations are cut too, so a window reads the same history.
	require.Len(t, snap.Operations, 3)
}

// TestIssueSnapshotAtBeforeCreation: an issue created after the cut did not
// exist, and `get --at` says so rather than inventing an empty one.
func TestIssueSnapshotAtBeforeCreation(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	i := newIssueAt(t, repo, author, t0, "a title", nil)

	_, err := IssueSnapshotAt(repo, i.Id().String(), t0.Add(-time.Second))
	require.ErrorContains(t, err, "did not exist at")
}

// TestIssueListAtArchived: archived is an ordinary field, so the input leaves
// out what was archived at the time and keeps what was not, with no special
// case on the replay path; a program with no filter of its own sees no
// archived issue, and include_archive brings them back
// (doc/design/include-archive.md, I1, I4).
func TestIssueListAtArchived(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	t2 := t1.Add(24 * time.Hour)

	old := newIssueAt(t, repo, author, t0, "the old one", nil)
	setAt(t, old, author, t2, issue.ArchivedKey, issue.MustValue(true))

	newIssueAt(t, repo, author, t2, "the late one", nil)

	titlesOf := func(program string, at time.Time, includeArchive bool) []string {
		values, err := IssueListAt(repo, program, at, includeArchive)
		require.NoError(t, err)
		items, ok := IssueItems(values)
		if !ok {
			return nil
		}
		var out []string
		for _, item := range items {
			fields, _ := item["fields"].(map[string]any)
			out = append(out, StringOr(fields["title"], ""))
		}
		return out
	}
	titles := func(at time.Time) []string { return titlesOf("", at, false) }

	// At t1 the old issue is open and the late one does not exist yet.
	require.Equal(t, []string{"the old one"}, titles(t1))

	// At t2 the old one is archived out of the default list and the late one
	// has appeared.
	require.Equal(t, []string{"the late one"}, titles(t2))

	// And now, which is the live path, agrees with the replay at now.
	require.Equal(t, titles(time.Time{}), titles(time.Now().Add(time.Hour)))

	// `.` is the whole input, and the input has no archived issue in it,
	// live or replayed.
	require.Equal(t, []string{"the late one"}, titlesOf(".", time.Time{}, false))
	require.Equal(t, []string{"the late one"}, titlesOf(".", t2, false))

	// The switch brings them back: every issue that existed then.
	require.Equal(t, []string{"the old one", "the late one"}, titlesOf(".", time.Time{}, true))
	require.Equal(t, []string{"the old one", "the late one"}, titlesOf(".", t2, true))
	require.Equal(t, []string{"the old one"}, titlesOf(".", t1, true))
}

// TestIssueLogBetweenArchived: the log's PROGRAM form selects from the same
// input, so an archived issue is out of it unless include_archive brings it
// back, and an id names its issue archived or not.
func TestIssueLogBetweenArchived(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	gone := newIssueAt(t, repo, author, t0, "gone", nil)
	setAt(t, gone, author, t0.Add(time.Hour), issue.ArchivedKey, issue.MustValue(true))
	newIssueAt(t, repo, author, t0, "kept", nil)

	issues := func(idOrProgram string, includeArchive bool) map[string]int {
		entries, err := IssueLogBetween(repo, idOrProgram, time.Time{}, time.Time{}, includeArchive)
		require.NoError(t, err)
		out := map[string]int{}
		for _, entry := range entries {
			out[entry.Issue]++
		}
		return out
	}

	require.NotContains(t, issues(".", false), gone.Id().String())
	require.Len(t, issues(".", false), 1)
	require.Equal(t, 2, issues(".", true)[gone.Id().String()])
	require.Equal(t, 2, issues(gone.Id().String(), false)[gone.Id().String()])
}

// TestIssueListAtExcerptShape: the replayed excerpt is the same shape the live
// one is, so a jq program written for the present reads the past unchanged.
func TestIssueListAtExcerptShape(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	newIssueAt(t, repo, author, t0, "a title", map[string]issue.Value{"status": str("open")})

	values, err := IssueListAt(repo, ".", t0, false)
	require.NoError(t, err)
	items, ok := IssueItems(values)
	require.True(t, ok)
	require.Len(t, items, 1)

	for _, key := range []string{
		"id", "human_id", "create_time", "edit_time",
		"fields", "author", "actors", "participants", "comments", "metadata",
	} {
		require.Contains(t, items[0], key)
	}

	create, _ := items[0]["create_time"].(map[string]any)
	require.EqualValues(t, t0.Unix(), create["timestamp"])
	require.Contains(t, create, "lamport", "the create lamport is the live one")

	edit, _ := items[0]["edit_time"].(map[string]any)
	require.NotContains(t, edit, "lamport", "no honest last-edit lamport in the past")
}

// TestIssueLogBetween: a window selects operations by their own time, and the
// entries name the issue they belong to whether one was asked for or many.
func TestIssueLogBetween(t *testing.T) {
	repo, author := atTestRepo(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	t2 := t1.Add(24 * time.Hour)

	a := newIssueAt(t, repo, author, t0, "a", nil)
	setAt(t, a, author, t1, "status", str("v1"))
	setAt(t, a, author, t2, "status", str("v2"))

	b := newIssueAt(t, repo, author, t1, "b", nil)

	// One issue, the whole history.
	entries, err := IssueLogBetween(repo, a.Id().String(), time.Time{}, time.Time{}, false)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	for _, entry := range entries {
		require.Equal(t, a.Id().String(), entry.Issue)
	}

	// Half-open: t1 is in, t2 is out.
	entries, err = IssueLogBetween(repo, a.Id().String(), t1, t2, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.EqualValues(t, t1.Unix(), entries[0].UnixTime)
	require.True(t, entries[0].Time.Equal(t1), "the same moment, written out")

	// A program selects many issues, and the entries come back by time.
	entries, err = IssueLogBetween(repo, "map(select(.fields.title != null))", t1, t2, false)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.EqualValues(t, t1.Unix(), entries[0].UnixTime)
	require.EqualValues(t, t1.Unix(), entries[1].UnixTime)

	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Issue] = true
	}
	require.True(t, seen[a.Id().String()])
	require.True(t, seen[b.Id().String()])

	// No argument is the default selection, every unarchived issue.
	entries, err = IssueLogBetween(repo, "", time.Time{}, time.Time{}, false)
	require.NoError(t, err)
	require.Len(t, entries, 4)
}

// TestIssueLogSelectionErrors: a mistyped id reads as a mistyped id, not as a
// jq syntax error, and a program that returns something else says so.
// TestIssueLogBetweenPastCacheLimit: a selection wider than the cache's load
// limit still reads every issue. The cache locks an evicted entity for good,
// so a log that loaded them all before reading any would block forever.
func TestIssueLogBetweenPastCacheLimit(t *testing.T) {
	repo, author := atTestRepo(t)
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		newIssueAt(t, repo, author, t0, "issue", nil)
	}
	repo.Issues().SetCacheSize(1)

	done := make(chan struct{})
	var entries []cmdjson.IssueOperation
	var err error
	go func() {
		defer close(done)
		entries, err = IssueLogBetween(repo, "", time.Time{}, time.Time{}, false)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the log blocked on an evicted issue")
	}
	require.NoError(t, err)
	require.Len(t, entries, 3)
}

func TestIssueLogSelectionErrors(t *testing.T) {
	repo, author := atTestRepo(t)
	newIssueAt(t, repo, author, time.Now(), "a", nil)

	_, err := IssueLogBetween(repo, "deadbeef", time.Time{}, time.Time{}, false)
	require.ErrorContains(t, err, "neither an issue")
	require.ErrorContains(t, err, "nor a jq program")

	_, err = IssueLogBetween(repo, "map(.fields.title)", time.Time{}, time.Time{}, false)
	require.ErrorContains(t, err, "did not return a list of issues")

	// A program that matches nothing is no issues, not a shape problem.
	entries, err := IssueLogBetween(repo, `map(select(.fields.title == "nothing"))`, time.Time{}, time.Time{}, false)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestParseTime: the three forms, and what a bad one says.
func TestParseTime(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	at, err := ParseTime("2026-09-21", now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local), at)

	at, err = ParseTime("2026-09-21T09:00:00Z", now)
	require.NoError(t, err)
	require.True(t, at.Equal(time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)))

	at, err = ParseTime("7d", now)
	require.NoError(t, err)
	require.True(t, at.Equal(now.AddDate(0, 0, -7)))

	at, err = ParseTime("2w", now)
	require.NoError(t, err)
	require.True(t, at.Equal(now.AddDate(0, 0, -14)))

	at, err = ParseTime("12h", now)
	require.NoError(t, err)
	require.True(t, at.Equal(now.Add(-12*time.Hour)))

	at, err = ParseTime("", now)
	require.NoError(t, err)
	require.True(t, at.IsZero(), "an absent flag is the zero time")

	_, err = ParseTime("last tuesday", now)
	require.ErrorContains(t, err, "is not a time")
}
