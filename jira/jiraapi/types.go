package jiraapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Time is a Jira timestamp. It parses the platform form
// "2015-12-02T07:39:15.000-0800", with or without milliseconds, and the
// RFC 3339 forms ("Z", "+10:00") sprint dates use (§13, api-vetting.md C3,
// C4). The offset of the response is kept: it is the site's zone, not the
// caller's (C6). JSON null and "" decode to the zero Time, which encodes as
// null. A JSON number is epoch seconds, or milliseconds above 1e11, the form
// of the bulk-changelog example (§4.2).
type Time struct{ time.Time }

// TimeLayout is the form Jira emits and the form Time encodes to.
const TimeLayout = "2006-01-02T15:04:05.000-0700"

// ParseTime parses any form Time accepts. Fractional seconds are optional
// in every layout (time.Parse accepts them after the seconds field).
func ParseTime(s string) (time.Time, error) {
	var err error
	for _, l := range []string{"2006-01-02T15:04:05Z0700", time.RFC3339} {
		var t time.Time
		if t, err = time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("jira: unrecognised timestamp %q", s)
}

func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Format(TimeLayout) + `"`), nil
}

func (t *Time) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" || string(b) == `""` {
		t.Time = time.Time{}
		return nil
	}
	if len(b) > 0 && b[0] != '"' {
		n, err := strconv.ParseFloat(string(b), 64)
		if err != nil {
			return fmt.Errorf("jira: unrecognised timestamp %s", b)
		}
		if n > 1e11 {
			t.Time = time.UnixMilli(int64(n))
		} else {
			t.Time = time.Unix(int64(n), 0)
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseTime(s)
	t.Time = v
	return err
}

// Date is a calendar date "YYYY-MM-DD" (duedate, date pickers), held as
// midnight UTC. The zero Date encodes as null.
type Date struct{ time.Time }

const DateLayout = "2006-01-02"

func (d Date) String() string { return d.Format(DateLayout) }

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == nil || *s == "" {
		d.Time = time.Time{}
		return nil
	}
	v, err := time.Parse(DateLayout, *s)
	d.Time = v
	return err
}

// Issue is an IssueBean (§3.1). Fields stays raw: field ids are
// site-specific, and interpreting them is the mapping layer's business.
// System decodes the system fields; Decode any one field.
type Issue struct {
	ID         string                     `json:"id"`
	Key        string                     `json:"key"`
	Self       string                     `json:"self,omitempty"`
	Fields     map[string]json.RawMessage `json:"fields,omitempty"`
	Changelog  *Changelog                 `json:"changelog,omitempty"`
	Names      map[string]string          `json:"names,omitempty"`
	Schema     map[string]FieldSchema     `json:"schema,omitempty"`
	Properties map[string]json.RawMessage `json:"properties,omitempty"`
}

// Decode unmarshals field id into v. It reports false, and leaves v alone,
// when the field is absent or null (parent, for one, comes both ways, R4).
func (i *Issue) Decode(id string, v any) (bool, error) {
	raw, ok := i.Fields[id]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("jira: issue %s field %s: %w", i.Key, id, err)
	}
	return true, nil
}

// SystemFields are the system fields the mapping layer reads, by their
// fixed ids (§3.1). A pointer is nil when the field is absent or null.
type SystemFields struct {
	Summary        string          `json:"summary"`
	Description    json.RawMessage `json:"description"` // ADF, or null
	IssueType      *IssueType      `json:"issuetype"`
	Project        *Project        `json:"project"`
	Status         *Status         `json:"status"`
	Priority       *Priority       `json:"priority"`
	Resolution     *Resolution     `json:"resolution"`
	Assignee       *User           `json:"assignee"`
	Reporter       *User           `json:"reporter"`
	Creator        *User           `json:"creator"`
	Labels         []string        `json:"labels"`
	DueDate        Date            `json:"duedate"`
	Created        Time            `json:"created"`
	Updated        Time            `json:"updated"`
	ResolutionDate Time            `json:"resolutiondate"`
	Parent         *IssueRef       `json:"parent"`
	Subtasks       []IssueRef      `json:"subtasks"`
	IssueLinks     []IssueLink     `json:"issuelinks"`
}

// System decodes the system fields present in Fields.
func (i *Issue) System() (SystemFields, error) {
	var s SystemFields
	b, err := json.Marshal(i.Fields)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		return s, fmt.Errorf("jira: issue %s: %w", i.Key, err)
	}
	return s, nil
}

