package issue

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/util/text"
	"github.com/git-bug/git-bug/util/timestamp"
)

var _ Operation = &CreateOperation{}
var _ dag.OperationWithFiles = &CreateOperation{}

// CreateOperation define the initial creation of an issue.
//
// Title, Message and Files keep entities/bug's names, order and type code,
// so that an issue created with the same inputs and nonce serializes to the same bytes
// and therefore the same id: that is what lets the migration keep ids (bf6f392).
// Fields is the addition, omitted when empty for the same reason.
type CreateOperation struct {
	dag.OpBase
	Title   string            `json:"title"`
	Message string            `json:"message"`
	Files   []repository.Hash `json:"files"`
	// Fields set at creation, title excluded (it has its own member above).
	Fields map[string]Value `json:"fields,omitempty"`
}

func (op *CreateOperation) Id() entity.Id {
	return dag.IdOperation(op, &op.OpBase)
}

func (op *CreateOperation) Apply(snapshot *Snapshot) {
	// sanity check: will fail when adding a second Create
	if snapshot.id != "" && snapshot.id != entity.UnsetId && snapshot.id != op.Id() {
		return
	}

	// the Id of the Issue/Snapshot is the Id of the first Operation: CreateOperation
	opId := op.Id()
	snapshot.id = opId

	snapshot.addActor(op.Author())
	snapshot.addParticipant(op.Author())

	snapshot.setField(TitleKey, StringValue(op.Title))
	for key, value := range op.Fields {
		if key == TitleKey {
			continue
		}
		snapshot.setField(key, value)
	}

	comment := Comment{
		combinedId: entity.CombineIds(snapshot.id, opId),
		targetId:   opId,
		Message:    op.Message,
		Author:     op.Author(),
		Files:      op.Files,
		unixTime:   timestamp.Timestamp(op.UnixTime),
	}

	snapshot.Comments = []Comment{comment}
	snapshot.Author = op.Author()
	snapshot.CreateTime = op.Time()

	snapshot.Timeline = []TimelineItem{
		&CreateTimelineItem{
			CommentTimelineItem: NewCommentTimelineItem(comment),
		},
	}
}

func (op *CreateOperation) GetFiles() []repository.Hash {
	return op.Files
}

func (op *CreateOperation) Validate() error {
	if err := op.OpBase.Validate(op, CreateOp); err != nil {
		return err
	}

	if text.Empty(op.Title) {
		return fmt.Errorf("title is empty")
	}
	if !text.SafeOneLine(op.Title) {
		return fmt.Errorf("title has unsafe characters")
	}

	if !text.Safe(op.Message) {
		return fmt.Errorf("message is not fully printable")
	}

	for key, value := range op.Fields {
		if key == TitleKey {
			return fmt.Errorf("title is set by its own member, not in fields")
		}
		if err := ValidateKey(key); err != nil {
			return errors.Wrap(err, "field")
		}
		if err := ValidateValue(key, value); err != nil {
			return errors.Wrapf(err, "field %s", key)
		}
	}

	return nil
}

func NewCreateOp(author identity.Interface, unixTime int64, title, message string, files []repository.Hash, fields map[string]Value) *CreateOperation {
	return &CreateOperation{
		OpBase:  dag.NewOpBase(CreateOp, author, unixTime),
		Title:   title,
		Message: message,
		Files:   files,
		Fields:  fields,
	}
}

// CreateTimelineItem replace a Create operation in the Timeline and hold its edition history
type CreateTimelineItem struct {
	CommentTimelineItem
}

// Create is a convenience function to create an issue
func Create(author identity.Interface, unixTime int64, title, message string, files []repository.Hash, fields map[string]Value, metadata map[string]string) (*Issue, *CreateOperation, error) {
	i := NewIssue()
	op := NewCreateOp(author, unixTime, title, message, files, fields)
	for key, val := range metadata {
		op.SetMetadata(key, val)
	}
	if err := op.Validate(); err != nil {
		return nil, op, err
	}
	i.Append(op)
	return i, op, nil
}
