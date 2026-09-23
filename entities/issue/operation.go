package issue

import (
	"encoding/json"
	"fmt"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

// Operation type codes.
//
// The codes shared with entities/bug are kept on purpose:
// an operation's id hashes its JSON, code included,
// so keeping Create at 1 and AddComment at 3
// is what lets the migration (bf6f392) preserve entity and comment ids.
// The retired codes 2, 4 and 5 (SetTitle, SetStatus, LabelChange)
// are never reused,
// so a pack from the old format can not be misread as a valid new one
// even if the version gate were bypassed.
const (
	_             dag.OperationType = iota
	CreateOp                        // 1, kept
	_                               // 2, retired: SetTitle
	AddCommentOp                    // 3, kept
	_                               // 4, retired: SetStatus
	_                               // 5, retired: LabelChange
	EditCommentOp                   // 6, kept
	NoOpOp                          // 7, kept
	SetMetadataOp                   // 8, kept
	SetFieldOp                      // 9
	AddValueOp                      // 10
	RemoveValueOp                   // 11
)

// OperationTypeName names an operation type for the outside world.
//
// The type code is what the store holds and what an id hashes;
// the name is what `git work issue log` prints and what a script matches on,
// so it is kebab-case like every other key on the command line.
func OperationTypeName(t dag.OperationType) string {
	switch t {
	case CreateOp:
		return "create"
	case AddCommentOp:
		return "add-comment"
	case EditCommentOp:
		return "edit-comment"
	case NoOpOp:
		return "noop"
	case SetMetadataOp:
		return "set-metadata"
	case SetFieldOp:
		return "set-field"
	case AddValueOp:
		return "add-value"
	case RemoveValueOp:
		return "remove-value"
	default:
		return fmt.Sprintf("unknown-%d", int(t))
	}
}

// Operation define the interface to fulfill for an edit operation of an Issue
type Operation = dag.OperationWithApply[*Snapshot]

// make sure that package external operations do conform to our interface
var _ Operation = &dag.NoOpOperation[*Snapshot]{}
var _ Operation = &dag.SetMetadataOperation[*Snapshot]{}
var _ Operation = &dag.UnknownOperation[*Snapshot]{}

func operationUnmarshaler(raw json.RawMessage, resolvers entity.Resolvers) (dag.Operation, error) {
	var t struct {
		OperationType dag.OperationType `json:"type"`
	}

	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}

	var op dag.Operation

	switch t.OperationType {
	case AddCommentOp:
		op = &AddCommentOperation{}
	case AddValueOp:
		op = &AddValueOperation{}
	case CreateOp:
		op = &CreateOperation{}
	case EditCommentOp:
		op = &EditCommentOperation{}
	case NoOpOp:
		op = &dag.NoOpOperation[*Snapshot]{}
	case RemoveValueOp:
		op = &RemoveValueOperation{}
	case SetFieldOp:
		op = &SetFieldOperation{}
	case SetMetadataOp:
		op = &dag.SetMetadataOperation[*Snapshot]{}
	default:
		return dag.NewUnknownOp[*Snapshot](raw)
	}

	err := json.Unmarshal(raw, &op)
	if err != nil {
		return nil, err
	}

	return op, nil
}
