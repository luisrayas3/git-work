package jiratest

import "time"

// Status categories, as the real site reports them (jira-api-vetting.md R2).
// The spec's examples use "in-flight" and "completed", which no site sends.
const (
	CategoryNew           = "new"
	CategoryIndeterminate = "indeterminate"
	CategoryDone          = "done"
)

// Site is everything a fake Jira holds before the first request:
// projects with their types and workflows, and the site-wide configuration.
type Site struct {
	Title       string
	TimeZone    string // IANA; the "system default user time zone" timestamps render in (C6)
	Projects    []Project
	Users       []User
	Priorities  []Priority
	Resolutions []Resolution
	LinkTypes   []LinkType
	Fields      []CustomField

	// Me is the account the credentials of Credentials() belong to.
	// WebUser is the account the web-UI helpers act as.
	Me      string
	WebUser string
}

// Project is one Jira project. TeamManaged makes it next-gen:
// simplified, with project-scoped issue types.
type Project struct {
	ID          string
	Key         string
	Name        string
	TeamManaged bool
	Lead        string // accountId
	IssueTypes  []IssueType
	Statuses    []Status
	Sprints     []Sprint
}

// IssueType is an issue type with its screens and its workflow.
//
// Fields are the fields associated with the type (its field configuration
// context): what PUT accepts, since PUT no longer checks screens (jira-api.md §5.3).
// CreateScreen is the subset createmeta lists and POST /issue accepts;
// nil means all of Fields. summary, issuetype and project are always there.
type IssueType struct {
	ID             string
	Name           string
	Description    string
	HierarchyLevel int // -1 sub-task, 0 base, 1 epic (jira-api.md §8.4)
	Fields         []string
	CreateScreen   []string
	Required       []string // required on create, beyond summary, issuetype and project
	Workflow       Workflow
}

// Subtask reports whether the type is a sub-task type.
func (t IssueType) Subtask() bool { return t.HierarchyLevel == -1 }

// Workflow is a status graph. Transitions with no From are global.
type Workflow struct {
	Initial     string // status id
	Transitions []Transition
}

// Transition is one edge of a workflow.
type Transition struct {
	ID     string
	Name   string
	From   []string // status ids; empty makes the transition global
	To     string   // status id
	Screen []TransitionField
	// Resolution, when set, is the resolution id a post function sets.
	// A transition to a status whose category is not done clears the resolution.
	Resolution string
}

// TransitionField is a field on a transition screen.
type TransitionField struct {
	FieldID  string
	Required bool
}

// Status is a workflow status. Category is one of the Category constants;
// a name that differs from its category ("Ready for QA", "Won't Do") is the point.
type Status struct {
	ID          string
	Name        string
	Category    string
	Description string
}

// Sprint is a Jira Software sprint, as the Sprint field shows it.
type Sprint struct {
	ID       int
	Name     string
	State    string // future, active, closed
	BoardID  int
	Goal     string
	Start    time.Time
	End      time.Time
	Complete time.Time
}

// User is a Jira account. APIToken, when set, lets the account authenticate
// with Basic email:token (or Bearer token).
type User struct {
	AccountID    string
	DisplayName  string
	Email        string
	EmailHidden  bool   // emailAddress is omitted, and user search needs the exact address
	AccountType  string // atlassian (default), app, customer
	Inactive     bool
	TimeZone     string // IANA; the zone JQL date literals are read in (C6); "" is the site zone
	APIToken     string
	ProjectAdmin bool // may send notifyUsers=false (C5)
}

// Priority is a site priority.
type Priority struct {
	ID      string
	Name    string
	Color   string
	Default bool
}

// Resolution is a site resolution.
type Resolution struct {
	ID          string
	Name        string
	Description string
}

// LinkType is an issue link type: "source Outward destination",
// "destination Inward source".
type LinkType struct {
	ID      string
	Name    string
	Inward  string
	Outward string
}

// FieldKind is the value shape of a custom field.
type FieldKind int

const (
	KindNumber FieldKind = iota // a JSON number
	KindString                  // a single-line text field, a plain string
	KindText                    // a paragraph field, ADF
	KindDate                    // "YYYY-MM-DD"
	KindOption                  // a single select, {"id"} or {"value"}
	KindSprint                  // gh-sprint: an array of sprints on read, a number on write
	KindRank                    // gh-lexo-rank: read only
)

// CustomField is a custom field. Custom is the schema.custom type key;
// empty picks the usual key for the kind. ProjectID scopes the field to a
// team-managed project, as /field reports it.
type CustomField struct {
	ID        string // customfield_NNNNN
	Name      string
	Kind      FieldKind
	Custom    string
	ProjectID string
	Options   []SelectOption
}

// SelectOption is a value of a select field.
type SelectOption struct {
	ID    string
	Value string
}
