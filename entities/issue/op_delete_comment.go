package issue

import (
	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ Operation = &DeleteCommentOperation{}

// DeleteCommentOperation hides a comment from the issue
// (doc/design/issue.md, "Delete hides, the history keeps").
//
// Target is the add-comment operation of the comment, as for EditComment.
// Applying it removes the comment from Comments and leaves everything else,
// the timeline and the operations, untouched:
// the text stays in the history
// (doc/design/issue.md, "Delete hides, the history keeps").
// There is no undelete, and an edit applied after the delete finds no comment
// to change, so a delete wins over a concurrent edit with no rule of its own
// (doc/design/issue.md, "Delete is final").
type DeleteCommentOperation struct {
	dag.OpBase
	Target entity.Id `json:"target"`
}

func (op *DeleteCommentOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *DeleteCommentOperation) Apply(snapshot *Snapshot) {
	combinedId := entity.CombineIds(snapshot.Id(), op.Target)

	for i := range snapshot.Comments {
		if snapshot.Comments[i].CombinedId() != combinedId {
			continue
		}
		// The description, the create operation's comment, is never deleted
		// (doc/design/issue.md, "The description cannot be deleted").
		if i == 0 {
			return
		}
		snapshot.Comments = append(snapshot.Comments[:i:i], snapshot.Comments[i+1:]...)
		snapshot.addActor(op.Author())
		return
	}
	// Target not found, or already deleted: a no-op.
}

func (op *DeleteCommentOperation) Validate() error {
	if err := op.OpBase.Validate(op, DeleteCommentOp); err != nil {
		return err
	}

	if err := op.Target.Validate(); err != nil {
		return errors.Wrap(err, "target hash is invalid")
	}

	return nil
}

func NewDeleteCommentOp(author identity.Interface, unixTime int64, target entity.Id) *DeleteCommentOperation {
	return &DeleteCommentOperation{
		OpBase: dag.NewOpBase(DeleteCommentOp, author, unixTime),
		Target: target,
	}
}

// DeleteComment is a convenience function to apply the operation.
func DeleteComment(i Interface, author identity.Interface, unixTime int64, target entity.Id, metadata map[string]string) (*DeleteCommentOperation, error) {
	op := NewDeleteCommentOp(author, unixTime, target)
	for key, val := range metadata {
		op.SetMetadata(key, val)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	i.Append(op)
	return op, nil
}
