package jiratest

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Actor makes changes the way a person in Jira's web UI would: through
// the same validation as the API, bumping updated and writing the
// changelog, but past rate limits and Deny, and not counted in Writes.
// Every method fails the test on error.
type Actor struct {
	s  *Server
	id string
}

// As is an actor for the account.
func (s *Server) As(accountID string) *Actor {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.user(accountID) == nil {
		s.t.Fatalf("jiratest: no user %q", accountID)
	}
	return &Actor{s: s, id: accountID}
}

// UI is the actor for Site.WebUser.
func (s *Server) UI() *Actor { return s.As(s.site.WebUser) }

// do runs f under the server lock, fails the test on its error, and moves
// the clock on as a write does.
func (a *Actor) do(f func() error) {
	a.s.t.Helper()
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if err := f(); err != nil {
		a.s.t.Fatalf("jiratest: %v", err)
	}
	a.s.tock()
}

func (a *Actor) mustIssue(key string) (*record, error) {
	rec := a.s.lookup(key)
	if rec == nil {
		return nil, fmt.Errorf("no issue %s", key)
	}
	return rec, nil
}

// IssueSpec is an issue to create. Type and Status are names; Parent is a
// key; Fields holds any other field as its REST JSON value
// (e.g. FieldStoryPoints: 3, FieldSprint: 37).
type IssueSpec struct {
	Project     string
	Type        string
	Summary     string
	Description string // plain text, stored as ADF
	Status      string
	Priority    string
	Assignee    string
	Labels      []string
	DueDate     string
	Parent      string
	Fields      map[string]any
}

// CreateIssue creates an issue and returns its key. A Status other than the
// initial one is reached by setting it, with its changelog entry.
func (a *Actor) CreateIssue(spec IssueSpec) string {
	a.s.t.Helper()
	var key string
	a.do(func() error {
		p := a.s.projectByKey(spec.Project)
		if p == nil {
			return fmt.Errorf("no project %s", spec.Project)
		}
		t := p.issueType(spec.Type)
		if t == nil {
			return fmt.Errorf("no issue type %s in %s", spec.Type, spec.Project)
		}
		fields := map[string]any{
			"project":   map[string]string{"key": p.def.Key},
			"issuetype": map[string]string{"id": t.ID},
			"summary":   spec.Summary,
		}
		if spec.Description != "" {
			fields["description"] = TextADF(spec.Description)
		}
		if spec.Priority != "" {
			fields["priority"] = map[string]string{"name": spec.Priority}
		}
		if spec.Assignee != "" {
			fields["assignee"] = map[string]string{"accountId": spec.Assignee}
		}
		if spec.Labels != nil {
			fields["labels"] = spec.Labels
		}
		if spec.DueDate != "" {
			fields["duedate"] = spec.DueDate
		}
		if spec.Parent != "" {
			fields["parent"] = map[string]string{"key": spec.Parent}
		}
		for k, v := range spec.Fields {
			fields[k] = v
		}
		up, err := toUpdate(fields, nil)
		if err != nil {
			return err
		}
		// The web UI can set any field of the type, not only the create screen's.
		rec, _, err := a.s.create(up, a.id, func(t *IssueType, id string) bool {
			return onCreateScreen(t, id) || typeHas(t, id)
		})
		if err != nil {
			return err
		}
		key = rec.cur.key
		if spec.Status == "" {
			return nil
		}
		var st *Status
		for _, s := range workflowStatuses(p, t) {
			if strings.EqualFold(s.Name, spec.Status) {
				st = s
			}
		}
		if st == nil {
			return fmt.Errorf("no status %q in the %s workflow", spec.Status, t.Name)
		}
		next := rec.cur.clone()
		next.status = st
		if st.Category == CategoryDone {
			next.resolution, next.resolved = a.s.resolutions[0].ID, a.s.clock()
		}
		a.s.commit(rec, next, a.id, false)
		return nil
	})
	return key
}

func toUpdate(fields map[string]any, update map[string]any) (*issueUpdate, error) {
	b, err := json.Marshal(map[string]any{"fields": fields, "update": update})
	if err != nil {
		return nil, err
	}
	var up issueUpdate
	return &up, json.Unmarshal(b, &up)
}

// Edit sets fields, given as in a PUT body's "fields".
func (a *Actor) Edit(key string, fields map[string]any) {
	a.s.t.Helper()
	a.EditUpdate(key, fields, nil)
}

// EditUpdate is Edit with a PUT body's "update" as well,
// e.g. {"labels": [{"add": "x"}]}.
func (a *Actor) EditUpdate(key string, fields, update map[string]any) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		up, err := toUpdate(fields, update)
		if err != nil {
			return err
		}
		return a.s.edit(rec, up, a.id)
	})
}

