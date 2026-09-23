package host

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
)

// Me returns the identity this repository writes as.
//
// It is the one name with no command-line equivalent
// (doc/design/cli-convention.md):
// a script that filters "my issues" has to ask who that is,
// and a shell has `git work user`.
func Me(repo *cache.RepoCache) (*cmdjson.Identity, error) {
	i, err := repo.GetUserIdentity()
	if err != nil {
		return nil, err
	}
	out := cmdjson.NewIdentity(i)
	return &out, nil
}
