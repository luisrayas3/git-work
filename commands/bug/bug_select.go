package bugcmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	_select "github.com/git-bug/git-bug/commands/select"
	"github.com/git-bug/git-bug/entities/bug"
)

func ResolveSelected(repo *cache.RepoCache, args []string) (*cache.BugCache, []string, error) {
	return _select.Resolve[*cache.BugCache](repo, bug.Typename, bug.Namespace, repo.Bugs(), args)
}

func newBugSelectCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "select ISSUE_ID",
		Short: "Select an issue for implicit use in future commands",
		Example: `git work issue select 2f15
git work issue comment
git work issue status
`,
		Long: `Select an issue for implicit use in future commands.

This command allows you to omit any issue ID argument, for example:
  git work issue show
instead of
  git work issue show 2f153ca

The complementary command is "git work issue deselect" performing the opposite operation.
`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runBugSelect(env, args)
		}),
		ValidArgsFunction: BugCompletion(env),
	}

	return cmd
}

func runBugSelect(env *execenv.Env, args []string) error {
	if len(args) == 0 {
		return errors.New("an issue id must be provided")
	}

	prefix := args[0]

	b, err := env.Backend.Bugs().ResolvePrefix(prefix)
	if err != nil {
		return err
	}

	err = _select.Select(env.Backend, bug.Namespace, b.Id())
	if err != nil {
		return err
	}

	env.Out.Printf("selected issue %s: %s\n", b.Id().Human(), b.Snapshot().Title)

	return nil
}
