// Package viewcmd is the `git work view` command tree.
//
// A view is a module, not a surface (doc/design/cli-convention.md):
// `git work view board '{"columns":"status"}'` and
// `work.view.board(columns="status")` are the same call into `host.View`,
// with the same keywords, the same defaults and the same refusals.
//
// The command is the view (decided 2026-09-24): its whole input is one JSON
// object of keyword arguments, `query` included, so nothing is piped in and
// no intermediate document is printed out. The view reads the store itself,
// draws, and blocks until the user quits.
//
// There is no `tui` command. A terminal is where the terminal renderer draws,
// so `git work view list` in one is the interactive list; outside one it says
// so rather than printing something nobody asked for.
package viewcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/tui"
	"github.com/git-bug/git-bug/view"
)

type viewOptions struct {
	gui bool
}

func NewViewCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "Draw the issues",
		Long: `Draw the issues: a list, a board, a gantt chart, or one issue.

A view takes one JSON object of keyword arguments and nothing else. Every kind
but ` + "`show`" + ` takes a ` + "`query`" + `, the jq program its issues come from, so a kanban
with no flow at all is one command:

  git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'

The view draws in the terminal and blocks until you quit it. Edits made in it
are written through the same path a command writes through.`,
	}

	for _, kind := range view.KindNames() {
		cmd.AddCommand(newViewKindCommand(env, kind))
	}

	return cmd
}

func newViewKindCommand(env *execenv.Env, kind string) *cobra.Command {
	options := viewOptions{}

	cmd := &cobra.Command{
		Use:   kind + " [KWARGS|-]",
		Short: kindShort(kind),
		Long:  kindLong(kind),
		Args:  cobra.MaximumNArgs(1),
		// A view writes: an edit made in it is an operation like any other,
		// so it needs the identity every writer needs.
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runView(env, options, kind, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.gui, "gui", false, "Draw the view in the browser")

	return cmd
}

// kindLong documents a kind from its argument table, so that the help and the
// validation can never drift apart.
//
// The note about the optional tier is printed only where the kind has one.
// It said "in the table and not drawn yet" until 2026-10-02, from before any
// kind was drawn, and went on saying it after they all were, so a reader took
// `group_by` and `expand` for sketches (f9c991e).
// kindShort is the one line a kind gets in the command list: what it draws,
// and for `new`, what it writes, because `new` is the one view that is a
// writer (doc/design/create.md, C4).
func kindShort(kind string) string {
	if kind == view.KindNew {
		return "Create an issue in a form: show's page over a draft, written on Create"
	}
	return "Draw a " + kind
}

func kindLong(kind string) string {
	var b strings.Builder
	if kind == view.KindNew {
		b.WriteString("Create an issue in a form: show's page over an issue that does not exist yet.\n")
		b.WriteString("Nothing is written until Create, which commits the draft as `issue new` would,\n")
		b.WriteString("one operation; the created id is printed, or nothing when the form is left.\n\n")
	} else {
		fmt.Fprintf(&b, "Draw a %s.\n\n", kind)
	}
	b.WriteString("KWARGS is a JSON object of this view's arguments, read from standard\ninput when it is \"-\":\n")
	b.WriteString(view.Help(kind))
	if hasOptionalArg(kind) {
		b.WriteString("\nAn `optional` argument has no default: name it and the view does that\nthing, leave it out and it does not.\n")
	}
	return b.String()
}

func hasOptionalArg(kind string) bool {
	for _, arg := range view.Kinds[kind] {
		if arg.Tier == view.Optional {
			return true
		}
	}
	return false
}

func runView(env *execenv.Env, opts viewOptions, kind string, args []string) error {
	if opts.gui {
		return view.ErrNoGui
	}

	kwargs, err := readKwargs(env, args)
	if err != nil {
		return err
	}

	// With no terminal the call is still checked, the schema's half included,
	// so that a misspelled argument or a bad `show` entry is reported as
	// itself rather than hidden behind the missing terminal: host.View
	// answers ErrNoTerminal only after its checks.
	var renderer view.Renderer
	if r, ok := tui.New(env.Out.Raw()); ok {
		renderer = r
	}

	answer, err := host.View(env.Ctx, env.Backend, renderer, kind, kwargs)
	if err != nil {
		return err
	}
	if answer == nil {
		return nil
	}

	var value any
	if err := json.Unmarshal(answer, &value); err != nil {
		return err
	}
	// `new` answers the id it created, and a writer prints its id as a line,
	// as `issue new` does (doc/design/create.md, C4)
	if id, ok := value.(string); ok {
		env.Out.Println(id)
		return nil
	}
	return env.Out.PrintJSON(value)
}

// readKwargs reads the optional JSON object of arguments, from the argument
// or, as "-", from standard input.
func readKwargs(env *execenv.Env, args []string) (map[string]json.RawMessage, error) {
	if len(args) == 0 {
		return nil, nil
	}
	return execenv.ReadKwargs(env, args[0])
}
