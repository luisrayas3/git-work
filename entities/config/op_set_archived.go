package config

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ Operation = &SetArchivedOperation{}

// SetArchivedOperation is the replicated removal.
//
// A ref cannot be deleted across clones — it comes back on the next pull —
// so removing a config entity is an operation like any other,
// and it can be undone by setting the flag back (E7).
type SetArchivedOperation struct {
	dag.OpBase
	Archived bool `json:"archived"`
}

func (op *SetArchivedOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *SetArchivedOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())
	snapshot.Archived = op.Archived
}

func (op *SetArchivedOperation) Validate() error {
	return op.OpBase.Validate(op, SetArchivedOp)
}

func NewSetArchivedOp(author identity.Interface, unixTime int64, archived bool) *SetArchivedOperation {
	return &SetArchivedOperation{
		OpBase:   dag.NewOpBase(SetArchivedOp, author, unixTime),
		Archived: archived,
	}
}

// SetArchived is a convenience function to archive or unarchive a config entity.
func SetArchived(c Interface, author identity.Interface, unixTime int64, archived bool, metadata map[string]string) (*SetArchivedOperation, error) {
	op := NewSetArchivedOp(author, unixTime, archived)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	c.Append(op)
	return op, nil
}
