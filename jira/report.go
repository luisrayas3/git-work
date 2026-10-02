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
	ActionPending  = "pending" // nothing moved; what waits is in pending
	ActionLinked   = "linked"
	ActionGone     = "gone"
	ActionSkipped  = "skipped" // refused or waiting, reported: a person may look
	ActionFailed   = "failed"
	// ActionConsolidated is a second local copy of one Jira issue,
	// archived into the copy that reached Jira first (JS27).
	ActionConsolidated = "consolidated"
)

// Line is one JSON line of the report: exactly one of Schema, Issue and
// Summary is set.
type Line struct {
	Schema    []schema.Change            `json:"schema,omitempty"`
	Issue     entity.Id                  `json:"issue,omitempty"`
	Jira      string                     `json:"jira,omitempty"`
	Action    string                     `json:"action,omitempty"`
	Adopted   entity.Id                  `json:"adopted,omitempty"` // the absent entity the property named (JS27)
	Imported  map[string]json.RawMessage `json:"imported,omitempty"`
	Exported  map[string]json.RawMessage `json:"exported,omitempty"`
	Comments  *CommentCounts             `json:"comments,omitempty"`
	Conflicts []Conflict                 `json:"conflicts,omitempty"`
	Pending   []Skip                     `json:"pending,omitempty"`
	OffSchema []Skip                     `json:"off_schema,omitempty"` // written; the schema's policy would refuse it
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
	Imported     int       `json:"imported"`
	Created      int       `json:"created"`
	Updated      int       `json:"updated"`
	Linked       int       `json:"linked"`
	Gone         int       `json:"gone"`
	Adopted      int       `json:"adopted"`      // imports past the --adopt bound (JS27)
	Consolidated int       `json:"consolidated"` // second copies archived (JS27)
	Orphans      int       `json:"orphans"`      // hits still skipped for an absent entity (JS27)
	Conflicts    int       `json:"conflicts"`
	Pending      int       `json:"pending"`
	OffSchema    int       `json:"off_schema"` // keys written that the schema's policy would refuse
	Failed       int       `json:"failed"`
	Skipped      int       `json:"skipped"`   // reported lines of action skipped
	Unchanged    int       `json:"unchanged"` // candidates with nothing to do, not reported
	Cursor       time.Time `json:"cursor,omitzero"`
}

// count adds one issue's line to the summary.
func (s *Summary) count(l Line) {
	switch l.Action {
	case ActionImported:
		s.Imported++
		if l.Adopted != "" {
			s.Adopted++
		}
	case ActionConsolidated:
		s.Consolidated++
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
	s.OffSchema += len(l.OffSchema)
}

// moved reports whether the line changed anything, on either side.
func (l *Line) moved() bool {
	return len(l.Imported)+len(l.Exported)+len(l.Conflicts) > 0 || !l.Comments.empty()
}
