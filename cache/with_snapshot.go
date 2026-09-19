package cache

import (
	"sync"

	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

var _ dag.Interface[dag.Snapshot, dag.OperationWithApply[dag.Snapshot]] = &withSnapshot[dag.Snapshot, dag.OperationWithApply[dag.Snapshot]]{}

// stagedOperations exposes the operations appended but not yet committed.
// dag.Entity keeps its staging area private, so the wrapper below records what
// goes through it — which is everything, since every mutation appends through
// the cached entity. CachedEntityBase.Commit replays these onto a freshly read
// entity so the commit lands on the current tip (2a51f66).
type stagedOperations[OpT dag.Operation] interface {
	StagedOperations() []OpT
}

// withSnapshot encapsulate an entity and maintain a snapshot efficiently.
type withSnapshot[SnapT dag.Snapshot, OpT dag.OperationWithApply[SnapT]] struct {
	dag.Interface[SnapT, OpT]
	mu     sync.Mutex
	snap   *SnapT
	staged []OpT
}

func (ws *withSnapshot[SnapT, OpT]) Compile() SnapT {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.snap == nil {
		snap := ws.Interface.Compile()
		ws.snap = &snap
	}
	return *ws.snap
}

// Append intercept Bug.Append() to update the snapshot efficiently
func (ws *withSnapshot[SnapT, OpT]) Append(op OpT) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.Interface.Append(op)
	ws.staged = append(ws.staged, op)

	if ws.snap == nil {
		return
	}

	op.Apply(*ws.snap)
	(*ws.snap).AppendOperation(op)
}

// Commit intercept Bug.Commit() to update the snapshot efficiently
func (ws *withSnapshot[SnapT, OpT]) Commit(repo repository.ClockedRepo) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	err := ws.Interface.Commit(repo)
	if err != nil {
		ws.snap = nil
		return err
	}

	ws.staged = nil
	return nil
}

func (ws *withSnapshot[SnapT, OpT]) StagedOperations() []OpT {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return append([]OpT(nil), ws.staged...)
}
