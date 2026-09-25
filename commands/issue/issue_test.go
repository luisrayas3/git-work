package issuecmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
)

func newTestEnv(t *testing.T) *execenv.Env {
	t.Helper()
	color.NoColor = true

	env := execenv.NewTestEnv(t)

	i, err := env.Backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(i))

	return env
}

// newTestIssue creates an issue through the command, so that every test runs
// over what the command line actually writes.
func newTestIssue(t *testing.T, env *execenv.Env, doc string) entity.Id {
	t.Helper()

	env.Out.Reset()
	require.NoError(t, runIssueNew(env, []string{doc}))
	id := entity.Id(strings.TrimSpace(env.Out.String()))
	require.NoError(t, id.Validate())
	env.Out.Reset()

	return id
}

func newDefaultIssue(t *testing.T, env *execenv.Env) entity.Id {
	t.Helper()
	return newTestIssue(t, env,
		`{"fields":{"title":"this is a title","status":"open"},"body":"this is a body"}`)
}

// commitCount counts the commits of an issue's ref: one write is one commit,
// however many operations it carries.
func commitCount(t *testing.T, env *execenv.Env, id entity.Id) int {
	t.Helper()
	commits, err := env.Repo.ListCommits(fmt.Sprintf("refs/%s/%s", issue.Namespace, id.String()))
	require.NoError(t, err)
	return len(commits)
}

func TestIssueNew(t *testing.T) {
	env := newTestEnv(t)

	require.NoError(t, runIssueNew(env, []string{
		`{"fields":{"title":"title","status":"open","estimate":3,"labels":["a","b"]},
		  "body":"message","aliases":{"jira":"PROJ-12"}}`,
	}))

	// the id, and nothing else
	id := entity.Id(strings.TrimSpace(env.Out.String()))
	require.NoError(t, id.Validate())
	require.Equal(t, id.String()+"\n", env.Out.String())

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	snap := i.Snapshot()
	require.Equal(t, "title", snap.Title())
	require.JSONEq(t, `"open"`, string(snap.Fields["status"]))
	require.JSONEq(t, `3`, string(snap.Fields["estimate"]))
	require.JSONEq(t, `["a","b"]`, string(snap.Fields["labels"]))
	require.Equal(t, "message", snap.Comments[0].Message)

	// the alias is create-op metadata, so that it can never change
	alias, ok := snap.GetCreateMetadata("alias:jira")
	require.True(t, ok)
	require.Equal(t, "PROJ-12", alias)
}

func TestIssueNewRefusals(t *testing.T) {
	env := newTestEnv(t)

	// a title is required, in fields
	require.Error(t, runIssueNew(env, []string{`{"body":"message"}`}))
	require.Error(t, runIssueNew(env, []string{`{"fields":{"status":"open"}}`}))
	// a title is a string
	require.Error(t, runIssueNew(env, []string{`{"fields":{"title":3}}`}))
	// an unknown key is a mistake, not a no-op
	require.Error(t, runIssueNew(env, []string{`{"fields":{"title":"t"},"titel":"t"}`}))
	// a bad field key is refused before anything is written
	require.Error(t, runIssueNew(env, []string{`{"fields":{"title":"t","Bad Key":1}}`}))
	// so is a document that is not one JSON object
	require.Error(t, runIssueNew(env, []string{`{"fields":{"title":"t"}} {"fields":{"title":"u"}}`}))
	require.Error(t, runIssueNew(env, []string{`not json`}))

	require.Empty(t, env.Backend.Issues().AllIds())
}

func TestIssueNewFromStdin(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.In.(*execenv.TestIn).WriteString(`{"fields":{"title":"piped"}}`)
	require.NoError(t, err)
	require.NoError(t, runIssueNew(env, []string{"-"}))

	id := entity.Id(strings.TrimSpace(env.Out.String()))
	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.Equal(t, "piped", i.Snapshot().Title())
}

