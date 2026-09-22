package issue

import (
	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/util/timestamp"
)

var _ Operation = &SetFieldOperation{}

// SetFieldOperation sets one field to a value, or clears it with null.
//
// It is the only way an issue's properties change,
// title and status included.
// Concurrent sets of the same field resolve last-writer-wins
// in the dag's deterministic order (lamport edit time, then pack id).
// Whether the value fits the field's kind is the schema's business,
// checked above this package on write and never here (configurable-schema.md D6).
type SetFieldOperation struct {
	dag.OpBase
	Key   string `json:"key"`
	Value Value  `json:"value"`
}

func (op *SetFieldOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *SetFieldOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())

	was := snapshot.Fields[op.Key]
	snapshot.setField(op.Key, op.Value)

	id := op.Id()
	snapshot.Timeline = append(snapshot.Timeline, &SetFieldTimelineItem{
		combinedId: entity.CombineIds(snapshot.Id(), id),
		Author:     op.Author(),
		UnixTime:   timestamp.Timestamp(op.UnixTime),
		Key:        op.Key,
		Value:      op.Value,
		Was:        was,
	})
}

func (op *SetFieldOperation) Validate() error {
	if err := op.OpBase.Validate(op, SetFieldOp); err != nil {
		return err
	}
	if err := ValidateKey(op.Key); err != nil {
		return errors.Wrap(err, "field")
	}
	if err := ValidateValue(op.Key, op.Value); err != nil {
		return errors.Wrapf(err, "field %s", op.Key)
	}
	return nil
}

func NewSetFieldOp(author identity.Interface, unixTime int64, key string, value Value) *SetFieldOperation {
	return &SetFieldOperation{
		OpBase: dag.NewOpBase(SetFieldOp, author, unixTime),
		Key:    key,
		Value:  value,
	}
}

// SetField is a convenience function to set a field on an issue.
// A null value clears the field.
func SetField(i Interface, author identity.Interface, unixTime int64, key string, value Value, metadata map[string]string) (*SetFieldOperation, error) {
	op := NewSetFieldOp(author, unixTime, key, value)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	i.Append(op)
	return op, nil
}