// IssueRef is another issue as embedded in parent, subtasks and issuelinks,
// and the result of a create.
type IssueRef struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Self   string `json:"self,omitempty"`
	Fields *struct {
		Summary   string     `json:"summary"`
		Status    *Status    `json:"status"`
		Priority  *Priority  `json:"priority"`
		IssueType *IssueType `json:"issuetype"`
	} `json:"fields,omitempty"`
}

// IssueLink carries exactly one of InwardIssue and OutwardIssue inside
// fields.issuelinks, relative to the issue viewed, and both on
// GET /issueLink/{id} (§9.2, §9.3). Viewed from A, OutwardIssue B reads
// "A <Type.Outward> B".
type IssueLink struct {
	ID           string        `json:"id"`
	Self         string        `json:"self,omitempty"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *IssueRef     `json:"inwardIssue,omitempty"`
	OutwardIssue *IssueRef     `json:"outwardIssue,omitempty"`
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
	Self    string `json:"self,omitempty"`
}

type IssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Self           string `json:"self,omitempty"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel int    `json:"hierarchyLevel"` // -1 sub-task, 0 base, 1 epic, ≥2 above (§8.4)
	EntityID       string `json:"entityId,omitempty"`
	Scope          *Scope `json:"scope,omitempty"`
}

// Scope is set on team-managed (project-scoped) types, fields and statuses.
type Scope struct {
	Type    string `json:"type"` // PROJECT or GLOBAL
	Project *struct {
		ID string `json:"id"`
	} `json:"project,omitempty"`
}

type Status struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Self           string         `json:"self,omitempty"`
	StatusCategory StatusCategory `json:"statusCategory"`
}

// StatusCategory keys are new, indeterminate, done and undefined (§3.1, R2);
// map workflow meaning on Key, never on a status name.
type StatusCategory struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	ColorName string `json:"colorName,omitempty"`
	Self      string `json:"self,omitempty"`
}

type Priority struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	StatusColor string `json:"statusColor,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
	Self        string `json:"self,omitempty"`
}

type Resolution struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Self        string `json:"self,omitempty"`
}

// User keys on AccountID. EmailAddress is empty whether Jira omitted it,
// sent null or sent "" (R5).
type User struct {
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType,omitempty"` // atlassian, app, customer, unknown
	EmailAddress string `json:"emailAddress,omitempty"`
	DisplayName  string `json:"displayName"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone,omitempty"`
	Locale       string `json:"locale,omitempty"`
	Self         string `json:"self,omitempty"`
}

// Location is the user's profile zone, the zone JQL date literals are read
// in when the user is the caller (§2.5, C6).
func (u *User) Location() (*time.Location, error) {
	if u.TimeZone == "" {
		return nil, fmt.Errorf("jira: user %s has no time zone", u.AccountID)
	}
	return time.LoadLocation(u.TimeZone)
}

type Project struct {
	ID             string      `json:"id"`
	Key            string      `json:"key"`
	Name           string      `json:"name"`
	Self           string      `json:"self,omitempty"`
	ProjectTypeKey string      `json:"projectTypeKey,omitempty"`
	Simplified     bool        `json:"simplified"`
	Style          string      `json:"style,omitempty"` // classic or next-gen
	Archived       bool        `json:"archived"`
	IsPrivate      bool        `json:"isPrivate"`
	Lead           *User       `json:"lead,omitempty"`
	IssueTypes     []IssueType `json:"issueTypes,omitempty"`
}

// IssueTypeStatuses is one entry of GET /project/{key}/statuses (§8.3).
type IssueTypeStatuses struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Self     string   `json:"self,omitempty"`
	Subtask  bool     `json:"subtask"`
	Statuses []Status `json:"statuses"`
}

// Field is a FieldDetails of GET /field (§8.1). Discover well-known custom
// fields on Schema.Custom, never on Name.
type Field struct {
	ID               string       `json:"id"`
	Key              string       `json:"key"`
	Name             string       `json:"name"`
	UntranslatedName string       `json:"untranslatedName,omitempty"`
	Custom           bool         `json:"custom"`
	Orderable        bool         `json:"orderable"`
	Navigable        bool         `json:"navigable"`
	Searchable       bool         `json:"searchable"`
	ClauseNames      []string     `json:"clauseNames,omitempty"`
	Schema           *FieldSchema `json:"schema,omitempty"`
	Scope            *Scope       `json:"scope,omitempty"`
}

