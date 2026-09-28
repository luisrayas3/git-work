package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

func (s *Server) getIssue(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil || c.user == nil {
		return 0, nil, issueNotFound()
	}
	st := s.readable(rec)
	if st == nil {
		return 0, nil, issueNotFound()
	}
	return http.StatusOK, s.issueJSON(rec, st, renderOpts{
		sel:    parseFieldSel(c.q["fields"], "*all"),
		expand: parseExpand(c.q["expand"]...),
		props:  splitList(c.q["properties"]),
	}), nil
}

func splitList(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (s *Server) createIssue(c *call) (int, any, error) {
	var up issueUpdate
	if err := c.decode(&up); err != nil {
		return 0, nil, err
	}
	rec, trErr, err := s.create(&up, c.user.AccountID, nil)
	if err != nil {
		return 0, nil, err
	}
	out := map[string]any{
		"id":   strconv.Itoa(rec.cur.id),
		"key":  rec.cur.key,
		"self": s.self(v3 + "/issue/" + strconv.Itoa(rec.cur.id)),
	}
	if up.Transition != nil {
		nested := map[string]any{"status": http.StatusNoContent,
			"errorCollection": map[string]any{"errorMessages": []string{}, "errors": map[string]string{}}}
		if trErr != nil {
			nested["status"] = trErr.status
			nested["errorCollection"] = map[string]any{"errorMessages": nonNil(trErr.messages), "errors": trErr.errors}
		}
		out["transition"] = nested
	}
	return http.StatusCreated, out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// create is POST /issue for a caller and for the helpers. screen defaults
// to the type's create screen. The transition of the body, if any, runs
// after the create; its failure does not undo it (the nested response).
func (s *Server) create(up *issueUpdate, author string, screen func(*IssueType, string) bool) (*record, *apiError, error) {
	if screen == nil {
		screen = onCreateScreen
	}
	errs := map[string]string{}
	var p *project
	if r, ok := parseRef(up.Fields["project"]); ok {
		if r.Key != "" {
			p = s.projectByKey(r.Key)
		} else if id := r.id(); id != "" {
			p = s.projectByKey(id)
		}
	}
	if p == nil {
		errs["project"] = "Specify a valid project ID or key"
		return nil, nil, fieldErrors(errs)
	}
	var t *IssueType
	// Only {"id"} identifies the type; the name form is not relied on (jira-api-vetting.md §3).
	if r, ok := parseRef(up.Fields["issuetype"]); ok && r.id() != "" {
		for i := range p.def.IssueTypes {
			if p.def.IssueTypes[i].ID == r.id() {
				t = &p.def.IssueTypes[i]
			}
		}
	}
	if t == nil {
		errs["issuetype"] = "Specify an issue type"
		return nil, nil, fieldErrors(errs)
	}
	now := s.clock()
	st := &issueState{
		id: s.next.issue + 1, project: p, typ: t, status: p.status(t.Workflow.Initial),
		reporter: author, creator: author, custom: map[string]any{},
		created: now, updated: now, catChanged: now,
	}
	_, errs = s.apply(st, up, modeCreate, func(id string) bool { return screen(t, id) })
	if st.summary == "" && errs["summary"] == "" {
		errs["summary"] = "You must specify a summary of the issue."
	}
	for _, id := range t.Required {
		if unset(st, id) && errs[id] == "" {
			errs[id] = fmt.Sprintf("%s is required.", s.fieldName(id))
		}
	}
	if t.Subtask() && st.parent == 0 && errs["parent"] == "" {
		errs["parent"] = "A sub-task must have a parent."
	}
	if msg := checkProperties(up); msg != "" {
		errs["properties"] = msg
	}
	if len(errs) > 0 {
		return nil, nil, fieldErrors(errs)
	}
	if st.priority == "" && typeHas(t, "priority") {
		for _, pr := range s.priorities {
			if pr.Default {
				st.priority = pr.ID
			}
		}
	}
	if rank := s.rankField(); rank != "" && typeHas(t, rank) {
		st.custom[rank] = s.newRank()
	}
	s.next.issue++
	st.key = fmt.Sprintf("%s-%d", p.def.Key, p.nextNum)
	p.nextNum++
	rec := &record{props: map[string]json.RawMessage{}}
	s.issues[st.id] = rec
	s.keys[st.key] = st.id
	s.commit(rec, st, author, false)
	for _, pr := range up.Properties {
		rec.props[pr.Key] = pr.Value
	}
	s.indexProps(rec)
	var trErr *apiError
	if up.Transition != nil {
		if err := s.transition(rec, rawID(up.Transition.ID), &issueUpdate{}, author); err != nil {
			trErr = err
		}
	}
	return rec, trErr, nil
}

func rawID(raw json.RawMessage) string {
	return ref{ID: raw}.id()
}

func unset(st *issueState, id string) bool {
	switch id {
	case "summary":
		return st.summary == ""
	case "description":
		return st.description == nil
	case "priority":
		return st.priority == ""
	case "assignee":
		return st.assignee == ""
	case "reporter":
		return st.reporter == ""
	case "labels":
		return len(st.labels) == 0
	case "duedate":
		return st.duedate == ""
	case "parent":
		return st.parent == 0
	case "resolution":
		return st.resolution == ""
	}
	_, ok := st.custom[id]
	return !ok
}

// checkProperties validates inline properties: a key, and a non-empty
// JSON value of at most 32768 characters (jira-api-vetting.md §4.1).
func checkProperties(up *issueUpdate) string {
	for _, p := range up.Properties {
		if p.Key == "" || len(p.Key) > 255 {
			return "The property key must be between 1 and 255 characters."
		}
		if len(p.Value) == 0 || len(p.Value) > 32768 {
			return "The property value must be non-empty JSON of at most 32768 characters."
		}
	}
	return ""
}

func (s *Server) editIssue(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	if c.q.Get("overrideScreenSecurity") == "true" || c.q.Get("overrideEditableFlag") == "true" {
		return 0, nil, &apiError{status: http.StatusForbidden,
			messages: []string{"Only Connect and Forge app users with Administer Jira global permission can override screen security."}}
	}
	// C5: notifyUsers=false fails the whole edit for a non-admin.
	if c.q.Get("notifyUsers") == "false" && !c.user.ProjectAdmin {
		return 0, nil, badRequest(msgNotifyUsers)
	}
	var up issueUpdate
	if err := c.decode(&up); err != nil {
		return 0, nil, err
	}
	if err := s.checkWriteLimit(rec); err != nil {
		return 0, nil, err
	}
	if err := s.edit(rec, &up, c.user.AccountID); err != nil {
		return 0, nil, err
	}
	if c.q.Get("returnIssue") == "true" {
		return http.StatusOK, s.issueJSON(rec, rec.cur, renderOpts{
			sel: parseFieldSel(nil, "*all"), expand: parseExpand(c.q["expand"]...),
		}), nil
	}
	return http.StatusNoContent, nil, nil
}

func (s *Server) edit(rec *record, up *issueUpdate, author string) error {
	next := rec.cur.clone()
	app, errs := s.apply(next, up, modeEdit, nil)
	if msg := checkProperties(up); msg != "" {
		errs["properties"] = msg
	}
	if len(errs) > 0 {
		return fieldErrors(errs)
	}
	for _, body := range app.comments {
		s.newComment(next, body, author, nil)
	}
	s.commit(rec, next, author, len(app.comments) > 0)
	for _, p := range up.Properties {
		rec.props[p.Key] = p.Value
	}
	if len(up.Properties) > 0 {
		s.indexProps(rec)
	}
	return nil
}

func (s *Server) deleteIssue(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	if err := s.delete(rec, c.q.Get("deleteSubtasks") == "true", c.user.AccountID); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, nil
}

// delete drops the issue; search stops returning it once the index
// catches up, and nothing records it (jira-api-vetting.md §4.5).
func (s *Server) delete(rec *record, subtasks bool, author string) error {
	kids := s.children(rec.cur.id)
	var subs []*issueState
	for _, k := range kids {
		if k.typ.Subtask() {
			subs = append(subs, k)
		}
	}
	if len(subs) > 0 && !subtasks {
		return badRequest(fmt.Sprintf("The issue '%s' has subtasks. You must specify the 'deleteSubtasks' parameter to delete this issue with subtasks.", rec.cur.key))
	}
	for _, sub := range subs {
		if err := s.delete(s.issues[sub.id], true, author); err != nil {
			return err
		}
	}
	for _, k := range kids {
		if !k.typ.Subtask() {
			child := s.issues[k.id]
			next := child.cur.clone()
			next.parent = 0
			s.commit(child, next, author, false)
		}
	}
	for _, lid := range rec.cur.links {
		l := s.links[lid]
		other := l.source
		if other == rec.cur.id {
			other = l.dest
		}
		if o := s.issues[other]; o != nil {
			next := o.cur.clone()
			next.links = slices.DeleteFunc(next.links, func(x int) bool { return x == lid })
			s.quietly(o, next)
		}
		delete(s.links, lid)
	}
	next := rec.cur.clone()
	next.deleted = true
	next.links = nil
	s.quietly(rec, next)
	return nil
}

// quietly replaces the version with no changelog and no bump.
func (s *Server) quietly(rec *record, next *issueState) {
	rec.cur = next
	rec.versions = append(rec.versions, version{st: next, at: s.clock(), tick: s.tick})
}

// paging reads startAt and maxResults, capping maxResults at limit.
func paging(c *call, def, limit int) (int, int, error) {
	start, max := 0, def
	if v := c.q.Get("startAt"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, 0, badRequest("The 'startAt' parameter must be a non-negative integer.")
		}
		start = n
	}
	if v := c.q.Get("maxResults"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, 0, badRequest("The 'maxResults' parameter must be a non-negative integer.")
		}
		max = n
	}
	if max > limit {
		max = limit
	}
	return start, max, nil
}

