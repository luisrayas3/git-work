package flowcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
)

const boardFlow = `def board(iteration="current", limit=20):
    """Kanban of one iteration, a column per status."""
    return None
`

const reportFlow = `def report(since):
    """Weekly status, generated from the op log."""
    return None
`

func newTestEnv(t *testing.T) *execenv.Env {
	t.Helper()
	color.NoColor = true

	env := execenv.NewTestEnv(t)

	i, err := env.Backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(i))

	return env
}

// writeFlow writes a script to a file, the way a human authoring one would.
func writeFlow(t *testing.T, dir string, name string, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o644))
	return path
}

// importFlows runs an import and returns the ids it printed.
func importFlows(t *testing.T, env *execenv.Env, opts flowImportOptions, args ...string) []string {
	t.Helper()
	env.Out.Reset()
	require.NoError(t, runFlowImport(env, opts, args))
	out := strings.TrimSpace(env.Out.String())
	env.Out.Reset()
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// operationCount counts the operations of every flow, through `flow log`.
// A write that emits nothing has to leave this untouched.
func operationCount(t *testing.T, env *execenv.Env) int {
	t.Helper()
	env.Out.Reset()
	require.NoError(t, runFlowLog(env, flowLogOptions{format: "json"}, nil))
	out := strings.TrimSpace(env.Out.String())
	env.Out.Reset()
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

func listEntries(t *testing.T, env *execenv.Env) []host.FlowEntry {
	t.Helper()
	env.Out.Reset()
	require.NoError(t, runFlowList(env, flowListOptions{format: "json"}))
	var entries []host.FlowEntry
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &entries))
	env.Out.Reset()
	return entries
}

func TestFlowImportFile(t *testing.T) {
	env := newTestEnv(t)
	path := writeFlow(t, t.TempDir(), "anything.star", boardFlow)

	ids := importFlows(t, env, flowImportOptions{}, path)
	require.Len(t, ids, 1)
	require.NoError(t, entity.Id(ids[0]).Validate())

	entries := listEntries(t, env)
	require.Len(t, entries, 1)
	require.Equal(t, "board", entries[0].Name)
	require.Equal(t, "Kanban of one iteration, a column per status.", entries[0].Description)
	require.False(t, entries[0].Archived)

	// the arguments come from the signature, defaults included
	require.Len(t, entries[0].Params, 2)
	require.Equal(t, "iteration", entries[0].Params[0].Name)
	require.False(t, entries[0].Params[0].Required)
	require.JSONEq(t, `"current"`, string(entries[0].Params[0].Default))
	require.Equal(t, "limit", entries[0].Params[1].Name)
	require.JSONEq(t, `20`, string(entries[0].Params[1].Default))
}

func TestFlowImportDirectory(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	writeFlow(t, dir, "board.star", boardFlow)
	writeFlow(t, dir, "report.star", reportFlow)
	// a file that is not a .star is not a flow, whatever it holds
	writeFlow(t, dir, "notes.md", "def nope():\n    pass\n")

	ids := importFlows(t, env, flowImportOptions{}, dir)
	require.Len(t, ids, 2)

	entries := listEntries(t, env)
	require.Len(t, entries, 2)
	require.Equal(t, "board", entries[0].Name)
	require.Equal(t, "report", entries[1].Name)

	// a parameter with no default is required and has no default printed
	require.Len(t, entries[1].Params, 1)
	require.Equal(t, "since", entries[1].Params[0].Name)
	require.True(t, entries[1].Params[0].Required)
	require.Nil(t, entries[1].Params[0].Default)
}

func TestFlowImportStdin(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.In.(*execenv.TestIn).WriteString(boardFlow)
	require.NoError(t, err)

	ids := importFlows(t, env, flowImportOptions{}, "-")
	require.Len(t, ids, 1)

	entries := listEntries(t, env)
	require.Len(t, entries, 1)
	require.Equal(t, "board", entries[0].Name)
}

