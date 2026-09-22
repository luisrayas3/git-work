package issue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/repository"
)

// TestIdsMatchBug pins the property the store migration (bf6f392) rests on:
// a Create or AddComment operation built from the same inputs and nonce
// serializes to the same bytes as entities/bug's, and so has the same id.
// This test leaves with entities/bug.
func TestIdsMatchBug(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()
	files := []repository.Hash{"hash1"}

	oldCreate := bug.NewCreateOp(rene, unix, "title", "message", files)
	oldCreate.SetMetadata("origin", "jira")
	newCreate := NewCreateOp(rene, unix, "title", "message", files, nil)
	newCreate.OpBase = oldCreate.OpBase

	oldBytes, err := json.Marshal(oldCreate)
	require.NoError(t, err)
	newBytes, err := json.Marshal(newCreate)
	require.NoError(t, err)
	require.Equal(t, string(oldBytes), string(newBytes))
	require.Equal(t, oldCreate.Id(), newCreate.Id())

	oldComment := bug.NewAddCommentOp(rene, unix, "comment", nil)
	newComment := NewAddCommentOp(rene, unix, "comment", nil)
	newComment.OpBase = oldComment.OpBase
	require.Equal(t, oldComment.Id(), newComment.Id())

	oldEdit := bug.NewEditCommentOp(rene, unix, oldComment.Id(), "edited", nil)
	newEdit := NewEditCommentOp(rene, unix, newComment.Id(), "edited", nil)
	newEdit.OpBase = oldEdit.OpBase
	require.Equal(t, oldEdit.Id(), newEdit.Id())
}