// Transition moves the issue to the named status through an available
// transition, filling a required screen field with its first allowed value.
func (a *Actor) Transition(key, status string) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		for _, t := range available(rec.cur) {
			to := rec.cur.project.status(t.To)
			if !strings.EqualFold(to.Name, status) {
				continue
			}
			fields := map[string]any{}
			for _, f := range t.Screen {
				if f.Required && f.FieldID == "resolution" {
					fields["resolution"] = map[string]string{"id": a.s.resolutions[0].ID}
				}
			}
			up, err := toUpdate(fields, nil)
			if err != nil {
				return err
			}
			if e := a.s.transition(rec, t.ID, up, a.id); e != nil {
				return e
			}
			return nil
		}
		return fmt.Errorf("%s: no transition from %q to %q", key, rec.cur.status.Name, status)
	})
}

// AddComment adds a plain-text comment and returns its id.
func (a *Actor) AddComment(key, text string) string {
	a.s.t.Helper()
	var id string
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		next := rec.cur.clone()
		body, _ := a.s.adfValue(TextADF(text))
		c := a.s.newComment(next, body, a.id, nil)
		a.s.commit(rec, next, a.id, true)
		id = strconv.Itoa(c.id)
		return nil
	})
	return id
}

// EditComment replaces a comment's text.
func (a *Actor) EditComment(key, id, text string) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		i := findComment(rec.cur, id)
		if i < 0 {
			return fmt.Errorf("%s: no comment %s", key, id)
		}
		body, _ := a.s.adfValue(TextADF(text))
		a.s.editComment(rec, i, body, nil, a.id)
		return nil
	})
}

// DeleteComment deletes a comment.
func (a *Actor) DeleteComment(key, id string) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		i := findComment(rec.cur, id)
		if i < 0 {
			return fmt.Errorf("%s: no comment %s", key, id)
		}
		a.s.removeComment(rec, i, a.id)
		return nil
	})
}

// Delete deletes the issue and its sub-tasks.
func (a *Actor) Delete(key string) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		return a.s.delete(rec, true, a.id)
	})
}

// Move moves the issue to another project, under a type of the same name,
// and returns its new key. The id stays; the old key keeps resolving
// (api-vetting.md §4.7); the changelog gets Key and project items.
func (a *Actor) Move(key, projectKey string) string {
	a.s.t.Helper()
	var newKey string
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		p := a.s.projectByKey(projectKey)
		if p == nil {
			return fmt.Errorf("no project %s", projectKey)
		}
		t := p.issueType(rec.cur.typ.Name)
		if t == nil {
			return fmt.Errorf("project %s has no type %s", projectKey, rec.cur.typ.Name)
		}
		next := rec.cur.clone()
		next.project, next.typ = p, t
		next.key = fmt.Sprintf("%s-%d", p.def.Key, p.nextNum)
		p.nextNum++
		if !slices.ContainsFunc(workflowStatuses(p, t), func(s *Status) bool { return s.ID == next.status.ID }) {
			next.status = p.status(t.Workflow.Initial)
		} else {
			next.status = p.status(next.status.ID)
		}
		if next.parent != 0 && a.s.issues[next.parent].cur.project != p {
			next.parent = 0
		}
		a.s.keys[next.key] = next.id
		a.s.commit(rec, next, a.id, false)
		newKey = next.key
		return nil
	})
	return newKey
}

// Link links source <outward> dest ("source blocks dest") and returns the link id.
func (a *Actor) Link(source, linkType, dest string) string {
	a.s.t.Helper()
	var id int
	a.do(func() error {
		src, err := a.mustIssue(source)
		if err != nil {
			return err
		}
		dst, err := a.mustIssue(dest)
		if err != nil {
			return err
		}
		lt := a.s.linkType("", linkType)
		if lt == nil {
			return fmt.Errorf("no link type %s", linkType)
		}
		id = a.s.link(src, dst, *lt, a.id, nil)
		return nil
	})
	return strconv.Itoa(id)
}

// SetProperty sets an issue property, which bumps nothing.
func (a *Actor) SetProperty(key, prop string, value any) {
	a.s.t.Helper()
	a.do(func() error {
		rec, err := a.mustIssue(key)
		if err != nil {
			return err
		}
		b, err := json.Marshal(value)
		rec.props[prop] = b
		return err
	})
}

// The helpers on Server act as Site.WebUser.