func TestIssueSet(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	before := commitCount(t, env, id)

	// every key of one document lands in one commit
	require.NoError(t, runIssueSet(env, writeOptions{}, []string{
		id.Human(), `{"status":"closed","estimate":5,"note":"5"}`,
	}))
	require.Equal(t, "", env.Out.String())
	require.Equal(t, before+1, commitCount(t, env, id))

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	snap := i.Snapshot()
	require.JSONEq(t, `"closed"`, string(snap.Fields["status"]))
	require.JSONEq(t, `5`, string(snap.Fields["estimate"]))
	require.JSONEq(t, `"5"`, string(snap.Fields["note"]))

	// null clears, and the excerpt follows
	require.NoError(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{"estimate":null}`}))
	excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	_, ok := excerpt.Fields["estimate"]
	require.False(t, ok)
}

func TestIssueSetRefusals(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	at := commitCount(t, env, id)

	// the title is a field, but never an absent one
	require.Error(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{"title":null}`}))
	// a bad key fails the whole document, before anything is written
	require.Error(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{"status":"done","Bad Key":1}`}))
	// so does an empty document, or one that is not an object
	require.Error(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{}`}))
	require.Error(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `["status"]`}))

	require.Equal(t, at, commitCount(t, env, id))
	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.JSONEq(t, `"open"`, string(i.Snapshot().Fields["status"]))
}

func TestIssueAddRemove(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	other := newTestIssue(t, env, `{"fields":{"title":"another"}}`)
	before := commitCount(t, env, id)

	// items of two fields, four operations, one commit
	require.NoError(t, runIssueAdd(env, writeOptions{}, []string{
		id.Human(), `{"labels":["area:core","prio:high"],"blocks":["` + other.Human() + `"]}`,
	}))
	require.Equal(t, "", env.Out.String())
	require.Equal(t, before+1, commitCount(t, env, id))

	excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `["area:core","prio:high"]`, string(excerpt.Fields["labels"]))
	// With no schema, nothing knows which fields hold ids, so an item that is
	// the prefix of one issue is stored as its full id — the guess that keeps
	// a bootstrap repository able to write a parent.
	require.JSONEq(t, `["`+other.String()+`"]`, string(excerpt.Fields["blocks"]))

	// adding what is there is a no-op, removing takes one away
	require.NoError(t, runIssueAdd(env, writeOptions{}, []string{id.Human(), `{"labels":["area:core"]}`}))
	require.NoError(t, runIssueRemove(env, writeOptions{}, []string{id.Human(), `{"labels":["prio:high"]}`}))
	excerpt, err = env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `["area:core"]`, string(excerpt.Fields["labels"]))

	require.NoError(t, runIssueRemove(env, writeOptions{}, []string{id.Human(), `{"blocks":["` + other.Human() + `"]}`}))
	excerpt, err = env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(excerpt.Fields["blocks"]))

	// a list of no item, or a value that is not a list, is a usage error
	require.Error(t, runIssueAdd(env, writeOptions{}, []string{id.Human(), `{"labels":[]}`}))
	require.Error(t, runIssueAdd(env, writeOptions{}, []string{id.Human(), `{"labels":"area:core"}`}))
	// null is not an item: clearing a field is set's business
	require.Error(t, runIssueAdd(env, writeOptions{}, []string{id.Human(), `{"labels":[null]}`}))
}

// TestIssueAddResolvesOnlyRelations pins the thing the prefix lookup must not
// do once the schema can tell it apart: rewrite a label that happens to read
// like an id prefix (bb9e89e).
func TestIssueAddResolvesOnlyRelations(t *testing.T) {
	env := newTestEnv(t)
	_, _, err := host.SchemaInit(env.Backend, "jira", false)
	require.NoError(t, err)

	id := newTestIssue(t, env, `{"fields":{"title":"one","type":"task"}}`)
	other := newTestIssue(t, env, `{"fields":{"title":"two","type":"task"}}`)
	prefix := other.Human()

	require.NoError(t, runIssueAdd(env, writeOptions{}, []string{
		id.Human(), `{"blocks":["` + prefix + `"],"labels":["` + prefix + `"]}`,
	}))

	excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	// blocks is a multi-relation: its item is the issue it names
	require.JSONEq(t, `["`+other.String()+`"]`, string(excerpt.Fields["blocks"]))
	// labels is not: the item is the label it is, prefix-shaped or not
	require.JSONEq(t, `["`+prefix+`"]`, string(excerpt.Fields["labels"]))
}

