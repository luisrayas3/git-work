package issuecmd

import (
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
)

// resolveItem turns an argument into an item.
//
// Without a schema this layer can not know which fields hold issue ids,
// so an argument that is the unambiguous prefix of exactly one issue
// is taken as that issue's full id; anything else is parsed like a value.
// Once the schema names the relation kinds (bb9e89e) the lookup is confined to them.
func resolveItem(env *execenv.Env, key string, arg string) issue.Value {
	if len(arg) >= 4 {
		if target, err := env.Backend.Issues().ResolveExcerptPrefix(arg); err == nil {
			return issue.StringValue(target.Id().String())
		}
	}
	return parseValue(arg)
}
