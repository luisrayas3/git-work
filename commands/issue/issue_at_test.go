package issuecmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
)

// issueAt creates an issue whose create operation carries a chosen time, so
// that --at and --from/--to can be measured against times a test names.
func issueAt(t *testing.T, env *execenv.Env, when time.Time, title string, fields map[string]issue.Value) *cache.IssueCache {
	t.Helper()

	author, err := env.Backend.GetUserIdentity()
	require.NoError(t, err)

	i, _, err := env.Backend.Issues().NewRaw(author, when.Unix(), title, "body", nil, fields, nil)
	require.NoError(t, err)
	return i
}

// setFieldAt commits one SetField operation stamped with a chosen time.
func setFieldAt(t *testing.T, env *execenv.Env, i *cache.IssueCache, when time.Time, key, value string) {
	t.Helper()

	author, err := env.Backend.GetUserIdentity()
	require.NoError(t, err)

	raw, err := json.Marshal(value)
	require.NoError(t, err)

	op := issue.NewSetFieldOp(author, when.Unix(), key, issue.Value(raw))
	require.NoError(t, op.Validate())
	require.NoError(t, i.CommitOperations([]issue.Operation{op}))
}

// TestIssueGetAt: --at prints the issue as it stood then, in both forms, and
// refuses an issue that did not exist yet.
func TestIssueGetAt(t *testing.T) {
	env := newTestEnv(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	i := issueAt(t, env, t0, "a title", map[string]issue.Value{"status": issue.StringValue("to-do")})
	setFieldAt(t, env, i, t1, "status", "done")

	get := func(at string, format string) string {
		env.Out.Reset()
		require.NoError(t, runIssueGet(env, issueGetOptions{at: at, format: format}, []string{i.Id().Human()}))
		return env.Out.String()
	}

	var document struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(get("2026-09-01T12:00:00Z", "json")), &document))
	require.JSONEq(t, `"to-do"`, string(document.Fields["status"]))

	require.NoError(t, json.Unmarshal([]byte(get("", "json")), &document))
	require.JSONEq(t, `"done"`, string(document.Fields["status"]))

	// The text form reads the same replayed snapshot.
	require.Contains(t, get("2026-09-01T12:00:00Z", "text"), "[to-do]")
	require.Contains(t, get("", "text"), "[done]")

	env.Out.Reset()
	err := runIssueGet(env, issueGetOptions{at: "2026-08-01", format: "json"}, []string{i.Id().Human()})
	require.ErrorContains(t, err, "did not exist at")

	env.Out.Reset()
	err = runIssueGet(env, issueGetOptions{at: "last tuesday", format: "json"}, []string{i.Id().Human()})
	require.ErrorContains(t, err, "--at")
	require.ErrorContains(t, err, "is not a time")
}

// TestIssueListAt: the program runs over the issues as they stood then, and
// an issue created later is simply absent.
func TestIssueListAt(t *testing.T) {
	env := newTestEnv(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	issueAt(t, env, t0, "the old one", nil)
	issueAt(t, env, t1, "the new one", nil)

	titles := func(at string) []string {
		env.Out.Reset()
		require.NoError(t, runIssueList(env, issueListOptions{at: at, format: "json"}, nil))
		var items []struct {
			Fields struct {
				Title string `json:"title"`
			} `json:"fields"`
		}
		require.NoError(t, json.Unmarshal(env.Out.Bytes(), &items))
		out := make([]string, len(items))
		for i, item := range items {
			out[i] = item.Fields.Title
		}
		return out
	}

	require.Equal(t, []string{"the old one"}, titles("2026-09-01T12:00:00Z"))
	require.ElementsMatch(t, []string{"the old one", "the new one"}, titles(""))
}

// TestIssueLogWindow: --from/--to is half-open, and a PROGRAM selects many
// issues whose entries each name the issue they belong to.
func TestIssueLogWindow(t *testing.T) {
	env := newTestEnv(t)

	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	t2 := t1.Add(24 * time.Hour)

	a := issueAt(t, env, t0, "a", nil)
	setFieldAt(t, env, a, t1, "status", "in-progress")
	setFieldAt(t, env, a, t2, "status", "done")

	b := issueAt(t, env, t1, "b", nil)

	type entry struct {
		Issue    string `json:"issue"`
		Type     string `json:"type"`
		UnixTime int64  `json:"unix_time"`
	}
	log := func(opts issueLogOptions, args ...string) []entry {
		env.Out.Reset()
		opts.format = "json"
		require.NoError(t, runIssueLog(env, opts, args))
		var entries []entry
		require.NoError(t, json.Unmarshal(env.Out.Bytes(), &entries))
		return entries
	}

	// Half-open: t1 is in, t2 is out.
	entries := log(issueLogOptions{from: "2026-09-02T00:00:00Z", to: "2026-09-03T00:00:00Z"}, a.Id().Human())
	require.Len(t, entries, 1)
	require.EqualValues(t, t1.Unix(), entries[0].UnixTime)
	require.Equal(t, a.Id().String(), entries[0].Issue)

	// A program selects both issues, ordered by time.
	entries = log(issueLogOptions{from: "2026-09-02T00:00:00Z", to: "2026-09-03T00:00:00Z"},
		`map(select(.fields.title != null))`)
	require.Len(t, entries, 2)
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Issue] = true
	}
	require.True(t, seen[a.Id().String()])
	require.True(t, seen[b.Id().String()])

	// No argument is every unarchived issue, whole history.
	require.Len(t, log(issueLogOptions{}), 4)

	// The text form names the issue first.
	env.Out.Reset()
	require.NoError(t, runIssueLog(env, issueLogOptions{format: "text"}, []string{a.Id().Human()}))
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 3)
	require.True(t, strings.HasPrefix(lines[0], a.Id().Human()+"\t"))

	// A mistyped id reads as a mistyped id, and a bad window names its flag.
	env.Out.Reset()
	err := runIssueLog(env, issueLogOptions{format: "json"}, []string{"deadbeef"})
	require.ErrorContains(t, err, "neither an issue")

	err = runIssueLog(env, issueLogOptions{format: "json", from: "yesterday"}, nil)
	require.ErrorContains(t, err, "--from")
}
