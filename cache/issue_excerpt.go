package cache

import (
	"encoding/gob"
	"strings"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/util/lamport"
)

// Package initialisation used to register the type for (de)serialization
func init() {
	gob.Register(IssueExcerpt{})
}

var _ Excerpt = &IssueExcerpt{}

// IssueExcerpt hold a subset of the issue values to be able to sort and filter issues
// efficiently without having to read and compile each raw issue.
//
// Fields is carried whole: it is what every filter and every view keys on,
// and it is small, because long text lives in comments.
type IssueExcerpt struct {
	id entity.Id

	CreateLamportTime lamport.Time
	EditLamportTime   lamport.Time
	CreateUnixTime    int64
	EditUnixTime      int64

	AuthorId     entity.Id
	Fields       map[string]issue.Value
	LenComments  int
	Actors       []entity.Id
	Participants []entity.Id

	CreateMetadata map[string]string
}

func NewIssueExcerpt(i *IssueCache) *IssueExcerpt {
	snap := i.Snapshot()
	participantsIds := make([]entity.Id, 0, len(snap.Participants))
	for _, participant := range snap.Participants {
		participantsIds = append(participantsIds, participant.Id())
	}

	actorsIds := make([]entity.Id, 0, len(snap.Actors))
	for _, actor := range snap.Actors {
		actorsIds = append(actorsIds, actor.Id())
	}

	fields := make(map[string]issue.Value, len(snap.Fields))
	for k, v := range snap.Fields {
		fields[k] = v
	}

	e := &IssueExcerpt{
		id:                i.Id(),
		CreateLamportTime: i.CreateLamportTime(),
		EditLamportTime:   i.EditLamportTime(),
		CreateUnixTime:    snap.Operations[0].Time().Unix(),
		EditUnixTime:      snap.EditTime().Unix(),
		AuthorId:          snap.Author.Id(),
		Fields:            fields,
		Actors:            actorsIds,
		Participants:      participantsIds,
		LenComments:       len(snap.Comments),
		CreateMetadata:    snap.Operations[0].AllMetadata(),
	}

	return e
}

func (e *IssueExcerpt) setId(id entity.Id) {
	e.id = id
}

func (e *IssueExcerpt) Id() entity.Id {
	return e.id
}

func (e *IssueExcerpt) CreateTime() time.Time {
	return time.Unix(e.CreateUnixTime, 0)
}

func (e *IssueExcerpt) EditTime() time.Time {
	return time.Unix(e.EditUnixTime, 0)
}

// Title returns the title field, which every valid issue has.
func (e *IssueExcerpt) Title() string {
	title, _ := issue.String(e.Fields[issue.TitleKey])
	return title
}

// Aliases returns the issue's external ids, by alias name.
//
// They live in the create operation's metadata because an alias is immutable:
// a Jira key names the same issue for as long as both exist,
// and the entity id stays the hash (483dbe2).
func (e *IssueExcerpt) Aliases() map[string]string {
	var aliases map[string]string
	for key, value := range e.CreateMetadata {
		name, ok := strings.CutPrefix(key, AliasMetadataPrefix)
		if !ok {
			continue
		}
		if aliases == nil {
			aliases = make(map[string]string)
		}
		aliases[name] = value
	}
	return aliases
}

// HasAlias reports whether any of the issue's aliases is that external id.
func (e *IssueExcerpt) HasAlias(alias string) bool {
	if alias == "" {
		return false
	}
	for key, value := range e.CreateMetadata {
		if strings.HasPrefix(key, AliasMetadataPrefix) && value == alias {
			return true
		}
	}
	return false
}

// FieldString returns a field decoded as a string, and whether it is one.
func (e *IssueExcerpt) FieldString(key string) (string, bool) {
	v, ok := e.Fields[key]
	if !ok {
		return "", false
	}
	return issue.String(v)
}

/*
 * Sorting
 */

type IssuesById []*IssueExcerpt

func (b IssuesById) Len() int {
	return len(b)
}

func (b IssuesById) Less(i, j int) bool {
	return b[i].id < b[j].id
}

func (b IssuesById) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

type IssuesByCreationTime []*IssueExcerpt

func (b IssuesByCreationTime) Len() int {
	return len(b)
}

func (b IssuesByCreationTime) Less(i, j int) bool {
	if b[i].CreateLamportTime < b[j].CreateLamportTime {
		return true
	}

	if b[i].CreateLamportTime > b[j].CreateLamportTime {
		return false
	}

	// When the logical clocks are identical, that means we had a concurrent
	// edition. In this case we rely on the timestamp. While the timestamp might
	// be incorrect due to a badly set clock, the drift in sorting is bounded
	// by the first sorting using the logical clock. That means that if users
	// synchronize their issues regularly, the timestamp will rarely be used, and
	// should still provide a kinda accurate sorting when needed.
	return b[i].CreateUnixTime < b[j].CreateUnixTime
}

func (b IssuesByCreationTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

type IssuesByEditTime []*IssueExcerpt

func (b IssuesByEditTime) Len() int {
	return len(b)
}

func (b IssuesByEditTime) Less(i, j int) bool {
	if b[i].EditLamportTime < b[j].EditLamportTime {
		return true
	}

	if b[i].EditLamportTime > b[j].EditLamportTime {
		return false
	}

	return b[i].EditUnixTime < b[j].EditUnixTime
}

func (b IssuesByEditTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
