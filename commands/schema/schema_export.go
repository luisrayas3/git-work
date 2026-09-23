package schemacmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/schema"
)

func newSchemaExportCommand(env *execenv.Env) *cobra.Command {
	options := formatOptions{}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Print the live schema as a document",
		Long: `Print every type and field entity as the document a human edits, YAML by
default and JSON for an agent.

The output is deterministic — types in order, then their fields, then each
field's values — and carries no ordinals, because list position is the order.
Importing what export printed emits no operation, which is the round trip.`,
		Example: `git work schema export > schema.yaml
$EDITOR schema.yaml
git work schema import schema.yaml`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaExport(env, options)
		}),
	}

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runSchemaExport(env *execenv.Env, opts formatOptions) error {
	s, err := loadSchema(env)
	if err != nil {
		return err
	}

	raw, err := schema.Export(s).Marshal(opts.format)
	if err != nil {
		return err
	}

	env.Out.Println(strings.TrimRight(string(raw), "\n"))

	return nil
}
