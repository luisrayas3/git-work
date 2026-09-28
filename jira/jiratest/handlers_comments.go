package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

type commentBody struct {
	Body       json.RawMessage `json:"body"`
	Visibility json.RawMessage `json:"visibility"`
}

// newComment appends a comment to st and returns it.
func (s *Server) newComment(st *issueState, body json.RawMessage, author string, visibility json.RawMessage) *comment {
	s.next.comment++
	now := s.clock()
	c := &comment{id: s.next.comment, author: author, updateAuthor: author, body: body,
		created: now, updated: now, visibility: visibility}
	st.comments = append(st.comments, c)
	return c
}

func findComment(st *issueState, id string) int {
	return slices.IndexFunc(st.comments, func(c *comment) bool { return strconv.Itoa(c.id) == id })
}

func commentNotFound(id string) error {
	return notFound(fmt.Sprintf("Can not find a comment for key: %s.", id))
}

func (s *Server) getComments(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil || c.user == nil {
		return 0, nil, issueNotFound()
	}
	st := s.readable(rec)
	if st == nil {
		return 0, nil, issueNotFound()
	}
	cs := slices.Clone(st.comments)
	switch c.q.Get("orderBy") {
	case "", "created", "+created":
	case "-created":
		slices.Reverse(cs)
	default:
		return 0, nil, badRequest("Invalid orderBy parameter: only 'created', '-created' and '+created' are supported.")
	}
	start, max, err := paging(c, 100, 5000)
	if err != nil {
		return 0, nil, err
	}
	rendered := parseExpand(c.q["expand"]...)["renderedBody"]
	page := []map[string]any{}
	for i := start; i < len(cs) && i < start+max; i++ {
		page = append(page, s.commentJSON(st, cs[i], rendered))
	}
	// The legacy offset shape: no isLast (api.md §0).
	return http.StatusOK, map[string]any{"startAt": start, "maxResults": max, "total": len(cs), "comments": page}, nil
}

func (s *Server) getComment(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil || c.user == nil {
		return 0, nil, issueNotFound()
	}
	i := findComment(rec.cur, c.v("id"))
	if i < 0 {
		return 0, nil, commentNotFound(c.v("id"))
	}
	return http.StatusOK, s.commentJSON(rec.cur, rec.cur.comments[i], parseExpand(c.q["expand"]...)["renderedBody"]), nil
}

func (s *Server) readCommentBody(c *call) (json.RawMessage, json.RawMessage, error) {
	var in commentBody
	if err := c.decode(&in); err != nil {
		return nil, nil, err
	}
	doc, msg := s.adfValue(in.Body)
	if msg != "" {
		return nil, nil, fieldErrors(map[string]string{"comment": msg})
	}
	return doc, in.Visibility, nil
}

func (s *Server) addComment(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	body, vis, err := s.readCommentBody(c)
	if err != nil {
		return 0, nil, err
	}
	if err := s.checkWriteLimit(rec); err != nil {
		return 0, nil, err
	}
	next := rec.cur.clone()
	cm := s.newComment(next, body, c.user.AccountID, vis)
	s.commit(rec, next, c.user.AccountID, true) // an add bumps updated (§7.1)
	return http.StatusCreated, s.commentJSON(next, cm, false), nil
}

func (s *Server) updateComment(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	i := findComment(rec.cur, c.v("id"))
	if i < 0 {
		return 0, nil, commentNotFound(c.v("id"))
	}
	body, vis, err := s.readCommentBody(c)
	if err != nil {
		return 0, nil, err
	}
	if err := s.checkWriteLimit(rec); err != nil {
		return 0, nil, err
	}
	cm := s.editComment(rec, i, body, vis, c.user.AccountID)
	return http.StatusOK, s.commentJSON(rec.cur, cm, parseExpand(c.q["expand"]...)["renderedBody"]), nil
}

func (s *Server) editComment(rec *record, i int, body, vis json.RawMessage, author string) *comment {
	next := rec.cur.clone()
	cm := *next.comments[i]
	cm.body, cm.updateAuthor, cm.updated = body, author, s.clock()
	if vis != nil {
		cm.visibility = vis
	}
	next.comments[i] = &cm
	s.commit(rec, next, author, s.cfg.commentEditBumps)
	return &cm
}

func (s *Server) deleteComment(c *call) (int, any, error) {
	if c.user == nil {
		return 0, nil, &apiError{status: http.StatusMethodNotAllowed, messages: []string{"Anonymous users cannot delete comments."}}
	}
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	i := findComment(rec.cur, c.v("id"))
	if i < 0 {
		return 0, nil, commentNotFound(c.v("id"))
	}
	if err := s.checkWriteLimit(rec); err != nil {
		return 0, nil, err
	}
	s.removeComment(rec, i, c.user.AccountID)
	return http.StatusNoContent, nil, nil
}

func (s *Server) removeComment(rec *record, i int, author string) {
	next := rec.cur.clone()
	next.comments = slices.Delete(next.comments, i, i+1)
	s.commit(rec, next, author, s.cfg.commentDeleteBumps)
}

// commentsByIDs is POST /comment/list: at most 1000 ids, a PageBeanComment (C13).
func (s *Server) commentsByIDs(c *call) (int, any, error) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.decode(&in); err != nil {
		return 0, nil, err
	}
	if len(in.IDs) == 0 || len(in.IDs) > 1000 {
		return 0, nil, badRequest("The number of comment ids must be between 1 and 1000.")
	}
	want := map[int]bool{}
	for _, id := range in.IDs {
		want[int(id)] = true
	}
	rendered := parseExpand(c.q["expand"]...)["renderedBody"]
	values := []map[string]any{}
	if !c.denied {
		for _, id := range sortedIssueIDs(s.issues) {
			st := s.issues[id].cur
			if st.deleted {
				continue
			}
			for _, cm := range st.comments {
				if want[cm.id] {
					values = append(values, s.commentJSON(st, cm, rendered))
				}
			}
		}
	}
	return http.StatusOK, map[string]any{
		"self": s.self(v3 + "/comment/list"), "maxResults": 1000, "startAt": 0,
		"total": len(values), "isLast": true, "values": values,
	}, nil
}

func sortedIssueIDs(m map[int]*record) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
