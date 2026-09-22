package cmdjson

import (
	"encoding/json"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
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
		HumanId:    excerpt.Id().Human(),
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
