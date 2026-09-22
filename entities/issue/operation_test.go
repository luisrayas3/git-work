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

func TestValidate(t *testing.T) {
	repo := repository.NewMockRepoClock()

	makeIdentity := func(t *testing.T, name, email string) *identity.Identity {
		i, err := identity.NewIdentity(repo, name, email)
		require.NoError(t, err)
		return i
	}

	rene := makeIdentity(t, "René Descartes", "rene@descartes.fr")
	target := entity.DeriveId([]byte("target"))

	unix := time.Now().Unix()

	good := []Operation{
		NewCreateOp(rene, unix, "title", "message", nil, nil),
		NewAddCommentOp(rene, unix, "message2", nil),
		NewEditCommentOp(rene, unix, target, "edited", nil),
		NewSetFieldOp(rene, unix, "status", StringValue("closed")),
		NewAddValueOp(rene, unix, "blocks", StringValue(target.String())),
		NewRemoveValueOp(rene, unix, "blocks", StringValue(target.String())),
	}

	for _, op := range good {
		require.NoError(t, op.Validate())
	}

	bad := []Operation{
		// opbase
		NewSetFieldOp(makeIdentity(t, "", "rene@descartes.fr"), unix, "status", StringValue("x")),
		NewSetFieldOp(makeIdentity(t, "René Descartes\u001b", "rene@descartes.fr"), unix, "status", StringValue("x")),
		NewSetFieldOp(makeIdentity(t, "René Descartes", "rene@descartes.fr\u001b"), unix, "status", StringValue("x")),
		NewSetFieldOp(makeIdentity(t, "René \nDescartes", "rene@descartes.fr"), unix, "status", StringValue("x")),
		NewSetFieldOp(makeIdentity(t, "René Descartes", "rene@\ndescartes.fr"), unix, "status", StringValue("x")),
		&CreateOperation{OpBase: dag.NewOpBase(CreateOp, rene, 0),
			Title:   "title",
			Message: "message",
		},

		NewCreateOp(rene, unix, "multi\nline", "message", nil, nil),
		NewCreateOp(rene, unix, "title", "message", []repository.Hash{repository.Hash("invalid")}, nil),
		NewCreateOp(rene, unix, "title\u001b", "message", nil, nil),
		NewCreateOp(rene, unix, "title", "message\u001b", nil, nil),
		NewAddCommentOp(rene, unix, "message\u001b", nil),
		NewAddCommentOp(rene, unix, "message", []repository.Hash{repository.Hash("invalid")}),
		NewEditCommentOp(rene, unix, entity.UnsetId, "edited", nil),
	}

	for i, op := range bad {
		require.Error(t, op.Validate(), "validation should have failed %d %v", i, op)
	}
}

func TestUnknownOpUnmarshaler(t *testing.T) {
	// An operation type that no version of this client knows about should be
	// preserved as an UnknownOperation rather than causing a panic.
	raw, err := json.Marshal(struct {
		Type      dag.OperationType `json:"type"`
		Timestamp int64             `json:"timestamp"`
		Nonce     []byte            `json:"nonce"`
		Extra     string            `json:"future_field"`
	}{
		Type:      dag.OperationType(999),
		Timestamp: time.Now().Unix(),
		Nonce:     make([]byte, 20),
		Extra:     "preserved",
	})
	require.NoError(t, err)

	op, err := operationUnmarshaler(raw, nil)
	require.NoError(t, err)

	unknown, ok := op.(*dag.UnknownOperation[*Snapshot])
	require.True(t, ok, "expected *dag.UnknownOperation[*Snapshot]")
	require.JSONEq(t, string(raw), string(unknown.RawJSON))

	marshaled, err := json.Marshal(op)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(marshaled))
}

func TestRetiredOpsAreUnknown(t *testing.T) {
	// The type codes of the operations that do not exist in this format
	// are never reused, so they fall through to UnknownOperation.
	for _, code := range []dag.OperationType{2, 4, 5} {
		raw, err := json.Marshal(struct {
			Type      dag.OperationType `json:"type"`
			Timestamp int64             `json:"timestamp"`
			Nonce     []byte            `json:"nonce"`
		}{Type: code, Timestamp: time.Now().Unix(), Nonce: make([]byte, 20)})
		require.NoError(t, err)

		op, err := operationUnmarshaler(raw, nil)
		require.NoError(t, err)
		_, ok := op.(*dag.UnknownOperation[*Snapshot])
		require.True(t, ok, "code %d", code)
	}
}

func TestMetadata(t *testing.T) {
	repo := repository.NewMockRepoClock()

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)

	op := NewCreateOp(rene, time.Now().Unix(), "title", "message", nil, nil)

	op.SetMetadata("key", "value")

	val, ok := op.GetMetadata("key")
	require.True(t, ok)
	require.Equal(t, val, "value")
}

func TestID(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)

	repos := []repository.ClockedRepo{
		repository.NewMockRepo(),
		repo,
	}

	for _, repo := range repos {
		rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
		require.NoError(t, err)
		err = rene.Commit(repo)
		require.NoError(t, err)

		i, op, err := Create(rene, time.Now().Unix(), "title", "message", nil, nil, nil)
		require.NoError(t, err)

		id1 := op.Id()
		require.NoError(t, id1.Validate())

		err = i.Commit(repo)
		require.NoError(t, err)

		op2 := i.FirstOp()

		id2 := op2.Id()
		require.NoError(t, id2.Validate())
		require.Equal(t, id1, id2)

		i2, err := Read(repo, i.Id())
		require.NoError(t, err)

		op3 := i2.FirstOp()

		id3 := op3.Id()
		require.NoError(t, id3.Validate())
		require.Equal(t, id1, id3)
	}
}

func TestReadBack(t *testing.T) {
	// Everything the model holds survives a commit and a read from git.
	repo := repository.CreateGoGitTestRepo(t, false)

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(repo))

	unix := time.Now().Unix()
	other, _, err := Create(rene, unix, "parent", "message", nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, other.Commit(repo))

	i, _, err := Create(rene, unix, "title", "message", nil, map[string]Value{"type": StringValue("task")}, nil)
	require.NoError(t, err)
	_, err = SetField(i, rene, unix, "status", StringValue("closed"), nil)
	require.NoError(t, err)
	_, err = SetField(i, rene, unix, "labels", MustValue([]string{"a", "b"}), nil)
	require.NoError(t, err)
	_, err = SetField(i, rene, unix, "parent", StringValue(other.Id().String()), nil)
	require.NoError(t, err)
	_, err = AddValue(i, rene, unix, "blocks", StringValue(other.Id().String()), nil)
	require.NoError(t, err)
	commentId, _, err := AddComment(i, rene, unix, "a comment", nil, nil)
	require.NoError(t, err)
	require.NoError(t, i.Commit(repo))

	read, err := Read(repo, i.Id())
	require.NoError(t, err)
	require.NoError(t, read.Validate())

	want := i.Compile()
	got := read.Compile()
	require.Equal(t, want.Fields, got.Fields)
	require.Equal(t, "title", got.Title())
	require.Len(t, got.Comments, 2)
	require.Equal(t, commentId, got.Comments[1].CombinedId())
	require.Len(t, got.Timeline, 6)
	require.Len(t, got.Items("blocks"), 1)

	ids, err := ListLocalIds(repo)
	require.NoError(t, err)
	require.ElementsMatch(t, []entity.Id{other.Id(), i.Id()}, ids)
}
