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

// TestConcurrentCacheWritersKeepBothOperations is the same race as above, run
// through the cache the way every writer actually reaches the store. Both
// operations have to survive: the second writer's commit is rebased onto the
// first writer's, under the write lock (d35de2e, 2a51f66).
func TestConcurrentCacheWritersKeepBothOperations(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	// two caches over one repository, standing in for two processes. Opening
	// the second one at all is part of what is being tested: the old
	// process-lifetime lock made this an error.
	first, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	second, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	author, err := first.Identities().New("René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, first.SetUserIdentity(author))

	b, _, err := first.Bugs().New("title", "message")
	require.NoError(t, err)

	secondAuthor, err := second.Identities().Resolve(author.Id())
	require.NoError(t, err)
	require.NoError(t, second.SetUserIdentity(secondAuthor))

	// both read the issue at the same tip, before either has written
	fromFirst, err := first.Bugs().Resolve(b.Id())
	require.NoError(t, err)
	fromSecond, err := second.Bugs().Resolve(b.Id())
	require.NoError(t, err)

	_, _, err = fromFirst.AddComment("from the first writer")
	require.NoError(t, err)
	require.NoError(t, fromFirst.Commit())

	_, _, err = fromSecond.AddComment("from the second writer")
	require.NoError(t, err)
	require.NoError(t, fromSecond.Commit())

	reread, err := bug.Read(repo, b.Id())
	require.NoError(t, err)

	var messages []string
	for _, comment := range reread.Compile().Comments {
		messages = append(messages, comment.Message)
	}
	require.Equal(t, []string{"message", "from the first writer", "from the second writer"}, messages)
}
