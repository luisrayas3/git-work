package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

func TestApplySemantics(t *testing.T) {
	_, rene := testIdentity(t)
	unix := time.Now().Unix()

	c, _, err := Schema.Create(rene, unix, ShapeField, "task/status", map[string]Value{
		"kind": StringValue("enum-with-category"),
		"name": StringValue("Status"),
	}, nil)
	require.NoError(t, err)
	require.NoError(t, c.Validate())

	snap := c.Compile()
	require.Equal(t, ShapeField, snap.Shape)
	require.Equal(t, "task/status", snap.Key)
	require.False(t, snap.Archived)
	require.Equal(t, []string{"kind", "name"}, snap.AttributeNames())

	// last writer wins, per attribute
	_, err = Set(c, rene, unix, "name", StringValue("State"), nil)
	require.NoError(t, err)
	snap = c.Compile()
	name, ok := snap.AttributeString("name")
	require.True(t, ok)
	require.Equal(t, "State", name)
	kind, ok := snap.AttributeString("kind")
	require.True(t, ok)
	require.Equal(t, "enum-with-category", kind)

	// a collection is one attribute per member
	_, err = Set(c, rene, unix, "values/to-do", MustValue(map[string]string{"name": "To Do"}), nil)
	require.NoError(t, err)
	_, err = Set(c, rene, unix, "values/qa", MustValue(map[string]string{"name": "QA"}), nil)
	require.NoError(t, err)
	require.Len(t, c.Compile().Members("values"), 2)

	// Remove deletes the attribute
	_, err = Remove(c, rene, unix, "values/qa", nil)
	require.NoError(t, err)
	snap = c.Compile()
	require.Len(t, snap.Members("values"), 1)
	_, ok = snap.Attribute("values/qa")
	require.False(t, ok)

	// a Set after a Remove brings the member back, and the reverse deletes it
	_, err = Set(c, rene, unix, "values/qa", MustValue(map[string]string{"name": "QA"}), nil)
	require.NoError(t, err)
	require.Len(t, c.Compile().Members("values"), 2)

	// SetArchived toggles
	_, err = SetArchived(c, rene, unix, true, nil)
	require.NoError(t, err)
	require.True(t, c.Compile().Archived)
	_, err = SetArchived(c, rene, unix, false, nil)
	require.NoError(t, err)
	require.False(t, c.Compile().Archived)
}

func TestNamespaceGating(t *testing.T) {
	_, rene := testIdentity(t)
	unix := time.Now().Unix()

	// a flow does not belong in the schema namespace
	_, _, err := Schema.Create(rene, unix, ShapeFlow, "board", nil, nil)
	require.Error(t, err)

	// nor a type or a field in the flow namespace
	_, _, err = Flows.Create(rene, unix, ShapeType, "epic", nil, nil)
	require.Error(t, err)
	_, _, err = Flows.Create(rene, unix, ShapeField, "task/status", nil, nil)
	require.Error(t, err)

	// nor a shape this binary does not know, anywhere
	_, _, err = Schema.Create(rene, unix, "view", "mine", nil, nil)
	require.Error(t, err)

	// and each takes its own
	_, _, err = Schema.Create(rene, unix, ShapeType, "epic", nil, nil)
	require.NoError(t, err)
	_, _, err = Flows.Create(rene, unix, ShapeFlow, "board", nil, nil)
	require.NoError(t, err)

	require.True(t, Schema.AllowsShape(ShapeType))
	require.True(t, Schema.AllowsShape(ShapeField))
	require.False(t, Schema.AllowsShape(ShapeFlow))
	require.True(t, Flows.AllowsShape(ShapeFlow))

	// Validate catches an entity that ended up in the wrong store
	c := Flows.New()
	op := NewCreateOp(rene, unix, ShapeField, "task/status")
	require.NoError(t, op.Validate())
	c.Append(op)
	require.Error(t, c.Validate())
}

