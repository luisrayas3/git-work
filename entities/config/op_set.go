package config

import (
	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ Operation = &SetOperation{}

// SetOperation sets one attribute to a value.
//
// Concurrent sets of the same attribute resolve last-writer-wins
// in the dag's deterministic order, lamport edit time then pack id,
// which is where the CRDT comes from: no code of ours decides it (E2).
// A set- or map-valued attribute is one attribute per member,
// `values/<id>`, so two people adding two members both keep theirs.
type SetOperation struct {
	dag.OpBase
	Name  string `json:"name"`
	Value Value  `json:"value"`
}

func (op *SetOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *SetOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())
	snapshot.setAttribute(op.Name, op.Value)
}

func (op *SetOperation) Validate() error {
	if err := op.OpBase.Validate(op, SetOp); err != nil {
		return err
	}
	if err := ValidateName(op.Name); err != nil {
		return err
	}
	if err := ValidateValue(op.Value); err != nil {
		return errors.Wrapf(err, "attribute %s", op.Name)
	}
	return nil
}

func NewSetOp(author identity.Interface, unixTime int64, name string, value Value) *SetOperation {
	return &SetOperation{
		OpBase: dag.NewOpBase(SetOp, author, unixTime),
		Name:   name,
		Value:  value,
	}
}

// Set is a convenience function to set one attribute of a config entity.
func Set(c Interface, author identity.Interface, unixTime int64, name string, value Value, metadata map[string]string) (*SetOperation, error) {
	op := NewSetOp(author, unixTime, name, value)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	c.Append(op)
	return op, nil
}
