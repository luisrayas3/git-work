package cache

import (
	"bytes"
	"encoding/json"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// Every issue write resolves its relation values to full ids here,
// before the schema check and before an operation is built
// (2086c12, schema.FullIdValue):
// a create (newRaw), a plan (PlanSetFields, PlanAddValues,
// PlanRemoveValues) and an Update or UpdateShape batch.
// Those are the only ways a field reaches the store,
// so every writer above — the command, Starlark, the views, the Jira pull —
// stores a full id without knowing it, and --dry-run prints one.
// What does not resolve is left as written, for the check to refuse.

// fullIdFields returns fields with every relation value as its full id,
// a copy when anything changed, fields itself otherwise.
func fullIdFields(checker *schema.Checker, typeKey string, fields map[string]issue.Value) map[string]issue.Value {
	if checker == nil {
		return fields
	}
	var out map[string]issue.Value
	for key, value := range fields {
		full := issue.Value(checker.FullIdValue(typeKey, key, json.RawMessage(value)))
		if bytes.Equal(full, value) {
			continue
		}
		if out == nil {
			out = make(map[string]issue.Value, len(fields))
			for k, v := range fields {
				out[k] = v
			}
		}
		out[key] = full
	}
	if out == nil {
		return fields
	}
	return out
}

// fullIdItems is fullIdFields for the items of an add or a remove.
//
// held is what the issue holds now, for a remove: an item it holds verbatim is
// removed verbatim, so that a value stored before 2086c12 as a prefix can
// still be taken away by naming it as it is.
func fullIdItems(checker *schema.Checker, typeKey string, items map[string][]issue.Value, held func(key string) []issue.Value) map[string][]issue.Value {
	if checker == nil {
		return items
	}
	out := make(map[string][]issue.Value, len(items))
	for key, list := range items {
		resolved := make([]issue.Value, len(list))
		for at, item := range list {
			resolved[at] = fullIdItem(checker, typeKey, key, item, held)
		}
		out[key] = resolved
	}
	return out
}

func fullIdItem(checker *schema.Checker, typeKey, key string, item issue.Value, held func(key string) []issue.Value) issue.Value {
	if held != nil && holds(held(key), item) {
		return item
	}
	return issue.Value(checker.FullIdItem(typeKey, key, json.RawMessage(item)))
}

func holds(items []issue.Value, item issue.Value) bool {
	want := compact(item)
	for _, it := range items {
		if bytes.Equal(compact(it), want) {
			return true
		}
	}
	return false
}

func compact(v issue.Value) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return v
	}
	return buf.Bytes()
}

// fullIdOperations rewrites a batch's field operations to carry full ids,
// each one that changes replaced by a fresh operation with the same author,
// time and metadata, since an operation's id may already be fixed.
//
// typeKey is the type the batch leaves the issue with, as checkOperations
// measures it; snap is the issue the batch applies to, for a remove.
func fullIdOperations(checker *schema.Checker, typeKey string, snap *issue.Snapshot, ops []issue.Operation) []issue.Operation {
	if checker == nil {
		return ops
	}
	held := func(key string) []issue.Value { return snap.Items(key) }

	out := ops
	copied := false
	replace := func(at int, op issue.Operation) {
		if !copied {
			out = append([]issue.Operation(nil), ops...)
			copied = true
		}
		out[at] = op
	}
	for at, op := range ops {
		switch op := op.(type) {
		case *issue.SetFieldOperation:
			full := issue.Value(checker.FullIdValue(typeKey, op.Key, json.RawMessage(op.Value)))
			if !bytes.Equal(full, op.Value) {
				replace(at, withMetadata(issue.NewSetFieldOp(op.Author(), op.UnixTime, op.Key, full), op.Metadata))
			}
		case *issue.AddValueOperation:
			full := fullIdItem(checker, typeKey, op.Key, op.Item, nil)
			if !bytes.Equal(full, op.Item) {
				replace(at, withMetadata(issue.NewAddValueOp(op.Author(), op.UnixTime, op.Key, full), op.Metadata))
			}
		case *issue.RemoveValueOperation:
			full := fullIdItem(checker, typeKey, op.Key, op.Item, held)
			if !bytes.Equal(full, op.Item) {
				replace(at, withMetadata(issue.NewRemoveValueOp(op.Author(), op.UnixTime, op.Key, full), op.Metadata))
			}
		}
	}
	return out
}

func withMetadata[T interface{ SetMetadata(key, value string) }](op T, metadata map[string]string) T {
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	return op
}