// fieldMeta is a FieldMetadata (editmeta, transition screens) or, with
// fieldId added, a FieldCreateMetadata (jira-api.md §8.8, §8.9).
func (s *Server) fieldMeta(p *project, t *IssueType, id string, required bool) map[string]any {
	ops := []string{"set"}
	if f := sysField(id); f != nil {
		ops = f.operations
	} else if f := s.field(id); f != nil {
		ops = f.operations()
	}
	m := map[string]any{
		"required": required, "schema": s.fieldSchema(id), "name": s.fieldName(id),
		"key": id, "operations": ops, "hasDefaultValue": false,
	}
	switch id {
	case "priority":
		var vals []any
		for _, pr := range s.priorities {
			vals = append(vals, s.priorityJSON(pr.ID))
			if pr.Default {
				m["hasDefaultValue"], m["defaultValue"] = true, s.priorityJSON(pr.ID)
			}
		}
		m["allowedValues"] = vals
	case "resolution":
		var vals []any
		for _, r := range s.resolutions {
			vals = append(vals, s.resolutionJSON(r.ID))
		}
		m["allowedValues"] = vals
	case "issuetype":
		m["allowedValues"] = []any{s.issueTypeJSON(p, t)}
	case "project":
		m["allowedValues"] = []any{s.projectRefJSON(p)}
	case "assignee", "reporter":
		m["autoCompleteUrl"] = s.self(v3 + "/user/search?query=")
		if id == "reporter" {
			m["hasDefaultValue"] = true
		}
	}
	if f := s.field(id); f != nil && f.Kind == KindOption {
		var vals []any
		for _, o := range f.Options {
			vals = append(vals, map[string]any{"self": s.self(v3 + "/customFieldOption/" + o.ID), "id": o.ID, "value": o.Value})
		}
		m["allowedValues"] = vals
	}
	return m
}

