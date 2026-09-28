package jiratest

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

func (s *Server) getMyself(c *call) (int, any, error) {
	m := s.userJSON(c.user.AccountID).(map[string]any)
	m["locale"] = "en_US"
	m["groups"] = map[string]any{"size": 1, "items": []any{}}
	m["applicationRoles"] = map[string]any{"size": 1, "items": []any{}}
	return http.StatusOK, m, nil
}

func (s *Server) getServerInfo(c *call) (int, any, error) {
	return http.StatusOK, map[string]any{
		"baseUrl":        s.srv.URL,
		"displayUrl":     s.srv.URL,
		"version":        "1001.0.0-SNAPSHOT",
		"versionNumbers": []int{1001, 0, 0},
		"deploymentType": "Cloud",
		"buildNumber":    100280,
		"buildDate":      s.ts(s.cfg.start.AddDate(0, 0, -3)),
		"serverTime":     s.ts(s.clock()),
		"scmInfo":        "1f51473f5c7b75c1a69a0090f4832cdc5053702a",
		"serverTitle":    s.site.Title,
		"serverTimeZone": s.siteZone.String(),
		"defaultLocale":  map[string]any{"locale": "en_US"},
	}, nil
}

// getFields is /field: system fields, then custom ones; a caller who can
// browse no project sees the system fields only (jira-api.md §8.1).
func (s *Server) getFields(c *call) (int, any, error) {
	out := []map[string]any{}
	for _, f := range systemFields {
		out = append(out, map[string]any{
			"id": f.id, "key": f.id, "name": f.name, "custom": false,
			"orderable": f.orderable, "navigable": f.navigable, "searchable": true,
			"clauseNames": []string{f.id}, "schema": f.schema,
		})
	}
	if c.denied {
		return http.StatusOK, out, nil
	}
	for _, f := range s.fields {
		m := map[string]any{
			"id": f.ID, "key": f.ID, "name": f.Name, "untranslatedName": f.Name, "custom": true,
			"orderable": true, "navigable": true, "searchable": true,
			"clauseNames": []string{fmt.Sprintf("cf[%d]", f.numericID()), f.Name},
			"schema":      f.schema(),
		}
		if f.ProjectID != "" {
			m["scope"] = map[string]any{"type": "PROJECT", "project": map[string]any{"id": f.ProjectID}}
		}
		out = append(out, m)
	}
	return http.StatusOK, out, nil
}

func (s *Server) projectOr404(key string) (*project, error) {
	p := s.projectByKey(key)
	if p == nil {
		return nil, notFound(fmt.Sprintf("No project could be found with key '%s'.", key))
	}
	return p, nil
}

func (s *Server) getProject(c *call) (int, any, error) {
	p, err := s.projectOr404(c.v("projectIdOrKey"))
	if err != nil {
		return 0, nil, err
	}
	types := []map[string]any{}
	for i := range p.def.IssueTypes {
		types = append(types, s.issueTypeJSON(p, &p.def.IssueTypes[i]))
	}
	style := "classic"
	if p.def.TeamManaged {
		style = "next-gen"
	}
	m := s.projectRefJSON(p)
	m["description"] = ""
	m["lead"] = s.userJSON(p.def.Lead)
	m["assigneeType"] = "UNASSIGNED"
	m["style"] = style
	m["isPrivate"] = false
	m["archived"] = false
	m["issueTypes"] = types
	m["versions"] = []any{}
	m["components"] = []any{}
	m["roles"] = map[string]any{}
	if parseExpand(c.q["expand"]...)["projectKeys"] {
		m["projectKeys"] = []string{p.def.Key}
	}
	return http.StatusOK, m, nil
}

// workflowStatuses are the statuses a type's workflow uses, in project order.
func workflowStatuses(p *project, t *IssueType) []*Status {
	used := map[string]bool{t.Workflow.Initial: true}
	for _, tr := range t.Workflow.Transitions {
		used[tr.To] = true
		for _, f := range tr.From {
			used[f] = true
		}
	}
	var out []*Status
	for i := range p.def.Statuses {
		if used[p.def.Statuses[i].ID] {
			out = append(out, &p.def.Statuses[i])
		}
	}
	return out
}

func (s *Server) getProjectStatuses(c *call) (int, any, error) {
	p, err := s.projectOr404(c.v("projectIdOrKey"))
	if err != nil {
		return 0, nil, err
	}
	out := []map[string]any{}
	for i := range p.def.IssueTypes {
		t := &p.def.IssueTypes[i]
		sts := []map[string]any{}
		for _, st := range workflowStatuses(p, t) {
			sts = append(sts, s.statusJSON(p, st))
		}
		out = append(out, map[string]any{
			"self": s.self(v3 + "/issueType/" + t.ID), "id": t.ID, "name": t.Name,
			"subtask": t.Subtask(), "statuses": sts,
		})
	}
	return http.StatusOK, out, nil
}

