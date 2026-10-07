package host

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entity"
)

// UserMe returns the identity this repository writes as.
//
// It is `git work user me` and `work.user.me()`, the same call twice over
// (doc/design/cli-convention.md):
// a script that filters "my issues" has to ask who that is,
// and so does a shell.
func UserMe(repo *cache.RepoCache) (*cmdjson.Identity, error) {
	i, err := repo.GetUserIdentity()
	if err != nil {
		return nil, err
	}
	out := cmdjson.NewIdentity(i)
	return &out, nil
}

// UserName is an identity-valued field (an assignee, a reporter) as a person
// reads it: the name of the identity it holds, not the 64-character hash the
// field stores.
//
// A Jira user becomes an identity named after its Jira display name (JS16),
// so an imported assignee reads as Jira shows it. An identity with no name
// falls back to its login (the Jira account id, when Jira gave no name); an
// id the store does not resolve, an identity not pulled yet, is its short
// id, because the value is still true, just not nameable.
//
// Only text is named: JSON keeps the id, because the id is the value, and
// the agent-facing contract is the stored one.
func UserName(repo *cache.RepoCache, id string) string {
	if id == "" {
		return ""
	}
	short := id
	if len(short) > entity.HumanIdLength {
		short = short[:entity.HumanIdLength]
	}

	excerpt, err := repo.Identities().ResolveExcerpt(entity.Id(id))
	if err != nil {
		// the schema check accepts a prefix (cache.schemaResolver), so a
		// value written by hand may be stored as one
		excerpt, err = repo.Identities().ResolveExcerptPrefix(id)
		if err != nil {
			return short
		}
	}
	// an identity has a name or a login, or it does not validate
	// (entities/identity), so there is no email to fall back to
	if excerpt.Name != "" {
		return excerpt.Name
	}
	if excerpt.Login != "" {
		return excerpt.Login
	}
	return short
}

// UserList returns every identity the repository knows about.
//
// It is `git work user` and `work.user.list()`, the same call twice over:
// an identity field holds an id, so a flow that reads work by person
// has to ask who the people are, and so does a shell.
func UserList(repo *cache.RepoCache) ([]cmdjson.Identity, error) {
	ids := repo.Identities().AllIds()
	out := make([]cmdjson.Identity, 0, len(ids))
	for _, id := range ids {
		excerpt, err := repo.Identities().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		out = append(out, cmdjson.NewIdentityFromExcerpt(excerpt))
	}
	return out, nil
}
