// Package jira keeps a git-work store and one Jira project converged
// (doc/design/jira-sync.md).
//
// The mapping (discovery, Derive, Compile, Mapping) turns Jira's documents
// into local terms and back; the merge is a pure function over three
// documents per issue; the engine is the only part that does I/O.
// This file holds the types the two halves share: the seam.
package jira

import (
	"encoding/json"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
)

// Every metadata key and property the sync writes (JS8).
const (
	MetaId        = "jira-id"         // create op: the Jira issue id, the link (I3)
	MetaAlias     = "alias:jira"      // create op: the Jira key at link time
	MetaSync      = "jira-sync"       // create op or NoOp: the Base as JSON
	MetaCreate    = "jira-create"     // NoOp before a POST /issue: its time on Jira's clock (JS15)
	MetaCommentId = "jira-comment-id" // add-comment op: the Jira comment id (I3)
	MetaNote      = "jira-note"       // add-comment op of a note: conflict | deleted
	MetaAccountId = "jira-account-id" // identity, immutable: the Jira accountId
	PropertyKey   = "git-work"        // Jira issue and comment property
)

// Level grades a Note.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Note is one thing the mapping has to say about the schema or an issue,
// such as a status Jira has and the schema does not.
type Note struct {
	Level   Level  `json:"level"`
	Key     string `json:"key"` // "task/status:in-review", "task/estimate", ...
	Message string `json:"message"`
}

// Doc is one issue in local terms, from either side (JS1):
// the local snapshot through Mapping.Local, or Jira's through Mapping.fromIssue.
type Doc struct {
	Id, Key  string                 // remote only
	Updated  time.Time              // remote only
	Created  time.Time              // remote only
	Reporter string                 // remote only: an accountId
	Status   string                 // remote only: the Jira status name
	Type     string                 // the local type key
	Fields   map[string]issue.Value // mapped keys, title, type and BodyKey included
	Lossy    bool                   // remote only: the description holds what git-work cannot write back (JS11)
	Comments []docComment           // #1 on
	Skip     []Skip                 // remote only: what could not convert now
}

// docText is a comment's text in the local text model (jiraapi.ADFToText).
// Lossless is false when writing the text back would drop something Jira has;
// such a text is never overwritten (JS11). Local texts are always lossless.
type docText struct {
	Text     string
	Lossless bool
}

// docComment is a comment after #0.
type docComment struct {
	JiraId         string    // local: the jira-comment-id fact; remote: the Jira id
	Op             entity.Id // local: the add-comment op; remote: from the git-work property
	Author, Editor string    // accountIds (remote) or identity ids (local)
	At, Edited     time.Time
	Text           docText
	Note           bool // a jira-note: never exported
}

// change is one key's merged outcome to write on the other side:
// Set for a scalar, Add and Remove for a multi-value.
type change struct {
	Key         string
	Set         issue.Value
	Add, Remove []issue.Value
}

// Skip is a key that could not be converted or written now, and why.
// Retry keeps the issue a candidate on the next run (I1).
type Skip struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	Retry  bool   `json:"retry,omitempty"`
}

// writeKind says how a Write reaches Jira.
type writeKind int

const (
	writeEdit       writeKind = iota // batched into one PUT /issue
	writeTransition                  // POST /transitions
	writeLink                        // POST and DELETE /issueLink
)

// jiraWrite is one local key's change as Jira takes it; success is reported per jiraWrite.
type jiraWrite struct {
	Key    string
	Kind   writeKind
	Field  string          // Edit: the Jira field id
	Set    json.RawMessage // Edit: fields.<Field>; null clears
	Update []jiraapi.Op    // Edit: update.<Field>, when Set cannot say it
	Status string          // Transition: the target status id
	Add    []newLink       // Link
	Remove []string        // Link: issueLink ids
}

// newLink is a link to create: Source <LinkType> Destination, as Jira ids,
// in the orientation of POST /issueLink (jira-api-vetting.md C1).
type newLink struct {
	LinkType            string
	Source, Destination string
}
