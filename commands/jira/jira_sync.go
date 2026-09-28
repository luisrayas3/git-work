package jiracmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/util/sorted"
)

type syncOptions struct {
	dryRun, full, acceptDeletes bool
	format                      string
}

func newJiraSyncCommand(env *execenv.Env) *cobra.Command {
	options := syncOptions{}

	cmd := &cobra.Command{
		Use:   "sync [ID...]",
		Short: "Sync the store with the Jira project, both ways",
		Long: `Converge the store and the bound Jira project, one issue at a time: a field
edited on one side reaches the other; a field edited on both takes Jira's value
and says so in a note on the issue; a new issue on either side appears on the
other. A run interrupted anywhere is finished by the next one.

Output is one JSON object per line: the schema changes derived from Jira, one
line per issue the run touched, left pending, skipped or failed on, then a
summary; an issue with nothing to do is only counted, as unchanged. Notes on
what the mapping leaves out go to stderr in a run that changed the schema;
git work jira schema prints them all. With IDs (id
prefixes or aliases, a Jira key included) only those issues are synced, with no
search. --full searches the whole project and marks issues deleted in Jira or
moved out of it as gone; more than 10 at once are held unless --accept-deletes.

The exit status is 1 when an issue failed, the run stopped, or deletes were held.
sync never pushes: run git work push to publish.`,
		Example: `git work jira sync --dry-run
git work jira sync
git work jira sync PROJ-12
git work jira sync --full`,
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runJiraSync(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false
	flags.BoolVar(&options.dryRun, "dry-run", false, "Read both sides and write neither; print the plan")
	flags.BoolVar(&options.full, "full", false, "Search the whole project, and mark deleted or moved issues gone")
	flags.BoolVar(&options.acceptDeletes, "accept-deletes", false, "With --full, mark gone however many issues are missing")
	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runJiraSync(env *execenv.Env, opts syncOptions, args []string) error {
	o := jira.Options{DryRun: opts.dryRun, Full: opts.full, AcceptDeletes: opts.acceptDeletes}
	for _, arg := range args {
		ic, err := env.Backend.Issues().ResolvePrefixOrAlias(arg)
		if err != nil {
			return err
		}
		o.Ids = append(o.Ids, ic.Id())
	}

	var werr error
	emit := func(l jira.Line) {
		if werr != nil {
			return
		}
		if opts.format == "text" {
			env.Out.Println(textLine(l))
			return
		}
		raw, err := json.Marshal(l)
		if err != nil {
			werr = err
			return
		}
		env.Out.Println(string(raw))
	}

	sum, notes, err := host.JiraSync(env.Ctx, env.Backend, o, emit)
	warnNotes(env, notes)
	if err != nil {
		return err
	}
	if werr != nil {
		return werr
	}
	if sum.Failed > 0 {
		return fmt.Errorf("%d issue(s) failed to sync", sum.Failed)
	}
	return nil
}

// textLine is one line per issue for a human.
func textLine(l jira.Line) string {
	switch {
	case l.Summary != nil:
		s := l.Summary
		cursor := "none"
		if !s.Cursor.IsZero() {
			cursor = s.Cursor.Format("2006-01-02T15:04:05Z07:00")
		}
		return fmt.Sprintf("summary: %d imported, %d created, %d updated, %d linked, %d gone, %d conflicts, %d pending, %d failed, %d skipped, %d unchanged; cursor %s",
			s.Imported, s.Created, s.Updated, s.Linked, s.Gone, s.Conflicts, s.Pending, s.Failed, s.Skipped, s.Unchanged, cursor)
	case l.Schema != nil:
		keys := make([]string, len(l.Schema))
		for i, c := range l.Schema {
			keys[i] = string(c.Action) + " " + c.Key
		}
		return "schema: " + strings.Join(keys, ", ")
	}
	parts := []string{human(l.Issue), orDash(l.Jira), l.Action}
	if l.DryRun {
		parts = append(parts, "(dry run)")
	}
	if len(l.Imported) > 0 {
		parts = append(parts, "imported "+strings.Join(sorted.Keys(l.Imported), ","))
	}
	if len(l.Exported) > 0 {
		parts = append(parts, "exported "+strings.Join(sorted.Keys(l.Exported), ","))
	}
	for _, c := range l.Conflicts {
		parts = append(parts, "conflict "+c.Key)
	}
	for _, p := range l.Pending {
		parts = append(parts, "pending "+p.Key+" ("+p.Reason+")")
	}
	if l.Error != "" {
		parts = append(parts, "error: "+l.Error)
	}
	return strings.Join(parts, "  ")
}

func human(id entity.Id) string {
	if id == "" {
		return "-"
	}
	return id.Human()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
