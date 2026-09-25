package flowcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/flow"
	"github.com/git-bug/git-bug/flow/run"
	"github.com/git-bug/git-bug/tui"
	"github.com/git-bug/git-bug/view"
)

type flowRunOptions struct {
	gui bool
}

func newFlowRunCommand(env *execenv.Env) *cobra.Command {
	options := flowRunOptions{}

	cmd := &cobra.Command{
		Use:   "run NAME|- [KWARGS|-]",
		Short: "Run a flow",
		Long: `Run a flow and print what it returned.

NAME is a flow in the store, or "-" to run a script from standard input
without importing it: one function, the same shape import takes.

KWARGS is a JSON object of the flow's arguments, given as the argument or on
standard input as "-" (not when the script is). Defaults in the signature fill
what the object omits, an unknown key is an error naming the arguments, and an
argument with no default that nobody named is an error too. ` + "`git work flow`" + `
lists them.

A flow that returns a value prints it as JSON; one that returns nothing prints
nothing.

A flow that calls a view draws it here and blocks until you quit it, so a
saved view is a flow that calls one. That needs a terminal: without one, the
view call is what fails, and a flow that draws nothing runs as it always did.`,
		Example: `git work flow run board
git work flow run board '{"iteration":"2026-Q4-S3"}'
echo '{"iteration":"current"}' | git work flow run board -
git work flow run - '{"status":"done"}' < scratch.star`,
		Args:    cobra.RangeArgs(1, 2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowRun(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.gui, "gui", false,
		"Draw what the flow renders in the browser")

	return cmd
}

func runFlowRun(env *execenv.Env, opts flowRunOptions, args []string) error {
	warnDuplicates(env)

	if opts.gui {
		return view.ErrNoGui
	}

	kwargs, err := readKwargs(env, args)
	if err != nil {
		return err
	}

	// A flow needs no renderer until it calls a view, so a missing terminal
	// is not an error here: it is the absence host.View reports if and when
	// the script asks to draw something.
	var renderer view.Renderer
	if terminal, ok := tui.New(env.Out.Raw()); ok {
		renderer = terminal
	}

	// A flow writes through the cache like any command, as the user that
	// LoadBackendEnsureUser settled, and print() goes to stderr.
	options := run.Options{Stderr: env.Err.Raw(), Renderer: renderer}
	var raw json.RawMessage
	if args[0] == "-" {
		raw, err = runScript(env, options, kwargs)
	} else {
		raw, err = run.Flow(env.Ctx, env.Backend, options, args[0], kwargs)
	}
	if err != nil {
		return err
	}
	if raw == nil {
		// The flow returned None: a command that prints nothing.
		return nil
	}

	value, err := decodeValue(raw)
	if err != nil {
		return err
	}
	return env.Out.PrintJSON(value)
}

// runScript runs the flow on standard input without importing it,
// which is how a flow is tried before it is worth a name in the store.
func runScript(env *execenv.Env, options run.Options, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	src, err := io.ReadAll(env.In)
	if err != nil {
		return nil, fmt.Errorf("reading the standard input: %w", err)
	}

	def, err := flow.Parse(string(src))
	if err != nil {
		return nil, err
	}

	return run.Run(env.Ctx, env.Backend, options, def, string(src), kwargs)
}

// readKwargs reads the optional JSON object of arguments.
//
// The one thing this command has that a view does not is two arguments that
// can each be "-", and standard input is one stream: the guard is here,
// because it is about this command's shape and nothing else.
func readKwargs(env *execenv.Env, args []string) (map[string]json.RawMessage, error) {
	if len(args) < 2 {
		return nil, nil
	}
	if args[1] == "-" && args[0] == "-" {
		return nil, errors.New("the script and the arguments can not both come from standard input")
	}
	return execenv.ReadKwargs(env, args[1])
}

func decodeValue(raw json.RawMessage) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}
