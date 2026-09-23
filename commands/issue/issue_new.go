package issuecmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/util/text"
)

// issueDocument is what `new` takes: the issue as a document.
//
// Fields carries the whole of what the issue is, title included;
// body is the first comment, the one an issue always has;
// aliases are external ids, stored as create-op metadata (483dbe2).
type issueDocument struct {
	Fields  map[string]issue.Value `json:"fields"`
	Body    string                 `json:"body"`
	Aliases map[string]string      `json:"aliases"`
}

func newIssueNewCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new DOC|-",
		Short: "Create a new issue from a JSON document",
		Long: `Create an issue from a JSON document, given as the argument or on standard input.

  {"fields": {"title": "…", "type": "task", "status": "open"},
   "body": "the first comment",
   "aliases": {"jira": "PROJ-12"}}

A title is required and lives in fields, like every other property of an issue.
An alias is an external id, immutable, accepted wherever an id is.
The new issue's id is printed, and nothing else.`,
		Example: `git work issue new '{"fields":{"title":"Task: rework the CLI","type":"task"}}'
echo "$doc" | git work issue new -`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runIssueNew(env, args)
		}),
	}

	return cmd
}

func runIssueNew(env *execenv.Env, args []string) error {
	data, err := readArg(env, args[0])
	if err != nil {
		return err
	}

	var doc issueDocument
	if err := decodeJSON(data, &doc); err != nil {
		return err
	}

	title, fields, err := splitTitle(doc.Fields)
	if err != nil {
		return err
	}

	metadata, err := aliasMetadata(doc.Aliases)
	if err != nil {
		return err
	}

	i, _, err := env.Backend.Issues().NewWithMetadata(
		text.CleanupOneLine(title),
		text.Cleanup(doc.Body),
		fields,
		metadata,
	)
	if err != nil {
		return err
	}

	env.Out.Println(i.Id().String())

	return nil
}

// splitTitle takes the title out of the fields map,
// because the create operation carries it in its own member:
// that is what keeps an issue's id equal to the bug's it was migrated from (bf6f392).
func splitTitle(all map[string]issue.Value) (string, map[string]issue.Value, error) {
	raw, ok := all[issue.TitleKey]
	if !ok {
		return "", nil, fmt.Errorf("a title is required, in fields")
	}
	if err := issue.ValidateValue(issue.TitleKey, raw); err != nil {
		return "", nil, err
	}
	title, _ := issue.String(raw)

	fields := make(map[string]issue.Value, len(all))
	for key, value := range all {
		if key == issue.TitleKey {
			continue
		}
		if err := issue.ValidateKey(key); err != nil {
			return "", nil, err
		}
		if err := issue.ValidateValue(key, value); err != nil {
			return "", nil, fmt.Errorf("field %s: %w", key, err)
		}
		fields[key] = value
	}

	return title, fields, nil
}

// aliasMetadata turns the document's aliases into create-op metadata,
// one `alias:<name>` key each.
func aliasMetadata(aliases map[string]string) (map[string]string, error) {
	if len(aliases) == 0 {
		return nil, nil
	}
	metadata := make(map[string]string, len(aliases))
	for name, value := range aliases {
		if err := issue.ValidateKey(name); err != nil {
			return nil, fmt.Errorf("alias: %w", err)
		}
		if value == "" {
			return nil, fmt.Errorf("alias %s is empty", name)
		}
		metadata[cache.AliasMetadataPrefix+name] = value
	}
	return metadata, nil
}
