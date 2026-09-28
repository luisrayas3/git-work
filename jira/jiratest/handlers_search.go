package jiratest

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// searchReq is SearchAndReconcileRequestBean; expand is a comma-delimited
// string even in the POST body (api.md §2.1).
type searchReq struct {
	JQL             string   `json:"jql"`
	NextPageToken   string   `json:"nextPageToken"`
	MaxResults      *int     `json:"maxResults"`
	Fields          []string `json:"fields"`
	Expand          string   `json:"expand"`
	Properties      []string `json:"properties"`
	FieldsByKeys    bool     `json:"fieldsByKeys"`
	FailFast        bool     `json:"failFast"`
	ReconcileIssues []int64  `json:"reconcileIssues"`
}

// cursor is a search in progress: the result fixed at its first page,
// so the pages of one search agree whatever is written meanwhile.
type cursor struct {
	fingerprint string
	reconcile   string
	results     []hit
	offset      int
}

type hit struct {
	rec *record
	st  *issueState
}

func (s *Server) searchGet(c *call) (int, any, error) {
	req := searchReq{
		JQL:           c.q.Get("jql"),
		NextPageToken: c.q.Get("nextPageToken"),
		Fields:        c.q["fields"],
		Expand:        strings.Join(c.q["expand"], ","),
		Properties:    c.q["properties"],
	}
	if v := c.q.Get("maxResults"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, nil, badRequest("The 'maxResults' parameter must be an integer.")
		}
		req.MaxResults = &n
	}
	for _, v := range splitList(c.q["reconcileIssues"]) {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, nil, badRequest(fmt.Sprintf("The value '%s' of 'reconcileIssues' is not an issue id.", v))
		}
		req.ReconcileIssues = append(req.ReconcileIssues, n)
	}
	return s.search(c, req)
}

func (s *Server) searchPost(c *call) (int, any, error) {
	var req searchReq
	if err := c.decode(&req); err != nil {
		return 0, nil, err
	}
	return s.search(c, req)
}

// env is the JQL environment of the caller: dates in their profile zone (C6).
func (s *Server) env(c *call) (*jqlEnv, error) {
	loc := s.siteZone
	if c.user != nil && c.user.TimeZone != "" {
		l, err := time.LoadLocation(c.user.TimeZone)
		if err != nil {
			return nil, err
		}
		loc = l
	}
	return &jqlEnv{s: s, user: c.user, loc: loc, now: s.clock(), hidden: c.denied}, nil
}

// query runs a JQL query against the index, with the reconciled ids read
// from the database (api.md §2.4).
func (s *Server) query(c *call, jql string, reconcile []int64) ([]hit, error) {
	q, perr := parseJQL(jql)
	if perr != nil {
		return nil, perr
	}
	if q.where == nil {
		return nil, badRequest(msgUnbounded)
	}
	env, err := s.env(c)
	if err != nil {
		return nil, err
	}
	match, perr := env.compile(q.where)
	if perr != nil {
		return nil, perr
	}
	order, perr := env.sorter(q.order)
	if perr != nil {
		return nil, perr
	}
	if c.user == nil || c.denied {
		// Anonymous and project-less callers see nothing, with a 200 (§17.5).
		return nil, nil
	}
	fresh := map[int]bool{}
	for _, id := range reconcile {
		fresh[int(id)] = true
	}
	var hits []hit
	for _, id := range sortedIssueIDs(s.issues) {
		rec := s.issues[id]
		st := s.indexed(rec)
		if fresh[id] {
			st = rec.cur
		}
		if st == nil || st.deleted || !match(st) {
			continue
		}
		hits = append(hits, hit{rec, st})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return order(a.st, b.st) })
	return hits, nil
}

func (s *Server) search(c *call, req searchReq) (int, any, error) {
	s.tick++
	if len(req.ReconcileIssues) > 50 {
		return 0, nil, badRequest("The 'reconcileIssues' parameter accepts at most 50 issue ids.")
	}
	// maxResults is advisory and at most 5000; pages come shorter (§4.9).
	page := 50
	if req.MaxResults != nil {
		page = *req.MaxResults
	}
	page = min(max(page, 1), 5000)
	if s.cfg.pageCap > 0 {
		page = min(page, s.cfg.pageCap)
	}
	reconcile := fmt.Sprint(req.ReconcileIssues)
	var cur *cursor
	if req.NextPageToken != "" {
		cur = s.cursors[req.NextPageToken]
		if cur == nil {
			return 0, nil, badRequest("The provided next page token is invalid or expired.")
		}
		if cur.fingerprint != req.JQL {
			return 0, nil, badRequest("The JQL query does not match the one of the next page token.")
		}
		// The ids "should be consistent with each paginated request" (api-vetting.md §4.8).
		if cur.reconcile != reconcile {
			return 0, nil, badRequest("The 'reconcileIssues' parameter must be the same on every page of a search.")
		}
	} else {
		hits, err := s.query(c, req.JQL, req.ReconcileIssues)
		if err != nil {
			return 0, nil, err
		}
		cur = &cursor{fingerprint: req.JQL, reconcile: reconcile, results: hits}
	}

	end := min(cur.offset+page, len(cur.results))
	expand := parseExpand(req.Expand)
	perIssue := map[string]bool{}
	for k := range expand {
		if k != "names" && k != "schema" {
			perIssue[k] = true
		}
	}
	opts := renderOpts{sel: parseFieldSel(req.Fields, "id"), expand: perIssue, props: splitList(req.Properties)}
	issues := []map[string]any{}
	names, schema := map[string]any{}, map[string]any{}
	for _, h := range cur.results[cur.offset:end] {
		j := s.issueJSON(h.rec, h.st, opts)
		issues = append(issues, j)
		if fields, ok := j["fields"].(map[string]any); ok {
			for id := range fields {
				names[id], schema[id] = s.fieldName(id), s.fieldSchema(id)
			}
		}
	}
	out := map[string]any{"issues": issues, "isLast": end >= len(cur.results)}
	if end < len(cur.results) {
		// The token is omitted on the last page, with isLast (R13).
		s.next.cursor++
		token := fmt.Sprintf("tok%dx%d", s.next.cursor, end)
		s.cursors[token] = &cursor{fingerprint: cur.fingerprint, reconcile: cur.reconcile, results: cur.results, offset: end}
		out["nextPageToken"] = token
	}
	if expand["names"] {
		out["names"] = names
	}
	if expand["schema"] {
		out["schema"] = schema
	}
	return http.StatusOK, out, nil
}

func (s *Server) countIssues(c *call) (int, any, error) {
	var req struct {
		JQL string `json:"jql"`
	}
	if err := c.decode(&req); err != nil {
		return 0, nil, err
	}
	s.tick++
	hits, err := s.query(c, req.JQL, nil)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"count": len(hits)}, nil
}
