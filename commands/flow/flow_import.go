package flowcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/flow"
)

// The actions an import reports, per flow.
const (
	actionCreate    = "create"
	actionUpdate    = "update"
	actionArchive   = "archive"
	actionUnchanged = "unchanged"
)

// flowChange is what an import does to one flow.
//
// Changes is the attributes it writes, in the shape the entity stores them,
// which is what --dry-run prints and what the write then applies.
type flowChange struct {
	Name    string                  `json:"name"`
	Action  string                  `json:"action"`
	Changes map[string]config.Value `json:"changes"`
}

// flowInput is one parsed input file.
type flowInput struct {
	origin string
	def    *flow.Def
	script string
}

type flowImportOptions struct {
	prune  bool
	dryRun bool
}

func newFlowImportCommand(env *execenv.Env) *cobra.Command {
	options := flowImportOptions{}

	cmd := &cobra.Command{
		Use:   "import FILE|DIR|-...",
		Short: "Import flows from Starlark files",
		Long: `Import flows from files, directories of *.star files, or standard input.

A file is one flow: exactly one top-level def, whose name is the flow's name,
whose docstring is the description and whose parameters are its arguments. The
file's name and location never matter, so a scratch file anywhere imports the
same as one under the repository.

Import is an upsert keyed on the def's name: a flow that is not there is
created and its id printed, one whose script or description differs is
updated, and one that is unchanged emits nothing. --prune additionally
archives every flow the inputs do not mention, which is the only way an import
removes anything.

Every input is parsed before anything is written, so a file that does not
parse aborts the whole import.`,
		Example: `git work flow import flows/
git work flow import flows/board.star flows/report.star
git work flow export board | git work flow import -`,
		Args:    cobra.MinimumNArgs(1),
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowImport(env, options, args)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.BoolVar(&options.prune, "prune", false,
		"Archive every flow the inputs do not mention")
	flags.BoolVar(&options.dryRun, "dry-run", false,
		"Print what would be done, and write nothing")

	return cmd
}

func runFlowImport(env *execenv.Env, opts flowImportOptions, args []string) error {
	warnDuplicates(env)

	inputs, err := readInputs(env, args)
	if err != nil {
		return err
	}

	changes, err := planImport(env, inputs, opts.prune)
	if err != nil {
		return err
	}

	if opts.dryRun {
		return env.Out.PrintJSON(changes)
	}

	return applyImport(env, changes)
}

// readInputs reads and parses every input, and writes nothing.
//
// A half-applied import is tolerable — every entity is valid on its own (E9) —
// but a half-applied import of a file with a typo in it is not,
// so parsing is separated from writing and comes first.
func readInputs(env *execenv.Env, args []string) ([]flowInput, error) {
	var inputs []flowInput

	for _, arg := range args {
		sources, err := readSources(env, arg)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, sources...)
	}

	byName := make(map[string]string, len(inputs))
	for i := range inputs {
		def, err := flow.Parse(inputs[i].script)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inputs[i].origin, err)
		}
		if origin, ok := byName[def.Name]; ok {
			return nil, fmt.Errorf("%s: flow %s is already defined by %s",
				inputs[i].origin, def.Name, origin)
		}
		byName[def.Name] = inputs[i].origin
		inputs[i].def = def
	}

	return inputs, nil
}

// readSources turns one argument into the files it names:
// standard input, one file, or every *.star of a directory, not recursively.
func readSources(env *execenv.Env, arg string) ([]flowInput, error) {
	if arg == "-" {
		data, err := io.ReadAll(env.In)
		if err != nil {
			return nil, fmt.Errorf("reading the standard input: %w", err)
		}
		return []flowInput{{origin: "standard input", script: string(data)}}, nil
	}

	info, err := os.Stat(arg)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, err
		}
		return []flowInput{{origin: arg, script: string(data)}}, nil
	}

	paths, err := filepath.Glob(filepath.Join(arg, "*.star"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	inputs := make([]flowInput, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, flowInput{origin: path, script: string(data)})
	}
	return inputs, nil
}

// planImport computes what the import would do, per flow, by name.
func planImport(env *execenv.Env, inputs []flowInput, prune bool) ([]flowChange, error) {
	current := make(map[string]*cache.ConfigExcerpt)
	for _, name := range env.Backend.Flows().Keys(config.ShapeFlow) {
		excerpt, err := env.Backend.Flows().CurrentExcerpt(config.ShapeFlow, name)
		if err != nil {
			return nil, err
		}
		current[name] = excerpt
	}

	var changes []flowChange
	desired := make(map[string]struct{}, len(inputs))

	for _, input := range inputs {
		name := input.def.Name
		desired[name] = struct{}{}

		excerpt, exists := current[name]
		if !exists {
			attributes := map[string]config.Value{
				attrScript: config.StringValue(input.script),
			}
			if input.def.Description != "" {
				attributes[attrDescription] = config.StringValue(input.def.Description)
			}
			changes = append(changes, flowChange{
				Name: name, Action: actionCreate, Changes: attributes,
			})
			continue
		}

		// The comparison is on the decoded value, not on the stored bytes:
		// what matters is whether the flow differs,
		// not how a previous binary encoded the same string.
		set := map[string]config.Value{}
		if script, _ := excerpt.AttributeString(attrScript); script != input.script {
			set[attrScript] = config.StringValue(input.script)
		}
		if description, _ := excerpt.AttributeString(attrDescription); description != input.def.Description {
			set[attrDescription] = config.StringValue(input.def.Description)
		}

		action := actionUpdate
		if len(set) == 0 {
			action = actionUnchanged
		}
		changes = append(changes, flowChange{Name: name, Action: action, Changes: set})
	}

	if prune {
		for name := range current {
			if _, ok := desired[name]; ok {
				continue
			}
			changes = append(changes, flowChange{
				Name:    name,
				Action:  actionArchive,
				Changes: map[string]config.Value{"archived": config.MustValue(true)},
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })

	return changes, nil
}

// applyImport writes the plan, one entity at a time through the cache.
//
// An import that touches five flows is five commits;
// a failure on the third leaves two applied,
// which is the same non-atomicity every multi-entity change has
// and reads correctly at every step (E9).
func applyImport(env *execenv.Env, changes []flowChange) error {
	for _, change := range changes {
		switch change.Action {
		case actionCreate:
			cached, _, err := env.Backend.Flows().New(config.ShapeFlow, change.Name, change.Changes)
			if err != nil {
				return fmt.Errorf("flow %s: %w", change.Name, err)
			}
			env.Out.Println(cached.Id().String())

		case actionUpdate:
			cached, err := current(env, change.Name)
			if err != nil {
				return err
			}
			if err := cached.Update(change.Changes, nil); err != nil {
				return fmt.Errorf("flow %s: %w", change.Name, err)
			}

		case actionArchive:
			cached, err := current(env, change.Name)
			if err != nil {
				return err
			}
			if _, err := cached.SetArchived(true); err != nil {
				return fmt.Errorf("flow %s: %w", change.Name, err)
			}

		case actionUnchanged:
			// nothing to write, and nothing to say about it
		}
	}

	return nil
}
