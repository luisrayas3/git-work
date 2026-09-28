package jira_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiratest"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// world is one clone bound to one fake site: a real repository, the cache,
// and the real client, through host.JiraSync as the command runs it.
type world struct {
	t    *testing.T
	srv  *jiratest.Server
	repo repository.TestedRepo
	c    *cache.RepoCache
}

// newWorld has no index lag unless opts set one: the scenarios that are
// about lag say so.
func newWorld(t *testing.T, opts ...jiratest.Option) *world {
	t.Helper()
	srv := jiratest.New(t, append([]jiratest.Option{jiratest.WithIndexLag(0, 0)}, opts...)...)
	return newClone(t, srv, repository.CreateGoGitTestRepo(t, false), true)
}

// newClone binds repo to srv; review is the first mapping, reviewed and
// imported (JS5).
func newClone(t *testing.T, srv *jiratest.Server, repo repository.TestedRepo, review bool) *world {
	t.Helper()
	c, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	me, err := c.Identities().New("Runner", "runner@example.com")
	require.NoError(t, err)
	require.NoError(t, c.SetUserIdentity(me))

	email, token := srv.Credentials()
	cfg := repo.LocalConfig()
	require.NoError(t, cfg.StoreString(host.JiraURLKey, srv.URL()))
	require.NoError(t, cfg.StoreString(host.JiraProjectKey, "PROJ"))
	require.NoError(t, cfg.StoreString(host.JiraEmailKey, email))
	t.Setenv(host.JiraTokenEnv, token)

	w := &world{t: t, srv: srv, repo: repo, c: c}
	if review {
		preset, err := schema.Preset("jira")
		require.NoError(t, err)
		_, _, err = host.SchemaImport(c, preset, false, false)
		require.NoError(t, err)
		doc, _, err := host.JiraSchema(context.Background(), c)
		require.NoError(t, err)
		_, _, err = host.SchemaImport(c, doc, false, false)
		require.NoError(t, err)
	}
	return w
}

// sync runs one sync and returns its issue lines and summary.
func (w *world) sync(opts jira.Options) ([]jira.Line, jira.Summary, error) {
	w.t.Helper()
	var lines []jira.Line
	sum, _, err := host.JiraSync(context.Background(), w.c, opts, func(l jira.Line) {
		if l.Summary == nil && l.Schema == nil {
			lines = append(lines, l)
		}
	})
	return lines, sum, err
}

func (w *world) mustSync(opts jira.Options) ([]jira.Line, jira.Summary) {
	w.t.Helper()
	lines, sum, err := w.sync(opts)
	require.NoError(w.t, err)
	require.Zero(w.t, sum.Failed, "%+v", lines)
	return lines, sum
}

// quiet asserts a run that writes nothing on either side (I1, JS14).
func (w *world) quiet() {
	w.t.Helper()
	w.srv.ResetRequests()
	before := w.refs()
	lines, _ := w.mustSync(jira.Options{})
	require.Empty(w.t, lines)
	require.Zero(w.t, w.srv.Writes(), "no Jira write")
	require.Equal(w.t, before, w.refs(), "no local commit")
}

func (w *world) refs() map[string]string {
	w.t.Helper()
	refs, err := w.repo.ListRefs("refs/")
	require.NoError(w.t, err)
	out := map[string]string{}
	for _, r := range refs {
		h, err := w.repo.ResolveRef(r)
		require.NoError(w.t, err)
		out[r] = string(h)
	}
	return out
}

func (w *world) byKey(key string) *cache.IssueCache {
	w.t.Helper()
	ic, err := w.c.Issues().ResolvePrefixOrAlias(key)
	require.NoError(w.t, err)
	return ic
}

func field(t *testing.T, ic *cache.IssueCache, key string) string {
	t.Helper()
	v, ok := ic.Snapshot().Fields[key]
	if !ok {
		return ""
	}
	return string(v)
}

func (w *world) newLocal(title, body string, fields map[string]issue.Value) entity.Id {
	w.t.Helper()
	if fields == nil {
		fields = map[string]issue.Value{}
	}
	if _, ok := fields["type"]; !ok {
		fields["type"] = issue.StringValue("task")
	}
	ic, _, err := w.c.Issues().New(title, body, fields)
	require.NoError(w.t, err)
	return ic.Id()
}

func (w *world) set(id entity.Id, key string, v issue.Value) {
	w.t.Helper()
	ic, err := w.c.Issues().Resolve(id)
	require.NoError(w.t, err)
	ops, err := ic.PlanSetFields(map[string]issue.Value{key: v})
	require.NoError(w.t, err)
	require.NoError(w.t, ic.CommitOperations(ops))
}

func TestSmoke(t *testing.T) {
	w := newWorld(t)
	key := w.srv.CreateIssue(jiratest.IssueSpec{Project: "PROJ", Type: "Task", Summary: "From Jira", Description: "hello"})
	lines, sum := w.mustSync(jira.Options{})
	t.Logf("%+v %+v", lines, sum)
	ic := w.byKey(key)
	require.Equal(t, `"From Jira"`, field(t, ic, "title"))
	w.quiet()
}
