package migrate

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// repoSchema compiles the repository's own schema.yaml, the one the
// migration is written against.
func repoSchema(t *testing.T) *schema.Schema {
	t.Helper()
	data, err := os.ReadFile("../schema.yaml")
	require.NoError(t, err)
	doc, err := schema.ParseDocument(data)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))

	changes, err := schema.Reconcile(doc, nil, false)
	require.NoError(t, err)
	var entries []schema.Entry
	for i, change := range changes {
		require.Equal(t, schema.ActionCreate, change.Action)
		attributes := map[string]config.Value{}
		for name, value := range change.Set {
			attributes[name] = value
		}
		entries = append(entries, schema.Entry{
			Id:         entity.Id(fmt.Sprintf("%040x", i+1)),
			Shape:      change.Shape,
			Key:        change.Key,
			Attributes: attributes,
		})
	}
	s, err := schema.Compile(entries)
	require.NoError(t, err)
	require.Empty(t, s.Problems)
	return s
}

// oldStore is a git-bug store the way the tracker was before the migration:
// identities under refs/identities/*, bugs under refs/issues/*.
type oldStore struct {
	repo  repository.ClockedRepo
	rene  *identity.Identity
	story *bug.Bug
	task  *bug.Bug
	quiet *bug.Bug
	// commentId is the comment added to task, then edited.
	commentId entity.CombinedId
}

func newOldStore(t *testing.T) *oldStore {
	t.Helper()
	repo := repository.NewMockRepo()

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(repo))
	// the identity landed under the new constants; put it where git-bug had it
	newRef := "refs/" + identity.Namespace + "/" + rene.Id().String()
	require.NoError(t, repo.CopyRef(newRef, oldIdentityPrefix+rene.Id().String()))
	require.NoError(t, repo.RemoveRef(newRef))

	unix := int64(1700000000)
	commit := func(b *bug.Bug) {
		require.NoError(t, b.Commit(repo))
	}

	// interleaved edits, so that the global edit order crosses issues
	story, _, err := bug.Create(rene, unix, "Story: the one", "body", nil, nil)
	require.NoError(t, err)
	commit(story) // create 1, edit 1
	task, _, err := bug.Create(rene, unix+1, "Task: do it", "body", nil, nil)
	require.NoError(t, err)
	commit(task) // create 2, edit 2

	_, _, err = bug.ChangeLabels(story, rene, unix+2, []string{"type:story", "area:core", "prio:high"}, nil, nil)
	require.NoError(t, err)
	commit(story) // edit 3
	_, _, err = bug.ChangeLabels(task, rene, unix+3,
		[]string{"type:spike", "area:cli", "area:core", "prio:med", "phase:4-flows", "story:" + story.Id().String()[:7], "wontfix"}, nil, nil)
	require.NoError(t, err)
	commit(task) // edit 4
	_, _, err = bug.ChangeLabels(task, rene, unix+4, []string{"type:task", "prio:low"}, []string{"type:spike", "area:core", "prio:med"}, nil)
	require.NoError(t, err)
	commit(task) // edit 5
	_, _, err = bug.ChangeLabels(task, rene, unix+5, nil, []string{"phase:4-flows"}, nil)
	require.NoError(t, err)
	commit(task) // edit 6

	_, err = bug.SetTitle(task, rene, unix+6, "Task: do it well", nil)
	require.NoError(t, err)
	commit(task) // edit 7
	_, err = bug.Close(task, rene, unix+7, nil)
	require.NoError(t, err)
	commit(task) // edit 8

	commentId, _, err := bug.AddComment(task, rene, unix+8, "a comment", nil, nil)
	require.NoError(t, err)
	commit(task) // edit 9
	comment, err := task.Compile().SearchComment(commentId)
	require.NoError(t, err)
	_, _, err = bug.EditComment(task, rene, unix+9, comment.TargetId(), "an edited comment", nil, nil)
	require.NoError(t, err)
	commit(task) // edit 10

	// one that never got a type, was reopened, and held two priorities at once
	quiet, _, err := bug.Create(rene, unix+10, "untyped", "body", nil, nil)
	require.NoError(t, err)
	commit(quiet) // create 3, edit 11
	_, err = bug.Close(quiet, rene, unix+11, nil)
	require.NoError(t, err)
	commit(quiet) // edit 12
	_, err = bug.Open(quiet, rene, unix+12, nil)
	require.NoError(t, err)
	commit(quiet) // edit 13
	_, _, err = bug.ChangeLabels(quiet, rene, unix+13, []string{"prio:med"}, nil, nil)
	require.NoError(t, err)
	commit(quiet) // edit 14
	_, _, err = bug.ChangeLabels(quiet, rene, unix+14, []string{"prio:high"}, nil, nil)
	require.NoError(t, err)
	commit(quiet) // edit 15
	_, _, err = bug.ChangeLabels(quiet, rene, unix+15, nil, []string{"prio:med"}, nil)
	require.NoError(t, err)
	commit(quiet) // edit 16

	return &oldStore{repo: repo, rene: rene, story: story, task: task, quiet: quiet, commentId: commentId}
}

