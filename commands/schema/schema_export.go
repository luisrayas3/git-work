package schemacmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
)

func newSchemaExportCommand(env *execenv.Env) *cobra.Command {
	options := exportOptions{}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Print the live schema as a document",
		Long: `Print every unarchived type and field entity as the document a human edits,
YAML by default and JSON for an agent. --include-archive puts back the archived
types, fields and values, each marked archived: true.

The output is deterministic — types in order, then their fields, then each
field's values — and carries no ordinals, because list position is the order.
Importing what export printed emits no operation, with or without the
archived, which is the round trip: an import leaves what a file omits as it
stands.`,
		Example: `git work schema export > schema.yaml
$EDITOR schema.yaml
git work schema import schema.yaml`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaExport(env, options)
		}),
	}

	addExportFlags(cmd, &options)

	return cmd
}

func runSchemaExport(env *execenv.Env, opts exportOptions) error {
	doc, warnings, err := host.SchemaExport(env.Backend, opts.includeArchive)
	env.Warn(warnings)
	if err != nil {
		return err
	}

	raw, err := doc.Marshal(opts.format)
	if err != nil {
		return err
	}

	env.Out.Println(strings.TrimRight(string(raw), "\n"))

	return nil
}
