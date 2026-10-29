package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// migrateSchema holds every key migrate.Needs lists, and nothing more.
const migrateSchema = `
shared:
  status: &status
    kind: enum
    values: [{id: to-do, category: unstarted}, {id: done, category: completed}]
  priority: &priority
    kind: ordinal-enum
    values: [{id: high}, {id: medium}, {id: low}]
  phase: &phase
    kind: enum
    values: [{id: 4-flows}]
  area: &area
    kind: multi-enum
    values: [{id: core}]
  labels: &labels
    kind: multi-enum
    freeform: true
  parent: &parent
    kind: relation
    inverse: children
    target_types: [story]
types:
  story:
    fields:
      status: *status
      priority: *priority
      phase: *phase
      area: *area
      parent: *parent
      labels: *labels
  task:
    fields:
      status: *status
      priority: *priority
      phase: *phase
      area: *area
      parent: *parent
      labels: *labels
  decision:
    fields:
      status: *status
      priority: *priority
      phase: *phase
      area: *area
      parent: *parent
      labels: *labels
`

// legacyRemote is a bare remote holding a git-bug store, one identity under
// refs/identities/* and one bug under refs/issues/*, and nothing else.
func legacyRemote(t *testing.T) (remote repository.TestedRepo, bugId string) {
	t.Helper()

	remote = repository.CreateGoGitTestRepo(t, true)
	old := repository.CreateGoGitTestRepo(t, false)
	require.NoError(t, old.AddRemote("origin", remote.GetLocalRemote()))

	rene, err := identity.NewIdentity(old, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(old))
	// the identity landed under the new constants; put it where git-bug had it
	newRef := "refs/" + identity.Namespace + "/" + rene.Id().String()
	require.NoError(t, old.CopyRef(newRef, "refs/identities/"+rene.Id().String()))
	require.NoError(t, old.RemoveRef(newRef))

	b, _, err := bug.Create(rene, time.Now().Unix(), "an old one", "body", nil, nil)
	require.NoError(t, err)
	require.NoError(t, b.Commit(old))

	_, err = old.PushRefs("origin", "identities", bug.Namespace)
	require.NoError(t, err)
	return remote, b.Id().String()
}

// TestMigrateFrom: --from fetches git-bug's store, which pull no longer
// carries, into the refs the migration reads; --dry-run fetches it too and
// writes no issue (6274f33).
func TestMigrateFrom(t *testing.T) {
	remote, bugId := legacyRemote(t)

	env := execenv.NewTestEnv(t)
	repo := env.Repo.(repository.TestedRepo)
	require.NoError(t, repo.AddRemote("origin", remote.GetLocalRemote()))

	me, err := env.Backend.Identities().New("Alice", "alice@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(me))

	doc, err := schema.ParseDocument([]byte(migrateSchema))
	require.NoError(t, err)
	_, _, err = host.SchemaImport(env.Backend, doc, false, false)
	require.NoError(t, err)

	// a pull leaves the old store on the remote
	// (and a remote that holds nothing pull carries cannot be pulled from)
	require.NoError(t, runPush(env, nil))
	require.NoError(t, runPull(env, nil))
	old, err := repo.ListRefs("refs/" + bug.Namespace + "/")
	require.NoError(t, err)
	require.Empty(t, old)

	require.NoError(t, runMigrate(env, migrateOptions{dryRun: true, from: "origin"}))
	require.Contains(t, env.Out.String(), "fetched from origin: 1 identities and 1 issues new or updated")
	require.Contains(t, env.Out.String(), "dry run: nothing written")
	old, err = repo.ListRefs("refs/" + bug.Namespace + "/")
	require.NoError(t, err)
	require.Equal(t, []string{"refs/issues/" + bugId}, old)
	identities, err := repo.ListRefs("refs/identities/")
	require.NoError(t, err)
	require.Len(t, identities, 1)
	users, err := repo.ListRefs("refs/" + identity.Namespace + "/")
	require.NoError(t, err)
	require.Len(t, users, 2, "Alice, and the copied René")
	migrated, err := repo.ListRefs("refs/work-issues/")
	require.NoError(t, err)
	require.Empty(t, migrated)

	// the second fetch finds nothing new, and the migration runs as ever
	env.Out.Reset()
	require.NoError(t, runMigrate(env, migrateOptions{from: "origin"}))
	require.Contains(t, env.Out.String(), "fetched from origin: 0 identities and 0 issues new or updated")
	migrated, err = repo.ListRefs("refs/work-issues/")
	require.NoError(t, err)
	require.Equal(t, []string{"refs/work-issues/" + bugId}, migrated)
}
