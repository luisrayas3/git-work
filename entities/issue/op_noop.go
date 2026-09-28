package issue

import (
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity/dag"
)

// NewNoOpOp builds an operation that changes nothing but carries metadata,
// which is what the Jira sync's base marker is (jira-sync.md, JS8).
//
// NoOp has been decoded under its code since the entity existed, so a marker
// needs no new operation type and no format version: every binary reading
// this store reads it. The metadata is set before anything can ask for the
// operation's id, which hashes it.
func NewNoOpOp(author identity.Interface, unixTime int64, metadata map[string]string) *dag.NoOpOperation[*Snapshot] {
	op := dag.NewNoOpOp[*Snapshot](NoOpOp, author, unixTime)
	for key, value := range metadata {
		op.SetMetadata(key, value)
	}
	return op
}
