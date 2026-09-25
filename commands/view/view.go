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
		Use:   kind + " [KWARGS]",
		Short: "Draw a " + kind,
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
// The note about undrawn arguments is printed only where there is one, so
// that a kind whose whole table is drawn does not warn about nothing.
func kindLong(kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Draw a %s.\n\n", kind)
	b.WriteString("KWARGS is a JSON object of this view's arguments:\n")
	b.WriteString(view.Help(kind))
	if hasFeatureArg(kind) {
		b.WriteString("\nA `feature` argument is in the table and not drawn yet.\n")
	}
	return b.String()
}

func hasFeatureArg(kind string) bool {
	for _, arg := range view.Kinds[kind] {
		if arg.Tier == view.Feature {
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

	renderer, ok := tui.New(env.Out.Raw())
	if !ok {
		// The call is still parsed, so that a misspelled argument is reported
		// as itself rather than hidden behind the missing terminal.
		if _, err := view.Parse(kind, kwargs); err != nil {
			return err
		}
		return view.ErrNoTerminal
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
