package flowcmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity/dag"
)

// flowOperation is one entry of `git work flow log`:
// what the operation is, who wrote it and when,
// plus the operation itself in the shape the store holds it.
type flowOperation struct {
	Id       string           `json:"id"`
	Type     string           `json:"type"`
	Author   cmdjson.Identity `json:"author"`
	UnixTime int64            `json:"unix_time"`
	Op       json.RawMessage  `json:"op"`
}

func newFlowLogCommand(env *execenv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log [NAME]",
		Short: "Print the operations flows are made of",
		Long: `Print the operations of one flow, or of every flow, oldest first and one JSON
object per line: who changed a flow, when, and to what.`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runFlowLog(env, args)
		}),
		ValidArgsFunction: FlowCompletion(env),
	}

	return cmd
}

func runFlowLog(env *execenv.Env, args []string) error {
	warnDuplicates(env)

	names := args
	if len(names) == 0 {
		names = env.Backend.Flows().Keys(config.ShapeFlow)
	}

	for _, name := range names {
		cached, err := archivedToo(env, name)
		if err != nil {
			return err
		}
		for _, op := range cached.Snapshot().AllOperations() {
			entry, err := newFlowOperation(op)
			if err != nil {
				return err
			}
			// one object per line: a log is a stream, not a document
			raw, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			env.Out.Println(string(raw))
		}
	}

	return nil
}

func newFlowOperation(op dag.Operation) (flowOperation, error) {
	raw, err := json.Marshal(op)
	if err != nil {
		return flowOperation{}, err
	}

	return flowOperation{
		Id:       op.Id().String(),
		Type:     config.OperationTypeName(op.Type()),
		Author:   cmdjson.NewIdentity(op.Author()),
		UnixTime: op.Time().Unix(),
		Op:       raw,
	}, nil
}
