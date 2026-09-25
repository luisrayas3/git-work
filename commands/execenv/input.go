package execenv

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// The command trees share these because the convention is shared:
// one --format flag, one way to say "-", one way to print a warning
// (doc/design/cli-convention.md). A copy per tree is how two trees end up
// spelling the same rule two ways, which is what a user then has to learn.

// AddFormatFlag adds the one output flag every reader has.
//
// The values are this reader's, and the first of them is the default, because
// a reader that offers json and text defaults to json and one that offers
// yaml and json defaults to yaml: the order in the call is the answer.
func AddFormatFlag(cmd *cobra.Command, format *string, values ...string) {
	if len(values) == 0 {
		panic("a format flag with no values")
	}

	cmd.Flags().StringVarP(format, "format", "f", values[0],
		fmt.Sprintf("Select the output formatting style. Valid values are [%s]",
			joinValues(values)))

	// The completion is spelled out rather than taken from `completion`,
	// which imports this package: one flag's list of choices is not worth
	// the cycle.
	_ = cmd.RegisterFlagCompletionFunc("format",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return values, cobra.ShellCompDirectiveNoFileComp
		})
}

func joinValues(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}

// Warn prints, on stderr, what a reader should be told about what it read.
//
// The host returns its warnings rather than printing them, because a script's
// diagnostics and a command's go to different places; this is where a
// command's go.
func (env *Env) Warn(warnings []string) {
	for _, warning := range warnings {
		env.Err.Printf("warning: %s\n", warning)
	}
}

// ReadLiteralOrStdin returns an argument verbatim, or the whole of standard
// input when it is "-".
//
// This is how a document argument is taken: the document itself on the command
// line, so that a shell can paste it, or "-" so that a program can pipe it.
func ReadLiteralOrStdin(env *Env, arg string) ([]byte, error) {
	if arg != "-" {
		return []byte(arg), nil
	}
	return readStdin(env)
}

// ReadFileOrStdin returns the contents of the file an argument names, or the
// whole of standard input when it is "-".
//
// This is the other half of the convention: an argument that names a file
// rather than carrying the document, which is what a schema or a flow is
// authored as.
func ReadFileOrStdin(env *Env, arg string) ([]byte, error) {
	if arg == "-" {
		return readStdin(env)
	}
	return os.ReadFile(arg)
}

// ReadKwargs reads the JSON object of keyword arguments a view or a flow
// takes, from the argument itself or, as "-", from standard input.
func ReadKwargs(env *Env, arg string) (map[string]json.RawMessage, error) {
	data, err := ReadLiteralOrStdin(env, arg)
	if err != nil {
		return nil, err
	}

	var kwargs map[string]json.RawMessage
	if err := json.Unmarshal(data, &kwargs); err != nil {
		return nil, fmt.Errorf("the arguments are a JSON object: %w", err)
	}
	return kwargs, nil
}

func readStdin(env *Env) ([]byte, error) {
	data, err := io.ReadAll(env.In)
	if err != nil {
		return nil, fmt.Errorf("reading the standard input: %w", err)
	}
	return data, nil
}
