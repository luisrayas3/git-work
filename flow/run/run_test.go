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
    a = work.issue.new({"fields": {"title": "first", "status": "open"}})
    work.issue.new({"fields": {"title": "second", "status": "done"}})
    work.issue.set(a, estimate=3)
    items = work.issue.list('map(select(.fields.status == "%s"))' % status)
    return work.view.board(items, columns="status", card_title="title")
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
	// work.issue.set landed, through the cache, as one commit
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
    return work.flow.run("inner", n=n) + 1
`, nil)
	require.NoError(t, err)
	require.EqualValues(t, 7, value)
}

func TestFlowRunHasADepthCap(t *testing.T) {
	repo := testRepo(t)

	// a cycle: each flow calls the other, forever
	importFlow(t, repo, `def ping():
    """Call pong."""
    return work.flow.run("pong")
`)
	importFlow(t, repo, `def pong():
    """Call ping."""
    return work.flow.run("ping")
`)

	_, _, err := run(t, repo, `def start():
    """Enter the cycle."""
    return work.flow.run("ping")
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
    return work.issue.get("deadbeef")
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "issue.get")
	require.Contains(t, err.Error(), "flow bad")
}

func TestViewErrorsReachTheScript(t *testing.T) {
	repo := testRepo(t)

	_, _, err := run(t, repo, `def bad():
    """A board with no columns."""
    return work.view.board([])
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	_, _, err = run(t, repo, `def bad():
    """A binding this view does not have."""
    return work.view.gantt([], start="a", end="b", colour="c")
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

func TestUserMeIsTheIdentity(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def who():
    """Who am I."""
    return work.user.me()
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
    work.issue.new({"fields": {"title": "made"}})
`, nil)
	require.NoError(t, err)
	require.Nil(t, value)
	require.Len(t, repo.Issues().AllIds(), 1)
}

func TestCommentsAndTheWholeIssue(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def talk():
    """Create, comment, read back."""
    id = work.issue.new({"fields": {"title": "a title"}, "body": "the body"})
    work.issue.comment.new(id, "a comment")
    return work.issue.get(id)
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
    id = work.issue.new({"fields": {"title": "a title"}})
    work.issue.add(id, labels=["area:core", "prio:high"])
    work.issue.remove(id, labels=["prio:high"])
    work.issue.archive(id)
    return work.issue.get(id)["fields"]
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
    return [work.flow.list(), work.flow.get("inner")["description"]]
`, nil)
	require.NoError(t, err)

	pair := value.([]any)
	listed := pair[0].([]any)
	require.Len(t, listed, 1)
	require.Equal(t, "inner", listed[0].(map[string]any)["name"])
	require.Equal(t, "Double a number.", pair[1])
}

func TestTheOnlyGlobalIsTheWorkModule(t *testing.T) {
	repo := testRepo(t)

	// `load` is not available, so a flow can only reach the host
	_, _, err := run(t, repo, `def sneaky():
    """Reach for something that is not there."""
    return bridge
`, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bridge")

	// the SDK is one name, so every old top-level name is gone
	for _, name := range []string{"issue", "schema", "flow", "view", "me"} {
		_, _, err = run(t, repo, `def bare():
    """Reach for a name the SDK no longer predeclares."""
    return `+name+`
`, nil)
		require.ErrorContains(t, err, name, "%s is not predeclared any more", name)
	}

	// `work` is, and a verb it does not have is the script's mistake
	_, _, err = run(t, repo, `def absent():
    """Call a verb the SDK does not have."""
    return work.issue.absent()
`, nil)
	require.ErrorContains(t, err, "absent")
}

// TestTheSDKNamesAreFreeForAScript is the whole point of one module:
// `issue` and `flow` read best as a script's own variables,
// and nothing but `work` is taken.
func TestTheSDKNamesAreFreeForAScript(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def local_names():
    """Name locals after the things they hold."""
    id = work.issue.new({"fields": {"title": "a title"}})
    issue = work.issue.get(id)
    flow = work.flow.list()
    schema = work.schema.export()
    view = work.view.list([issue])
    return [issue["fields"]["title"], len(flow), len(schema["types"]), view["view"]]
`, nil)
	require.NoError(t, err)

	got := value.([]any)
	require.Equal(t, "a title", got[0])
	require.EqualValues(t, 0, got[1])
	require.EqualValues(t, 0, got[2])
	require.Equal(t, "list", got[3])
}

func TestSchemaInitThenAWriteItValidates(t *testing.T) {
	repo := testRepo(t)

	// a preset's type and one of its statuses go through
	value, _, err := run(t, repo, `def setup():
    """Apply the jira preset and create a task."""
    work.schema.init()
    return work.issue.new({"fields": {"title": "a task", "type": "task", "status": "to-do"}})
`, nil)
	require.NoError(t, err)
	require.Len(t, repo.Issues().AllIds(), 1)

	id, ok := value.(string)
	require.True(t, ok)
	require.NotEmpty(t, id)

	// and a status the schema does not have is refused, naming the ones it has
	_, _, err = run(t, repo, `def refused():
    """Set a status that is not in the schema."""
    return work.issue.new({"fields": {"title": "another", "type": "task", "status": "shipped"}})
`, nil)
	require.ErrorContains(t, err, "valid values: backlog, to-do, in-progress, in-review, done, canceled")
	require.Len(t, repo.Issues().AllIds(), 1)

	// init refuses a second time, as the command does
	_, _, err = run(t, repo, `def again():
    """Apply the preset twice."""
    return work.schema.init()
`, nil)
	require.ErrorContains(t, err, "already has")
}

func TestSchemaExportRoundTripsThroughImport(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def roundtrip():
    """Write the schema back exactly as it was read."""
    work.schema.init()
    return [work.schema.export()["types"]["task"]["fields"]["status"]["kind"],
            work.schema.import_(work.schema.export())]
`, nil)
	require.NoError(t, err)

	pair := value.([]any)
	require.Equal(t, "enum", pair[0], "the document is the one --format json prints")
	require.Equal(t, []any{}, pair[1], "export | import emits no change")

	// a document the store differs from does emit one, and dry_run writes nothing
	value, _, err = run(t, repo, `def edit():
    """Rename one value of one field."""
    doc = work.schema.export()
    doc["types"]["task"]["fields"]["status"]["values"][0]["name"] = "Someday"
    return work.schema.import_(doc, dry_run=True)
`, nil)
	require.NoError(t, err)

	changes := value.([]any)
	require.Len(t, changes, 1)
	require.Equal(t, "update", changes[0].(map[string]any)["action"])
	require.Equal(t, "task/status", changes[0].(map[string]any)["key"])

	after, _, err := run(t, repo, `def unchanged():
    """Nothing was written, so the round trip is still clean."""
    return work.schema.import_(work.schema.export())
`, nil)
	require.NoError(t, err)
	require.Equal(t, []any{}, after)
}

func TestSchemaLogReturnsOperations(t *testing.T) {
	repo := testRepo(t)

	value, _, err := run(t, repo, `def history():
    """Who added a status, and when."""
    work.schema.init()
    return [work.schema.log("task/status"), len(work.schema.log())]
`, nil)
	require.NoError(t, err)

	pair := value.([]any)
	entries := pair[0].([]any)
	require.NotEmpty(t, entries)

	first := entries[0].(map[string]any)
	require.Equal(t, "create", first["type"])
	require.Equal(t, "field", first["shape"])
	require.Equal(t, "task/status", first["key"])
	require.Equal(t, "John Doe", first["author"].(map[string]any)["name"])

	// with no key, every entity's operations
	require.Greater(t, pair[1], float64(len(entries)))
}

func TestSchemaArchiveAndRmFromAScript(t *testing.T) {
	repo := testRepo(t)

	_, stderr, err := run(t, repo, `def clean():
    """Archive a field, archive a type, then drop a local ref."""
    work.schema.init()
    work.schema.archive("task/estimate")
    work.schema.archive("bug")
    work.schema.rm("task/due")
`, nil)
	require.NoError(t, err)
	require.Contains(t, stderr, "still live on the archived type bug")

	s, err := repo.LoadSchema()
	require.NoError(t, err)
	_, ok := s.Field("task", "estimate")
	require.False(t, ok)
	_, ok = s.Field("task", "due")
	require.False(t, ok)
}
