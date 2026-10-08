package cmdjson

import (
	"encoding/json"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/util/lamport"
)

// fieldsJSON passes the stored values through verbatim.
func fieldsJSON(fields map[string]issue.Value) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		out[k] = json.RawMessage(v)
	}
	return out
}

type IssueSnapshot struct {
	Id           string                     `json:"id"`
	HumanId      string                     `json:"human_id"`
	CreateTime   Time                       `json:"create_time"`
	EditTime     Time                       `json:"edit_time"`
	Fields       map[string]json.RawMessage `json:"fields"`
	Author       Identity                   `json:"author"`
	Actors       []Identity                 `json:"actors"`
	Participants []Identity                 `json:"participants"`
	Comments     []IssueComment             `json:"comments"`
}

func NewIssueSnapshot(snap *issue.Snapshot) IssueSnapshot {
	out := IssueSnapshot{
		Id:         snap.Id().String(),
		HumanId:    snap.Id().Human(),
		CreateTime: NewTime(snap.CreateTime, 0),
		EditTime:   NewTime(snap.EditTime(), 0),
		Fields:     fieldsJSON(snap.Fields),
		Author:     NewIdentity(snap.Author),
	}

	out.Actors = make([]Identity, len(snap.Actors))
	for i, element := range snap.Actors {
		out.Actors[i] = NewIdentity(element)
	}

	out.Participants = make([]Identity, len(snap.Participants))
	for i, element := range snap.Participants {
		out.Participants[i] = NewIdentity(element)
	}

	out.Comments = make([]IssueComment, len(snap.Comments))
	for i, comment := range snap.Comments {
		out.Comments[i] = NewIssueComment(comment)
	}

	return out
}

type IssueComment struct {
	Id      string   `json:"id"`
	HumanId string   `json:"human_id"`
	Author  Identity `json:"author"`
	Message string   `json:"message"`
}

func NewIssueComment(comment issue.Comment) IssueComment {
	return IssueComment{
		Id:      comment.CombinedId().String(),
		HumanId: comment.CombinedId().Human(),
		Author:  NewIdentity(comment.Author),
		Message: comment.Message,
	}
}

// IssueOperation is one entry of `git work issue log`:
// which issue it belongs to, what the operation is, who wrote it and when,
// plus the operation itself in the shape the store holds it.
//
// Issue is carried whether the log was asked for one issue or for many
// (doc/design/report.md): a shape that changed with the argument
// would be a shape every caller has to branch on.
// Time is the same moment as UnixTime, written out, so that a reader — a
// report flow among them — can print a date without owning a calendar.
type IssueOperation struct {
	Issue    string          `json:"issue"`
	Id       string          `json:"id"`
	HumanId  string          `json:"human_id"`
	Type     string          `json:"type"`
	Author   Identity        `json:"author"`
	UnixTime int64           `json:"unix_time"`
	Time     time.Time       `json:"time"`
	Op       json.RawMessage `json:"op"`
}

func NewIssueOperation(issueId entity.Id, op dag.Operation) (IssueOperation, error) {
	raw, err := json.Marshal(op)
	if err != nil {
		return IssueOperation{}, err
	}

	return IssueOperation{
		Issue:    issueId.String(),
		Id:       op.Id().String(),
		HumanId:  op.Id().Human(),
		Type:     issue.OperationTypeName(op.Type()),
		Author:   NewIdentity(op.Author()),
		UnixTime: op.Time().Unix(),
		Time:     op.Time(),
		Op:       raw,
	}, nil
}

type IssueExcerpt struct {
	Id         string `json:"id"`
	HumanId    string `json:"human_id"`
	CreateTime Time   `json:"create_time"`
	EditTime   Time   `json:"edit_time"`

	Fields       map[string]json.RawMessage `json:"fields"`
	Actors       []Identity                 `json:"actors"`
	Participants []Identity                 `json:"participants"`
	Author       Identity                   `json:"author"`

	Comments int               `json:"comments"`
	Metadata map[string]string `json:"metadata"`
}

func NewIssueExcerpt(backend *cache.RepoCache, excerpt *cache.IssueExcerpt) (IssueExcerpt, error) {
	out := IssueExcerpt{
		Id:         excerpt.Id().String(),
		HumanId:    backend.IssueHumanId(excerpt.Id()), // alias-ids.md A4
		CreateTime: NewTime(excerpt.CreateTime(), excerpt.CreateLamportTime),
		EditTime:   NewTime(excerpt.EditTime(), excerpt.EditLamportTime),
		Fields:     fieldsJSON(excerpt.Fields),
		Comments:   excerpt.LenComments,
		Metadata:   excerpt.CreateMetadata,
	}

	author, err := backend.Identities().ResolveExcerpt(excerpt.AuthorId)
	if err != nil {
		return IssueExcerpt{}, err
	}
	out.Author = NewIdentityFromExcerpt(author)

	out.Actors = make([]Identity, len(excerpt.Actors))
	for i, element := range excerpt.Actors {
		actor, err := backend.Identities().ResolveExcerpt(element)
		if err != nil {
			return IssueExcerpt{}, err
		}
		out.Actors[i] = NewIdentityFromExcerpt(actor)
	}

	out.Participants = make([]Identity, len(excerpt.Participants))
	for i, element := range excerpt.Participants {
		participant, err := backend.Identities().ResolveExcerpt(element)
		if err != nil {
			return IssueExcerpt{}, err
		}
		out.Participants[i] = NewIdentityFromExcerpt(participant)
	}

	return out, nil
}

// NewIssueExcerptAt builds the same excerpt from a snapshot replayed at a past
// time, so that a jq program written for `git work issue list` reads `--at` unchanged.
//
// It is built here rather than from a cache.IssueExcerpt because the cache
// holds the present only (doc/design/report.md). createLamport is the create
// operation's, which the live excerpt knows and which no replay changes.
//
// edit_time.lamport is left at zero, and so omitted: a lamport time belongs to
// the entity, not to an operation, so there is no honest last-edit lamport for
// a moment in the past. edit_time.timestamp is exact.
func NewIssueExcerptAt(snap *issue.Snapshot, createLamport lamport.Time) IssueExcerpt {
	out := IssueExcerpt{
		Id:         snap.Id().String(),
		HumanId:    snap.Id().Human(),
		CreateTime: NewTime(snap.CreateTime, createLamport),
		EditTime:   NewTime(snap.EditTime(), 0),
		Fields:     fieldsJSON(snap.Fields),
		Author:     NewIdentity(snap.Author),
		Comments:   len(snap.Comments),
		Metadata:   snap.Operations[0].AllMetadata(),
	}

	out.Actors = make([]Identity, len(snap.Actors))
	for i, element := range snap.Actors {
		out.Actors[i] = NewIdentity(element)
	}

	out.Participants = make([]Identity, len(snap.Participants))
	for i, element := range snap.Participants {
		out.Participants[i] = NewIdentity(element)
	}

	return out
}
