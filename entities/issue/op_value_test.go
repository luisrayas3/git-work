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

func TestAddRemoveValue(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()

	i, _, err := Create(rene, unix, "title", "message", nil, nil, nil)
	require.NoError(t, err)

	// adding to an unset field creates the list
	_, err = AddValue(i, rene, unix, "labels", StringValue("b"), nil)
	require.NoError(t, err)
	require.JSONEq(t, `["b"]`, string(i.Compile().Fields["labels"]))

	// items are a sorted set: order of arrival does not show, duplicates do not count
	_, err = AddValue(i, rene, unix, "labels", StringValue("a"), nil)
	require.NoError(t, err)
	_, err = AddValue(i, rene, unix, "labels", Value(` "a" `), nil)
	require.NoError(t, err)
	require.JSONEq(t, `["a","b"]`, string(i.Compile().Fields["labels"]))
	require.True(t, i.Compile().HasItem("labels", StringValue("a")))

	// removing leaves an empty list, not an unset field
	_, err = RemoveValue(i, rene, unix, "labels", StringValue("a"), nil)
	require.NoError(t, err)
	_, err = RemoveValue(i, rene, unix, "labels", StringValue("b"), nil)
	require.NoError(t, err)
	_, err = RemoveValue(i, rene, unix, "labels", StringValue("b"), nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(i.Compile().Fields["labels"]))

	// removing from an unset field does not create it
	_, err = RemoveValue(i, rene, unix, "components", StringValue("x"), nil)
	require.NoError(t, err)
	_, ok := i.Compile().Fields["components"]
	require.False(t, ok)

	// a scalar field is treated as empty by an add, untouched by a remove
	_, err = SetField(i, rene, unix, "status", StringValue("open"), nil)
	require.NoError(t, err)
	_, err = RemoveValue(i, rene, unix, "status", StringValue("open"), nil)
	require.NoError(t, err)
	require.JSONEq(t, `"open"`, string(i.Compile().Fields["status"]))
	_, err = AddValue(i, rene, unix, "status", StringValue("open"), nil)
	require.NoError(t, err)
	require.JSONEq(t, `["open"]`, string(i.Compile().Fields["status"]))

	// relations of many cardinality are just this, with ids as items
	target := entity.DeriveId([]byte("target"))
	_, err = AddValue(i, rene, unix, "blocks", StringValue(target.String()), nil)
	require.NoError(t, err)
	items := i.Compile().Items("blocks")
	require.Len(t, items, 1)
	s, _ := String(items[0])
	require.Equal(t, target.String(), s)

	item := i.Compile().Timeline[len(i.Compile().Timeline)-1].(*ValueTimelineItem)
	require.Equal(t, "blocks", item.Key)
	require.True(t, item.Added)
}

func TestValueSerialize(t *testing.T) {
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*AddValueOperation, entity.Resolvers) {
		return NewAddValueOp(author, unixTime, "labels", StringValue("a")), nil
	})
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*RemoveValueOperation, entity.Resolvers) {
		return NewRemoveValueOp(author, unixTime, "labels", MustValue(map[string]int{"n": 1})), nil
	})
}

func TestValueValidate(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()

	require.NoError(t, NewAddValueOp(rene, unix, "labels", StringValue("a")).Validate())
	require.NoError(t, NewRemoveValueOp(rene, unix, "blocked-by", MustValue(3)).Validate())

	require.Error(t, NewAddValueOp(rene, unix, "", StringValue("a")).Validate())
	require.Error(t, NewAddValueOp(rene, unix, "Labels", StringValue("a")).Validate())
	require.Error(t, NewAddValueOp(rene, unix, TitleKey, StringValue("a")).Validate(), "title is not a list")
	require.Error(t, NewAddValueOp(rene, unix, "labels", MustValue(nil)).Validate(), "null is not an item")
	require.Error(t, NewAddValueOp(rene, unix, "labels", Value("")).Validate())
	require.Error(t, NewRemoveValueOp(rene, unix, "labels", Value("not json")).Validate())
}
