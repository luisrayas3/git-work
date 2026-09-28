package jiratest

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// tsLayout is the platform timestamp: milliseconds and an offset with no
// colon, in the site's default user zone, never assumed +0000 (C4, C6).
const tsLayout = "2006-01-02T15:04:05.000-0700"

// sprintLayout is the Sprint field's dates, which real sites send in UTC (C3).
const sprintLayout = "2006-01-02T15:04:05.000Z"

func (s *Server) ts(t time.Time) string { return t.In(s.siteZone).Format(tsLayout) }

func (s *Server) tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return s.ts(t)
}

func (s *Server) self(path string) string { return s.srv.URL + path }

type category struct {
	id               int
	key, name, color string
}

var categories = []category{
	{1, "undefined", "No Category", "medium-gray"},
	{2, CategoryNew, "To Do", "blue-gray"},
	{4, CategoryIndeterminate, "In Progress", "yellow"},
	{3, CategoryDone, "Done", "green"},
}

func categoryByKey(key string) category {
	for _, c := range categories {
		if c.key == key {
			return c
		}
	}
	return categories[0]
}

func (s *Server) categoryJSON(key string) map[string]any {
	c := categoryByKey(key)
	return map[string]any{
		"self": s.self(v3 + "/statuscategory/" + strconv.Itoa(c.id)),
		"id":   c.id, "key": c.key, "name": c.name, "colorName": c.color,
	}
}

func (s *Server) statusJSON(p *project, st *Status) map[string]any {
	m := map[string]any{
		"self":           s.self(v3 + "/status/" + st.ID),
		"description":    st.Description,
		"iconUrl":        s.self("/images/icons/statuses/generic.png"),
		"name":           st.Name,
		"id":             st.ID,
		"statusCategory": s.categoryJSON(st.Category),
	}
	if p.def.TeamManaged {
		m["scope"] = map[string]any{"type": "PROJECT", "project": map[string]any{"id": p.def.ID}}
	}
	return m
}

func (s *Server) issueTypeJSON(p *project, t *IssueType) map[string]any {
	m := map[string]any{
		"self":           s.self(v3 + "/issuetype/" + t.ID),
		"id":             t.ID,
		"description":    t.Description,
		"iconUrl":        s.self("/rest/api/2/universal_avatar/view/type/issuetype/avatar/" + t.ID),
		"name":           t.Name,
		"subtask":        t.Subtask(),
		"avatarId":       10300,
		"hierarchyLevel": t.HierarchyLevel,
	}
	if p.def.TeamManaged {
		m["entityId"] = "00000000-0000-4000-8000-0000000" + t.ID
		m["scope"] = map[string]any{"type": "PROJECT", "project": map[string]any{"id": p.def.ID}}
	}
	return m
}

func (s *Server) priorityJSON(id string) any {
	p := s.priority(id)
	if p == nil {
		return nil
	}
	return map[string]any{
		"self":    s.self(v3 + "/priority/" + p.ID),
		"iconUrl": s.self("/images/icons/priorities/" + strings.ToLower(p.Name) + ".svg"),
		"name":    p.Name,
		"id":      p.ID,
	}
}

func (s *Server) resolutionJSON(id string) any {
	r := s.resolution(id)
	if r == nil {
		return nil
	}
	return map[string]any{"self": s.self(v3 + "/resolution/" + r.ID), "id": r.ID, "description": r.Description, "name": r.Name}
}

func avatars(seed string) map[string]string {
	u := "https://avatar-management.example/" + url.PathEscape(seed)
	return map[string]string{"16x16": u + "/16", "24x24": u + "/24", "32x32": u + "/32", "48x48": u + "/48"}
}

func (s *Server) userZone(u *User) string {
	if u != nil && u.TimeZone != "" {
		return u.TimeZone
	}
	return s.siteZone.String()
}

// userJSON is a UserDetails, emailAddress omitted when hidden (R5), or nil.
func (s *Server) userJSON(accountID string) any {
	u := s.user(accountID)
	if u == nil {
		return nil
	}
	m := map[string]any{
		"self":        s.self(v3 + "/user?accountId=" + url.QueryEscape(u.AccountID)),
		"accountId":   u.AccountID,
		"accountType": u.AccountType,
		"avatarUrls":  avatars(u.AccountID),
		"displayName": u.DisplayName,
		"active":      !u.Inactive,
		"timeZone":    s.userZone(u),
	}
	if !u.EmailHidden && u.Email != "" {
		m["emailAddress"] = u.Email
	}
	return m
}

