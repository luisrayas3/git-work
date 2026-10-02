package commands

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	jiracmd "github.com/git-bug/git-bug/commands/jira"
)

type syncOptions struct {
	jira   bool
	dryRun bool
	format string
}

func newSyncCommand(env *execenv.Env) *cobra.Command {
	options := syncOptions{}

	cmd := &cobra.Command{
		Use:   "sync [REMOTE]",
		Short: "Pull from a git remote, then push back to it",
		Long: `Run git work pull, then git work push, against one remote: the whole store in
and the whole store out, in the one order that works, since pushing over a
tracker that was never pulled is rejected anyway. The remote is the argument,
else the git-work.remote config, else origin.

With --jira, git work jira sync runs between the two, so one run takes Jira's
changes in and publishes the store with them. The Jira step is opt-in: without
the flag nothing Jira-related is read or checked. When it fails the push still
happens — the tracker is published whatever Jira did — and the command exits 1.
A pull that fails stops the command.

The output is the pull's, then the Jira sync's one JSON object per line
(--format text for a human), then the push's. Only the Jira step is
dry-runnable, so --dry-run needs --jira.`,
		Example: `git work sync
git work sync --jira
git work sync upstream`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := options.validate(); err != nil {
				return err
			}
			// The Jira step writes issues as you, so it wants an identity
			// settled first, the way git work jira sync does; a pull and a
			// push need none.
			if options.jira {
				return execenv.LoadBackendEnsureUser(env)(cmd, args)
			}
			return execenv.LoadBackend(env)(cmd, args)
		},
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSync(env, options, args)
		}),
		ValidArgsFunction: completion.GitRemote(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false
	flags.BoolVar(&options.jira, "jira", false, "Sync with the bound Jira project between the pull and the push")
	flags.BoolVar(&options.dryRun, "dry-run", false, "With --jira, read both sides of Jira and write neither; the pull and the push still run")
	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func (opts syncOptions) validate() error {
	if opts.dryRun && !opts.jira {
		return errors.New("--dry-run applies to the jira step only: it needs --jira")
	}
	return nil
}

func runSync(env *execenv.Env, opts syncOptions, args []string) error {
	if err := opts.validate(); err != nil {
		return err
	}

	if err := runPull(env, args); err != nil {
		return err
	}

	// A failed Jira step does not hold the push back: what the pull brought in
	// belongs on the remote either way. The error comes back at the end, so
	// the command still exits 1.
	var jiraErr error
	if opts.jira {
		jiraErr = jiracmd.RunSync(env, opts.dryRun, opts.format)
	}

	if err := runPush(env, args); err != nil {
		return err
	}

	return jiraErr
}