// FieldSchema is a JsonTypeBean (§3.2).
type FieldSchema struct {
	Type     string `json:"type"`
	Items    string `json:"items,omitempty"`
	System   string `json:"system,omitempty"`
	Custom   string `json:"custom,omitempty"`
	CustomID int64  `json:"customId,omitempty"`
}

// FieldMeta is a field as createmeta (FieldCreateMetadata, §8.8), editmeta
// and transition screens (FieldMetadata, §6.1) describe it. FieldID is set
// by createmeta only.
type FieldMeta struct {
	FieldID         string            `json:"fieldId,omitempty"`
	Key             string            `json:"key"`
	Name            string            `json:"name"`
	Required        bool              `json:"required"`
	HasDefaultValue bool              `json:"hasDefaultValue"`
	Operations      []string          `json:"operations,omitempty"`
	Schema          FieldSchema       `json:"schema"`
	AllowedValues   []json.RawMessage `json:"allowedValues,omitempty"`
	DefaultValue    json.RawMessage   `json:"defaultValue,omitempty"`
	AutoCompleteURL string            `json:"autoCompleteUrl,omitempty"`
}

// Transition is an IssueTransition (§6.1). Fields is filled because
// Transitions always expands transitions.fields.
type Transition struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	To            Status               `json:"to"`
	HasScreen     bool                 `json:"hasScreen"`
	IsGlobal      bool                 `json:"isGlobal"`
	IsInitial     bool                 `json:"isInitial"`
	IsAvailable   bool                 `json:"isAvailable"`
	IsConditional bool                 `json:"isConditional"`
	Fields        map[string]FieldMeta `json:"fields,omitempty"`
}

// Comment bodies are ADF (§7.1).
type Comment struct {
	ID           string          `json:"id"`
	Self         string          `json:"self,omitempty"`
	Author       *User           `json:"author,omitempty"`
	UpdateAuthor *User           `json:"updateAuthor,omitempty"`
	Body         json.RawMessage `json:"body"`
	Created      Time            `json:"created"`
	Updated      Time            `json:"updated"`
	Visibility   *Visibility     `json:"visibility,omitempty"`
}

type Visibility struct {
	Type       string `json:"type"` // group or role
	Value      string `json:"value"`
	Identifier string `json:"identifier,omitempty"`
}

// Changelog is the embedded page of expand=changelog (§3.3), newest first,
// truncated: use it as a hint only.
type Changelog struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Histories  []History `json:"histories"`
}

// History comes in no guaranteed order; sort on (Created, numeric ID) (C8).
type History struct {
	ID      string       `json:"id"`
	Author  *User        `json:"author,omitempty"`
	Created Time         `json:"created"`
	Items   []ChangeItem `json:"items"`
}

// ChangeItem values are strings, null decoding to "" (§4.1).
type ChangeItem struct {
	Field      string `json:"field"`
	FieldType  string `json:"fieldtype"`
	FieldID    string `json:"fieldId,omitempty"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

// FieldKey is what to match an item on: FieldID when present, else Field
// lower-cased, since fieldId is often absent and field's case varies (C2, C8).
func (it ChangeItem) FieldKey() string {
	if it.FieldID != "" {
		return it.FieldID
	}
	return strings.ToLower(it.Field)
}

type ServerInfo struct {
	BaseURL        string `json:"baseUrl"`
	Version        string `json:"version"`
	DeploymentType string `json:"deploymentType"` // "Cloud"; anything else is not Cloud
	BuildNumber    int    `json:"buildNumber"`
	ServerTime     Time   `json:"serverTime"`
	ServerTimeZone string `json:"serverTimeZone,omitempty"`
	ServerTitle    string `json:"serverTitle,omitempty"`
}

// Property is an entity property set inline on create (api-vetting.md §4.1).
type Property struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Op is one FieldUpdateOperation of an edit's update map (§5.3).
type Op struct {
	Verb  string // set, add, remove, edit or copy
	Value any
}

func OpSet(v any) Op    { return Op{"set", v} }
func OpAdd(v any) Op    { return Op{"add", v} }
func OpRemove(v any) Op { return Op{"remove", v} }
func OpEdit(v any) Op   { return Op{"edit", v} }

func (o Op) MarshalJSON() ([]byte, error) { return marshal(map[string]any{o.Verb: o.Value}) }
