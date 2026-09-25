package cmdjson

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/util/colors"
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

// WriteConfigOperations prints a config entity's history.
//
// Schema and flows are the same entity in two namespaces, so their logs are
// one shape and one printer: a compact JSON object per line, because a log is
// a stream and not a document, or one line a human reads.
func WriteConfigOperations(w io.Writer, format string, entries []ConfigOperation) error {
	// the format is wrong or right before the first entry, not after it
	if format != "json" && format != "text" {
		return fmt.Errorf("unknown format %s", format)
	}

	for _, entry := range entries {
		switch format {
		case "json":
			raw, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, string(raw)); err != nil {
				return err
			}
		case "text":
			_, err := fmt.Fprintf(w, "%s\t%s %s\t%s\t%s\t%s\n",
				colors.Cyan(entry.HumanId),
				entry.Shape,
				colors.Green(entry.Key),
				colors.Yellow(entry.Type),
				time.Unix(entry.UnixTime, 0).Format(time.RFC3339),
				colors.Magenta(entry.Author.Name),
			)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown format %s", format)
		}
	}

	return nil
}
