// Package schemacmd is the `git work schema` command tree, over the type and
// field config entities of refs/work-schema.
//
// It follows the rules of doc/design/cli-convention.md:
// a reader prints the schema as YAML or JSON, a writer prints the ids it
// created and nothing else, `--dry-run` prints what would be committed, and
// `rm` is local where `archive` replicates.
//
// The refs are the runtime source of truth; schema.yaml in the tree is an
// authoring file that reaches them only through `import` (E1, 0740bf3).
package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
)

func NewSchemaCommand(env *execenv.Env) *cobra.Command {
	options := formatOptions{}

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Show the schema",
		Long: `Print the live schema: every type and field entity, compiled, in the document
a human edits. This is an alias of export.

The three built-in fields — title, type and archived — exist in code on every
type and are not printed unless an entity overrides one of them.`,
		Example: `git work schema
git work schema --format json | jq '.types.task.fields | keys'`,
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaExport(env, options)
		}),
	}

	addFormatFlag(cmd, &options.format)

	cmd.AddCommand(newSchemaArchiveCommand(env))
	cmd.AddCommand(newSchemaExportCommand(env))
	cmd.AddCommand(newSchemaImportCommand(env))
	cmd.AddCommand(newSchemaInitCommand(env))
	cmd.AddCommand(newSchemaLogCommand(env))
	cmd.AddCommand(newSchemaRmCommand(env))

	return cmd
}

// formatOptions is the one output flag the readers share.
type formatOptions struct {
	format string
}

func addFormatFlag(cmd *cobra.Command, format *string) {
	cmd.Flags().StringVarP(format, "format", "f", "yaml",
		"Select the output formatting style. Valid values are [yaml,json]")
	cmd.RegisterFlagCompletionFunc("format", completion.From([]string{"yaml", "json"}))
}
