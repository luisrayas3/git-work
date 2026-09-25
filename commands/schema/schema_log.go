package schemacmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/util/colors"
)

type logOptions struct {
	format string
}

func newSchemaLogCommand(env *execenv.Env) *cobra.Command {
	options := logOptions{}

	cmd := &cobra.Command{
		Use:   "log [KEY]",
		Short: "Print the schema's history",
		Long: `Print the schema's history: every operation of every type and field entity,
one JSON object per line, oldest first within each entity. With a KEY, only
that entity's.

This is what says who added a status and when. KEY is a type key or a field
key, <type>/<field>.`,
		Example: `git work schema log
git work schema log task/status`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaLog(env, options, args)
		}),
		ValidArgsFunction: KeyCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.format, "format", "f", "json",
		"Select the output formatting style. Valid values are [json,text]")

	return cmd
}

func runSchemaLog(env *execenv.Env, opts logOptions, args []string) error {
	warnDuplicates(env)

	key := ""
	if len(args) == 1 {
		key = args[0]
	}

	entries, err := host.SchemaLog(env.Backend, key)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := printOperation(env, opts.format, entry); err != nil {
			return err
		}
	}

	return nil
}

// printOperation prints one entry: a compact JSON object per line, because a
// log is a stream, or one line a human reads.
func printOperation(env *execenv.Env, format string, entry cmdjson.ConfigOperation) error {
	switch format {
	case "json":
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		env.Out.Println(string(raw))
		return nil
	case "text":
		env.Out.Printf("%s\t%s %s\t%s\t%s\t%s\n",
			colors.Cyan(entry.HumanId),
			entry.Shape,
			colors.Green(entry.Key),
			colors.Yellow(entry.Type),
			time.Unix(entry.UnixTime, 0).Format(time.RFC3339),
			colors.Magenta(entry.Author.Name),
		)
		return nil
	default:
		return fmt.Errorf("unknown format %s", format)
	}
}
