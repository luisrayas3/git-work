// Package config holds the config entity:
// the one storage form every configurable thing in the tracker shares
// (doc/design/config-entity.md, `3556569`).
//
// A config entity is a plain document —
// a shape, a key, a map of attributes with last-writer-wins per attribute,
// and an archived flag —
// and four operations write it (E2).
// What differs between an issue type, a field and a flow
// is meaning, consumer and namespace, not storage (E1),
// so the entity code here is written once
// and parameterised by a Store, which carries the dag.Definition
// and the shapes its namespace accepts.
//
// Two definitions live in this one package
// because two subcaches cannot share a Typename
// and the two namespaces are separate merge tiers.
//
// As in entities/issue,
// an operation is validated for shape only:
// a Validate that fails makes history unreadable (E6, fact 2),
// so everything a later binary might learn
// is checked on the write path instead.
package config

import (
	"fmt"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// Shape is what a config entity configures.
//
// It is fixed by the create operation and never changes.
// The word "shape" is deliberate:
// `kind` is a field's data type and nothing else uses the word (E2).
type Shape string

const (
	ShapeType  Shape = "type"
	ShapeField Shape = "field"
	ShapeFlow  Shape = "flow"
)

// 1: the original config document: shape, key, attributes, archived
const formatVersion = 1

const (
	// SchemaTypename names the schema entity for human consumption,
	// and is the key the observer switch and the cache file use.
	SchemaTypename = "schema"
	// SchemaNamespace holds types and fields,
	// which are imported, exported and reconciled together (E1).
	SchemaNamespace = "work-schema"

	FlowTypename  = "flow"
	FlowNamespace = "work-flows"
)

// SchemaDefinition is the dag definition of the schema namespace.
var SchemaDefinition = dag.Definition{
	Typename:             SchemaTypename,
	Namespace:            SchemaNamespace,
	OperationUnmarshaler: operationUnmarshaler,
	FormatVersion:        formatVersion,
}

// FlowDefinition is the dag definition of the flow namespace.
var FlowDefinition = dag.Definition{
	Typename:             FlowTypename,
	Namespace:            FlowNamespace,
	OperationUnmarshaler: operationUnmarshaler,
	FormatVersion:        formatVersion,
}

// ClockLoader loads the lamport clocks of both config namespaces.
var ClockLoader = dag.ClockLoader(SchemaDefinition, FlowDefinition)

// Store binds the generic entity code to one definition.
//
// It is the package's whole surface for reading and writing:
// every function that would otherwise be a package function
// parameterised by a dag.Definition is a method here.
type Store struct {
	def    dag.Definition
	shapes []Shape
}

// Schema is the store of types and fields, under refs/work-schema.
var Schema = &Store{
	def:    SchemaDefinition,
	shapes: []Shape{ShapeType, ShapeField},
}

// Flows is the store of flows, under refs/work-flows.
var Flows = &Store{
	def:    FlowDefinition,
	shapes: []Shape{ShapeFlow},
}

// Definition returns the dag definition this store reads and writes.
func (s *Store) Definition() dag.Definition {
	return s.def
}

// Typename returns the entity type name of this store's namespace.
func (s *Store) Typename() string {
	return s.def.Typename
}

// Namespace returns the git namespace, without the refs/ prefix.
func (s *Store) Namespace() string {
	return s.def.Namespace
}

// Shapes returns the shapes this store's namespace accepts.
func (s *Store) Shapes() []Shape {
	return append([]Shape(nil), s.shapes...)
}

// AllowsShape reports whether this store's namespace accepts the shape.
func (s *Store) AllowsShape(shape Shape) bool {
	for _, allowed := range s.shapes {
		if allowed == shape {
			return true
		}
	}
	return false
}

// checkShape refuses a shape that does not belong in this namespace.
//
// This is a write-path rule, not an operation rule:
// an operation may never reject what a later binary writes (E6),
// but a store that accepted a flow among the fields
// would put it in the wrong merge tier and the wrong export.
func (s *Store) checkShape(shape Shape) error {
	if err := ValidateShape(shape); err != nil {
		return err
	}
	if !s.AllowsShape(shape) {
		return fmt.Errorf("shape %q does not belong in namespace %s, which holds %s",
			shape, s.def.Namespace, shapeList(s.shapes))
	}
	return nil
}

func shapeList(shapes []Shape) string {
	out := ""
	for i, shape := range shapes {
		if i > 0 {
			out += ", "
		}
		out += string(shape)
	}
	return out
}

var _ Interface = &Config{}
var _ entity.Interface = &Config{}

type Interface interface {
	dag.Interface[*Snapshot, Operation]
}

// Config holds the operations of one config entity,
// organized the way they are persisted in git.
type Config struct {
	*dag.Entity
	store *Store
}

// New creates an empty config entity in this store's namespace.
func (s *Store) New() *Config {
	return s.wrapper(dag.New(s.def))
}

func (s *Store) wrapper(e *dag.Entity) *Config {
	return &Config{Entity: e, store: s}
}

// Store returns the store this entity was read from or created in.
func (c *Config) Store() *Store {
	return c.store
}

func (s *Store) simpleResolvers(repo repository.ClockedRepo) entity.Resolvers {
	return entity.Resolvers{
		&identity.Identity{}: identity.NewSimpleResolver(repo),
	}
}

// Read reads one config entity from the repository.
func (s *Store) Read(repo repository.ClockedRepo, id entity.Id) (*Config, error) {
	return s.ReadWithResolver(repo, s.simpleResolvers(repo), id)
}

// ReadWithResolver reads one config entity with custom resolvers.
func (s *Store) ReadWithResolver(repo repository.ClockedRepo, resolvers entity.Resolvers, id entity.Id) (*Config, error) {
	return dag.Read(s.def, s.wrapper, repo, resolvers, id)
}

// ReadAll reads and parses all local config entities of this namespace.
func (s *Store) ReadAll(repo repository.ClockedRepo) <-chan entity.StreamedEntity[*Config] {
	return dag.ReadAll(s.def, s.wrapper, repo, s.simpleResolvers(repo))
}

// ReadAllWithResolver reads and parses all local config entities of this namespace.
func (s *Store) ReadAllWithResolver(repo repository.ClockedRepo, resolvers entity.Resolvers) <-chan entity.StreamedEntity[*Config] {
	return dag.ReadAll(s.def, s.wrapper, repo, resolvers)
}

// ListLocalIds lists the ids of every local entity of this namespace.
func (s *Store) ListLocalIds(repo repository.Repo) ([]entity.Id, error) {
	return dag.ListLocalIds(s.def, repo)
}

// Validate checks that the entity is structurally sound
// and belongs in its store's namespace.
//
// It is stricter than the operations are, on purpose:
// nothing on the read path calls it,
// so refusing a shape this binary does not know here
// costs no history.
func (c *Config) Validate() error {
	if err := c.Entity.Validate(); err != nil {
		return err
	}

	firstOp := c.FirstOp()
	if firstOp == nil || firstOp.Type() != CreateOp {
		return fmt.Errorf("first operation should be a Create op")
	}

	for idx, op := range c.Entity.Operations() {
		if idx == 0 {
			continue
		}
		if op.Type() == CreateOp {
			return fmt.Errorf("only one Create op allowed")
		}
	}

	create := firstOp.(*CreateOperation)
	if c.store != nil {
		if err := c.store.checkShape(create.Shape); err != nil {
			return err
		}
	}
	return ValidateKey(create.Shape, create.Key)
}

// Append adds a new operation to the entity's staging area.
func (c *Config) Append(op Operation) {
	c.Entity.Append(op)
}

// Operations returns the ordered operations.
func (c *Config) Operations() []Operation {
	source := c.Entity.Operations()
	result := make([]Operation, len(source))
	for idx, op := range source {
		result[idx] = op.(Operation)
	}
	return result
}

// Compile builds the snapshot by applying the operations in the dag's order,
// which is what makes last-writer-wins per attribute deterministic
// across clones with no code of ours (E2, fact 1).
func (c *Config) Compile() *Snapshot {
	snap := &Snapshot{
		id:         c.Id(),
		Attributes: map[string]Value{},
	}

	for _, op := range c.Operations() {
		op.Apply(snap)
		snap.Operations = append(snap.Operations, op)
	}

	return snap
}

// FirstOp looks up the very first operation, a CreateOperation on a valid entity.
func (c *Config) FirstOp() Operation {
	if fo := c.Entity.FirstOp(); fo != nil {
		return fo.(Operation)
	}
	return nil
}

// LastOp looks up the very last operation.
func (c *Config) LastOp() Operation {
	if lo := c.Entity.LastOp(); lo != nil {
		return lo.(Operation)
	}
	return nil
}