func TestFlowImportUnchanged(t *testing.T) {
	env := newTestEnv(t)
	path := writeFlow(t, t.TempDir(), "board.star", boardFlow)

	importFlows(t, env, flowImportOptions{}, path)
	before := operationCount(t, env)

	// the same file again: no id printed, and no operation written
	ids := importFlows(t, env, flowImportOptions{}, path)
	require.Empty(t, ids)
	require.Equal(t, before, operationCount(t, env))
}

func TestFlowImportUpdate(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	path := writeFlow(t, dir, "board.star", boardFlow)

	importFlows(t, env, flowImportOptions{}, path)
	before := operationCount(t, env)

	changed := `def board(iteration="next", limit=20):
    """A kanban, revised."""
    return None
`
	writeFlow(t, dir, "board.star", changed)

	// an update creates nothing, so it prints nothing
	ids := importFlows(t, env, flowImportOptions{}, path)
	require.Empty(t, ids)
	// one Set for the script and one for the description, in one commit
	require.Equal(t, before+2, operationCount(t, env))

	entries := listEntries(t, env)
	require.Len(t, entries, 1)
	require.Equal(t, "A kanban, revised.", entries[0].Description)
	require.JSONEq(t, `"next"`, string(entries[0].Params[0].Default))

	env.Out.Reset()
	require.NoError(t, runFlowExport(env, flowExportOptions{}, []string{"board"}))
	require.Equal(t, changed, env.Out.String())
}

func TestFlowImportPrune(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	writeFlow(t, dir, "board.star", boardFlow)
	writeFlow(t, dir, "report.star", reportFlow)
	importFlows(t, env, flowImportOptions{}, dir)
	require.Len(t, listEntries(t, env), 2)

	// without --prune, importing one file leaves the other alone
	only := writeFlow(t, t.TempDir(), "board.star", boardFlow)
	importFlows(t, env, flowImportOptions{}, only)
	require.Len(t, listEntries(t, env), 2)

	// with it, what the inputs do not mention is archived
	pruned, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, "report")
	require.NoError(t, err)
	importFlows(t, env, flowImportOptions{prune: true}, only)
	entries := listEntries(t, env)
	require.Len(t, entries, 1)
	require.Equal(t, "board", entries[0].Name)

	// archived, not removed: the entity is still there
	require.Len(t, env.Backend.Flows().AllIds(), 2)
	// and the archive is committed, not left on the cached entity
	require.True(t, archivedInGit(t, env, pruned.Id()))
}

func TestFlowImportDryRun(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	writeFlow(t, dir, "board.star", boardFlow)
	writeFlow(t, dir, "report.star", reportFlow)

	importFlows(t, env, flowImportOptions{}, filepath.Join(dir, "board.star"))
	at := operationCount(t, env)

	env.Out.Reset()
	require.NoError(t, runFlowImport(env, flowImportOptions{dryRun: true, prune: true}, []string{
		filepath.Join(dir, "report.star"),
	}))

	var changes []struct {
		Name    string                     `json:"name"`
		Action  string                     `json:"action"`
		Changes map[string]json.RawMessage `json:"changes"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &changes))
	require.Len(t, changes, 2)
	require.Equal(t, "board", changes[0].Name)
	require.Equal(t, host.FlowActionArchive, changes[0].Action)
	require.Equal(t, "report", changes[1].Name)
	require.Equal(t, host.FlowActionCreate, changes[1].Action)
	require.JSONEq(t, `"Weekly status, generated from the op log."`,
		string(changes[1].Changes["description"]))

	// and nothing was written
	env.Out.Reset()
	require.Equal(t, at, operationCount(t, env))
	require.Len(t, listEntries(t, env), 1)

	// an unchanged flow is reported too, so that a plan lists every input
	env.Out.Reset()
	require.NoError(t, runFlowImport(env, flowImportOptions{dryRun: true}, []string{
		filepath.Join(dir, "board.star"),
	}))
	var unchanged []struct {
		Name    string                     `json:"name"`
		Action  string                     `json:"action"`
		Changes map[string]json.RawMessage `json:"changes"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &unchanged))
	require.Len(t, unchanged, 1)
	require.Equal(t, host.FlowActionUnchanged, unchanged[0].Action)
	require.Empty(t, unchanged[0].Changes)
}

