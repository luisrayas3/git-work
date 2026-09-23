package cache

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

type RepoCacheIssue struct {
	*SubCache[*issue.Issue, *IssueExcerpt, *IssueCache]

	// checker compiles the live schema, which a create is measured against
	// before the entity is written (bb9e89e).
	checker checkerFunc
}

// AliasMetadataPrefix is the create-op metadata key prefix that holds an
// issue's external ids: `alias:jira` = "PROJ-12".
//
// The create operation is the one immutable place in the entity,
// which is what an alias has to be (483dbe2).
const AliasMetadataPrefix = "alias:"

func NewRepoCacheIssue(repo repository.ClockedRepo,
	resolvers func() entity.Resolvers,
	getUserIdentity getUserIdentityFunc,
	checker checkerFunc) *RepoCacheIssue {

	makeCached := func(i *issue.Issue, entityUpdated func(id entity.Id) error) *IssueCache {
		reload := func() (*issue.Issue, error) {
			return issue.ReadWithResolver(repo, resolvers(), i.Id())
		}
		return NewIssueCache(i, repo, getUserIdentity, entityUpdated, reload, checker)
	}

	actions := Actions[*issue.Issue]{
		ReadWithResolver:    issue.ReadWithResolver,
		ReadAllWithResolver: issue.ReadAllWithResolver,
		Remove:              issue.Remove,
		RemoveAll:           issue.RemoveAll,
		MergeAll:            issue.MergeAll,
	}

	sc := NewSubCache[*issue.Issue, *IssueExcerpt, *IssueCache](
		repo, resolvers, getUserIdentity,
		makeCached, NewIssueExcerpt, actions,
		issue.Typename, issue.Namespace,
		formatVersion, defaultMaxLoadedBugs,
	)

	return &RepoCacheIssue{SubCache: sc, checker: checker}
}

// checkNew measures a new issue against the live schema.
//
// The type is the one field a create can not do without once a schema exists:
// without it nothing can say which fields the issue has (D2).
// With no type entity at all, nothing is checked — the bootstrap state (E4).
func (c *RepoCacheIssue) checkNew(title string, fields map[string]issue.Value) error {
	if c.checker == nil {
		return nil
	}
	checker, err := c.checker()
	if err != nil {
		return err
	}

	all := rawValues(fields)
	all[schema.TitleKey] = json.RawMessage(issue.StringValue(title))

	return checker.CheckNew(all)
}

// ResolveAlias retrieves the issue carrying the given external id under any
// alias:<name> key of its create operation. It fails if several match.
func (c *RepoCacheIssue) ResolveAlias(alias string) (*IssueCache, error) {
	return c.ResolveMatcher(func(excerpt *IssueExcerpt) bool {
		return excerpt.HasAlias(alias)
	})
}

// ResolvePrefixOrAlias resolves an id prefix, and falls back to an alias when
// no issue has that prefix: every ID position of the command line takes both
// (483dbe2), so that a Jira key can be pasted wherever an id fits.
//
// An ambiguous alias is an error of its own;
// anything else reports the id prefix's failure, which is the common case.
func (c *RepoCacheIssue) ResolvePrefixOrAlias(prefix string) (*IssueCache, error) {
	i, err := c.ResolvePrefix(prefix)
	if err == nil {
		return i, nil
	}

	i, aliasErr := c.ResolveAlias(prefix)
	if aliasErr == nil {
		return i, nil
	}
	var multiple *entity.ErrMultipleMatch
	if errors.As(aliasErr, &multiple) {
		return nil, aliasErr
	}
	return nil, err
}

// ResolveIssueCreateMetadata retrieve an issue that has the exact given metadata on
// its Create operation, that is, the first operation. It fails if multiple issues
// match.
func (c *RepoCacheIssue) ResolveIssueCreateMetadata(key string, value string) (*IssueCache, error) {
	return c.ResolveMatcher(func(excerpt *IssueExcerpt) bool {
		return excerpt.CreateMetadata[key] == value
	})
}

// ResolveComment search for an Issue/Comment combination matching the merged
// issue/comment Id prefix. Returns the Issue containing the Comment and the Comment's
// Id.
func (c *RepoCacheIssue) ResolveComment(prefix string) (*IssueCache, entity.CombinedId, error) {
	issuePrefix, _ := entity.SeparateIds(prefix)
	candidates := make([]entity.Id, 0, 5)

	// build a list of possible matching issues
	c.mu.RLock()
	for _, excerpt := range c.excerpts {
		if excerpt.Id().HasPrefix(issuePrefix) {
			candidates = append(candidates, excerpt.Id())
		}
	}
	c.mu.RUnlock()

	matchingIds := make([]entity.Id, 0, 5)
	matchingCommentId := entity.UnsetCombinedId
	var matching *IssueCache

	// search for matching comments
	// searching every candidate allow for some collision with the issue prefix only,
	// before being refined with the full comment prefix
	for _, id := range candidates {
		i, err := c.Resolve(id)
		if err != nil {
			return nil, entity.UnsetCombinedId, err
		}

		for _, comment := range i.Snapshot().Comments {
			if comment.CombinedId().HasPrefix(prefix) {
				matchingIds = append(matchingIds, id)
				matching = i
				matchingCommentId = comment.CombinedId()
			}
		}
	}

	if len(matchingIds) > 1 {
		return nil, entity.UnsetCombinedId, entity.NewErrMultipleMatch("issue/comment", matchingIds)
	} else if len(matchingIds) == 0 {
		return nil, entity.UnsetCombinedId, errors.New("comment doesn't exist")
	}

	return matching, matchingCommentId, nil
}