func (s *Server) getProperty(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	v, ok := rec.props[c.v("propertyKey")]
	if !ok {
		return 0, nil, notFound(fmt.Sprintf("The property with key '%s' does not exist.", c.v("propertyKey")))
	}
	return http.StatusOK, map[string]any{"key": c.v("propertyKey"), "value": v}, nil
}

// setProperty is the sync marker's write: 201 created, 200 updated, and
// neither updated nor the changelog moves (jira-api-vetting.md §3).
func (s *Server) setProperty(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	var v json.RawMessage
	if err := c.decode(&v); err != nil {
		return 0, nil, err
	}
	if len(c.body) > 32768 {
		return 0, nil, badRequest("The property value exceeds the maximum length of 32768 characters.")
	}
	key := c.v("propertyKey")
	_, existed := rec.props[key]
	rec.props[key] = append(json.RawMessage(nil), v...)
	s.indexProps(rec)
	if existed {
		return http.StatusOK, nil, nil
	}
	return http.StatusCreated, nil, nil
}

func (s *Server) deleteProperty(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	key := c.v("propertyKey")
	if _, ok := rec.props[key]; !ok {
		return 0, nil, notFound(fmt.Sprintf("The property with key '%s' does not exist.", key))
	}
	delete(rec.props, key)
	s.indexProps(rec)
	return http.StatusNoContent, nil, nil
}
