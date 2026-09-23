package config

import (
	"encoding/json"
	"fmt"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

// Operation type codes.
//
// The four document operations are 1 to 4 (E2);
// dag's own NoOp and SetMetadata follow,
// as they do in entities/issue.
// A code is never reused: an operation an older binary does not know
// comes back as dag.UnknownOperation, is kept verbatim and is synced on.
const (
	_             dag.OperationType = iota
	CreateOp                        // 1
	SetOp                           // 2
	RemoveOp                        // 3
	SetArchivedOp                   // 4
	NoOpOp                          // 5
	SetMetadataOp                   // 6
)

// OperationTypeName names an operation type for the outside world.
//
// The type code is what the store holds and what an id hashes;
// the name is what `git work flow log` prints and what a script matches on,
// so it is kebab-case like every other key on the command line.
func OperationTypeName(t dag.OperationType) string {
	switch t {
	case CreateOp:
		return "create"
	case SetOp:
		return "set"
	case RemoveOp:
		return "remove"
	case SetArchivedOp:
		return "set-archived"
	case NoOpOp:
		return "noop"
	case SetMetadataOp:
		return "set-metadata"
	default:
		return fmt.Sprintf("unknown-%d", int(t))
	}
}

// Operation is the interface an edit of a config entity fulfills.
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
	case CreateOp:
		op = &CreateOperation{}
	case SetOp:
		op = &SetOperation{}
	case RemoveOp:
		op = &RemoveOperation{}
	case SetArchivedOp:
		op = &SetArchivedOperation{}
	case NoOpOp:
		op = &dag.NoOpOperation[*Snapshot]{}
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
