package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

type linkReq struct {
	Type         ref          `json:"type"`
	InwardIssue  ref          `json:"inwardIssue"`
	OutwardIssue ref          `json:"outwardIssue"`
	Comment      *commentBody `json:"comment"`
}

func refIssue(r ref) string {
	if r.Key != "" {
		return r.Key
	}
	return r.id()
}

// linkIssues is POST /issueLink: inwardIssue is the source and
// outwardIssue the destination, "source <outward> destination" (C1).
// 201 with no body; a duplicate creates nothing and still answers 201.
func (s *Server) linkIssues(c *call) (int, any, error) {
	var in linkReq
	if err := c.decode(&in); err != nil {
		return 0, nil, err
	}
	lt := s.linkType(in.Type.id(), in.Type.Name)
	if lt == nil {
		return 0, nil, notFound(fmt.Sprintf("No issue link type with name '%s' found.", in.Type.Name))
	}
	src, dst := s.lookup(refIssue(in.InwardIssue)), s.lookup(refIssue(in.OutwardIssue))
	if src == nil || dst == nil {
		return 0, nil, notFound("Issue Does Not Exist")
	}
	var body json.RawMessage
	if in.Comment != nil {
		doc, msg := s.adfValue(in.Comment.Body)
		if msg != "" {
			return 0, nil, fieldErrors(map[string]string{"comment": msg})
		}
		body = doc
	}
	if err := s.checkWriteLimit(src, dst); err != nil {
		return 0, nil, err
	}
	s.link(src, dst, *lt, c.user.AccountID, body)
	return http.StatusCreated, nil, nil
}

// link stores source <outward> dest once and puts it on both issues.
// A comment goes on the outwardIssue, as the spec says (C11).
func (s *Server) link(src, dst *record, lt LinkType, author string, comment json.RawMessage) int {
	for _, l := range s.links {
		if l.typ.ID == lt.ID && l.source == src.cur.id && l.dest == dst.cur.id {
			return l.id
		}
	}
	s.next.link++
	l := &link{id: s.next.link, typ: lt, source: src.cur.id, dest: dst.cur.id}
	s.links[l.id] = l
	for _, rec := range []*record{src, dst} {
		next := rec.cur.clone()
		next.links = append(next.links, l.id)
		force := false
		if rec == dst && comment != nil {
			s.newComment(next, comment, author, nil)
			force = true
		}
		s.commit(rec, next, author, force)
	}
	return l.id
}

func (s *Server) linkByID(c *call) (*link, error) {
	id, err := strconv.Atoi(c.v("linkId"))
	if err != nil {
		return nil, badRequest(fmt.Sprintf("The issue link id '%s' is invalid.", c.v("linkId")))
	}
	l := s.links[id]
	if l == nil {
		return nil, notFound(fmt.Sprintf("No issue link with id '%s' exists.", c.v("linkId")))
	}
	return l, nil
}

func (s *Server) deleteIssueLink(c *call) (int, any, error) {
	l, err := s.linkByID(c)
	if err != nil {
		return 0, nil, err
	}
	src, dst := s.issues[l.source], s.issues[l.dest]
	if err := s.checkWriteLimit(src, dst); err != nil {
		return 0, nil, err
	}
	s.unlink(l, c.user.AccountID)
	return http.StatusNoContent, nil, nil
}

func (s *Server) unlink(l *link, author string) {
	for _, id := range []int{l.source, l.dest} {
		rec := s.issues[id]
		next := rec.cur.clone()
		next.links = slices.DeleteFunc(next.links, func(x int) bool { return x == l.id })
		s.commit(rec, next, author, false)
	}
	delete(s.links, l.id) // after the commits, whose Link items read it
}

func (s *Server) getLinkTypes(c *call) (int, any, error) {
	out := []map[string]any{}
	for _, lt := range s.linkTypes {
		out = append(out, s.linkTypeJSON(lt))
	}
	return http.StatusOK, map[string]any{"issueLinkTypes": out}, nil
}
