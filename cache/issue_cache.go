package cache

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
)

// IssueCache is a wrapper around an Issue. It provides multiple functions:
//
// 1. Provide a higher level API to use than the raw API from Issue.
// 2. Maintain an up-to-date Snapshot available.
// 3. Deal with concurrency.
//
// Field values are accepted as the entity accepts them, by shape.
// Checking a value against the schema — kind, allowed values, target types —
// happens here, on the write path (bb9e89e, E6), at planning time,
// so that `--dry-run` reports what a commit would refuse.
// The entity stays free of any schema import,
// because an operation's Validate is frozen (D6).
type IssueCache struct {
	CachedEntityBase[*issue.Snapshot, issue.Operation]

	// checker compiles the live schema. It is a function rather than a value
	// because the schema changes under a long-lived cache: the ref watcher
	// refreshes the config subcache, and a plan must see what is there now.
	checker checkerFunc
}

// checkerFunc hands the issue cache the live schema's write checks.
type checkerFunc func() (*schema.Checker, error)

func NewIssueCache(i *issue.Issue, repo repository.ClockedRepo, getUserIdentity getUserIdentityFunc, entityUpdated func(id entity.Id) error, reload func() (*issue.Issue, error), checker checkerFunc) *IssueCache {
	return &IssueCache{
		CachedEntityBase: CachedEntityBase[*issue.Snapshot, issue.Operation]{
			repo:            repo,
			entityUpdated:   entityUpdated,
			getUserIdentity: getUserIdentity,
			entity:          &withSnapshot[*issue.Snapshot, issue.Operation]{Interface: i},
			reload: func() (dag.Interface[*issue.Snapshot, issue.Operation], error) {
				fresh, err := reload()
				if err != nil {
					return nil, err
				}
				return &withSnapshot[*issue.Snapshot, issue.Operation]{Interface: fresh}, nil
			},
		},
		checker: checker,
	}
}

// typeKey is the issue's type as it stands, the key to the rest of the schema.
func (c *IssueCache) typeKey() string {
	return issueTypeOf(c.Snapshot().Fields)
}

func (c *IssueCache) AddComment(message string) (entity.CombinedId, *issue.AddCommentOperation, error) {
	return c.AddCommentWithFiles(message, nil)
}

func (c *IssueCache) AddCommentWithFiles(message string, files []repository.Hash) (entity.CombinedId, *issue.AddCommentOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return entity.UnsetCombinedId, nil, err
	}

	return c.AddCommentRaw(author, time.Now().Unix(), message, files, nil)
}

func (c *IssueCache) AddCommentRaw(author identity.Interface, unixTime int64, message string, files []repository.Hash, metadata map[string]string) (entity.CombinedId, *issue.AddCommentOperation, error) {
	c.mu.Lock()
	commentId, op, err := issue.AddComment(c.entity, author, unixTime, message, files, metadata)
	c.mu.Unlock()
	if err != nil {
		return entity.UnsetCombinedId, nil, err
	}
	return commentId, op, c.notifyUpdated()
}

// A field is written through PlanSetFields, PlanAddValues or
// PlanRemoveValues and then CommitOperations, never one operation at a time:
// the plan is what the schema check runs over and what --dry-run prints, so a
// one-field shortcut past it would be a second way to write that nothing
// checks (bb9e89e).

// PlanSetFields builds the operations that SetFields would commit, one
// SetFieldOperation per key, in key order, and validates every one of them
// before returning any.
//
// Nothing is appended and nothing is written:
// this is what a writer's --dry-run prints,
// and what the same writer commits a moment later.
func (c *IssueCache) PlanSetFields(fields map[string]issue.Value) ([]issue.Operation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	if err := c.checkFields(fields); err != nil {
		return nil, err
	}
	unixTime := time.Now().Unix()

	ops := make([]issue.Operation, 0, len(fields))
	for _, key := range sorted.Keys(fields) {
		op := issue.NewSetFieldOp(author, unixTime, key, fields[key])
		if err := op.Validate(); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// PlanAddValues builds the operations that add every item of every key,
// one AddValueOperation per item, keys in order and items as given.
func (c *IssueCache) PlanAddValues(items map[string][]issue.Value) ([]issue.Operation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	if err := c.checkItems(items); err != nil {
		return nil, err
	}
	unixTime := time.Now().Unix()

	var ops []issue.Operation
	for _, key := range sorted.Keys(items) {
		for _, item := range items[key] {
			op := issue.NewAddValueOp(author, unixTime, key, item)
			if err := op.Validate(); err != nil {
				return nil, err
			}
			ops = append(ops, op)
		}
	}
	return ops, nil
}

// PlanRemoveValues is PlanAddValues' mirror, one RemoveValueOperation per item.
func (c *IssueCache) PlanRemoveValues(items map[string][]issue.Value) ([]issue.Operation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}
	if err := c.checkItems(items); err != nil {
		return nil, err
	}
	unixTime := time.Now().Unix()

	var ops []issue.Operation
	for _, key := range sorted.Keys(items) {
		for _, item := range items[key] {
			op := issue.NewRemoveValueOp(author, unixTime, key, item)
			if err := op.Validate(); err != nil {
				return nil, err
			}
			ops = append(ops, op)
		}
	}
	return ops, nil
}

// checkFields measures a `set` against the live schema,
// bubbling every problem as one error before anything is written.
//
// An empty schema validates nothing, which is the bootstrap state (E4):
// a repository with no config entities takes every write.
func (c *IssueCache) checkFields(fields map[string]issue.Value) error {
	checker, err := c.liveChecker()
	if err != nil || checker == nil {
		return err
	}
	return checker.CheckFields(c.typeKey(), rawValues(fields))
}

// checkItems measures an `add` or a `remove` against the live schema.
func (c *IssueCache) checkItems(items map[string][]issue.Value) error {
	checker, err := c.liveChecker()
	if err != nil || checker == nil {
		return err
	}

	raw := make(map[string][]json.RawMessage, len(items))
	for key, list := range items {
		out := make([]json.RawMessage, len(list))
		for at, item := range list {
			out[at] = json.RawMessage(item)
		}
		raw[key] = out
	}

	return checker.CheckItems(c.typeKey(), raw)
}

func (c *IssueCache) liveChecker() (*schema.Checker, error) {
	if c.checker == nil {
		return nil, nil
	}
	return c.checker()
}

// rawValues passes issue values to the schema layer as the JSON they are.
func rawValues(fields map[string]issue.Value) map[string]json.RawMessage {
	raw := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		raw[key] = json.RawMessage(value)
	}
	return raw
}