func TestFlowImportRefusals(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	good := writeFlow(t, dir, "board.star", boardFlow)

	for name, script := range map[string]string{
		"two defs":        "def a():\n    pass\n\ndef b():\n    pass\n",
		"load":            "load('other.star', 'helper')\n\ndef a():\n    pass\n",
		"bare expression": "def a():\n    pass\n\na()\n",
		"no def":          "x = 1\n",
	} {
		bad := writeFlow(t, dir, "bad.star", script)
		// the good file comes first: it must not be written either
		err := runFlowImport(env, flowImportOptions{}, []string{good, bad})
		require.Error(t, err, name)
		require.Contains(t, err.Error(), "bad.star", name)
		require.Empty(t, env.Backend.Flows().AllIds(), name)
		require.Equal(t, "", env.Out.String(), name)
	}

	// two files defining the same flow are a mistake, not a race
	writeFlow(t, dir, "again.star", boardFlow)
	require.Error(t, runFlowImport(env, flowImportOptions{},
		[]string{good, filepath.Join(dir, "again.star")}))
	require.Empty(t, env.Backend.Flows().AllIds())

	// a file that is not there is an error, not an empty import
	require.Error(t, runFlowImport(env, flowImportOptions{}, []string{filepath.Join(dir, "absent.star")}))
}

func TestFlowListText(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	writeFlow(t, dir, "board.star", boardFlow)
	writeFlow(t, dir, "report.star", reportFlow)
	importFlows(t, env, flowImportOptions{}, dir)

	env.Out.Reset()
	require.NoError(t, runFlowList(env, flowListOptions{format: "text"}))
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 2)
	require.Equal(t, "board\tKanban of one iteration, a column per status.", lines[0])
	require.Equal(t, "report\tWeekly status, generated from the op log.", lines[1])

	// an empty store lists an empty array, not null
	other := newTestEnv(t)
	other.Out.Reset()
	require.NoError(t, runFlowList(other, flowListOptions{format: "json"}))
	require.JSONEq(t, `[]`, other.Out.String())
}

func TestFlowExportOne(t *testing.T) {
	env := newTestEnv(t)
	path := writeFlow(t, t.TempDir(), "board.star", boardFlow)
	importFlows(t, env, flowImportOptions{}, path)

	// the script, verbatim: what import takes back unchanged
	env.Out.Reset()
	require.NoError(t, runFlowExport(env, flowExportOptions{}, []string{"board"}))
	require.Equal(t, boardFlow, env.Out.String())

	// a flow nobody defined is an error
	require.Error(t, runFlowExport(env, flowExportOptions{}, []string{"absent"}))
}

func TestFlowExportRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	writeFlow(t, dir, "one.star", boardFlow)
	writeFlow(t, dir, "two.star", reportFlow)
	importFlows(t, env, flowImportOptions{}, dir)
	at := operationCount(t, env)

	// export --all names each file after its flow, and prints nothing
	out := t.TempDir()
	env.Out.Reset()
	require.NoError(t, runFlowExport(env, flowExportOptions{all: true}, []string{filepath.Join(out, "flows")}))
	require.Equal(t, "", env.Out.String())

	written, err := os.ReadFile(filepath.Join(out, "flows", "board.star"))
	require.NoError(t, err)
	require.Equal(t, boardFlow, string(written))

	// export | import is a no-op, which is the round-trip test
	importFlows(t, env, flowImportOptions{prune: true}, filepath.Join(out, "flows"))
	require.Equal(t, at, operationCount(t, env))

	// and so is one flow through standard input
	env.Out.Reset()
	require.NoError(t, runFlowExport(env, flowExportOptions{}, []string{"board"}))
	_, err = env.In.(*execenv.TestIn).WriteString(env.Out.String())
	require.NoError(t, err)
	env.Out.Reset()
	require.Empty(t, importFlows(t, env, flowImportOptions{}, "-"))
	require.Equal(t, at, operationCount(t, env))
}

