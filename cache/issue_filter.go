package cache

import (
	"errors"
	"strings"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query"
)

// issueFilter is a predicate over issue excerpts.
type issueFilter func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool

// labelsKey is where the presets put labels, a freeform multi-enum.
// The query language of 3c9c24d will find fields by kind and role instead of by key;
// until then the label filters of the old language map onto this key.
const labelsKey = "labels"

func issueAuthorFilter(q string) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		q = strings.ToLower(q)

		author, err := entity.Resolve[*IdentityExcerpt](resolvers, excerpt.AuthorId)
		if err != nil {
			panic(err)
		}

		return author.Match(q)
	}
}

func issueMetadataFilter(pair query.StringPair) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		if value, ok := excerpt.CreateMetadata[pair.Key]; ok {
			return value == pair.Value
		}
		return false
	}
}

func issueIdentitiesFilter(q string, pick func(*IssueExcerpt) []entity.Id) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		q = strings.ToLower(q)

		for _, id := range pick(excerpt) {
			identityExcerpt, err := entity.Resolve[*IdentityExcerpt](resolvers, id)
			if err != nil {
				panic(err)
			}

			if identityExcerpt.Match(q) {
				return true
			}
		}
		return false
	}
}

func issueTitleFilter(q string) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		return strings.Contains(
			strings.ToLower(excerpt.Title()),
			strings.ToLower(q),
		)
	}
}

// issueFieldFilter matches a field whose value is the string,
// or a list of strings containing it.
func issueFieldFilter(key, want string) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		v, ok := excerpt.Fields[key]
		if !ok {
			return false
		}
		if s, ok := issue.String(v); ok {
			return s == want
		}
		if list, ok := issue.Strings(v); ok {
			for _, s := range list {
				if s == want {
					return true
				}
			}
		}
		return false
	}
}

func issueFieldUnsetFilter(key string) issueFilter {
	return func(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
		v, ok := excerpt.Fields[key]
		if !ok {
			return true
		}
		list, ok := issue.Strings(v)
		return ok && len(list) == 0
	}
}

// issueMatcher is a collection of filters that implement a complex filter
type issueMatcher struct {
	any []([]issueFilter)
	all []issueFilter
}

// compileIssueMatcher transforms query.Filters into a matcher over issue excerpts.
//
// Status filters are refused: open and closed are categories of the status field
// that only the schema can name (configurable-schema.md D3, 3c9c24d),
// and this layer has no schema yet.
func compileIssueMatcher(filters query.Filters) (*issueMatcher, error) {
	if len(filters.Status) > 0 {
		return nil, errors.New("status filters need the schema and are not available on issues yet (3c9c24d)")
	}

	m := &issueMatcher{}

	or := func(fs []issueFilter) {
		if len(fs) > 0 {
			m.any = append(m.any, fs)
		}
	}

	var author, metadata, actor, participant []issueFilter
	for _, value := range filters.Author {
		author = append(author, issueAuthorFilter(value))
	}
	for _, value := range filters.Metadata {
		metadata = append(metadata, issueMetadataFilter(value))
	}
	for _, value := range filters.Actor {
		actor = append(actor, issueIdentitiesFilter(value, func(e *IssueExcerpt) []entity.Id { return e.Actors }))
	}
	for _, value := range filters.Participant {
		participant = append(participant, issueIdentitiesFilter(value, func(e *IssueExcerpt) []entity.Id { return e.Participants }))
	}
	or(author)
	or(metadata)
	or(actor)
	or(participant)

	for _, value := range filters.Label {
		m.all = append(m.all, issueFieldFilter(labelsKey, value))
	}
	for _, value := range filters.Title {
		m.all = append(m.all, issueTitleFilter(value))
	}
	if filters.NoLabel {
		m.all = append(m.all, issueFieldUnsetFilter(labelsKey))
	}

	return m, nil
}

// Match check if an issue matches the set of filters
func (m *issueMatcher) Match(excerpt *IssueExcerpt, resolvers entity.Resolvers) bool {
	for _, group := range m.any {
		matched := false
		for _, f := range group {
			if f(excerpt, resolvers) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, f := range m.all {
		if !f(excerpt, resolvers) {
			return false
		}
	}
	return true
}