func TestIssueDryRun(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	at := commitCount(t, env, id)

	require.NoError(t, runIssueSet(env, writeOptions{dryRun: true}, []string{
		id.Human(), `{"estimate":3,"status":"done"}`,
	}))

	var ops []struct {
		Type  int             `json:"type"`
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &ops))
	require.Len(t, ops, 2)
	// the operations come in key order, in their wire shape
	require.Equal(t, "estimate", ops[0].Key)
	require.JSONEq(t, `3`, string(ops[0].Value))
	require.Equal(t, "status", ops[1].Key)
	require.Equal(t, int(issue.SetFieldOp), ops[0].Type)

	// and nothing was written
	require.Equal(t, at, commitCount(t, env, id))
	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.JSONEq(t, `"open"`, string(i.Snapshot().Fields["status"]))

	env.Out.Reset()
	require.NoError(t, runIssueAdd(env, writeOptions{dryRun: true}, []string{id.Human(), `{"labels":["a","b"]}`}))
	var items []struct {
		Type int    `json:"type"`
		Item string `json:"item"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &items))
	require.Len(t, items, 2)
	require.Equal(t, int(issue.AddValueOp), items[0].Type)
	require.Equal(t, at, commitCount(t, env, id))
}

func TestIssueArchive(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)

	require.NoError(t, runIssueArchive(env, writeOptions{}, []string{id.Human()}))
	require.Equal(t, "", env.Out.String())

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.JSONEq(t, `true`, string(i.Snapshot().Fields[issue.ArchivedKey]))

	// the default list leaves it out
	require.NoError(t, runIssueList(env, issueListOptions{format: "json"}, nil))
	require.JSONEq(t, `[]`, env.Out.String())
}

func TestIssueGet(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	require.NoError(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{"labels":["area:core"]}`}))

	require.NoError(t, runIssueGet(env, issueGetOptions{format: "json"}, []string{id.Human()}))
	var got struct {
		Id       string                     `json:"id"`
		Fields   map[string]json.RawMessage `json:"fields"`
		Comments []struct{ Message string } `json:"comments"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &got))
	require.Equal(t, id.String(), got.Id)
	require.JSONEq(t, `["area:core"]`, string(got.Fields["labels"]))
	require.Len(t, got.Comments, 1)
	require.Equal(t, "this is a body", got.Comments[0].Message)

	env.Out.Reset()
	require.NoError(t, runIssueGet(env, issueGetOptions{format: "text"}, []string{id.Human()}))
	require.Contains(t, env.Out.String(), "[open] this is a title")
	require.Contains(t, env.Out.String(), `labels: ["area:core"]`)
}

func TestIssueLog(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)
	require.NoError(t, runIssueSet(env, writeOptions{}, []string{id.Human(), `{"status":"done"}`}))

	require.NoError(t, runIssueLog(env, issueLogOptions{format: "json"}, []string{id.Human()}))
	var entries []struct {
		Id       string `json:"id"`
		Type     string `json:"type"`
		UnixTime int64  `json:"unix_time"`
		Author   struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"author"`
		Op struct {
			Type  int    `json:"type"`
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"op"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &entries))
	require.Len(t, entries, 2)
	require.Equal(t, "create", entries[0].Type)
	require.Equal(t, "this is a title", entries[0].Op.Title)
	require.Equal(t, "set-field", entries[1].Type)
	require.Equal(t, "status", entries[1].Op.Key)
	require.Equal(t, "John Doe", entries[1].Author.Name)
	require.NotEmpty(t, entries[1].Author.Id)
	require.NotZero(t, entries[1].UnixTime)

	env.Out.Reset()
	require.NoError(t, runIssueLog(env, issueLogOptions{format: "text"}, []string{id.Human()}))
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 2)
	require.Contains(t, lines[1], "set-field")
}

func TestIssueComment(t *testing.T) {
	env := newTestEnv(t)
	id := newDefaultIssue(t, env)

	require.NoError(t, runIssueCommentNew(env, []string{id.Human(), "hello"}))
	commentId := strings.TrimSpace(env.Out.String())
	require.Equal(t, commentId+"\n", env.Out.String())

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.Len(t, i.Snapshot().Comments, 2)
	require.Equal(t, i.Snapshot().Comments[1].CombinedId().String(), commentId)

	env.Out.Reset()
	require.NoError(t, runIssueCommentEdit(env, []string{commentId, "edited"}))
	require.Equal(t, "", env.Out.String())

	i, err = env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.Equal(t, "edited", i.Snapshot().Comments[1].Message)

	// an empty body is a usage error
	require.Error(t, runIssueCommentNew(env, []string{id.Human(), ""}))
}

