package issue

import (
	"time"

	"github.com/git-bug/git-bug/entity/dag"
)

// SnapshotAt compiles the issue as it stood at a moment in time.
//
// It replays the operations whose own wall-clock time is at or before `at`,
// in the order they are given — the DAG's order, which is what Compile walks —
// with the very same Apply every other reader uses.
// That is the point of putting it here:
// what a SetField with a null does, what set semantics an AddValue has,
// which field an archive writes,
// is this package's business and must have one implementation
// (doc/design/report.md).
//
// The cut is each operation's own time, never its lamport time:
// "since Monday" is a statement about a calendar,
// and an operation pulled late still lands in the window it was written in,
// so two clones that have exchanged their refs report the same week.
//
// An issue exists from its create operation onwards,
// so an `at` before that operation's time returns nil:
// the issue did not exist yet, and a caller says so or leaves it out.
// Operations are filtered one by one rather than truncated at the first late
// one, because wall clocks across clones are not monotonic in DAG order.
//
// The returned snapshot is fresh and owned by the caller;
// nothing here reads or writes a cache.
func SnapshotAt(ops []dag.Operation, at time.Time) *Snapshot {
	if len(ops) == 0 || ops[0].Time().After(at) {
		return nil
	}

	snap := &Snapshot{Fields: map[string]Value{}}
	for _, raw := range ops {
		op, ok := raw.(Operation)
		if !ok {
			continue
		}
		if op.Time().After(at) {
			continue
		}
		op.Apply(snap)
		snap.Operations = append(snap.Operations, op)
	}

	if len(snap.Operations) == 0 {
		return nil
	}
	return snap
}

// OperationsBetween returns the operations written in the half-open window
// [from, to), oldest first, keeping the order they were given in.
//
// Half-open so that back-to-back windows neither drop an operation nor count
// it twice. A zero `from` is the beginning of time and a zero `to` is the end
// of it, which is what an absent --from or --to means.
func OperationsBetween(ops []dag.Operation, from, to time.Time) []dag.Operation {
	var out []dag.Operation
	for _, op := range ops {
		t := op.Time()
		if !from.IsZero() && t.Before(from) {
			continue
		}
		if !to.IsZero() && !t.Before(to) {
			continue
		}
		out = append(out, op)
	}
	return out
}
