package schemacmd

import (
	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

type importOptions struct {
	prune  bool
	dryRun bool
}

func newSchemaImportCommand(env *execenv.Env) *cobra.Command {
	options := importOptions{}

	cmd := &cobra.Command{
		Use:   "import FILE|-",
		Short: "Apply a schema document to the store",
		Long: `Read a schema document, YAML or JSON, from a file or standard input, and write
what differs from the store: one commit per entity, nothing at all where the
document and the store already agree.

The whole document is validated before anything is written. An import is an
upsert: a type or a field the document does not mention is left alone, so a
partial file from anywhere can never archive anyone's work. --prune archives
what the document does not mention, which is the only way to remove.

The ids of the entities it creates are printed, one per line, and nothing else.`,
		Example: `git work schema import schema.yaml
git work schema import - --dry-run < schema.yaml`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runSchemaImport(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.prune, "prune", false,
		"Archive the types and fields the document does not mention")
	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print the changes that would be committed, and write nothing")

	return cmd
}

func runSchemaImport(env *execenv.Env, opts importOptions, args []string) error {
	data, err := execenv.ReadFileOrStdin(env, args[0])
	if err != nil {
		return err
	}

	doc, err := schema.ParseDocument(data)
	if err != nil {
		return err
	}

	return importDocument(env, doc, opts)
}

// importDocument is the whole of an import, shared with `schema init`:
// the host validates, reconciles and writes, and this prints what it returned.
//
// The ids created before a failure are printed too, because they are in the
// store whether the rest of the import landed or not.
func importDocument(env *execenv.Env, doc *schema.Document, opts importOptions) error {
	warnDuplicates(env)

	changes, created, err := host.SchemaImport(env.Backend, doc, opts.prune, opts.dryRun)
	if err != nil {
		printIds(env, created)
		return err
	}

	return printImport(env, opts.dryRun, changes, created)
}
