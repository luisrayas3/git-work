package config

import (
	"sort"

	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ Operation = &CreateOperation{}

// CreateOperation defines the initial creation of a config entity.
//
// Shape and key are immutable, and the id derives from this operation,
// so two clones creating the same key produce two entities
// and E7 picks the winner.
type CreateOperation struct {
	dag.OpBase
	Shape Shape  `json:"shape"`
	Key   string `json:"key"`
}

func (op *CreateOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *CreateOperation) Apply(snapshot *Snapshot) {
	// sanity check: will fail when adding a second Create
	if snapshot.id != "" && snapshot.id != entity.UnsetId && snapshot.id != op.Id() {
		return
	}

	snapshot.id = op.Id()
	snapshot.Shape = op.Shape
	snapshot.Key = op.Key
	snapshot.Author = op.Author()
	snapshot.CreateTime = op.Time()
	snapshot.addActor(op.Author())
}

// Validate checks only what must hold for every create operation ever written.
//
// It does not check that the shape is one this binary knows (E6):
// a failure here makes the entity unreadable,
// so a newer binary's entities must still read on an older one.
func (op *CreateOperation) Validate() error {
	if err := op.OpBase.Validate(op, CreateOp); err != nil {
		return err
	}
	if err := ValidateConfigKey(string(op.Shape)); err != nil {
		return errors.Wrap(err, "shape")
	}
	if err := ValidateConfigKey(op.Key); err != nil {
		return errors.Wrap(err, "key")
	}
	return nil
}

func NewCreateOp(author identity.Interface, unixTime int64, shape Shape, key string) *CreateOperation {
	return &CreateOperation{
		OpBase: dag.NewOpBase(CreateOp, author, unixTime),
		Shape:  shape,
		Key:    key,
	}
}

// Create makes a config entity in this store's namespace,
// with its initial attributes set in the same pack.
//
// The attributes are separate Set operations rather than members of Create,
// so that the entity's id derives from shape and key alone
// and an attribute later added to a shape needs no new operation type.
func (s *Store) Create(author identity.Interface, unixTime int64, shape Shape, key string, attributes map[string]Value, metadata map[string]string) (*Config, *CreateOperation, error) {
	if err := s.checkShape(shape); err != nil {
		return nil, nil, err
	}
	if err := ValidateKey(shape, key); err != nil {
		return nil, nil, err
	}

	c := s.New()
	op := NewCreateOp(author, unixTime, shape, key)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, op, err
	}
	c.Append(op)

	// sorted, so that the same inputs write the same pack
	names := make([]string, 0, len(attributes))
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, err := Set(c, author, unixTime, name, attributes[name], nil); err != nil {
			return nil, op, err
		}
	}

	return c, op, nil
}
