package jiratest

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

var timeZero time.Time

type project struct {
	def     Project
	nextNum int
}

func (p *project) status(id string) *Status {
	for i := range p.def.Statuses {
		if p.def.Statuses[i].ID == id {
			return &p.def.Statuses[i]
		}
	}
	return nil
}

func (p *project) issueType(idOrName string) *IssueType {
	for i := range p.def.IssueTypes {
		t := &p.def.IssueTypes[i]
		if t.ID == idOrName || strings.EqualFold(t.Name, idOrName) {
			return t
		}
	}
	return nil
}

func (p *project) sprint(id int) *Sprint {
	for i := range p.def.Sprints {
		if p.def.Sprints[i].ID == id {
			return &p.def.Sprints[i]
		}
	}
	return nil
}

// issueState is one version of an issue. Versions are immutable once
// committed: a write clones the current one, so the lagging index can keep
// serving an old version.
type issueState struct {
	id          int
	key         string
	project     *project
	typ         *IssueType
	status      *Status
	summary     string
	description json.RawMessage // ADF, nil when empty
	priority    string          // id
	assignee    string          // accountId
	reporter    string
	creator     string
	labels      []string // sorted
	duedate     string
	created     time.Time
	updated     time.Time
	catChanged  time.Time
	resolution  string // id
	resolved    time.Time
	parent      int // issue id, 0 for none
	custom      map[string]any
	links       []int
	comments    []*comment
	deleted     bool
}

func (st *issueState) clone() *issueState {
	c := *st
	c.labels = slices.Clone(st.labels)
	c.custom = maps.Clone(st.custom)
	c.links = slices.Clone(st.links)
	c.comments = slices.Clone(st.comments)
	return &c
}

// comment is immutable; an edit replaces it.
type comment struct {
	id           int
	author       string
	updateAuthor string
	body         json.RawMessage
	created      time.Time
	updated      time.Time
	visibility   json.RawMessage
}

// link is a stored issue link: source <outward> dest, which POST /issueLink
// spells {inwardIssue: source, outwardIssue: dest} (C1).
type link struct {
	id     int
	typ    LinkType
	source int
	dest   int
}

type history struct {
	id      int
	author  string
	created time.Time
	items   []item
}

// item is a ChangeDetails; nil strings are JSON nulls, and an empty
// fieldID is omitted (C2, C8).
type item struct {
	Field      string  `json:"field"`
	FieldType  string  `json:"fieldtype"`
	FieldID    string  `json:"fieldId,omitempty"`
	From       *string `json:"from"`
	FromString *string `json:"fromString"`
	To         *string `json:"to"`
	ToString   *string `json:"toString"`
}

type version struct {
	st   *issueState
	at   time.Time
	tick int
}

type record struct {
	cur       *issueState
	versions  []version
	histories []*history
	props     map[string]json.RawMessage
	writes    []time.Time
}

func (s *Server) seed(site Site) error {
	s.site = site
	zone := site.TimeZone
	if s.cfg.siteZone != "" {
		zone = s.cfg.siteZone
	}
	if zone == "" {
		zone = "America/Los_Angeles"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return fmt.Errorf("site time zone: %w", err)
	}
	s.siteZone = loc
	for i := range site.Users {
		u := site.Users[i]
		if u.AccountType == "" {
			u.AccountType = "atlassian"
		}
		s.users = append(s.users, &u)
	}
	if s.user(site.Me) == nil || s.user(site.Me).APIToken == "" {
		return fmt.Errorf("site Me %q is not a user with an API token", site.Me)
	}
	if s.user(site.WebUser) == nil {
		return fmt.Errorf("site WebUser %q is not a user", site.WebUser)
	}
	s.priorities = slices.Clone(site.Priorities)
	s.resolutions = slices.Clone(site.Resolutions)
	s.linkTypes = slices.Clone(site.LinkTypes)
	for i := range site.Fields {
		f := site.Fields[i]
		s.fields = append(s.fields, &f)
	}
	for _, p := range site.Projects {
		if err := s.addProject(p); err != nil {
			return err
		}
	}
	return nil
}

// Seed adds a project to a running site.
func (s *Server) Seed(p Project) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.addProject(p); err != nil {
		s.t.Fatalf("jiratest: %v", err)
	}
}

func (s *Server) addProject(def Project) error {
	if s.projectByKey(def.Key) != nil {
		return fmt.Errorf("project %s seeded twice", def.Key)
	}
	def.IssueTypes = slices.Clone(def.IssueTypes)
	def.Statuses = slices.Clone(def.Statuses)
	def.Sprints = slices.Clone(def.Sprints)
	p := &project{def: def, nextNum: 1}
	for _, t := range def.IssueTypes {
		wf := t.Workflow
		if p.status(wf.Initial) == nil {
			return fmt.Errorf("%s/%s: initial status %q is not a project status", def.Key, t.Name, wf.Initial)
		}
		for _, tr := range wf.Transitions {
			for _, id := range append(slices.Clone(tr.From), tr.To) {
				if p.status(id) == nil {
					return fmt.Errorf("%s/%s: transition %s names status %q", def.Key, t.Name, tr.ID, id)
				}
			}
		}
		for _, f := range t.Fields {
			if !isSystemField(f) && s.field(f) == nil {
				return fmt.Errorf("%s/%s: field %q is not a site field", def.Key, t.Name, f)
			}
		}
	}
	s.projects = append(s.projects, p)
	return nil
}

