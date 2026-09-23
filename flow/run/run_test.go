package run

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/flow"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/repository"
)

// testRepo is a store with an identity set, which is what a writer needs.
func testRepo(t *testing.T) *cache.RepoCache {
	t.Helper()

	repo := repository.CreateGoGitTestRepo(t, false)
	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	i, err := backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, backend.SetUserIdentity(i))

	return backend
}

// run executes a script and returns what it returned, decoded.
func run(t *testing.T, repo *cache.RepoCache, script string, kwargs map[string]json.RawMessage) (any, string, error) {
	t.Helper()

	def, err := flow.Parse(script)
	require.NoError(t, err)

	stderr := &bytes.Buffer{}
	raw, err := Run(context.Background(), repo, stderr, def, script, kwargs)
	if err != nil {
		return nil, stderr.String(), err
	}
	if raw == nil {
		return nil, stderr.String(), nil
	}

	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	return value, stderr.String(), nil
}

// importFlow puts a script in the store, as `git work flow import` would.
func importFlow(t *testing.T, repo *cache.RepoCache, script string) {
	t.Helper()

	def, err := flow.Parse(script)
	require.NoError(t, err)

	_, _, err = repo.Flows().New(config.ShapeFlow, def.Name, map[string]config.Value{
		host.AttrScript:      config.StringValue(script),
		host.AttrDescription: config.StringValue(def.Description),
	})
	require.NoError(t, err)
}

const boardFlow = `def board(status="open"):
    """Kanban of what is not done."""
    a = issue.new({"fields": {"title": "first", "status": "open"}})
    issue.new({"fields": {"title": "second", "status": "done"}})
    issue.set(a, estimate=3)
    items = issue.list('map(select(.fields.status == "%s"))' % status)
    return view.board(items, columns="status", card_title="title")
`

func TestFlowWritesReadsAndReturnsASpec(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, boardFlow, nil)
	require.NoError(t, err)

	spec, ok := value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "board", spec["view"])
	require.Equal(t, map[string]any{"columns": "status", "card_title": "title"}, spec["bindings"])

	// the jq program selected one of the two issues the flow created
	items, ok := spec["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)

	item := items[0].(map[string]any)
	fields := item["fields"].(map[string]any)
	require.Equal(t, "first", fields["title"])
	// issue.set landed, through the cache, as one commit
	require.EqualValues(t, 3, fields["estimate"])

	// and both issues are really in the store
	require.Len(t, repo.Issues().AllIds(), 2)
}

func TestDefaultFillsAndAnArgumentOverridesIt(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, boardFlow, map[string]json.RawMessage{
		"status": json.RawMessage(`"done"`),
	})
	require.NoError(t, err)

	items := value.(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "second", items[0].(map[string]any)["fields"].(map[string]any)["title"])
}

