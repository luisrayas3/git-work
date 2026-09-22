package issue

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

func TestSetField(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()

	i, _, err := Create(rene, unix, "title", "message", nil, nil, nil)
	require.NoError(t, err)

	_, err = SetField(i, rene, unix, "status", StringValue("closed"), nil)
	require.NoError(t, err)
	snap := i.Compile()
	status, _ := snap.FieldString("status")
	require.Equal(t, "closed", status)

	// a second set records what it replaced
	_, err = SetField(i, rene, unix, "status", StringValue("open"), nil)
	require.NoError(t, err)
	snap = i.Compile()
	item := snap.Timeline[len(snap.Timeline)-1].(*SetFieldTimelineItem)
	require.Equal(t, "status", item.Key)
	require.JSONEq(t, `"open"`, string(item.Value))
	require.JSONEq(t, `"closed"`, string(item.Was))

	// null clears
	_, err = SetField(i, rene, unix, "status", MustValue(nil), nil)
	require.NoError(t, err)
	snap = i.Compile()
	_, ok := snap.Fields["status"]
	require.False(t, ok)
	require.Equal(t, []string{"title"}, snap.FieldKeys())

	// the title can change but never go away
	_, err = SetField(i, rene, unix, TitleKey, StringValue("renamed"), nil)
	require.NoError(t, err)
	require.Equal(t, "renamed", i.Compile().Title())
}

func TestSetFieldSerialize(t *testing.T) {
	for _, value := range []Value{
		StringValue("open"),
		MustValue(true),
		MustValue(2.5),
		MustValue([]string{"a", "b"}),
		MustValue(nil),
		MustValue(map[string]interface{}{"nested": []int{1, 2}}),
	} {
		dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*SetFieldOperation, entity.Resolvers) {
			return NewSetFieldOp(author, unixTime, "status", value), nil
		})
	}
}

func TestSetFieldValidate(t *testing.T) {
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	unix := time.Now().Unix()

	good := []*SetFieldOperation{
		NewSetFieldOp(rene, unix, "status", StringValue("open")),
		NewSetFieldOp(rene, unix, "story_points", MustValue(3)),
		NewSetFieldOp(rene, unix, "due-date", MustValue(nil)),
		NewSetFieldOp(rene, unix, TitleKey, StringValue("a title")),
	}
	for _, op := range good {
		require.NoError(t, op.Validate())
	}

	bad := map[string]*SetFieldOperation{
		"empty key":       NewSetFieldOp(rene, unix, "", StringValue("x")),
		"upper key":       NewSetFieldOp(rene, unix, "Status", StringValue("x")),
		"leading digit":   NewSetFieldOp(rene, unix, "1st", StringValue("x")),
		"long key":        NewSetFieldOp(rene, unix, strings.Repeat("k", MaxKeySize+1), StringValue("x")),
		"invalid json":    NewSetFieldOp(rene, unix, "status", Value("open")),
		"empty value":     NewSetFieldOp(rene, unix, "status", Value("")),
		"oversize":        NewSetFieldOp(rene, unix, "notes", StringValue(strings.Repeat("x", MaxValueSize))),
		"title null":      NewSetFieldOp(rene, unix, TitleKey, MustValue(nil)),
		"title number":    NewSetFieldOp(rene, unix, TitleKey, MustValue(1)),
		"title empty":     NewSetFieldOp(rene, unix, TitleKey, StringValue("  ")),
		"title multiline": NewSetFieldOp(rene, unix, TitleKey, StringValue("a\nb")),
	}
	for name, op := range bad {
		require.Error(t, op.Validate(), name)
	}
}
