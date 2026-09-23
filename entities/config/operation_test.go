package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

func testIdentity(t *testing.T) (repository.ClockedRepo, *identity.Identity) {
	t.Helper()
	repo := repository.NewMockRepo()
	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	return repo, rene
}

func TestSerializeRoundTrip(t *testing.T) {
	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*CreateOperation, entity.Resolvers) {
		return NewCreateOp(author, unixTime, ShapeField, "task/status"), nil
	})

	for _, value := range []Value{
		StringValue("Status"),
		MustValue(10),
		MustValue(true),
		MustValue(nil),
		MustValue(map[string]interface{}{"name": "QA", "category": "started"}),
		MustValue([]string{"epic", "initiative"}),
	} {
		dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*SetOperation, entity.Resolvers) {
			return NewSetOp(author, unixTime, "values/qa", value), nil
		})
	}

	dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*RemoveOperation, entity.Resolvers) {
		return NewRemoveOp(author, unixTime, "values/qa"), nil
	})

	for _, archived := range []bool{true, false} {
		dag.SerializeRoundTripTest(t, operationUnmarshaler, func(author identity.Interface, unixTime int64) (*SetArchivedOperation, entity.Resolvers) {
			return NewSetArchivedOp(author, unixTime, archived), nil
		})
	}
}

func TestOperationValidate(t *testing.T) {
	_, rene := testIdentity(t)
	unix := time.Now().Unix()

	good := []Operation{
		NewCreateOp(rene, unix, ShapeType, "epic"),
		NewCreateOp(rene, unix, ShapeField, "task/status"),
		NewCreateOp(rene, unix, ShapeFlow, "board"),
		NewSetOp(rene, unix, "name", StringValue("Status")),
		NewSetOp(rene, unix, "values/in-progress", MustValue(map[string]string{"name": "In Progress"})),
		NewSetOp(rene, unix, "target_types/epic", MustValue(map[string]string{})),
		NewRemoveOp(rene, unix, "values/in-progress"),
		NewSetArchivedOp(rene, unix, true),
	}
	for i, op := range good {
		require.NoError(t, op.Validate(), "good %d", i)
	}

	bad := map[string]Operation{
		"empty shape":        NewCreateOp(rene, unix, "", "epic"),
		"upper shape":        NewCreateOp(rene, unix, "Type", "epic"),
		"empty key":          NewCreateOp(rene, unix, ShapeType, ""),
		"upper key":          NewCreateOp(rene, unix, ShapeType, "Epic"),
		"two slashes":        NewCreateOp(rene, unix, ShapeField, "a/b/c"),
		"long key":           NewCreateOp(rene, unix, ShapeType, strings.Repeat("k", MaxKeySize+1)),
		"empty name":         NewSetOp(rene, unix, "", StringValue("x")),
		"upper name":         NewSetOp(rene, unix, "Name", StringValue("x")),
		"leading digit name": NewSetOp(rene, unix, "1st", StringValue("x")),
		"dash in name":       NewSetOp(rene, unix, "on-open", StringValue("x")),
		"two slashes name":   NewSetOp(rene, unix, "values/a/b", StringValue("x")),
		"long name":          NewSetOp(rene, unix, strings.Repeat("n", MaxNameSize+1), StringValue("x")),
		"invalid json":       NewSetOp(rene, unix, "name", Value("Status")),
		"empty value":        NewSetOp(rene, unix, "name", Value("")),
		"oversize value":     NewSetOp(rene, unix, "script", StringValue(strings.Repeat("x", MaxValueSize))),
		// a literal control character is neither valid JSON nor text.Safe;
		// escaped, as json.Marshal writes it, it is both.
		"control character":   NewSetOp(rene, unix, "name", Value("\"a\x1bb\"")),
		"empty remove name":   NewRemoveOp(rene, unix, ""),
		"slashed remove name": NewRemoveOp(rene, unix, "/qa"),
	}
	for name, op := range bad {
		require.Error(t, op.Validate(), name)
	}
}

func TestValidateShapeAndKey(t *testing.T) {
	// ValidateShape is the write-path rule: only the three known shapes.
	require.NoError(t, ValidateShape(ShapeType))
	require.NoError(t, ValidateShape(ShapeField))
	require.NoError(t, ValidateShape(ShapeFlow))
	require.Error(t, ValidateShape(""))
	require.Error(t, ValidateShape("view"))
	require.Error(t, ValidateShape("Type"))

	// per-shape keys
	require.NoError(t, ValidateKey(ShapeType, "epic"))
	require.NoError(t, ValidateKey(ShapeType, "in_progress-thing"))
	require.Error(t, ValidateKey(ShapeType, "task/status"))
	require.Error(t, ValidateKey(ShapeType, "1st"))

	require.NoError(t, ValidateKey(ShapeField, "task/status"))
	require.Error(t, ValidateKey(ShapeField, "status"))
	require.Error(t, ValidateKey(ShapeField, "task/"))
	require.Error(t, ValidateKey(ShapeField, "/status"))
	require.Error(t, ValidateKey(ShapeField, "Task/status"))

	require.NoError(t, ValidateKey(ShapeFlow, "board"))
	require.Error(t, ValidateKey(ShapeFlow, "my/board"))

	// the frozen rule underneath tolerates a shape this binary does not know
	require.NoError(t, ValidateKey("view", "anything.here"))

	// attribute names
	require.NoError(t, ValidateName("kind"))
	require.NoError(t, ValidateName("target_types/epic"))
	require.NoError(t, ValidateName("values/in-progress"))
	require.NoError(t, ValidateName("values/1"))
	require.Error(t, ValidateName(""))
	require.Error(t, ValidateName("values/"))
	require.Error(t, ValidateName(strings.Repeat("n", MaxNameSize+1)))
}

func TestUnknownOpUnmarshaler(t *testing.T) {
	// An operation type that no version of this client knows about should be
	// preserved as an UnknownOperation rather than causing a panic,
	// which is what lets a newer binary's shapes list and sync on an older one.
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

func TestMetadata(t *testing.T) {
	_, rene := testIdentity(t)

	op := NewCreateOp(rene, time.Now().Unix(), ShapeType, "epic")
	op.SetMetadata("origin", "jira")

	val, ok := op.GetMetadata("origin")
	require.True(t, ok)
	require.Equal(t, "jira", val)
}