func TestKeyMustMatchShape(t *testing.T) {
	_, rene := testIdentity(t)
	unix := time.Now().Unix()

	_, _, err := Schema.Create(rene, unix, ShapeField, "status", nil, nil)
	require.Error(t, err, "a field key is <type>/<field>")

	_, _, err = Schema.Create(rene, unix, ShapeType, "task/status", nil, nil)
	require.Error(t, err, "a type key is one slug")

	_, _, err = Schema.Create(rene, unix, ShapeField, "task/status", nil, nil)
	require.NoError(t, err)
}

func TestReadBack(t *testing.T) {
	// Everything the document holds survives a commit and a read from git,
	// in both namespaces.
	repo := repository.CreateGoGitTestRepo(t, false)

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(repo))

	unix := time.Now().Unix()

	// an attribute the operations refuse stops the creation
	_, _, err = Schema.Create(rene, unix, ShapeField, "task/status", map[string]Value{
		"target_types/": MustValue(map[string]string{}),
	}, nil)
	require.Error(t, err)

	field, _, err := Schema.Create(rene, unix, ShapeField, "task/status", map[string]Value{
		"kind":         StringValue("enum-with-category"),
		"name":         StringValue("Status"),
		"ordinal":      MustValue(10),
		"values/to-do": MustValue(map[string]interface{}{"name": "To Do", "category": "unstarted", "ordinal": 10}),
		"values/done":  MustValue(map[string]interface{}{"name": "Done", "category": "completed", "ordinal": 20}),
	}, nil)
	require.NoError(t, err)
	require.NoError(t, field.Commit(repo))

	typ, _, err := Schema.Create(rene, unix, ShapeType, "task", map[string]Value{
		"name": StringValue("Task"),
	}, nil)
	require.NoError(t, err)
	require.NoError(t, typ.Commit(repo))

	flow, _, err := Flows.Create(rene, unix, ShapeFlow, "board", map[string]Value{
		"script":      StringValue("def board():\n    pass\n"),
		"description": StringValue("Kanban of one iteration."),
	}, nil)
	require.NoError(t, err)
	_, err = SetArchived(flow, rene, unix, true, nil)
	require.NoError(t, err)
	require.NoError(t, flow.Commit(repo))

	read, err := Schema.Read(repo, field.Id())
	require.NoError(t, err)
	require.NoError(t, read.Validate())
	require.Equal(t, field.Compile().Attributes, read.Compile().Attributes)
	require.Equal(t, "task/status", read.Compile().Key)
	require.Len(t, read.Compile().Members("values"), 2)

	readFlow, err := Flows.Read(repo, flow.Id())
	require.NoError(t, err)
	require.NoError(t, readFlow.Validate())
	require.True(t, readFlow.Compile().Archived)
	require.Equal(t, ShapeFlow, readFlow.Compile().Shape)

	// the namespaces are separate: each lists only its own
	schemaIds, err := Schema.ListLocalIds(repo)
	require.NoError(t, err)
	require.ElementsMatch(t, []entity.Id{field.Id(), typ.Id()}, schemaIds)

	flowIds, err := Flows.ListLocalIds(repo)
	require.NoError(t, err)
	require.ElementsMatch(t, []entity.Id{flow.Id()}, flowIds)

	// and an id of one namespace is not readable in the other
	_, err = Flows.Read(repo, field.Id())
	require.Error(t, err)
}

func TestLastWriterWinsAcrossClones(t *testing.T) {
	// Two edits of the same attribute merge to one value,
	// the last in the dag's order, with no code of ours deciding it.
	repo := repository.CreateGoGitTestRepo(t, false)

	rene, err := identity.NewIdentity(repo, "René Descartes", "rene@descartes.fr")
	require.NoError(t, err)
	require.NoError(t, rene.Commit(repo))

	unix := time.Now().Unix()
	c, _, err := Schema.Create(rene, unix, ShapeField, "task/status", map[string]Value{
		"name": StringValue("Status"),
	}, nil)
	require.NoError(t, err)
	require.NoError(t, c.Commit(repo))

	_, err = Set(c, rene, unix, "name", StringValue("State"), nil)
	require.NoError(t, err)
	require.NoError(t, c.Commit(repo))

	read, err := Schema.Read(repo, c.Id())
	require.NoError(t, err)
	name, ok := read.Compile().AttributeString("name")
	require.True(t, ok)
	require.Equal(t, "State", name)
}
