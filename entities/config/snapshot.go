package config

import (
	"sort"
	"time"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
)

var _ dag.Snapshot = &Snapshot{}

// Snapshot is the compiled form of a config entity: a plain document.
//
// Shape and key come from the create operation and never change;
// Attributes is what the four operations write,
// and what a shape's attributes mean is decided above this package.
type Snapshot struct {
	id entity.Id

	Shape      Shape
	Key        string
	Attributes map[string]Value
	Archived   bool

	Author     identity.Interface
	Actors     []identity.Interface
	CreateTime time.Time

	Operations []dag.Operation
}

// Id returns the entity identifier.
func (snap *Snapshot) Id() entity.Id {
	if snap.id == "" {
		// simply panic as it would be a coding error (no id provided at construction)
		panic("no id")
	}
	return snap.id
}

func (snap *Snapshot) AllOperations() []dag.Operation {
	return snap.Operations
}

func (snap *Snapshot) AppendOperation(op dag.Operation) {
	snap.Operations = append(snap.Operations, op)
}

// EditTime returns the last time the entity was modified.
func (snap *Snapshot) EditTime() time.Time {
	if len(snap.Operations) == 0 {
		return time.Unix(0, 0)
	}
	return snap.Operations[len(snap.Operations)-1].Time()
}

// GetCreateMetadata returns the creation metadata.
func (snap *Snapshot) GetCreateMetadata(key string) (string, bool) {
	return snap.Operations[0].GetMetadata(key)
}

// Attribute returns one attribute, and whether it is set.
func (snap *Snapshot) Attribute(name string) (Value, bool) {
	v, ok := snap.Attributes[name]
	return v, ok
}

// AttributeString returns an attribute decoded as a string, and whether it is one.
func (snap *Snapshot) AttributeString(name string) (string, bool) {
	v, ok := snap.Attributes[name]
	if !ok {
		return "", false
	}
	return String(v)
}

// AttributeNames returns the set attribute names, sorted, for deterministic output.
func (snap *Snapshot) AttributeNames() []string {
	names := make([]string, 0, len(snap.Attributes))
	for name := range snap.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Members returns the members of a folded collection, keyed by member id:
// the `values/<id>` attributes for prefix `values`, say (E3).
func (snap *Snapshot) Members(prefix string) map[string]Value {
	out := map[string]Value{}
	for name, value := range snap.Attributes {
		p, member, found := SplitName(name)
		if !found || p != prefix {
			continue
		}
		out[member] = value
	}
	return out
}

func (snap *Snapshot) setAttribute(name string, value Value) {
	if snap.Attributes == nil {
		snap.Attributes = map[string]Value{}
	}
	snap.Attributes[name] = value
}

func (snap *Snapshot) removeAttribute(name string) {
	delete(snap.Attributes, name)
}

// append the operation author to the actors list
func (snap *Snapshot) addActor(actor identity.Interface) {
	for _, a := range snap.Actors {
		if actor.Id() == a.Id() {
			return
		}
	}
	snap.Actors = append(snap.Actors, actor)
}

// HasActor reports whether the id is an actor.
func (snap *Snapshot) HasActor(id entity.Id) bool {
	for _, a := range snap.Actors {
		if a.Id() == id {
			return true
		}
	}
	return false
}
