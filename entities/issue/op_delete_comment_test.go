package issue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

func deleteTestIssue(t *testing.T) (*Issue, identity.Interface, *AddCommentOperation) {
	t.Helper()
	repo := repository.NewMockRepoClock()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)

	unix := time.Now().Unix()
	i, _, err := Create(rene, unix, "title", "the description", nil, nil, nil)
	require.NoError(t, err)
	_, _, err = AddComment(i, rene, unix, "kept", nil, nil)
	require.NoError(t, err)
	_, add, err := AddComment(i, rene, unix, "on the wrong issue", nil, nil)
	require.NoError(t, err)
	return i, rene, add
}

func messages(snap *Snapshot) []string {
	var out []string
	for _, c := range snap.Comments {
		out = append(out, c.Message)
	}
	return out
}

func TestDeleteCommentHides(t *testing.T) {
	i, rene, add := deleteTestIssue(t)
	timeline := len(i.Compile().Timeline)

	_, err := DeleteComment(i, rene, time.Now().Unix(), add.Id(), nil)
	require.NoError(t, err)

	snap := i.Compile()
	require.Equal(t, []string{"the description", "kept"}, messages(snap))
	// the timeline and the operations are left alone (CD2)
	require.Len(t, snap.Timeline, timeline)
	require.Equal(t, DeleteCommentOp, snap.Operations[len(snap.Operations)-1].Type())
	require.Equal(t, "delete-comment", OperationTypeName(DeleteCommentOp))
}

func TestDeleteCommentOfTheDescriptionIsIgnored(t *testing.T) {
	i, rene, _ := deleteTestIssue(t)

	_, err := DeleteComment(i, rene, time.Now().Unix(), i.FirstOp().Id(), nil)
	require.NoError(t, err)

	require.Equal(t, []string{"the description", "kept", "on the wrong issue"}, messages(i.Compile()))
}

// An edit and a delete of one comment end deleted in either order (CD3).
func TestDeleteCommentWinsOverEdit(t *testing.T) {
	for _, editFirst := range []bool{true, false} {
		i, rene, add := deleteTestIssue(t)
		unix := time.Now().Unix()
		edit := NewEditCommentOp(rene, unix, add.Id(), "edited", nil)
		del := NewDeleteCommentOp(rene, unix, add.Id())
		if editFirst {
			i.Append(edit)
			i.Append(del)
		} else {
			i.Append(del)
			i.Append(edit)
		}

		require.Equal(t, []string{"the description", "kept"}, messages(i.Compile()), "edit first: %v", editFirst)
	}
}

func TestDeleteCommentTwiceIsANoOp(t *testing.T) {
	i, rene, add := deleteTestIssue(t)
	unix := time.Now().Unix()
	i.Append(NewDeleteCommentOp(rene, unix, add.Id()))
	i.Append(NewDeleteCommentOp(rene, unix+1, add.Id()))

	require.Equal(t, []string{"the description", "kept"}, messages(i.Compile()))
}

func TestDeleteCommentValidate(t *testing.T) {
	repo := repository.NewMockRepoClock()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)

	require.NoError(t, NewDeleteCommentOp(rene, time.Now().Unix(), entity.DeriveId([]byte("target"))).Validate())
	require.Error(t, NewDeleteCommentOp(rene, time.Now().Unix(), entity.UnsetId).Validate())
}

func TestDeleteCommentSerialize(t *testing.T) {
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*DeleteCommentOperation, entity.Resolvers) {
		return NewDeleteCommentOp(author, unixTime, "target"), nil
	})
}

// An older build reads type 12 as an unknown operation,
// which applies as a no-op: the comment still shows there (CD8).
func TestDeleteCommentAsUnknownIsANoOp(t *testing.T) {
	i, rene, add := deleteTestIssue(t)

	raw, err := json.Marshal(NewDeleteCommentOp(rene, time.Now().Unix(), add.Id()))
	require.NoError(t, err)
	unknown, err := dag.NewUnknownOp[*Snapshot](raw)
	require.NoError(t, err)
	i.Append(unknown)

	require.Equal(t, []string{"the description", "kept", "on the wrong issue"}, messages(i.Compile()))
}
