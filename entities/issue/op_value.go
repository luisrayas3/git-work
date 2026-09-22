package issue

import (
	"bytes"
	"sort"

	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/util/timestamp"
)

var _ Operation = &AddValueOperation{}
var _ Operation = &RemoveValueOperation{}

// AddValueOperation adds one item to a list-valued field, with set semantics.
//
// SetField replaces a field whole, last writer wins,
// which is wrong for labels, components, reviewers or the targets of a
// many-cardinality relation:
// two people adding two different items concurrently must both win.
// Per-item operations give that,
// the same way git-bug's label change did and the config entity's SetItem does.
// Adding an item that is present is a no-op.
// If the field holds something that is not a list, it is treated as empty.
type AddValueOperation struct {
	dag.OpBase
	Key  string `json:"key"`
	Item Value  `json:"item"`
}

func (op *AddValueOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *AddValueOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())

	item := canonical(op.Item)
	items := snapshot.Items(op.Key)
	if !containsItem(items, item) {
		items = append(items, item)
		sortItems(items)
	}
	snapshot.setField(op.Key, ItemsValue(items))

	id := op.Id()
	snapshot.Timeline = append(snapshot.Timeline, &ValueTimelineItem{
		combinedId: entity.CombineIds(snapshot.Id(), id),
		Author:     op.Author(),
		UnixTime:   timestamp.Timestamp(op.UnixTime),
		Key:        op.Key,
		Item:       item,
		Added:      true,
	})
}

func (op *AddValueOperation) Validate() error {
	if err := op.OpBase.Validate(op, AddValueOp); err != nil {
		return err
	}
	return validateKeyAndItem(op.Key, op.Item)
}

func NewAddValueOp(author identity.Interface, unixTime int64, key string, item Value) *AddValueOperation {
	return &AddValueOperation{
		OpBase: dag.NewOpBase(AddValueOp, author, unixTime),
		Key:    key,
		Item:   item,
	}
}

// RemoveValueOperation removes one item from a list-valued field.
// Removing an item that is absent is a no-op;
// removing from a field that is unset or not a list changes nothing.
type RemoveValueOperation struct {
	dag.OpBase
	Key  string `json:"key"`
	Item Value  `json:"item"`
}

func (op *RemoveValueOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *RemoveValueOperation) Apply(snapshot *Snapshot) {
	snapshot.addActor(op.Author())

	item := canonical(op.Item)
	if current, ok := snapshot.Fields[op.Key]; ok {
		if items, isList := Items(current); isList {
			kept := make([]Value, 0, len(items))
			for _, it := range items {
				if !bytes.Equal(canonical(it), item) {
					kept = append(kept, it)
				}
			}
			snapshot.setField(op.Key, ItemsValue(kept))
		}
	}

	id := op.Id()
	snapshot.Timeline = append(snapshot.Timeline, &ValueTimelineItem{
		combinedId: entity.CombineIds(snapshot.Id(), id),
		Author:     op.Author(),
		UnixTime:   timestamp.Timestamp(op.UnixTime),
		Key:        op.Key,
		Item:       item,
		Added:      false,
	})
}

func (op *RemoveValueOperation) Validate() error {
	if err := op.OpBase.Validate(op, RemoveValueOp); err != nil {
		return err
	}
	return validateKeyAndItem(op.Key, op.Item)
}

func NewRemoveValueOp(author identity.Interface, unixTime int64, key string, item Value) *RemoveValueOperation {
	return &RemoveValueOperation{
		OpBase: dag.NewOpBase(RemoveValueOp, author, unixTime),
		Key:    key,
		Item:   item,
	}
}

func validateKeyAndItem(key string, item Value) error {
	if err := ValidateKey(key); err != nil {
		return errors.Wrap(err, "field")
	}
	if err := ValidateItem(key, item); err != nil {
		return errors.Wrapf(err, "field %s", key)
	}
	return nil
}

func containsItem(items []Value, item Value) bool {
	for _, it := range items {
		if bytes.Equal(canonical(it), item) {
			return true
		}
	}
	return false
}

// sortItems orders items by their canonical bytes,
// so a set reads the same whatever order its items arrived in.
func sortItems(items []Value) {
	sort.Slice(items, func(i, j int) bool {
		return bytes.Compare(canonical(items[i]), canonical(items[j])) < 0
	})
}

// AddValue is a convenience function to add an item to a list-valued field
func AddValue(i Interface, author identity.Interface, unixTime int64, key string, item Value, metadata map[string]string) (*AddValueOperation, error) {
	op := NewAddValueOp(author, unixTime, key, item)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	i.Append(op)
	return op, nil
}

// RemoveValue is a convenience function to remove an item from a list-valued field
func RemoveValue(i Interface, author identity.Interface, unixTime int64, key string, item Value, metadata map[string]string) (*RemoveValueOperation, error) {
	op := NewRemoveValueOp(author, unixTime, key, item)
	for k, v := range metadata {
		op.SetMetadata(k, v)
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	i.Append(op)
	return op, nil
}
