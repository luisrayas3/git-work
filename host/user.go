package host

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
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