func TestFlowLog(t *testing.T) {
	env := newTestEnv(t)
	dir := t.TempDir()
	path := writeFlow(t, dir, "board.star", boardFlow)
	importFlows(t, env, flowImportOptions{}, path)

	env.Out.Reset()
	require.NoError(t, runFlowLog(env, flowLogOptions{format: "json"}, []string{"board"}))
	// a create with attributes is one Create and one Set per attribute,
	// in one commit
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 3)

	var entry struct {
		Id       string                `json:"id"`
		Type     string                `json:"type"`
		UnixTime int64                 `json:"unix_time"`
		Author   struct{ Name string } `json:"author"`
		Op       json.RawMessage       `json:"op"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
	require.Equal(t, "create", entry.Type)
	require.Equal(t, "John Doe", entry.Author.Name)
	require.NotZero(t, entry.UnixTime)
	require.NotEmpty(t, entry.Id)
	require.Contains(t, string(entry.Op), `"board"`)

	// an archive is an operation like any other
	require.NoError(t, runFlowArchive(env, []string{"board"}))
	env.Out.Reset()
	require.NoError(t, runFlowLog(env, flowLogOptions{format: "json"}, []string{"board"}))
	lines = strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 4)
	require.Contains(t, lines[3], `"set-archived"`)
}

// archivedInGit reads the entity back out of the refs, the way a rebuilt
// cache does.
//
// An archive that is only appended to the cached entity reads as archived
// everywhere in this process, and in the cache file this process writes at
// close, and is gone the moment the cache is rebuilt from git — which is why
// the assertion that matters is this one and not a listing (589ff1d).
func archivedInGit(t *testing.T, env *execenv.Env, id entity.Id) bool {
	t.Helper()
	stored, err := config.Flows.Read(env.Repo, id)
	require.NoError(t, err)
	return stored.Compile().Archived
}

func TestFlowArchive(t *testing.T) {
	env := newTestEnv(t)
	path := writeFlow(t, t.TempDir(), "board.star", boardFlow)
	importFlows(t, env, flowImportOptions{}, path)
	id := env.Backend.Flows().AllIds()[0]

	env.Out.Reset()
	require.NoError(t, runFlowArchive(env, []string{"board"}))
	require.Equal(t, "", env.Out.String())

	require.Empty(t, listEntries(t, env))
	// the ref is still there: archive is replicated, not local
	require.Len(t, env.Backend.Flows().AllIds(), 1)
	// and it is in the ref, not only in the cache
	require.True(t, archivedInGit(t, env, id))

	// an importable name again: the archived entity is not in the way
	ids := importFlows(t, env, flowImportOptions{}, path)
	require.Len(t, ids, 1)
	require.Len(t, listEntries(t, env), 1)

	require.Error(t, runFlowArchive(env, []string{"absent"}))
}

func TestFlowRm(t *testing.T) {
	env := newTestEnv(t)
	path := writeFlow(t, t.TempDir(), "board.star", boardFlow)
	importFlows(t, env, flowImportOptions{}, path)

	env.Out.Reset()
	require.NoError(t, runFlowRm(env, []string{"board"}))
	require.Equal(t, "", env.Out.String())
	require.Empty(t, env.Backend.Flows().AllIds())
	require.Empty(t, listEntries(t, env))

	require.Error(t, runFlowRm(env, []string{"board"}))
}

func TestFlowDuplicatesWarn(t *testing.T) {
	env := newTestEnv(t)

	// two clones defining the same flow before either pushes (E7)
	for range 2 {
		_, _, err := env.Backend.Flows().New(config.ShapeFlow, "board", map[string]config.Value{
			attrScript: config.StringValue(boardFlow),
		})
		require.NoError(t, err)
	}

	env.Err.Reset()
	entries := listEntries(t, env)
	require.Len(t, entries, 1)
	require.Contains(t, env.Err.String(), "board")
	require.Contains(t, env.Err.String(), "ignored")
}
