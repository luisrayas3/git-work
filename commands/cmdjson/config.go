package cmdjson

import (
	"encoding/json"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity/dag"
)

// ConfigOperation is one entry of `git work schema log`:
// what the operation is, who wrote it and when,
// plus the operation itself in the shape the store holds it.
//
// It is IssueOperation with the entity named,
// because a schema log crosses entities where an issue log does not:
// "Luis added value qa to field task/status" is what a reader needs (E2).
type ConfigOperation struct {
	Id       string          `json:"id"`
	HumanId  string          `json:"human_id"`
	Entity   string          `json:"entity"`
	Shape    config.Shape    `json:"shape"`
	Key      string          `json:"key"`
	Type     string          `json:"type"`
	Author   Identity        `json:"author"`
	UnixTime int64           `json:"unix_time"`
	Op       json.RawMessage `json:"op"`
}

func NewConfigOperation(snap *config.Snapshot, op dag.Operation) (ConfigOperation, error) {
	raw, err := json.Marshal(op)
	if err != nil {
		return ConfigOperation{}, err
	}

	return ConfigOperation{
		Id:       op.Id().String(),
		HumanId:  op.Id().Human(),
		Entity:   snap.Id().String(),
		Shape:    snap.Shape,
		Key:      snap.Key,
		Type:     config.OperationTypeName(op.Type()),
		Author:   NewIdentity(op.Author()),
		UnixTime: op.Time().Unix(),
		Op:       raw,
	}, nil
}
