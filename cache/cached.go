package cache

import (
	"sync"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/lamport"
)

var _ CacheEntity = &CachedEntityBase[dag.Snapshot, dag.Operation]{}

// CachedEntityBase provide the base function of an entity managed by the cache.
type CachedEntityBase[SnapT dag.Snapshot, OpT dag.Operation] struct {
	repo            repository.ClockedRepo
	entityUpdated   func(id entity.Id) error
	getUserIdentity getUserIdentityFunc

	// reload reads this entity from git again, wrapped the same way this one
	// is. Injected by the subcache, which is the only place that knows the
	// concrete entity type. See rebaseStaged.
	reload func() (dag.Interface[SnapT, OpT], error)

	mu     sync.RWMutex
	entity dag.Interface[SnapT, OpT]
}

func (e *CachedEntityBase[SnapT, OpT]) Id() entity.Id {
	return e.entity.Id()
}

func (e *CachedEntityBase[SnapT, OpT]) Snapshot() SnapT {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.entity.Compile()
}

func (e *CachedEntityBase[SnapT, OpT]) notifyUpdated() error {
	return e.entityUpdated(e.entity.Id())
}

// ResolveOperationWithMetadata will find an operation that has the matching metadata
func (e *CachedEntityBase[SnapT, OpT]) ResolveOperationWithMetadata(key string, value string) (entity.Id, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// preallocate but empty
	matching := make([]entity.Id, 0, 5)

	// Metadata can be added to an operation by a SetMetadataOperation, which only
	// takes effect when applied. Go through the snapshot to make sure it has been.
	for _, op := range e.entity.Compile().AllOperations() {
		opValue, ok := op.GetMetadata(key)
		if ok && value == opValue {
			matching = append(matching, op.Id())
		}
	}

	if len(matching) == 0 {
		return "", ErrNoMatchingOp
	}

	if len(matching) > 1 {
		return "", entity.NewErrMultipleMatch("operation", matching)
	}

	return matching[0], nil
}

func (e *CachedEntityBase[SnapT, OpT]) Validate() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.entity.Validate()
}

func (e *CachedEntityBase[SnapT, OpT]) Commit() error {
	unlock, err := lockWrite(e.repo)
	if err != nil {
		return err
	}
	defer unlock()

	e.mu.Lock()
	if err := e.rebaseStaged(); err != nil {
		e.mu.Unlock()
		return err
	}
	err = e.entity.Commit(e.repo)
	e.mu.Unlock()
	if err != nil {
		return err
	}
	return e.notifyUpdated()
}

func (e *CachedEntityBase[SnapT, OpT]) CommitAsNeeded() error {
	if !e.NeedCommit() {
		return nil
	}
	return e.Commit()
}

// rebaseStaged re-reads the entity and replays the staged operations onto it,
// so that the commit about to happen is parented on the current tip.
//
// This is the half of the concurrency fix that the write lock alone cannot
// give. dag.Entity.Commit finishes with an unconditional UpdateRef, so a
// commit built on a tip that has moved erases whoever moved it, with no error
// to anyone — see TestConcurrentWritersLoseOperations. entity/dag and
// repository are upstream's by contract, so a compare-and-swap ref update is
// not available; re-reading inside the lock makes the update a fast-forward in
// fact instead.
//
// Callers must hold both the write lock and e.mu.
func (e *CachedEntityBase[SnapT, OpT]) rebaseStaged() error {
	if e.reload == nil {
		return nil
	}

	staged, ok := e.entity.(stagedOperations[OpT])
	if !ok {
		return nil
	}

	ops := staged.StagedOperations()
	if len(ops) == 0 {
		return nil
	}

	fresh, err := e.reload()
	if err != nil {
		return err
	}

	for _, op := range ops {
		fresh.Append(op)
	}
	e.entity = fresh

	return nil
}

func (e *CachedEntityBase[SnapT, OpT]) NeedCommit() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.entity.NeedCommit()
}

func (e *CachedEntityBase[SnapT, OpT]) Lock() {
	e.mu.Lock()
}

func (e *CachedEntityBase[SnapT, OpT]) CreateLamportTime() lamport.Time {
	return e.entity.CreateLamportTime()
}

func (e *CachedEntityBase[SnapT, OpT]) EditLamportTime() lamport.Time {
	return e.entity.EditLamportTime()
}
