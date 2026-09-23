package flowcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/flow/run"
)

// ErrNoRenderer is what --gui and a spec in a terminal both hit today.
//
// A view builds a spec and a renderer consumes it;
// the terminal renderer is `84dfbde` and the HTTP one `8b06191`,
// and until one of them lands a spec is printed, not drawn.
var ErrNoRenderer = errors.New("no renderer yet (84dfbde, 8b06191)")

type flowRunOptions struct {
	format string
	gui    bool
}

func newFlowRunCommand(env *execenv.Env) *cobra.Command {
	options := flowRunOptions{}

	cmd := &cobra.Command{
		Use:   "run NAME [KWARGS]",
		Short: "Run a flow",
		Long: `Run a flow and print what it returned.

KWARGS is a JSON object of the flow's arguments, given as the argument or on
standard input as "-". Defaults in the signature fill what the object omits,
an unknown key is an error naming the arguments, and an argument with no
default that nobody named is an error too. ` + "`git work flow`" + ` lists them.

A flow that returns a value prints it as JSON; one that returns nothing prints
nothing. A flow that returns a view's spec prints the spec, until a renderer
exists to draw it.`,
		Example: `git work flow run board
git work flow run board '{"iteration":"2026-Q4-S3"}'
echo '{"iteration":"current"}' | git work flow run board -`,
		Args:    cobra.RangeArgs(1, 2),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowRun(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)
	flags.BoolVar(&options.gui, "gui", false,
		"Render what the flow returned in the browser")

	return cmd
}

func runFlowRun(env *execenv.Env, opts flowRunOptions, args []string) error {
	warnDuplicates(env)

	if opts.gui {
		return ErrNoRenderer
	}

	kwargs, err := readKwargs(env, args)
	if err != nil {
		return err
	}

	// A flow writes through the cache like any command, as the user that
	// LoadBackendEnsureUser settled, and print() goes to stderr.
	raw, err := run.Flow(env.Ctx, env.Backend, env.Err.Raw(), args[0], kwargs)
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

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(value)
	case "text":
		return printText(env, value)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

// readKwargs reads the optional JSON object of arguments.
func readKwargs(env *execenv.Env, args []string) (map[string]json.RawMessage, error) {
	if len(args) < 2 {
		return nil, nil
	}

	data := []byte(args[1])
	if args[1] == "-" {
		read, err := io.ReadAll(env.In)
		if err != nil {
			return nil, fmt.Errorf("reading the standard input: %w", err)
		}
		data = read
	}

	var kwargs map[string]json.RawMessage
	if err := json.Unmarshal(data, &kwargs); err != nil {
		return nil, fmt.Errorf("the arguments are a JSON object: %w", err)
	}
	return kwargs, nil
}

func decodeValue(raw json.RawMessage) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}