func (s *Server) projectRefJSON(p *project) map[string]any {
	return map[string]any{
		"self":           s.self(v3 + "/project/" + p.def.ID),
		"id":             p.def.ID,
		"key":            p.def.Key,
		"name":           p.def.Name,
		"projectTypeKey": "software",
		"simplified":     p.def.TeamManaged,
		"avatarUrls":     avatars(p.def.Key),
	}
}

// issueRef is the short issue of parent, subtasks and issuelinks (R4).
func (s *Server) issueRef(id int) map[string]any {
	rec := s.issues[id]
	if rec == nil {
		return nil
	}
	st := rec.cur
	return map[string]any{
		"id":   strconv.Itoa(st.id),
		"key":  st.key,
		"self": s.self(v3 + "/issue/" + strconv.Itoa(st.id)),
		"fields": map[string]any{
			"summary":   st.summary,
			"status":    s.statusJSON(st.project, st.status),
			"priority":  s.priorityJSON(st.priority),
			"issuetype": s.issueTypeJSON(st.project, st.typ),
		},
	}
}

func (s *Server) linkTypeJSON(lt LinkType) map[string]any {
	return map[string]any{"id": lt.ID, "name": lt.Name, "inward": lt.Inward, "outward": lt.Outward,
		"self": s.self(v3 + "/issueLinkType/" + lt.ID)}
}

// linkJSON is a link as seen from issue viewer: the other side only, under
// the key naming its role — the source sees outwardIssue: dest (C1).
// viewer 0 gives both sides, as GET /issueLink does.
func (s *Server) linkJSON(l *link, viewer int) map[string]any {
	m := map[string]any{
		"id":   strconv.Itoa(l.id),
		"self": s.self(v3 + "/issueLink/" + strconv.Itoa(l.id)),
		"type": s.linkTypeJSON(l.typ),
	}
	if viewer != l.dest {
		m["outwardIssue"] = s.issueRef(l.dest)
	}
	if viewer != l.source {
		m["inwardIssue"] = s.issueRef(l.source)
	}
	return m
}

func (s *Server) commentJSON(st *issueState, c *comment) map[string]any {
	m := map[string]any{
		"self":         s.self(v3 + "/issue/" + strconv.Itoa(st.id) + "/comment/" + strconv.Itoa(c.id)),
		"id":           strconv.Itoa(c.id),
		"author":       s.userJSON(c.author),
		"body":         c.body,
		"updateAuthor": s.userJSON(c.updateAuthor),
		"created":      s.ts(c.created),
		"updated":      s.ts(c.updated),
		"jsdPublic":    true,
	}
	if len(c.visibility) > 0 {
		m["visibility"] = c.visibility
	}
	return m
}

func (s *Server) historyJSON(h *history) map[string]any {
	return map[string]any{
		"id":      strconv.Itoa(h.id),
		"author":  s.userJSON(h.author),
		"created": s.ts(h.created),
		"items":   h.items,
	}
}

func (s *Server) sprintsJSON(p *project, ids []int) any {
	if len(ids) == 0 {
		return nil
	}
	var out []map[string]any
	for _, id := range ids {
		sp := p.sprint(id)
		if sp == nil {
			continue
		}
		m := map[string]any{"id": sp.ID, "name": sp.Name, "state": sp.State, "boardId": sp.BoardID, "goal": sp.Goal}
		for k, t := range map[string]time.Time{"startDate": sp.Start, "endDate": sp.End, "completeDate": sp.Complete} {
			if !t.IsZero() {
				m[k] = t.UTC().Format(sprintLayout)
			}
		}
		out = append(out, m)
	}
	return out
}