// issueMatchesTerms reports whether every term appears, case-insensitively,
// in the excerpt's title or one of its string-valued fields.
func issueMatchesTerms(excerpt *IssueExcerpt, terms []string) bool {
	var haystack strings.Builder
	for _, v := range excerpt.Fields {
		if s, ok := issue.String(v); ok {
			haystack.WriteString(strings.ToLower(s))
			haystack.WriteByte(0)
			continue
		}
		if list, ok := issue.Strings(v); ok {
			for _, s := range list {
				haystack.WriteString(strings.ToLower(s))
				haystack.WriteByte(0)
			}
		}
	}
	hay := haystack.String()

	for _, term := range terms {
		if !strings.Contains(hay, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

// Query return the id of all Issues matching the given Query
func (c *RepoCacheIssue) Query(q *query.Query) ([]entity.Id, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if q == nil {
		return c.AllIds(), nil
	}

	matcher, err := compileIssueMatcher(q.Filters)
	if err != nil {
		return nil, err
	}

	var filtered []*IssueExcerpt

	for _, excerpt := range c.excerpts {
		if q.Search != nil && !issueMatchesTerms(excerpt, q.Search) {
			continue
		}
		if matcher.Match(excerpt, c.resolvers()) {
			filtered = append(filtered, excerpt)
		}
	}

	var sorter sort.Interface

	switch q.OrderBy {
	case query.OrderById:
		sorter = IssuesById(filtered)
	case query.OrderByCreation:
		sorter = IssuesByCreationTime(filtered)
	case query.OrderByEdit:
		sorter = IssuesByEditTime(filtered)
	default:
		return nil, errors.New("missing sort type")
	}

	switch q.OrderDirection {
	case query.OrderAscending:
		// Nothing to do
	case query.OrderDescending:
		sorter = sort.Reverse(sorter)
	default:
		return nil, errors.New("missing sort direction")
	}

	sort.Sort(sorter)

	result := make([]entity.Id, len(filtered))

	for i, val := range filtered {
		result[i] = val.Id()
	}

	return result, nil
}

// New create a new issue
// The new issue is written in the repository (commit)
func (c *RepoCacheIssue) New(title string, message string, fields map[string]issue.Value) (*IssueCache, *issue.CreateOperation, error) {
	return c.NewWithFiles(title, message, nil, fields)
}

// NewWithMetadata creates a new issue with metadata on its create operation,
// which is where aliases live (483dbe2).
// The new issue is written in the repository (commit)
func (c *RepoCacheIssue) NewWithMetadata(title string, message string, fields map[string]issue.Value, metadata map[string]string) (*IssueCache, *issue.CreateOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, nil, err
	}

	return c.NewRaw(author, time.Now().Unix(), title, message, nil, fields, metadata)
}

// NewWithFiles create a new issue with attached files for the message
// The new issue is written in the repository (commit)
func (c *RepoCacheIssue) NewWithFiles(title string, message string, files []repository.Hash, fields map[string]issue.Value) (*IssueCache, *issue.CreateOperation, error) {
	author, err := c.getUserIdentity()
	if err != nil {
		return nil, nil, err
	}

	return c.NewRaw(author, time.Now().Unix(), title, message, files, fields, nil)
}

// NewRaw create a new issue with attached files for the message, initial fields, as
// well as metadata for the Create operation.
// The new issue is written in the repository (commit)
func (c *RepoCacheIssue) NewRaw(author identity.Interface, unixTime int64, title string, message string, files []repository.Hash, fields map[string]issue.Value, metadata map[string]string) (*IssueCache, *issue.CreateOperation, error) {
	if err := c.checkNew(title, fields); err != nil {
		return nil, nil, err
	}

	i, op, err := issue.Create(author, unixTime, title, message, files, fields, metadata)
	if err != nil {
		return nil, nil, err
	}

	// A new entity has no ref yet, so there is nothing to rebase onto — but
	// Commit increments the lamport clocks, which is a read-modify-write on a
	// file two processes must not interleave.
	unlock, err := lockWrite(c.repo)
	if err != nil {
		return nil, nil, err
	}
	err = i.Commit(c.repo)
	unlock()
	if err != nil {
		return nil, nil, err
	}

	cached, err := c.add(i)
	if err != nil {
		return nil, nil, err
	}

	return cached, op, nil
}