func (s *Server) user(accountID string) *User {
	for _, u := range s.users {
		if u.AccountID == accountID {
			return u
		}
	}
	return nil
}

func (s *Server) field(id string) *CustomField {
	for _, f := range s.fields {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// rankField is the id of the gh-lexo-rank field, "" when the site has none.
func (s *Server) rankField() string {
	for _, f := range s.fields {
		if f.Kind == KindRank {
			return f.ID
		}
	}
	return ""
}

func (s *Server) projectByKey(idOrKey string) *project {
	for _, p := range s.projects {
		if p.def.Key == idOrKey || p.def.ID == idOrKey {
			return p
		}
	}
	return nil
}

func (s *Server) priority(idOrName string) *Priority {
	for i := range s.priorities {
		if s.priorities[i].ID == idOrName || s.priorities[i].Name == idOrName {
			return &s.priorities[i]
		}
	}
	return nil
}

func (s *Server) resolution(idOrName string) *Resolution {
	for i := range s.resolutions {
		if s.resolutions[i].ID == idOrName || s.resolutions[i].Name == idOrName {
			return &s.resolutions[i]
		}
	}
	return nil
}

func (s *Server) linkType(id, name string) *LinkType {
	for i := range s.linkTypes {
		lt := &s.linkTypes[i]
		if (id != "" && lt.ID == id) || (name != "" && strings.EqualFold(lt.Name, name)) {
			return lt
		}
	}
	return nil
}

// lookup finds a live issue by id or key; a key that no longer matches is
// resolved "case-insensitively and for moved issues" (api.md §3.1).
func (s *Server) lookup(idOrKey string) *record {
	id, err := strconv.Atoi(idOrKey)
	if err != nil {
		id = s.keys[strings.ToUpper(idOrKey)]
	}
	rec := s.issues[id]
	if rec == nil || rec.cur.deleted {
		return nil
	}
	return rec
}

// readable is what GET /issue sees: the database, or the index with WithStaleReads.
func (s *Server) readable(rec *record) *issueState {
	if !s.cfg.staleReads {
		return rec.cur
	}
	if st := s.indexed(rec); st != nil && !st.deleted {
		return st
	}
	return nil
}

// indexed is the version search sees: the newest one older than the lag,
// nil when none is (an issue created too recently).
func (s *Server) indexed(rec *record) *issueState {
	now := s.clock()
	for i := len(rec.versions) - 1; i >= 0; i-- {
		v := rec.versions[i]
		if s.tick-v.tick > s.cfg.lagSearches && now.Sub(v.at) >= s.cfg.lagDuration {
			return v.st
		}
	}
	return nil
}

// commit makes next the current version of rec. A change that makes a
// changelog entry bumps updated, and so does force (a comment added);
// it reports whether updated moved.
func (s *Server) commit(rec *record, next *issueState, author string, force bool) bool {
	now := s.clock()
	bumped := force
	if rec.cur != nil {
		if items := s.diff(rec.cur, next); len(items) > 0 {
			s.next.history++
			rec.histories = append(rec.histories, &history{id: s.next.history, author: author, created: now, items: items})
			bumped = true
		}
		if rec.cur.status.Category != next.status.Category {
			next.catChanged = now
		}
	}
	if bumped {
		next.updated = now
	}
	rec.cur = next
	rec.versions = append(rec.versions, version{st: next, at: now, tick: s.tick})
	return bumped
}

// newRank is a LexoRank-shaped string that sorts in creation order (R8).
func (s *Server) newRank() string {
	s.next.rank++
	return "0|i" + fmt.Sprintf("%05s", strconv.FormatInt(int64(s.next.rank), 36)) + ":"
}

// checkWriteLimit applies the per-issue write limits (api.md §12.1) and
// counts the write when it passes.
func (s *Server) checkWriteLimit(recs ...*record) error {
	now := s.clock()
	for _, rec := range recs {
		for _, l := range s.cfg.perIssue {
			n := 0
			var oldest time.Time
			for _, t := range rec.writes {
				if now.Sub(t) < l.Window {
					if n == 0 {
						oldest = t
					}
					n++
				}
			}
			if n >= l.N {
				reset := oldest.Add(l.Window)
				return rateError("jira-per-issue-on-write", secondsUntil(now, reset), l.N, reset)
			}
		}
	}
	for _, rec := range recs {
		rec.writes = append(rec.writes, now)
	}
	return nil
}

func (s *Server) issueKey(id int) string {
	if rec := s.issues[id]; rec != nil {
		return rec.cur.key
	}
	return ""
}

// children are the live sub-tasks and child issues of id, in key order.
func (s *Server) children(id int) []*issueState {
	var out []*issueState
	for _, rec := range s.issues {
		if !rec.cur.deleted && rec.cur.parent == id {
			out = append(out, rec.cur)
		}
	}
	slices.SortFunc(out, func(a, b *issueState) int { return a.id - b.id })
	return out
}
