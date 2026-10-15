package host

import (
	"sort"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/query/jq"
)

// Search over the issues' id, aliases, title and description
// (doc/design/app.md, A9).
//
// It is an in-memory substring scan over the excerpts,
// which carry the description, so no entity is read:
// a few thousand issues is milliseconds.
// Comments past the description are not searched.
//
// The match is case-insensitive, because a finder is typed into
// and nobody types a title's capitals.
// The id matches by prefix, as every ID position reads it;
// an alias, the title and the description match anywhere in them.
//
// The order is how sure the match is:
// the issue an id or an alias names first,
// then a match in the title,
// then a match in the description alone;
// within each, last edited first, the order of the default list.

// Search tiers, best first.
const (
	searchById = iota
	searchByTitle
	searchByDescription
	searchMiss
)

// SearchExcerpts returns the issues whose id, alias, title or description
// contain text, best match first.
// The archived are left out unless includeArchive brings them back,
// as in the list (doc/design/include-archive.md).
// Empty text matches every issue, so a finder opened with nothing typed
// shows the default list.
//
// It returns the excerpts themselves, for a caller such as the finder that
// draws them; IssueSearch is the same answer as the command prints it.
func SearchExcerpts(repo *cache.RepoCache, text string, includeArchive bool) ([]*cache.IssueExcerpt, error) {
	excerpts, err := sortedExcerpts(repo)
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(text)

	type hit struct {
		excerpt *cache.IssueExcerpt
		tier    int
	}
	var hits []hit
	for _, excerpt := range excerpts {
		if !includeArchive && isArchived(excerpt.Fields) {
			continue
		}
		if tier := searchTier(excerpt, needle); tier != searchMiss {
			hits = append(hits, hit{excerpt, tier})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		// last edited first, as view.DefaultQuery sorts
		if a.excerpt.EditLamportTime != b.excerpt.EditLamportTime {
			return a.excerpt.EditLamportTime > b.excerpt.EditLamportTime
		}
		return a.excerpt.EditUnixTime > b.excerpt.EditUnixTime
	})

	out := make([]*cache.IssueExcerpt, len(hits))
	for i, h := range hits {
		out[i] = h.excerpt
	}
	return out, nil
}

// searchTier says where an excerpt matches a lowercased needle, best first.
func searchTier(excerpt *cache.IssueExcerpt, needle string) int {
	if strings.HasPrefix(excerpt.Id().String(), needle) {
		return searchById
	}
	for _, alias := range excerpt.Aliases() {
		if containsFold(alias, needle) {
			return searchById
		}
	}
	if containsFold(excerpt.Title(), needle) {
		return searchByTitle
	}
	if containsFold(excerpt.Description, needle) {
		return searchByDescription
	}
	return searchMiss
}

func containsFold(haystack, lowerNeedle string) bool {
	return strings.Contains(strings.ToLower(haystack), lowerNeedle)
}

// IssueSearch is `git work issue search TEXT` and `work.issue.search(text)`:
// the matching issues as one array of excerpts,
// the JSON `git work issue list` prints for each issue.
func IssueSearch(repo *cache.RepoCache, text string, includeArchive bool) (any, error) {
	excerpts, err := SearchExcerpts(repo, text, includeArchive)
	if err != nil {
		return nil, err
	}

	out := make([]cmdjson.IssueExcerpt, 0, len(excerpts))
	for _, excerpt := range excerpts {
		j, err := cmdjson.NewIssueExcerpt(repo, excerpt)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}

	return jq.Input(out)
}
