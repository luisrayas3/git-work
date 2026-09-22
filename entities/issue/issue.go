// Package issue holds the issue data model and its low-level functions.
//
// It is the owned successor of entities/bug (f4bac00):
// an issue is a structural core
// (id, author, timestamps, comments, timeline, actors and participants, relations)
// plus a fields map,
// and every property an issue has, title and status included, is a field.
// What a field means is decided by the schema, above this package;
// here an operation is validated for shape only,
// because a Validate that fails makes history unreadable.
package issue

import (
	"fmt"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

var _ Interface = &Issue{}
var _ entity.Interface = &Issue{}

// 1: original format
// 2: no more legacy identities
// 3: Ids are generated from the create operation serialized data instead of from the first git commit
// 4: with DAG entity framework
// 5: the owned model: fields map and relations, no title/status/label operations
//
// The version gate in entity/dag refuses a mismatch outright,
// which is the point:
// a binary that predates the model refuses the store instead of misreading it.
const formatVersion = 5

const Typename = "issue"

// Namespace is final (483dbe2): every git-work namespace carries the work- prefix
// so it reads as ours next to other refs.
// entities/bug keeps reading refs/issues/* beside this until the store is migrated (bf6f392);
// the two formats can not share a namespace,
// because a format-5 reader rejects a format-4 pack and vice versa.
// Entity ids do not depend on the namespace.
const Namespace = "work-issues"

var def = dag.Definition{
	Typename:             Typename,
	Namespace:            Namespace,
	OperationUnmarshaler: operationUnmarshaler,
	FormatVersion:        formatVersion,
}

var ClockLoader = dag.ClockLoader(def)

type Interface interface {
	dag.Interface[*Snapshot, Operation]
}

// Issue holds the data of an issue thread, organized in a way close to
// how it will be persisted inside Git. This is the data structure
// used to merge two different versions of the same Issue.
type Issue struct {
	*dag.Entity
}

// NewIssue create a new Issue
func NewIssue() *Issue {
	return wrapper(dag.New(def))
}

func wrapper(e *dag.Entity) *Issue {
	return &Issue{Entity: e}
}

func simpleResolvers(repo repository.ClockedRepo) entity.Resolvers {
	return entity.Resolvers{
		&identity.Identity{}: identity.NewSimpleResolver(repo),
	}
}

// Read will read an issue from a repository
func Read(repo repository.ClockedRepo, id entity.Id) (*Issue, error) {
	return ReadWithResolver(repo, simpleResolvers(repo), id)
}

// ReadWithResolver will read an issue from its Id, with custom resolvers
func ReadWithResolver(repo repository.ClockedRepo, resolvers entity.Resolvers, id entity.Id) (*Issue, error) {
	return dag.Read(def, wrapper, repo, resolvers, id)
}

// ReadAll read and parse all local issues
func ReadAll(repo repository.ClockedRepo) <-chan entity.StreamedEntity[*Issue] {
	return dag.ReadAll(def, wrapper, repo, simpleResolvers(repo))
}

// ReadAllWithResolver read and parse all local issues
func ReadAllWithResolver(repo repository.ClockedRepo, resolvers entity.Resolvers) <-chan entity.StreamedEntity[*Issue] {
	return dag.ReadAll(def, wrapper, repo, resolvers)
}

// ListLocalIds list all the available local issue ids
func ListLocalIds(repo repository.Repo) ([]entity.Id, error) {
	return dag.ListLocalIds(def, repo)
}

// Validate check if the Issue data is valid
func (i *Issue) Validate() error {
	if err := i.Entity.Validate(); err != nil {
		return err
	}

	// The very first Op should be a CreateOp
	firstOp := i.FirstOp()
	if firstOp == nil || firstOp.Type() != CreateOp {
		return fmt.Errorf("first operation should be a Create op")
	}

	// Check that there is no more CreateOp op
	for idx, op := range i.Entity.Operations() {
		if idx == 0 {
			continue
		}
		if op.Type() == CreateOp {
			return fmt.Errorf("only one Create op allowed")
		}
	}

	return nil
}

// Append add a new Operation to the Issue
func (i *Issue) Append(op Operation) {
	i.Entity.Append(op)
}

// Operations return the ordered operations
func (i *Issue) Operations() []Operation {
	source := i.Entity.Operations()
	result := make([]Operation, len(source))
	for idx, op := range source {
		result[idx] = op.(Operation)
	}
	return result
}

// Compile an issue in an easily usable snapshot
func (i *Issue) Compile() *Snapshot {
	snap := &Snapshot{
		id:     i.Id(),
		Fields: map[string]Value{},
	}

	for _, op := range i.Operations() {
		op.Apply(snap)
		snap.Operations = append(snap.Operations, op)
	}

	return snap
}

// FirstOp lookup for the very first operation of the issue.
// For a valid Issue, this operation should be a CreateOp
func (i *Issue) FirstOp() Operation {
	if fo := i.Entity.FirstOp(); fo != nil {
		return fo.(Operation)
	}
	return nil
}

// LastOp lookup for the very last operation of the issue.
// For a valid Issue, should never be nil
func (i *Issue) LastOp() Operation {
	if lo := i.Entity.LastOp(); lo != nil {
		return lo.(Operation)
	}
	return nil
}
