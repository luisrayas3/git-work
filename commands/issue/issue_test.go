package issuecmd

import (
	"encoding/json"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
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

func newTestIssue(t *testing.T, env *execenv.Env) entity.Id {
	t.Helper()
	i, _, err := env.Backend.Issues().New("this is a title", "this is a body", map[string]issue.Value{
		"status": issue.StringValue("open"),
	})
	require.NoError(t, err)
	return i.Id()
}

func TestIssueNew(t *testing.T) {
	env := newTestEnv(t)

	err := runIssueNew(env, issueNewOptions{
		title:   "title",
		message: "message",
		fields:  []string{"status=open", "estimate=3", `labels=["a","b"]`},
	})
	require.NoError(t, err)
	require.Regexp(t, "^[0-9A-Fa-f]{7} created\n$", env.Out.String())

	ids := env.Backend.Issues().AllIds()
	require.Len(t, ids, 1)
	i, err := env.Backend.Issues().Resolve(ids[0])
	require.NoError(t, err)
	snap := i.Snapshot()
	require.Equal(t, "title", snap.Title())
	require.JSONEq(t, `"open"`, string(snap.Fields["status"]))
	require.JSONEq(t, `3`, string(snap.Fields["estimate"]))
	require.JSONEq(t, `["a","b"]`, string(snap.Fields["labels"]))
}

func TestIssueNewNeedsTitle(t *testing.T) {
	env := newTestEnv(t)
	require.Error(t, runIssueNew(env, issueNewOptions{message: "message"}))
}

func TestIssueSet(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env)

	require.NoError(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "status", "closed"}))
	require.NoError(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "estimate", "5"}))
	require.NoError(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "note", `"5"`}))

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	snap := i.Snapshot()
	require.JSONEq(t, `"closed"`, string(snap.Fields["status"]))
	require.JSONEq(t, `5`, string(snap.Fields["estimate"]))
	require.JSONEq(t, `"5"`, string(snap.Fields["note"]))

	// null clears, and the excerpt follows
	require.NoError(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "estimate", "null"}))
	excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	_, ok := excerpt.Fields["estimate"]
	require.False(t, ok)

	// the title is a field, but never an absent one
	require.Error(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "title", "null"}))
	require.Error(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "Bad Key", "x"}))

	// a value and items together, or neither, is a usage error
	require.Error(t, runIssueSet(env, issueSetOptions{add: []string{"x"}}, []string{id.Human(), "labels", "y"}))
	require.Error(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "labels"}))
}

func TestIssueSetItems(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env)
	other := newTestIssue(t, env)

	// items on a list field, added and removed, with set semantics
	require.NoError(t, runIssueSet(env, issueSetOptions{add: []string{"area:core", "prio:high"}}, []string{id.Human(), "labels"}))
	require.NoError(t, runIssueSet(env, issueSetOptions{add: []string{"area:core"}, remove: []string{"prio:high"}}, []string{id.Human(), "labels"}))
	excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `["area:core"]`, string(excerpt.Fields["labels"]))

	// a many-cardinality relation is a list of ids; a prefix resolves to the full id
	require.NoError(t, runIssueSet(env, issueSetOptions{add: []string{other.Human()}}, []string{id.Human(), "blocks"}))
	excerpt, err = env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `["`+other.String()+`"]`, string(excerpt.Fields["blocks"]))

	require.NoError(t, runIssueSet(env, issueSetOptions{remove: []string{other.Human()}}, []string{id.Human(), "blocks"}))
	excerpt, err = env.Backend.Issues().ResolveExcerpt(id)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(excerpt.Fields["blocks"]))
}

func TestIssueCommentNewAndEdit(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env)

	require.NoError(t, runIssueCommentNew(env, issueCommentMessageOptions{message: "hello"}, []string{id.Human()}))
	require.Regexp(t, "^[0-9A-Fa-f]{7} created\n$", env.Out.String())

	i, err := env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.Len(t, i.Snapshot().Comments, 2)
	commentId := i.Snapshot().Comments[1].CombinedId()

	require.NoError(t, runIssueCommentEdit(env, issueCommentMessageOptions{message: "edited"}, []string{commentId.Human()}))
	i, err = env.Backend.Issues().Resolve(id)
	require.NoError(t, err)
	require.Equal(t, "edited", i.Snapshot().Comments[1].Message)

	require.Error(t, runIssueCommentNew(env, issueCommentMessageOptions{}, []string{id.Human()}))
}

func TestIssueListAndShowJSON(t *testing.T) {
	env := newTestEnv(t)
	id := newTestIssue(t, env)
	require.NoError(t, runIssueSet(env, issueSetOptions{}, []string{id.Human(), "labels", `["area:core"]`}))

	// list, filtered on the labels field, as json
	err := runIssue(env, issueOptions{
		labelQuery:    []string{"area:core"},
		sortBy:        "creation",
		sortDirection: "asc",
		outputFormat:  "json",
	}, nil)
	require.NoError(t, err)

	var listed []struct {
		Id     string                     `json:"id"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(env.Out.String()), &listed))
	require.Len(t, listed, 1)
	require.Equal(t, id.String(), listed[0].Id)
	require.JSONEq(t, `"this is a title"`, string(listed[0].Fields["title"]))

	// a label that is not there filters it out
	env.Out.Reset()
	err = runIssue(env, issueOptions{
		labelQuery:    []string{"area:gui"},
		sortBy:        "creation",
		sortDirection: "asc",
		outputFormat:  "id",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "", env.Out.String())

	// status filters are refused rather than guessed
	err = runIssue(env, issueOptions{sortBy: "creation", sortDirection: "asc"}, []string{"status:open"})
	require.Error(t, err)

	// show as json carries the fields verbatim
	env.Out.Reset()
	require.NoError(t, runIssueShow(env, issueShowOptions{format: "json"}, []string{id.Human()}))
	var shown struct {
		Fields   map[string]json.RawMessage `json:"fields"`
		Comments []struct{ Message string } `json:"comments"`
	}
	require.NoError(t, json.Unmarshal([]byte(env.Out.String()), &shown))
	require.JSONEq(t, `["area:core"]`, string(shown.Fields["labels"]))
	require.Len(t, shown.Comments, 1)
	require.Equal(t, "this is a body", shown.Comments[0].Message)

	// the default view names the status and the title
	env.Out.Reset()
	require.NoError(t, runIssueShow(env, issueShowOptions{format: "default"}, []string{id.Human()}))
	require.Contains(t, env.Out.String(), "[open] this is a title")
	require.Contains(t, env.Out.String(), `labels: ["area:core"]`)
}
