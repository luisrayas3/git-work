package issuecmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
)

// resolveIssue reads the issue named by the first argument, an id prefix,
// and returns the remaining arguments. Plumbing has no implicit selection.
func resolveIssue(backend *cache.RepoCache, args []string) (*cache.IssueCache, []string, error) {
	if len(args) == 0 {
		return nil, nil, errors.New("an issue id is required")
	}
	i, err := backend.Issues().ResolvePrefix(args[0])
	if err != nil {
		return nil, nil, err
	}
	return i, args[1:], nil
}

// IssueCompletion complete an issue id
func IssueCompletion(env *execenv.Env) completion.ValidArgsFunction {
	return func(cmd *cobra.Command, args []string, toComplete string) (completions []string, directives cobra.ShellCompDirective) {
		if err := execenv.LoadBackend(env)(cmd, args); err != nil {
			return completion.HandleError(err)
		}
		defer func() {
			_ = env.Backend.Close()
		}()

		for _, id := range env.Backend.Issues().AllIds() {
			if strings.Contains(id.String(), strings.TrimSpace(toComplete)) {
				excerpt, err := env.Backend.Issues().ResolveExcerpt(id)
				if err != nil {
					return completion.HandleError(err)
				}
				completions = append(completions, id.Human()+"\t"+excerpt.Title())
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}
