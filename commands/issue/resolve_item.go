package issuecmd

import (
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
)

// resolveItem turns one item of a list-valued field into what is stored.
//
// Without a schema this layer can not know which fields hold issue ids,
// so a string item that is the unambiguous prefix of exactly one issue
// is taken as that issue's full id; anything else is stored as it came.
// Once the schema names the relation kinds (bb9e89e) the lookup is confined to them.
func resolveItem(env *execenv.Env, key string, item issue.Value) issue.Value {
	prefix, ok := issue.String(item)
	if !ok || len(prefix) < 4 {
		return item
	}
	if target, err := env.Backend.Issues().ResolveExcerptPrefix(prefix); err == nil {
		return issue.StringValue(target.Id().String())
	}
	return item
}
