package cache

import (
	"sort"
	"time"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
)

// IssueCache is a wrapper around an Issue. It provides multiple functions:
//
// 1. Provide a higher level API to use than the raw API from Issue.
// 2. Maintain an up-to-date Snapshot available.
// 3. Deal with concurrency.
//
// Field values are accepted as the entity accepts them, by shape.
// Checking a value against the schema (kind, allowed values, built-ins)
// is the job of the schema layer once it exists (bb9e89e),
// and it belongs here, on the write path, not in the entity.
type IssueCache struct {
	CachedEntityBase[*issue.Snapshot, issue.Operation]
}

func NewIssueCache(i *issue.Issue, repo repository.ClockedRepo, getUserIdentity getUserIdentityFunc, entityUpdated func(id entity.Id) error, reload func() (*issue.Issue, error)) *IssueCache {
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
	}
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

// SetField sets one field; a JSON null clears it.
func (c *IssueCache) SetField(key string, value issue.Value) (*issue.SetFieldOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}

	return c.SetFieldRaw(author, time.Now().Unix(), key, value, nil)
}

func (c *IssueCache) SetFieldRaw(author identity.Interface, unixTime int64, key string, value issue.Value, metadata map[string]string) (*issue.SetFieldOperation, error) {
	c.mu.Lock()
	op, err := issue.SetField(c.entity, author, unixTime, key, value, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

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
	unixTime := time.Now().Unix()

	ops := make([]issue.Operation, 0, len(fields))
	for _, key := range sortedKeys(fields) {
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
	unixTime := time.Now().Unix()

	var ops []issue.Operation
	for _, key := range sortedKeys(items) {
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
	unixTime := time.Now().Unix()

	var ops []issue.Operation
	for _, key := range sortedKeys(items) {
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

// sortedKeys orders a map's keys, so that a plan reads and hashes the same twice.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetTitle is SetField on the title, the one field this layer names.
func (c *IssueCache) SetTitle(title string) (*issue.SetFieldOperation, error) {
	return c.SetField(issue.TitleKey, issue.StringValue(title))
}

// AddValue adds one item to a list-valued field, with set semantics.
func (c *IssueCache) AddValue(key string, item issue.Value) (*issue.AddValueOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}

	return c.AddValueRaw(author, time.Now().Unix(), key, item, nil)
}

func (c *IssueCache) AddValueRaw(author identity.Interface, unixTime int64, key string, item issue.Value, metadata map[string]string) (*issue.AddValueOperation, error) {
	c.mu.Lock()
	op, err := issue.AddValue(c.entity, author, unixTime, key, item, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

// RemoveValue removes one item from a list-valued field.
func (c *IssueCache) RemoveValue(key string, item issue.Value) (*issue.RemoveValueOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}

	return c.RemoveValueRaw(author, time.Now().Unix(), key, item, nil)
}

func (c *IssueCache) RemoveValueRaw(author identity.Interface, unixTime int64, key string, item issue.Value, metadata map[string]string) (*issue.RemoveValueOperation, error) {
	c.mu.Lock()
	op, err := issue.RemoveValue(c.entity, author, unixTime, key, item, metadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}

// EditCreateComment is a convenience function to edit the body of an issue (the first comment)
func (c *IssueCache) EditCreateComment(body string) (entity.CombinedId, *issue.EditCommentOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return entity.UnsetCombinedId, nil, err
	}

	return c.EditCreateCommentRaw(author, time.Now().Unix(), body, nil)
}

// EditCreateCommentRaw is a convenience function to edit the body of an issue (the first comment)
func (c *IssueCache) EditCreateCommentRaw(author identity.Interface, unixTime int64, body string, metadata map[string]string) (entity.CombinedId, *issue.EditCommentOperation, error) {
	c.mu.Lock()
	commentId, op, err := issue.EditCreateComment(c.entity, author, unixTime, body, nil, metadata)
	c.mu.Unlock()
	if err != nil {
		return entity.UnsetCombinedId, nil, err
	}
	return commentId, op, c.notifyUpdated()
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

func (c *IssueCache) SetMetadata(target entity.Id, newMetadata map[string]string) (*dag.SetMetadataOperation[*issue.Snapshot], error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, err
	}

	return c.SetMetadataRaw(author, time.Now().Unix(), target, newMetadata)
}

func (c *IssueCache) SetMetadataRaw(author identity.Interface, unixTime int64, target entity.Id, newMetadata map[string]string) (*dag.SetMetadataOperation[*issue.Snapshot], error) {
	c.mu.Lock()
	op, err := issue.SetMetadata(c.entity, author, unixTime, target, newMetadata)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return op, c.notifyUpdated()
}