func (s *Server) CreateIssue(spec IssueSpec) string      { s.t.Helper(); return s.UI().CreateIssue(spec) }
func (s *Server) Edit(key string, fields map[string]any) { s.t.Helper(); s.UI().Edit(key, fields) }
func (s *Server) Transition(key, status string)          { s.t.Helper(); s.UI().Transition(key, status) }
func (s *Server) AddComment(key, text string) string {
	s.t.Helper()
	return s.UI().AddComment(key, text)
}
func (s *Server) EditComment(key, id, text string) { s.t.Helper(); s.UI().EditComment(key, id, text) }
func (s *Server) DeleteComment(key, id string)     { s.t.Helper(); s.UI().DeleteComment(key, id) }
func (s *Server) Delete(key string)                { s.t.Helper(); s.UI().Delete(key) }
func (s *Server) Move(key, project string) string  { s.t.Helper(); return s.UI().Move(key, project) }
func (s *Server) Link(src, typ, dst string) string { s.t.Helper(); return s.UI().Link(src, typ, dst) }

// Issue is the stored state of an issue, for assertions.
type Issue struct {
	ID          string
	Key         string
	Project     string // key
	Type        string // name
	Status      string // name
	Category    string // status category key
	Summary     string
	Description json.RawMessage
	Priority    string // name
	Assignee    string // accountId
	Reporter    string
	Creator     string
	Labels      []string
	DueDate     string
	Created     time.Time
	Updated     time.Time
	Resolution  string         // name
	Parent      string         // key
	Custom      map[string]any // Sprint as []int, numbers as float64, ADF as json.RawMessage
	Links       []IssueLink
	Comments    []Comment
	Properties  map[string]json.RawMessage
	Changelog   []History // oldest first
}

// IssueLink is a link as seen from the issue: Outward when the issue is the source.
type IssueLink struct {
	ID      string
	Type    string
	Outward bool
	Other   string // key
}

// Comment is a stored comment.
type Comment struct {
	ID      string
	Author  string
	Body    json.RawMessage
	Text    string
	Created time.Time
	Updated time.Time
}

// History is a changelog entry; Items are as served.
type History struct {
	ID      string
	Author  string
	Created time.Time
	Items   []ChangeItem
}

// ChangeItem is a changelog item; a null is "".
type ChangeItem struct {
	Field, FieldType, FieldID, From, FromString, To, ToString string
}

// Issue is the current state of a live issue, by id or key (old keys
// resolve). It fails the test when there is none; see Exists.
func (s *Server) Issue(idOrKey string) Issue {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.lookup(idOrKey)
	if rec == nil {
		s.t.Fatalf("jiratest: no issue %s", idOrKey)
	}
	st := rec.cur
	out := Issue{
		ID: strconv.Itoa(st.id), Key: st.key, Project: st.project.def.Key, Type: st.typ.Name,
		Status: st.status.Name, Category: st.status.Category, Summary: st.summary, Description: st.description,
		Priority: s.priorityName(st.priority), Assignee: st.assignee, Reporter: st.reporter, Creator: st.creator,
		Labels: slices.Clone(st.labels), DueDate: st.duedate, Created: st.created, Updated: st.updated,
		Resolution: s.resolutionName(st.resolution), Parent: s.issueKey(st.parent),
		Custom: map[string]any{}, Properties: map[string]json.RawMessage{},
	}
	for k, v := range st.custom {
		out.Custom[k] = v
	}
	for _, lid := range st.links {
		l := s.links[lid]
		il := IssueLink{ID: strconv.Itoa(l.id), Type: l.typ.Name, Outward: l.source == st.id, Other: s.issueKey(l.source)}
		if il.Outward {
			il.Other = s.issueKey(l.dest)
		}
		out.Links = append(out.Links, il)
	}
	for _, c := range st.comments {
		out.Comments = append(out.Comments, Comment{ID: strconv.Itoa(c.id), Author: c.author, Body: c.body,
			Text: adfText(c.body), Created: c.created, Updated: c.updated})
	}
	for k, v := range rec.props {
		out.Properties[k] = v
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, h := range rec.histories {
		hh := History{ID: strconv.Itoa(h.id), Author: h.author, Created: h.created}
		for _, it := range h.items {
			hh.Items = append(hh.Items, ChangeItem{it.Field, it.FieldType, it.FieldID,
				deref(it.From), deref(it.FromString), deref(it.To), deref(it.ToString)})
		}
		out.Changelog = append(out.Changelog, hh)
	}
	return out
}

// Exists reports whether a live issue has the id or key.
func (s *Server) Exists(idOrKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(idOrKey) != nil
}

// Keys are the keys of the live issues, in id order.
func (s *Server) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, id := range sortedIssueIDs(s.issues) {
		if st := s.issues[id].cur; !st.deleted {
			out = append(out, st.key)
		}
	}
	return out
}