// fieldValue is the JSON of one field of st.
func (s *Server) fieldValue(st *issueState, id string) (any, bool) {
	switch id {
	case "summary":
		return st.summary, true
	case "description":
		if st.description == nil {
			return nil, true
		}
		return st.description, true
	case "issuetype":
		return s.issueTypeJSON(st.project, st.typ), true
	case "project":
		return s.projectRefJSON(st.project), true
	case "status":
		return s.statusJSON(st.project, st.status), true
	case "priority":
		return s.priorityJSON(st.priority), true
	case "assignee":
		return s.userJSON(st.assignee), true
	case "reporter":
		return s.userJSON(st.reporter), true
	case "creator":
		return s.userJSON(st.creator), true
	case "labels":
		return append([]string{}, st.labels...), true
	case "duedate":
		if st.duedate == "" {
			return nil, true
		}
		return st.duedate, true
	case "created":
		return s.ts(st.created), true
	case "updated":
		return s.ts(st.updated), true
	case "resolution":
		return s.resolutionJSON(st.resolution), true
	case "resolutiondate":
		return s.tsOrNil(st.resolved), true
	case "statuscategorychangedate":
		return s.ts(st.catChanged), true
	case "parent":
		if st.parent == 0 {
			return nil, false // absent with no parent (R4 accepts absent or null)
		}
		return s.issueRef(st.parent), true
	case "subtasks":
		subs := []map[string]any{}
		for _, c := range s.children(st.id) {
			if c.typ.Subtask() {
				subs = append(subs, s.issueRef(c.id))
			}
		}
		return subs, true
	case "issuelinks":
		out := []map[string]any{}
		for _, lid := range st.links {
			if l := s.links[lid]; l != nil {
				out = append(out, s.linkJSON(l, st.id))
			}
		}
		return out, true
	case "comment":
		cs := []map[string]any{}
		for _, c := range st.comments {
			cs = append(cs, s.commentJSON(st, c))
		}
		return map[string]any{
			"comments": cs, "self": s.self(v3 + "/issue/" + strconv.Itoa(st.id) + "/comment"),
			"maxResults": len(cs), "total": len(cs), "startAt": 0,
		}, true
	}
	f := s.field(id)
	if f == nil {
		return nil, false
	}
	v, ok := st.custom[id]
	if !ok {
		return nil, true
	}
	switch f.Kind {
	case KindSprint:
		return s.sprintsJSON(st.project, v.([]int)), true
	case KindOption:
		for _, o := range f.Options {
			if o.ID == v {
				return map[string]any{"self": s.self(v3 + "/customFieldOption/" + o.ID), "id": o.ID, "value": o.Value}, true
			}
		}
		return nil, true
	}
	return v, true
}

type renderOpts struct {
	sel    fieldSel
	expand map[string]bool
	props  []string
	search bool // properties as the index has them
}

func parseExpand(vals ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vals {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				m[p] = true
			}
		}
	}
	return m
}

// issueJSON is an IssueBean.
func (s *Server) issueJSON(rec *record, st *issueState, o renderOpts) map[string]any {
	out := map[string]any{"id": strconv.Itoa(st.id)}
	if o.sel.idOnly() && len(o.expand) == 0 && len(o.props) == 0 {
		return out
	}
	out["key"] = st.key
	out["self"] = s.self(v3 + "/issue/" + strconv.Itoa(st.id))
	out["expand"] = "renderedFields,names,schema,operations,editmeta,changelog,versionedRepresentations"

	fields := map[string]any{}
	names := map[string]any{}
	schema := map[string]any{}
	for _, id := range s.fieldIDs(st.typ) {
		nav := true
		if f := sysField(id); f != nil {
			nav = f.navigable
		}
		if !o.sel.wants(id, nav) {
			continue
		}
		v, ok := s.fieldValue(st, id)
		if !ok {
			continue
		}
		fields[id] = v
		names[id] = s.fieldName(id)
		schema[id] = s.fieldSchema(id)
	}
	if !o.sel.idOnly() {
		out["fields"] = fields
	}
	if o.expand["names"] {
		out["names"] = names
	}
	if o.expand["schema"] {
		out["schema"] = schema
	}
	if o.expand["changelog"] {
		out["changelog"] = s.embeddedChangelog(rec)
	}
	if o.expand["transitions"] {
		out["transitions"] = s.transitionsJSON(st, false)
	}
	if len(o.props) > 0 {
		src := rec.props
		if o.search {
			src = s.indexedProps(rec)
		}
		props := map[string]any{}
		for _, k := range o.props {
			if k == "*all" {
				for pk, v := range src {
					props[pk] = v
				}
				continue
			}
			if v, ok := src[k]; ok {
				props[k] = v
			}
		}
		out["properties"] = props
	}
	return out
}

// embeddedChangelog is expand=changelog: at most the 100 newest histories,
// newest first by the spec, shuffled unless WithOrderedChangelog (C8).
func (s *Server) embeddedChangelog(rec *record) map[string]any {
	hs := slices.Clone(rec.histories)
	slices.Reverse(hs)
	if len(hs) > 100 {
		hs = hs[:100]
	}
	if s.cfg.shuffleChangelog {
		s.rng.Shuffle(len(hs), func(i, j int) { hs[i], hs[j] = hs[j], hs[i] })
	}
	out := []map[string]any{}
	for _, h := range hs {
		out = append(out, s.historyJSON(h))
	}
	return map[string]any{"startAt": 0, "maxResults": len(out), "total": len(rec.histories), "histories": out}
}
