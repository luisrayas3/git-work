package jiratest

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func str(s string) *string { return &s }

// strOrNil is a changelog value: an empty one is null.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// diff is the changelog entry that turns a into b. The item shapes follow
// jira-api.md §4.1 as vetted: labels as whole space-joined sets, the parent as
// IssueParentAssociation with no fieldId (C2), Sprint ids comma-space
// joined (C8), links as Link items with no fieldId.
func (s *Server) diff(a, b *issueState) []item {
	var items []item
	sys := func(field string, from, fromS, to, toS *string) {
		items = append(items, item{Field: field, FieldType: "jira", FieldID: field,
			From: from, FromString: fromS, To: to, ToString: toS})
	}
	if a.key != b.key {
		items = append(items, item{Field: "Key", FieldType: "jira", FromString: str(a.key), ToString: str(b.key)})
	}
	if a.project != b.project {
		sys("project", str(a.project.def.ID), str(a.project.def.Name), str(b.project.def.ID), str(b.project.def.Name))
	}
	if a.typ.ID != b.typ.ID {
		sys("issuetype", str(a.typ.ID), str(a.typ.Name), str(b.typ.ID), str(b.typ.Name))
	}
	if a.summary != b.summary {
		sys("summary", nil, str(a.summary), nil, str(b.summary))
	}
	if string(a.description) != string(b.description) {
		sys("description", nil, strOrNil(adfText(a.description)), nil, strOrNil(adfText(b.description)))
	}
	if a.priority != b.priority {
		sys("priority", strOrNil(a.priority), strOrNil(s.priorityName(a.priority)),
			strOrNil(b.priority), strOrNil(s.priorityName(b.priority)))
	}
	for _, f := range []struct {
		id   string
		x, y string
	}{{"assignee", a.assignee, b.assignee}, {"reporter", a.reporter, b.reporter}} {
		if f.x != f.y {
			sys(f.id, strOrNil(f.x), strOrNil(s.displayName(f.x)), strOrNil(f.y), strOrNil(s.displayName(f.y)))
		}
	}
	if !slices.Equal(a.labels, b.labels) {
		sys("labels", nil, strOrNil(strings.Join(a.labels, " ")), nil, strOrNil(strings.Join(b.labels, " ")))
	}
	if a.duedate != b.duedate {
		sys("duedate", strOrNil(a.duedate), strOrNil(a.duedate), strOrNil(b.duedate), strOrNil(b.duedate))
	}
	if a.status.ID != b.status.ID {
		sys("status", str(a.status.ID), str(a.status.Name), str(b.status.ID), str(b.status.Name))
	}
	if a.resolution != b.resolution {
		sys("resolution", strOrNil(a.resolution), strOrNil(s.resolutionName(a.resolution)),
			strOrNil(b.resolution), strOrNil(s.resolutionName(b.resolution)))
	}
	if a.parent != b.parent {
		items = append(items, item{Field: "IssueParentAssociation", FieldType: "jira",
			From: strOrNil(idOrEmpty(a.parent)), FromString: strOrNil(s.issueKey(a.parent)),
			To: strOrNil(idOrEmpty(b.parent)), ToString: strOrNil(s.issueKey(b.parent))})
	}
	for _, f := range s.fields {
		x, y := a.custom[f.ID], b.custom[f.ID]
		if fmt.Sprint(x) == fmt.Sprint(y) {
			continue
		}
		it := item{Field: f.Name, FieldType: "custom", FieldID: f.ID}
		switch f.Kind {
		case KindSprint:
			it.From, it.FromString = s.sprintStrings(a.project, x)
			it.To, it.ToString = s.sprintStrings(b.project, y)
		case KindRank:
			it.Field, it.ToString = "Rank", str("Ranked higher")
		case KindOption:
			it.From, it.FromString = strOrNil(valueString(x)), strOrNil(optionValue(f, x))
			it.To, it.ToString = strOrNil(valueString(y)), strOrNil(optionValue(f, y))
		case KindText:
			it.FromString, it.ToString = strOrNil(adfTextAny(x)), strOrNil(adfTextAny(y))
		default:
			it.FromString, it.ToString = strOrNil(valueString(x)), strOrNil(valueString(y))
		}
		// Sprint items are seen with their fieldId; others often are not (C8).
		if f.Kind != KindSprint && s.cfg.omitCustomFieldIDs && s.rng.Intn(2) == 0 {
			it.FieldID = ""
		}
		items = append(items, it)
	}
	for _, lid := range b.links {
		if !slices.Contains(a.links, lid) {
			items = append(items, s.linkItem(lid, b.id, false))
		}
	}
	for _, lid := range a.links {
		if !slices.Contains(b.links, lid) {
			items = append(items, s.linkItem(lid, a.id, true))
		}
	}
	return items
}

func idOrEmpty(id int) string {
	if id == 0 {
		return ""
	}
	return strconv.Itoa(id)
}

// linkItem is a Link item: "This issue blocks PROJ-20" (§3 of api-vetting).
func (s *Server) linkItem(lid, viewer int, removed bool) item {
	l := s.links[lid] // a removed link leaves the map after both commits
	other, text := l.dest, l.typ.Outward
	if viewer == l.dest {
		other, text = l.source, l.typ.Inward
	}
	key := s.issueKey(other)
	it := item{Field: "Link", FieldType: "jira"}
	if removed {
		it.From, it.FromString = str(key), str("This issue "+text+" "+key)
	} else {
		it.To, it.ToString = str(key), str("This issue "+text+" "+key)
	}
	return it
}

func (s *Server) priorityName(id string) string {
	if p := s.priority(id); p != nil && id != "" {
		return p.Name
	}
	return ""
}

func (s *Server) resolutionName(id string) string {
	if r := s.resolution(id); r != nil && id != "" {
		return r.Name
	}
	return ""
}

func (s *Server) displayName(accountID string) string {
	if u := s.user(accountID); u != nil {
		return u.DisplayName
	}
	return ""
}

func (s *Server) sprintStrings(p *project, v any) (*string, *string) {
	ids, _ := v.([]int)
	if len(ids) == 0 {
		return nil, nil
	}
	var is, ns []string
	for _, id := range ids {
		is = append(is, strconv.Itoa(id))
		if sp := p.sprint(id); sp != nil {
			ns = append(ns, sp.Name)
		}
	}
	return str(strings.Join(is, ", ")), str(strings.Join(ns, ", "))
}

func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func optionValue(f *CustomField, v any) string {
	for _, o := range f.Options {
		if o.ID == v {
			return o.Value
		}
	}
	return ""
}

func adfTextAny(v any) string {
	b, _ := v.(json.RawMessage)
	return adfText(b)
}
