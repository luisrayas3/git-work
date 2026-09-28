package jira

import (
	"encoding/json"
	"time"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// Actions of a Line (JS22).
const (
	ActionImported = "imported" // new locally
	ActionCreated  = "created"  // new in Jira
	ActionUpdated  = "updated"
	ActionLinked   = "linked"
	ActionGone     = "gone"
	ActionSkipped  = "skipped" // reported, not synced
	ActionFailed   = "failed"
)

// Line is one JSON line of the report: exactly one of Schema, Issue and
// Summary is set.
type Line struct {
	Schema    []schema.Change            `json:"schema,omitempty"`
	Issue     entity.Id                  `json:"issue,omitempty"`
	Jira      string                     `json:"jira,omitempty"`
	Action    string                     `json:"action,omitempty"`
	Imported  map[string]json.RawMessage `json:"imported,omitempty"`
	Exported  map[string]json.RawMessage `json:"exported,omitempty"`
	Comments  *CommentCounts             `json:"comments,omitempty"`
	Conflicts []Conflict                 `json:"conflicts,omitempty"`
	Pending   []Skip                     `json:"pending,omitempty"`
	Error     string                     `json:"error,omitempty"`
	DryRun    bool                       `json:"dry_run,omitempty"`
	Summary   *Summary                   `json:"summary,omitempty"`
}

// CommentCounts are the comments one issue's sync moved.
type CommentCounts struct {
	Imported   int `json:"imported"`
	Exported   int `json:"exported"`
	Edited     int `json:"edited"`
	Tombstoned int `json:"tombstoned"`
}

func (c *CommentCounts) empty() bool { return c == nil || *c == CommentCounts{} }

// Summary closes the report.
type Summary struct {
	Imported  int       `json:"imported"`
	Created   int       `json:"created"`
	Updated   int       `json:"updated"`
	Linked    int       `json:"linked"`
	Gone      int       `json:"gone"`
	Conflicts int       `json:"conflicts"`
	Pending   int       `json:"pending"`
	Failed    int       `json:"failed"`
	Skipped   int       `json:"skipped"`
	Cursor    time.Time `json:"cursor"`
}

// count adds one issue's line to the summary.
func (s *Summary) count(l Line) {
	switch l.Action {
	case ActionImported:
		s.Imported++
	case ActionCreated:
		s.Created++
	case ActionUpdated:
		s.Updated++
	case ActionLinked:
		s.Linked++
	case ActionGone:
		s.Gone++
	case ActionFailed:
		s.Failed++
	case ActionSkipped:
		s.Skipped++
	}
	s.Conflicts += len(l.Conflicts)
	s.Pending += len(l.Pending)
}

// touched reports whether the line says anything beyond "nothing to do".
func (l *Line) touched() bool {
	return len(l.Imported)+len(l.Exported)+len(l.Conflicts)+len(l.Pending) > 0 || !l.Comments.empty() || l.Error != ""
}