func TestIssueList(t *testing.T) {
	env := newTestEnv(t)
	first := newTestIssue(t, env, `{"fields":{"title":"first","status":"open","labels":["area:core"]}}`)
	second := newTestIssue(t, env, `{"fields":{"title":"second","status":"done"}}`)

	// the default program: everything unarchived, last edited first
	require.NoError(t, runIssueList(env, issueListOptions{format: "json"}, nil))
	var listed []struct {
		Id     string                     `json:"id"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &listed))
	require.Len(t, listed, 2)
	require.Equal(t, second.String(), listed[0].Id)
	require.Equal(t, first.String(), listed[1].Id)

	// a program of one's own, selecting on a field
	env.Out.Reset()
	require.NoError(t, runIssueList(env, issueListOptions{format: "json"},
		[]string{`map(select(.fields.labels // [] | index("area:core"))) | map(.fields.title)`}))
	require.JSONEq(t, `["first"]`, env.Out.String())

	// a program that emits several values prints one per line
	env.Out.Reset()
	require.NoError(t, runIssueList(env, issueListOptions{format: "json"}, []string{`.[] | .fields.title`}))
	require.Equal(t, "\"first\"\n\"second\"\n", env.Out.String())

	// text prints one line per issue when the values are issues
	env.Out.Reset()
	require.NoError(t, runIssueList(env, issueListOptions{format: "text"}, []string{`.`}))
	lines := strings.Split(strings.TrimSpace(env.Out.String()), "\n")
	require.Len(t, lines, 2)
	require.Equal(t, first.Human()+"\topen\tfirst", lines[0])
	require.Equal(t, second.Human()+"\tdone\tsecond", lines[1])

	// and falls back to JSON when they are not
	env.Out.Reset()
	require.NoError(t, runIssueList(env, issueListOptions{format: "text"}, []string{`map(.fields.title)`}))
	require.JSONEq(t, `["first","second"]`, env.Out.String())

	// a program that does not compile is an error, not an empty list
	require.Error(t, runIssueList(env, issueListOptions{format: "json"}, []string{`map(`}))
}

func TestIssueAliases(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env,
		`{"fields":{"title":"aliased"},"aliases":{"jira":"PROJ-12","github":"42"}}`)
	other := newTestIssue(t, env,
		`{"fields":{"title":"also aliased"},"aliases":{"jira":"PROJ-13"}}`)

	// an alias is accepted wherever an id is
	require.NoError(t, runIssueSet(env, writeOptions{}, []string{"PROJ-12", `{"status":"done"}`}))
	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.JSONEq(t, `"done"`, string(i.Snapshot().Fields["status"]))

	// any alias name does, not just the first
	env.Out.Reset()
	require.NoError(t, runIssueGet(env, issueGetOptions{format: "json"}, []string{"42"}))
	require.Contains(t, env.Out.String(), id.String())

	env.Out.Reset()
	require.NoError(t, runIssueCommentNew(env, []string{"PROJ-13", "by alias"}))
	i, err = env.Backend.Issues().Resolve(other)
	require.NoError(t, err)
	require.Len(t, i.Snapshot().Comments, 2)

	require.NoError(t, runIssueArchive(env, writeOptions{}, []string{"PROJ-13"}))
	i, err = env.Backend.Issues().Resolve(other)
	require.NoError(t, err)
	require.JSONEq(t, `true`, string(i.Snapshot().Fields[issue.ArchivedKey]))

	// an alias nobody carries is an error
	require.Error(t, runIssueGet(env, issueGetOptions{format: "json"}, []string{"PROJ-99"}))

	// an alias two issues carry is an error too, rather than a coin flip
	newTestIssue(t, env, `{"fields":{"title":"one"},"aliases":{"jira":"SHARED-1"}}`)
	newTestIssue(t, env, `{"fields":{"title":"two"},"aliases":{"github":"SHARED-1"}}`)
	require.Error(t, runIssueSet(env, writeOptions{}, []string{"SHARED-1", `{"status":"done"}`}))
}

func TestIssueRm(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env, `{"fields":{"title":"gone"},"aliases":{"jira":"PROJ-12"}}`)

	require.NoError(t, runIssueRm(env, []string{"PROJ-12"}))
	require.Equal(t, "", env.Out.String())
	require.Empty(t, env.Backend.Issues().AllIds())

	require.Error(t, runIssueRm(env, []string{id.Human()}))
}
