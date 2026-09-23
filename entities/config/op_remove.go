package config

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ Operation = &RemoveOperation{}

// RemoveOperation deletes one attribute.
//
// It is how a member of a folded collection goes away,
// `Remove values/qa`,
// and it loses to a later Set on the same name in dag order,
// which is the same last-writer-wins rule Set follows.
type RemoveOperation struct {
	dag.OpBase
	Name string `json:"name"`
}

func (op *RemoveOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *RemoveOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())
	snapshot.removeAttribute(op.Name)
}

func (op *RemoveOperation) Validate() error {
	if err := op.OpBase.Validate(op, RemoveOp); err != nil {
		return err
	}
	return ValidateName(op.Name)
}

func NewRemoveOp(author identity.Interface, unixTime int64, name string) *RemoveOperation {
	return &RemoveOperation{
		OpBase: dag.NewOpBase(RemoveOp, author, unixTime),
		Name:   name,
	}
}

// Remove is a convenience function to delete one attribute of a config entity.
func Remove(c Interface, author identity.Interface, unixTime int64, name string, metadata map[string]string) (*RemoveOperation, error) {
	op := NewRemoveOp(author, unixTime, name)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	c.Append(op)
	return op, nil
}
