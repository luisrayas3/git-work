package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/repository"
)

// TestConcurrentWritersLoseOperations characterizes the hazard this package's
// write lock exists to prevent (e68d62b, 24e82d6).
//
// dag.Entity.Commit ends in repo.UpdateRef, which is a bare SetReference: it
// does not check what the ref pointed at. Two writers that read the same
// entity, each append an operation and each commit, both produce a commit
// parented on the tip they read, and the second SetReference overwrites the
// first. The losing operation is still in the object database, but nothing
// reaches it from the ref.
//
// This is not a bug to fix here: entity/dag and repository are upstream's,
// untouched by contract (//AGENTS.md), so a compare-and-swap ref update is not
// available to us. It is fixed a layer up, by never letting two writers into
// the read-modify-commit window at once. This test pins the reason that layer
// has to exist, so it stays a deliberate invariant rather than an accident.
func TestConcurrentWritersLoseOperations(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	author, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, author.Commit(repo))

	b, _, err := bug.Create(author, time.Now().Unix(), "title", "message", nil, nil)
	require.NoError(t, err)
	require.NoError(t, b.Commit(repo))

	// two processes, each having read the issue at the same tip
	first, err := bug.Read(repo, b.Id())
	require.NoError(t, err)
	second, err := bug.Read(repo, b.Id())
	require.NoError(t, err)

	_, _, err = bug.AddComment(first, author, time.Now().Unix(), "from the first writer", nil, nil)
	require.NoError(t, err)
	require.NoError(t, first.Commit(repo))

	_, _, err = bug.AddComment(second, author, time.Now().Unix(), "from the second writer", nil, nil)
	require.NoError(t, err)
	// no error: the second writer has no way to know it just erased the first
	require.NoError(t, second.Commit(repo))

	reread, err := bug.Read(repo, b.Id())
	require.NoError(t, err)

	var messages []string
	for _, comment := range reread.Compile().Comments {
		messages = append(messages, comment.Message)
	}

	// the loss, stated plainly: the second writer's comment is there, the
	// first writer's is not, and no error was returned to anyone.
	require.Equal(t, []string{"message", "from the second writer"}, messages)
}
