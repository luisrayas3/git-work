package issue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

func TestCreate(t *testing.T) {
	repo := repository.NewMockRepo()

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)

	fields := map[string]Value{"status": StringValue("open"), "estimate": MustValue(3)}
	i, op, err := Create(rene, time.Now().Unix(), "title", "message", nil, fields, nil)
	require.NoError(t, err)

	require.Equal(t, "title", op.Title)
	require.Equal(t, "message", op.Message)

	snap := i.Compile()
	require.Equal(t, rene, snap.Author)
	require.Equal(t, "title", snap.Title())
	require.Equal(t, []string{"estimate", "status", "title"}, snap.FieldKeys())
	status, ok := snap.FieldString("status")
	require.True(t, ok)
	require.Equal(t, "open", status)
	require.JSONEq(t, `3`, string(snap.Fields["estimate"]))
	require.Len(t, snap.Operations, 1)
	require.Equal(t, op, snap.Operations[0])

	require.Len(t, snap.Timeline, 1)
	require.Equal(t, entity.CombineIds(i.Id(), op.Id()), snap.Timeline[0].CombinedId())
	require.Equal(t, rene, snap.Timeline[0].(*CreateTimelineItem).Author)
	require.Equal(t, "message", snap.Timeline[0].(*CreateTimelineItem).Message)
}

func TestCreateSerialize(t *testing.T) {
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*CreateOperation, entity.Resolvers) {
		return NewCreateOp(author, unixTime, "title", "message", nil, nil), nil
	})
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*CreateOperation, entity.Resolvers) {
		return NewCreateOp(author, unixTime, "title", "message", []repository.Hash{"hash1", "hash2"}, nil), nil
	})
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*CreateOperation, entity.Resolvers) {
		return NewCreateOp(author, unixTime, "title", "message", nil, map[string]Value{
			"status": StringValue("open"),
			"labels": MustValue([]string{"a", "b"}),
		}), nil
	})
}

func TestCreateValidate(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()

	require.NoError(t, NewCreateOp(rene, unix, "title", "message", nil, map[string]Value{"status": StringValue("open")}).Validate())

	bad := map[string]map[string]Value{
		"title in fields": {TitleKey: StringValue("other")},
		"bad key":         {"Status": StringValue("open")},
		"bad json":        {"status": Value("open")},
		"empty value":     {"status": Value("")},
	}
	for name, fields := range bad {
		require.Error(t, NewCreateOp(rene, unix, "title", "message", nil, fields).Validate(), name)
	}
}
