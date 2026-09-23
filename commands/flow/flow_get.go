package flowcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/flow"
)

// flowDetail is one flow whole: what a listing gives, plus the script and the id.
type flowDetail struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Params      []paramEntry `json:"params"`
	Script      string       `json:"script"`
	Id          string       `json:"id"`
}

type flowGetOptions struct {
	format string
}

func newFlowGetCommand(env *execenv.Env) *cobra.Command {
	options := flowGetOptions{}

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Print one flow whole",
		Long: `Print a flow: its description, its arguments, its script and its entity id.

--format text prints the script verbatim, which is what export gives.`,
		Args:    cobra.ExactArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowGet(env, options, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	addFormatFlag(cmd, &options.format)

	return cmd
}

func runFlowGet(env *execenv.Env, opts flowGetOptions, args []string) error {
	warnDuplicates(env)

	name := args[0]

	excerpt, err := currentExcerpt(env, name)
	if err != nil {
		return err
	}

	script, err := scriptOf(excerpt)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		description, _ := excerpt.AttributeString(attrDescription)
		detail := flowDetail{
			Name:        name,
			Description: description,
			Params:      []paramEntry{},
			Script:      script,
			Id:          excerpt.Id().String(),
		}
		if def, err := flow.Parse(script); err == nil {
			detail.Params = paramEntries(def)
		} else {
			env.Err.Printf("warning: flow %s does not parse: %v\n", name, err)
		}
		return env.Out.PrintJSON(detail)
	case "text":
		// verbatim: what comes out has to import back unchanged
		env.Out.Print(script)
		return nil
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}