func TestMigrate(t *testing.T) {
	old := newOldStore(t)
	repo := old.repo
	s := repoSchema(t)

	// nothing reads the old store before the identities are where the
	// constants now point
	_, err := Prepare(repo, s)
	require.Error(t, err)

	copied, err := CopyIdentities(repo)
	require.NoError(t, err)
	require.Equal(t, 1, copied)
	copied, err = CopyIdentities(repo)
	require.NoError(t, err)
	require.Equal(t, 0, copied, "a second copy is a no-op")
	stillThere, err := repo.RefExist(oldIdentityPrefix + old.rene.Id().String())
	require.NoError(t, err)
	require.True(t, stillThere, "the old ref is kept")

	plan, err := Prepare(repo, s)
	require.NoError(t, err)
	require.Len(t, plan.Issues, 3)
	require.Equal(t, old.story.Id(), plan.Issues[0].Id, "creation order")
	require.Equal(t, old.task.Id(), plan.Issues[1].Id)
	require.Equal(t, old.quiet.Id(), plan.Issues[2].Id)
	for _, i := range plan.Issues {
		require.Empty(t, i.Problems, "%s: %v", i.Title, i.Problems)
	}

	// nothing is written by a plan
	ids, err := issue.ListLocalIds(repo)
	require.NoError(t, err)
	require.Empty(t, ids)

	require.NoError(t, plan.Apply(repo))

	// the old store is intact
	oldIds, err := bug.ListLocalIds(repo)
	require.NoError(t, err)
	require.Len(t, oldIds, 3)

	// a second run refuses
	_, err = Prepare(repo, s)
	require.ErrorContains(t, err, "already holds 3 issues")

	// ids, comment ids, lamport times
	story, err := issue.Read(repo, old.story.Id())
	require.NoError(t, err)
	task, err := issue.Read(repo, old.task.Id())
	require.NoError(t, err)
	quiet, err := issue.Read(repo, old.quiet.Id())
	require.NoError(t, err)

	require.Equal(t, old.story.CreateLamportTime(), story.CreateLamportTime())
	require.Equal(t, old.story.EditLamportTime(), story.EditLamportTime())
	require.Equal(t, old.task.CreateLamportTime(), task.CreateLamportTime())
	require.Equal(t, old.task.EditLamportTime(), task.EditLamportTime())
	require.Equal(t, old.quiet.EditLamportTime(), quiet.EditLamportTime())

	taskSnap := task.Compile()
	require.Len(t, taskSnap.Comments, 2)
	require.Equal(t, old.commentId, taskSnap.Comments[1].CombinedId())
	require.Equal(t, "an edited comment", taskSnap.Comments[1].Message)
	require.Len(t, task.Operations(), 19, "every label became one operation, type and status two more in the create pack")

	// the fields the labels became
	fields := func(i *issue.Issue) map[string]string {
		out := map[string]string{}
		for key, value := range i.Compile().Fields {
			out[key] = string(value)
		}
		return out
	}
	require.Equal(t, map[string]string{
		"title":    `"Story: the one"`,
		"type":     `"story"`,
		"status":   `"to-do"`,
		"priority": `"high"`,
		"area":     `["core"]`,
	}, fields(story), "open was implicit; it is a field now")
	require.Equal(t, map[string]string{
		"title":    `"Task: do it well"`,
		"type":     `"task"`,
		"status":   `"done"`,
		"priority": `"low"`,
		"area":     `["cli"]`,
		"parent":   `"` + old.story.Id().String() + `"`,
		"labels":   `["wontfix"]`,
	}, fields(task), "spike came and went with its label, phase was cleared, core was removed")
	require.Equal(t, map[string]string{
		"title":    `"untyped"`,
		"type":     `"task"`,
		"status":   `"to-do"`,
		"priority": `"high"`,
	}, fields(quiet), "removing one of two priorities leaves the other")
	var quietNames []string
	for _, op := range quiet.Operations() {
		quietNames = append(quietNames, issue.OperationTypeName(op.Type()))
	}
	require.Equal(t, []string{
		"create", "set-field", "set-field", // type, status
		"set-field", "set-field", // closed, reopened
		"set-field", "set-field", // medium, high
		"noop", // med removed: high stays, the pack keeps its place
	}, quietNames)

	// the history reads as it did: the type is in the create pack, then the
	// labels in the order they were changed
	var names []string
	for _, op := range task.Operations() {
		names = append(names, issue.OperationTypeName(op.Type()))
	}
	require.Equal(t, []string{
		"create", "set-field", "set-field", // type (spike is a task), status
		"set-field", "set-field", "set-field", "add-value", "add-value", "add-value", "add-value", // prio, phase, parent; area cli, area core, label spike, label wontfix
		"set-field", "set-field", "remove-value", "remove-value", // type task, prio low; label spike, area core
		"set-field", // phase cleared
		"set-field", // title
		"set-field", // status
		"add-comment", "edit-comment",
	}, names)
}

func TestMigrateRollsBackOnFailure(t *testing.T) {
	old := newOldStore(t)
	repo := old.repo
	_, err := CopyIdentities(repo)
	require.NoError(t, err)
	plan, err := Prepare(repo, repoSchema(t))
	require.NoError(t, err)

	// break the second issue's history so that its commit fails
	plan.packs[1].ops = nil
	require.Error(t, plan.Apply(repo))

	ids, err := issue.ListLocalIds(repo)
	require.NoError(t, err)
	require.Empty(t, ids, "nothing left behind")
}

func TestMigrateNeedsTheSchema(t *testing.T) {
	old := newOldStore(t)
	_, err := CopyIdentities(old.repo)
	require.NoError(t, err)

	preset, err := schema.Preset("jira")
	require.NoError(t, err)
	changes, err := schema.Reconcile(preset, nil, false)
	require.NoError(t, err)
	var entries []schema.Entry
	for i, change := range changes {
		attributes := map[string]config.Value{}
		for name, value := range change.Set {
			attributes[name] = value
		}
		entries = append(entries, schema.Entry{Id: entity.Id(fmt.Sprintf("%040x", i+1)), Shape: change.Shape, Key: change.Key, Attributes: attributes})
	}
	s, err := schema.Compile(entries)
	require.NoError(t, err)

	_, err = Prepare(old.repo, s)
	require.ErrorContains(t, err, "decision/area")
	require.ErrorContains(t, err, "schema import schema.yaml")
}
