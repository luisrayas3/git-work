package jiratest

import (
	"strconv"
	"strings"
)

// systemField is a FieldDetails of a system field (jira-api.md §8.1).
type systemField struct {
	id, name   string
	schema     map[string]any
	navigable  bool
	orderable  bool // settable on a screen
	operations []string
}

func sysSchema(typ string, id string, items ...string) map[string]any {
	m := map[string]any{"type": typ, "system": id}
	if len(items) > 0 {
		m["items"] = items[0]
	}
	return m
}

// systemFields are rendered in this order. comment is the one that is not
// navigable, so *navigable leaves it out and *all brings it (§4.2).
var systemFields = []systemField{
	{id: "summary", name: "Summary", schema: sysSchema("string", "summary"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "description", name: "Description", schema: sysSchema("string", "description"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "issuetype", name: "Issue Type", schema: sysSchema("issuetype", "issuetype"), navigable: true, orderable: true, operations: []string{}},
	{id: "project", name: "Project", schema: sysSchema("project", "project"), navigable: true, orderable: false, operations: []string{"set"}},
	{id: "status", name: "Status", schema: sysSchema("status", "status"), navigable: true},
	{id: "priority", name: "Priority", schema: sysSchema("priority", "priority"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "assignee", name: "Assignee", schema: sysSchema("user", "assignee"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "reporter", name: "Reporter", schema: sysSchema("user", "reporter"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "creator", name: "Creator", schema: sysSchema("user", "creator"), navigable: true},
	{id: "labels", name: "Labels", schema: sysSchema("array", "labels", "string"), navigable: true, orderable: true, operations: []string{"add", "set", "remove"}},
	{id: "duedate", name: "Due date", schema: sysSchema("date", "duedate"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "created", name: "Created", schema: sysSchema("datetime", "created"), navigable: true},
	{id: "updated", name: "Updated", schema: sysSchema("datetime", "updated"), navigable: true},
	{id: "resolution", name: "Resolution", schema: sysSchema("resolution", "resolution"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "resolutiondate", name: "Resolved", schema: sysSchema("datetime", "resolutiondate"), navigable: true},
	{id: "statuscategorychangedate", name: "Status Category Changed", schema: sysSchema("datetime", "statuscategorychangedate"), navigable: true},
	{id: "parent", name: "Parent", schema: sysSchema("issuelink", "parent"), navigable: true, orderable: true, operations: []string{"set"}},
	{id: "subtasks", name: "Sub-tasks", schema: sysSchema("array", "subtasks", "issuelinks"), navigable: true},
	{id: "issuelinks", name: "Linked Issues", schema: sysSchema("array", "issuelinks", "issuelinks"), navigable: true, orderable: true, operations: []string{"add", "copy"}},
	{id: "comment", name: "Comment", schema: sysSchema("comments-page", "comment"), navigable: false, orderable: true, operations: []string{"add", "edit", "remove"}},
}

func sysField(id string) *systemField {
	for i := range systemFields {
		if systemFields[i].id == id {
			return &systemFields[i]
		}
	}
	return nil
}

func isSystemField(id string) bool { return sysField(id) != nil }

// customKey is schema.custom for the field.
func (f *CustomField) customKey() string {
	if f.Custom != "" {
		return f.Custom
	}
	const sys = "com.atlassian.jira.plugin.system.customfieldtypes:"
	switch f.Kind {
	case KindNumber:
		return sys + "float"
	case KindString:
		return sys + "textfield"
	case KindText:
		return sys + "textarea"
	case KindDate:
		return sys + "datepicker"
	case KindOption:
		return sys + "select"
	case KindSprint:
		return "com.pyxis.greenhopper.jira:gh-sprint"
	case KindRank:
		return "com.pyxis.greenhopper.jira:gh-lexo-rank"
	}
	return ""
}

func (f *CustomField) numericID() int {
	n, _ := strconv.Atoi(strings.TrimPrefix(f.ID, "customfield_"))
	return n
}

// schema is the JsonTypeBean (jira-api.md §3.2): Sprint is array of "json" (R6).
func (f *CustomField) schema() map[string]any {
	m := map[string]any{"custom": f.customKey(), "customId": f.numericID()}
	switch f.Kind {
	case KindNumber:
		m["type"] = "number"
	case KindString, KindText:
		m["type"] = "string"
	case KindDate:
		m["type"] = "date"
	case KindOption:
		m["type"] = "option"
	case KindSprint:
		m["type"], m["items"] = "array", "json"
	case KindRank:
		m["type"] = "any"
	}
	return m
}

func (f *CustomField) operations() []string {
	switch f.Kind {
	case KindRank:
		return []string{}
	default:
		return []string{"set"}
	}
}

// fieldSel is a parsed fields parameter: *all, *navigable, ids, and -id
// exclusions, comma-separated or repeated (jira-api.md §2.1, §3.1).
type fieldSel struct {
	all, nav bool
	ids      map[string]bool
	excl     map[string]bool
}

func parseFieldSel(vals []string, def string) fieldSel {
	sel := fieldSel{ids: map[string]bool{}, excl: map[string]bool{}}
	var parts []string
	for _, v := range vals {
		parts = append(parts, strings.Split(v, ",")...)
	}
	named := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
		case p == "*all":
			sel.all, named = true, true
		case p == "*navigable":
			sel.nav, named = true, true
		case strings.HasPrefix(p, "-"):
			sel.excl[p[1:]] = true
		default:
			sel.ids[p], named = true, true
		}
	}
	if !named {
		switch def {
		case "*all":
			sel.all = true
		case "*navigable":
			sel.nav = true
		default:
			sel.ids[def] = true
		}
	}
	return sel
}

func (f fieldSel) wants(id string, navigable bool) bool {
	switch {
	case f.excl[id]:
		return false
	case f.ids[id], f.all:
		return true
	default:
		return f.nav && navigable
	}
}

// idOnly is the default of search: the issue is {"id": …} and nothing else.
func (f fieldSel) idOnly() bool {
	if f.all || f.nav {
		return false
	}
	for id := range f.ids {
		if id != "id" {
			return false
		}
	}
	return true
}

// fieldIDs are the fields an issue of the type has, system then custom.
func (s *Server) fieldIDs(t *IssueType) []string {
	var ids []string
	for _, f := range systemFields {
		ids = append(ids, f.id)
	}
	for _, f := range s.fields {
		if typeHas(t, f.ID) {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

func typeHas(t *IssueType, id string) bool {
	for _, f := range t.Fields {
		if f == id {
			return true
		}
	}
	return false
}

// onCreateScreen is whether POST /issue accepts the field for the type.
func onCreateScreen(t *IssueType, id string) bool {
	switch id {
	case "summary", "issuetype", "project":
		return true
	}
	screen := t.CreateScreen
	if screen == nil {
		screen = t.Fields
	}
	for _, f := range screen {
		if f == id {
			return true
		}
	}
	return false
}

func (s *Server) fieldName(id string) string {
	if f := sysField(id); f != nil {
		return f.name
	}
	if f := s.field(id); f != nil {
		return f.Name
	}
	return id
}

func (s *Server) fieldSchema(id string) map[string]any {
	if f := sysField(id); f != nil {
		return f.schema
	}
	if f := s.field(id); f != nil {
		return f.schema()
	}
	return nil
}
