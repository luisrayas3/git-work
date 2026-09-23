// Package host is the one code path behind the command line and a flow's script.
//
// Every `git work <module> <verb>` is a function here,
// taking the repository cache and JSON-shaped values,
// and returning what the command prints.
// `commands/` parses argv and formats the result;
// `flow/run` binds the same functions as Starlark builtins.
// The host API mirrors the command line one to one
// (doc/design/cli-convention.md, `3556569` E9),
// and this package is what makes that true by construction
// rather than by two implementations agreeing.
//
// Nothing here writes an entity ref directly:
// every write goes through `cache/`,
// which holds the write lock and re-reads inside it (AGENTS.md).
//
// The author of a write is not an argument.
// `cache` resolves it once, from the identity `EnsureUserIdentity` set,
// and the planning functions of `cache.IssueCache` take no author,
// so passing one here would be a second source of truth
// that half the writers could not honour.
package host

import (
	"github.com/git-bug/git-bug/cache"
)

// resolveIssue reads the issue named by an id prefix or by an alias.
//
// Plumbing has no implicit selection: every call names its issue,
// and it names it the same way from the shell and from a script.
func resolveIssue(repo *cache.RepoCache, id string) (*cache.IssueCache, error) {
	return repo.Issues().ResolvePrefixOrAlias(id)
}
