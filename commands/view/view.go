// Package viewcmd is the `git work view` command tree.
//
// A view is a module, not a surface (doc/design/cli-convention.md):
// `git work view board '{"columns":"status"}' < items.json`
// and `view.board(items, columns="status")` are the same call
// into package `view`, and both return a spec.
// A renderer consumes specs, so the terminal (`84dfbde`) and the browser
// (`8b06191`) never disagree about which views exist.
//
// Until a renderer lands, a spec is printed, and --gui says so.
package viewcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/view"
)

// ErrNoRenderer is what --gui hits until a renderer exists.
var ErrNoRenderer = errors.New("no renderer yet (84dfbde, 8b06191)")

type viewOptions struct {
	gui bool
}

func NewViewCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "Build a render spec from issues",
		Long: `Build a render spec: a view kind, the field bindings it was given, and the
issues it was handed.

The items are a JSON array on standard input, the same JSON ` + "`git work issue`" + `
prints, so a view is the second half of a pipe:

  git work issue 'map(select(.fields.status != "done"))' | git work view board '{"columns":"status"}'

A renderer consumes the spec. Until one exists, the spec is printed.`,
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
		Short: "Build a " + kind + " spec",
		Long:  kindLong(kind),
		Args:  cobra.MaximumNArgs(1),
		// No repository is loaded: a view is a pure function over the JSON it
		// is handed, which is what lets the same call serve a pipe and a flow.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(env, options, kind, args)
		},
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.gui, "gui", false, "Render the spec in the browser")

	return cmd
}

// kindLong documents a kind from the binding table, so that the help and the
// validation can never drift apart.
func kindLong(kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Build a %s spec from the issues on standard input.\n\n", kind)
	b.WriteString("KWARGS is a JSON object binding this view's slots to field keys:\n")
	for _, binding := range view.Kinds[kind] {
		required := "optional"
		if binding.Required {
			required = "required"
		}
		fmt.Fprintf(&b, "  %-12s %-8s %s\n", binding.Name, required, binding.Doc)
	}
	return b.String()
}

func runView(env *execenv.Env, opts viewOptions, kind string, args []string) error {
	if opts.gui {
		return ErrNoRenderer
	}

	items, err := readItems(env)
	if err != nil {
		return err
	}

	bindings, err := readBindings(args)
	if err != nil {
		return err
	}

	spec, err := view.Build(kind, items, bindings)
	if err != nil {
		return err
	}

	return env.Out.PrintJSON(spec)
}

// readItems reads the issues from standard input.
//
// One object is taken as a list of one, because a jq program that emitted a
// single issue is a pipe someone meant to work.
func readItems(env *execenv.Env) ([]any, error) {
	data, err := io.ReadAll(env.In)
	if err != nil {
		return nil, fmt.Errorf("reading the standard input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("no items on standard input: a view takes the JSON `git work issue` prints")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("the items are JSON: %w", err)
	}

	switch items := value.(type) {
	case []any:
		return items, nil
	case map[string]any:
		return []any{items}, nil
	default:
		return nil, fmt.Errorf("the items are an array of issues, or one issue")
	}
}

// readBindings reads the optional JSON object of bindings.
func readBindings(args []string) (map[string]string, error) {
	if len(args) == 0 {
		return nil, nil
	}

	var bindings map[string]string
	if err := json.Unmarshal([]byte(args[0]), &bindings); err != nil {
		return nil, fmt.Errorf("the bindings are a JSON object of field keys: %w", err)
	}
	return bindings, nil
}