// createMetaTypes is the paginated createmeta (jira-api.md §8.8); a caller
// without Create issues gets no types.
func (s *Server) createMetaTypes(c *call) (int, any, error) {
	p, err := s.projectOr404(c.v("projectIdOrKey"))
	if err != nil {
		return 0, nil, err
	}
	start, max, err := createMetaPaging(c)
	if err != nil {
		return 0, nil, err
	}
	var all []map[string]any
	if !c.denied {
		for i := range p.def.IssueTypes {
			all = append(all, s.issueTypeJSON(p, &p.def.IssueTypes[i]))
		}
	}
	page := window(all, start, max)
	return http.StatusOK, map[string]any{"issueTypes": page, "startAt": start, "maxResults": max, "total": len(all)}, nil
}

func (s *Server) createMetaFields(c *call) (int, any, error) {
	p, err := s.projectOr404(c.v("projectIdOrKey"))
	if err != nil {
		return 0, nil, err
	}
	t := p.issueType(c.v("issueTypeId"))
	if t == nil || t.ID != c.v("issueTypeId") {
		return 0, nil, notFound("Issue type with id '" + c.v("issueTypeId") + "' does not exist or is not in the project.")
	}
	start, max, err := createMetaPaging(c)
	if err != nil {
		return 0, nil, err
	}
	var all []map[string]any
	if !c.denied {
		for _, id := range s.fieldIDs(t) {
			if id == "resolution" || !onCreateScreen(t, id) {
				continue
			}
			required := id == "summary" || id == "issuetype" || id == "project" ||
				slices.Contains(t.Required, id) || (id == "parent" && t.Subtask())
			m := s.fieldMeta(p, t, id, required)
			m["fieldId"] = id
			all = append(all, m)
		}
	}
	page := window(all, start, max)
	return http.StatusOK, map[string]any{"fields": page, "startAt": start, "maxResults": max, "total": len(all)}, nil
}

func createMetaPaging(c *call) (int, int, error) {
	if v := c.q.Get("maxResults"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 200 {
			return 0, 0, badRequest("Parameter 'maxResults' must not exceed the limit '200'")
		}
	}
	return paging(c, 50, 200)
}

func window[T any](all []T, start, max int) []T {
	out := []T{}
	for i := start; i < len(all) && i < start+max; i++ {
		out = append(out, all[i])
	}
	return out
}

func (s *Server) priorityFull(p Priority) map[string]any {
	m := s.priorityJSON(p.ID).(map[string]any)
	m["statusColor"] = p.Color
	m["description"] = p.Name
	return m
}

// pageBean is the offset page bean with isLast (jira-api.md §0).
func (s *Server) pageBean(path string, all []map[string]any, start, max int) map[string]any {
	page := window(all, start, max)
	m := map[string]any{
		"self": s.self(fmt.Sprintf("%s?startAt=%d&maxResults=%d", path, start, max)), "maxResults": max, "startAt": start,
		"total": len(all), "isLast": start+max >= len(all), "values": page,
	}
	if start+max < len(all) {
		m["nextPage"] = s.self(fmt.Sprintf("%s?startAt=%d&maxResults=%d", path, start+max, max))
	}
	return m
}

func (s *Server) searchPriorities(c *call) (int, any, error) {
	start, max, err := paging(c, 50, 50)
	if err != nil {
		return 0, nil, err
	}
	ids := splitList(c.q["id"])
	var all []map[string]any
	for _, p := range s.priorities {
		if len(ids) > 0 && !slices.Contains(ids, p.ID) {
			continue
		}
		if n := c.q.Get("priorityName"); n != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(n)) {
			continue
		}
		m := s.priorityFull(p)
		m["isDefault"] = p.Default
		all = append(all, m)
	}
	return http.StatusOK, s.pageBean(v3+"/priority/search", all, start, max), nil
}

func (s *Server) getUser(c *call) (int, any, error) {
	id := c.q.Get("accountId")
	if id == "" {
		return 0, nil, badRequest("The accountId query parameter needs to be provided.")
	}
	u := s.userJSON(id)
	if u == nil {
		return 0, nil, notFound("Specified user does not exist or you do not have required permissions")
	}
	return http.StatusOK, u, nil
}