// CommitOperations appends a planned list of operations and commits them as one.
//
// A commit is the write unit (cli-convention.md):
// `set` over three keys is three operations in one commit,
// so that a reader never sees a half-applied document
// and the log reads as the one change it was.
func (c *IssueCache) CommitOperations(ops []issue.Operation) error {
	if len(ops) == 0 {
		return nil
	}

	c.mu.Lock()
	for _, op := range ops {
		c.entity.Append(op)
	}
	c.mu.Unlock()

	if err := c.notifyUpdated(); err != nil {
		return err
	}
	return c.Commit()
}

// Update takes the write lock, reads the issue again, and hands fn the fresh
// snapshot. The operations fn returns are validated and schema-checked as one
// change — against the type they set, if they set one — then appended and
// committed behind one ref update. No operations: nothing is written.
//
// It is the read-modify-commit a plan made outside the lock cannot be: a
// decision that depends on the issue's current state (a 3-way merge, say) is
// made on the entity as it is under the lock, so nothing committed in between
// is decided against blind (concurrent-store.md).
//
// fn runs under the entity's mutex: it may read other excerpts and a
// *schema.Checker obtained beforehand, but must not call a cache writer,
// c.Snapshot(), or the network. The lock is not re-entrant, and it is held
// for as long as fn runs.
func (c *IssueCache) Update(fn func(snap *issue.Snapshot) ([]issue.Operation, error)) error {
	unlock, err := lockWrite(c.repo)
	if err != nil {
		return err
	}
	defer unlock()

	c.mu.Lock()
	var staged []issue.Operation
	if s, ok := c.entity.(stagedOperations[issue.Operation]); ok {
		staged = s.StagedOperations()
	}
	if err := c.reloadLocked(staged); err != nil {
		c.mu.Unlock()
		return err
	}
	snap := c.entity.Compile()
	ops, err := fn(snap)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}

	// The check runs outside the entity's mutex, because resolving a relation
	// by alias reads a snapshot, possibly this one's. Nothing can commit in
	// between: the write lock is still held.
	if err := c.checkOperations(issueTypeOf(snap.Fields), ops); err != nil {
		return err
	}

	c.mu.Lock()
	for _, op := range ops {
		c.entity.Append(op)
	}
	err = c.commitLocked()
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return c.notifyUpdated()
}

// checkOperations validates a batch of operations and measures its field
// writes against the live schema, every problem reported at once.
//
// The batch is one change, so every field operation is checked against the
// type the batch leaves the issue with: a type set anywhere in it is the type
// its other keys belong to. Comments and metadata have no schema.
func (c *IssueCache) checkOperations(currentType string, ops []issue.Operation) error {
	for _, op := range ops {
		if err := op.Validate(); err != nil {
			return err
		}
	}

	checker, err := c.liveChecker()
	if err != nil || checker == nil {
		return err
	}

	typeKey := currentType
	for _, op := range ops {
		if set, ok := op.(*issue.SetFieldOperation); ok && set.Key == schema.TypeKey {
			if next, ok := issue.String(set.Value); ok {
				typeKey = next
			}
		}
	}

	problems := &schema.Problems{}
	collect := func(err error) error {
		var p *schema.Problems
		if errors.As(err, &p) {
			// one op at a time, so an unknown type would be said once per op
			for _, line := range p.List {
				if !slices.Contains(problems.List, line) {
					problems.List = append(problems.List, line)
				}
			}
			return nil
		}
		return err
	}
	for _, op := range ops {
		var err error
		switch op := op.(type) {
		case *issue.SetFieldOperation:
			err = checker.CheckFields(typeKey, map[string]json.RawMessage{op.Key: json.RawMessage(op.Value)})
		case *issue.AddValueOperation:
			err = checker.CheckItems(typeKey, map[string][]json.RawMessage{op.Key: {json.RawMessage(op.Item)}})
		case *issue.RemoveValueOperation:
			err = checker.CheckItems(typeKey, map[string][]json.RawMessage{op.Key: {json.RawMessage(op.Item)}})
		}
		if err := collect(err); err != nil {
			return err
		}
	}
	if len(problems.List) > 0 {
		return problems
	}
	return nil
}

func (c *IssueCache) EditComment(target entity.CombinedId, message string) (*issue.EditCommentOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}

	return c.EditCommentRaw(author, time.Now().Unix(), target, message, nil)
}

func (c *IssueCache) EditCommentRaw(author identity.Interface, unixTime int64, target entity.CombinedId, message string, metadata map[string]string) (*issue.EditCommentOperation, error) {
	comment, err := c.Snapshot().SearchComment(target)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	commentId, op, err := issue.EditComment(c.entity, author, unixTime, comment.TargetId(), message, nil, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if commentId != target {
		panic("EditComment returned unexpected comment id")
	}
	return op, c.notifyUpdated()
}
