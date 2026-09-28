package issue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/repository"
)

func TestNoOpCarriesMetadataThroughGit(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(repo))

	unix := time.Now().Unix()
	i, _, err := Create(rene, unix, "title", "message", nil, nil, nil)
	require.NoError(t, err)

	op := NewNoOpOp(rene, unix, map[string]string{"jira-sync": `{"v":1}`})
	require.NoError(t, op.Validate())
	require.Equal(t, NoOpOp, op.Type())
	i.Append(op)
	require.NoError(t, i.Commit(repo))

	read, err := Read(repo, i.Id())
	require.NoError(t, err)
	snap := read.Compile()

	// the marker changes nothing a reader sees, and is in the history
	require.Equal(t, i.Compile().Fields, snap.Fields)
	require.Len(t, snap.Timeline, 1)
	require.Len(t, snap.Operations, 2)
	got := snap.Operations[1]
	require.Equal(t, NoOpOp, got.Type())
	require.Equal(t, op.Id(), got.Id())
	value, ok := got.GetMetadata("jira-sync")
	require.True(t, ok)
	require.Equal(t, `{"v":1}`, value)
}