func TestUnknownArgumentNamesTheParameters(t *testing.T) {
	repo := testRepo(t)

	_, _, err := run(t, repo, boardFlow, map[string]json.RawMessage{
		"statuss": json.RawMessage(`"done"`),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "statuss")
	require.Contains(t, err.Error(), `status="open"`)
	require.Contains(t, err.Error(), "flow board")
}

func TestRequiredArgumentIsRequired(t *testing.T) {
	repo := testRepo(t)

	script := `def report(since):
    """Weekly status."""
    return since
`
	_, _, err := run(t, repo, script, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "since is required")

	value, _, err := run(t, repo, script, map[string]json.RawMessage{
		"since": json.RawMessage(`"2026-09-01"`),
	})
	require.NoError(t, err)
	require.Equal(t, "2026-09-01", value)
}

func TestArgumentsAreJSONOfEveryShape(t *testing.T) {
	repo := testRepo(t)

	script := `def echo(a, b, c, d, e):
    """Echo."""
    return [a, b, c, d, e]
`
	value, _, err := run(t, repo, script, map[string]json.RawMessage{
		"a": json.RawMessage(`3`),
		"b": json.RawMessage(`1.5`),
		"c": json.RawMessage(`true`),
		"d": json.RawMessage(`["x","y"]`),
		"e": json.RawMessage(`{"k":null}`),
	})
	require.NoError(t, err)
	require.Equal(t, []any{
		float64(3), 1.5, true,
		[]any{"x", "y"},
		map[string]any{"k": nil},
	}, value)
}

func TestFlowRunIsReentrant(t *testing.T) {
	repo := testRepo(t)

	importFlow(t, repo, `def inner(n=1):
    """Double a number."""
    return n * 2
`)

	value, _, err := run(t, repo, `def outer(n=3):
    """Call another flow."""
    return flow.run("inner", n=n) + 1
`, nil)
	require.NoError(t, err)
	require.EqualValues(t, 7, value)
}

func TestFlowRunHasADepthCap(t *testing.T) {
	repo := testRepo(t)

	// a cycle: each flow calls the other, forever
	importFlow(t, repo, `def ping():
    """Call pong."""
    return flow.run("pong")
`)
	importFlow(t, repo, `def pong():
    """Call ping."""
    return flow.run("ping")
`)

	_, _, err := run(t, repo, `def start():
    """Enter the cycle."""
    return flow.run("ping")
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func TestRuntimeErrorCarriesTheFlowNameAndLine(t *testing.T) {
	repo := testRepo(t)

	_, _, err := run(t, repo, `def broken():
    """Divide by nothing."""
    x = 1
    return x // 0
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "flow broken")
	// the backtrace names the line the failure is on
	require.Contains(t, err.Error(), ":4:")
	require.Contains(t, err.Error(), "broken.star")
}

func TestHostErrorNamesTheFunction(t *testing.T) {
	repo := testRepo(t)

	_, _, err := run(t, repo, `def bad():
    """Ask for an issue nobody created."""
    return issue.get("deadbeef")
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "issue.get")
	require.Contains(t, err.Error(), "flow bad")
}

func TestViewErrorsReachTheScript(t *testing.T) {
	repo := testRepo(t)

	_, _, err := run(t, repo, `def bad():
    """A board with no columns."""
    return view.board([])
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	_, _, err = run(t, repo, `def bad():
    """A binding this view does not have."""
    return view.gantt([], start="a", end="b", colour="c")
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "colour")
}

func TestStepCapTrips(t *testing.T) {
	repo := testRepo(t)

	// Starlark has no while, so a loop long enough is the runaway
	_, _, err := run(t, repo, `def forever():
    """Count for ever."""
    n = 0
    for i in range(1000000000):
        n = n + 1
    return n
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "flow forever")
	require.Contains(t, strings.ToLower(err.Error()), "step")
}

func TestContextCancellationStops(t *testing.T) {
	repo := testRepo(t)

	script := `def forever():
    """Count for ever."""
    n = 0
    for i in range(1000000000):
        n = n + 1
    return n
`
	def, err := flow.Parse(script)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = Run(ctx, repo, nil, def, script, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "flow forever")
}

func TestMeIsTheIdentity(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def who():
    """Who am I."""
    return me()
`, nil)
	require.NoError(t, err)

	identity := value.(map[string]any)
	require.Equal(t, "John Doe", identity["name"])
	require.NotEmpty(t, identity["id"])
}

func TestPrintGoesToStderr(t *testing.T) {
	repo := testRepo(t)

	value, stderr, err := run(t, repo, `def noisy():
    """Say something."""
    print("hello")
    return 1
`, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, value)
	require.Equal(t, "hello\n", stderr)
}

func TestNoneReturnsNothing(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def quiet():
    """Return nothing."""
    issue.new({"fields": {"title": "made"}})
`, nil)
	require.NoError(t, err)
	require.Nil(t, value)
	require.Len(t, repo.Issues().AllIds(), 1)
}

func TestCommentsAndTheWholeIssue(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def talk():
    """Create, comment, read back."""
    id = issue.new({"fields": {"title": "a title"}, "body": "the body"})
    issue.comment.new(id, "a comment")
    return issue.get(id)
`, nil)
	require.NoError(t, err)

	document := value.(map[string]any)
	comments := document["comments"].([]any)
	require.Len(t, comments, 2)
	require.Equal(t, "the body", comments[0].(map[string]any)["message"])
	require.Equal(t, "a comment", comments[1].(map[string]any)["message"])
}

func TestAddRemoveAndArchive(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def labels():
    """Add, remove, archive."""
    id = issue.new({"fields": {"title": "a title"}})
    issue.add(id, labels=["area:core", "prio:high"])
    issue.remove(id, labels=["prio:high"])
    issue.archive(id)
    return issue.get(id)["fields"]
`, nil)
	require.NoError(t, err)

	fields := value.(map[string]any)
	require.Equal(t, []any{"area:core"}, fields["labels"])
	require.Equal(t, true, fields["archived"])
}

func TestFlowListAndGet(t *testing.T) {
	repo := testRepo(t)
	importFlow(t, repo, `def inner(n=1):
    """Double a number."""
    return n * 2
`)

	value, _, err := run(t, repo, `def look():
    """Read the flows."""
    return [flow.list(), flow.get("inner")["description"]]
`, nil)
	require.NoError(t, err)

	pair := value.([]any)
	listed := pair[0].([]any)
	require.Len(t, listed, 1)
	require.Equal(t, "inner", listed[0].(map[string]any)["name"])
	require.Equal(t, "Double a number.", pair[1])
}

func TestTheOnlyGlobalsAreTheHostModules(t *testing.T) {
	repo := testRepo(t)

	// `load` is not available, so a flow can only reach the host
	_, _, err := run(t, repo, `def sneaky():
    """Reach for something that is not there."""
    return schema
`, nil)
	require.Error(t, err)

	// and a module cannot be reassigned out from under the rest of the script
	_, _, err = run(t, repo, `def shadow():
    """Shadow a module."""
    return issue.absent()
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "absent")
}
