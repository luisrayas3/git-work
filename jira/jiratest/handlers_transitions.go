package jiratest

import (
	"fmt"
	"net/http"
	"slices"
)

// available are the transitions out of the issue's status: its own
// edges and the global ones (api.md §6.1, §8.12).
func available(st *issueState) []Transition {
	var out []Transition
	for _, t := range st.typ.Workflow.Transitions {
		if len(t.From) == 0 || slices.Contains(t.From, st.status.ID) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) transitionJSON(st *issueState, t Transition, withFields bool) map[string]any {
	m := map[string]any{
		"id":            t.ID,
		"name":          t.Name,
		"to":            s.statusJSON(st.project, st.project.status(t.To)),
		"hasScreen":     len(t.Screen) > 0,
		"isGlobal":      len(t.From) == 0,
		"isInitial":     false,
		"isAvailable":   true,
		"isConditional": false,
		"isLooped":      false,
	}
	if withFields {
		fields := map[string]any{}
		for _, f := range t.Screen {
			fields[f.FieldID] = s.fieldMeta(st.project, st.typ, f.FieldID, f.Required)
		}
		m["fields"] = fields
	}
	return m
}

func (s *Server) transitionsJSON(st *issueState, withFields bool) []map[string]any {
	out := []map[string]any{}
	for _, t := range available(st) {
		out = append(out, s.transitionJSON(st, t, withFields))
	}
	return out
}

func (s *Server) getTransitions(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil || c.user == nil {
		return 0, nil, issueNotFound()
	}
	list := []map[string]any{}
	// Without Transition issues the list is empty, not a 403 (api.md §6.1).
	if !c.denied {
		withFields := parseExpand(c.q["expand"]...)["transitions.fields"]
		for _, t := range available(rec.cur) {
			if id := c.q.Get("transitionId"); id != "" && id != t.ID {
				continue
			}
			list = append(list, s.transitionJSON(rec.cur, t, withFields))
		}
	}
	return http.StatusOK, map[string]any{"expand": "transitions", "transitions": list}, nil
}

func (s *Server) doTransition(c *call) (int, any, error) {
	rec := s.lookup(c.v("issueIdOrKey"))
	if rec == nil {
		return 0, nil, issueNotFound()
	}
	if s.conflictNext > 0 {
		s.conflictNext--
		return 0, nil, &apiError{status: http.StatusConflict,
			messages: []string{"The issue could not be updated due to a conflicting update. Please try again."}}
	}
	var up issueUpdate
	if err := c.decode(&up); err != nil {
		return 0, nil, err
	}
	if up.Transition == nil || rawID(up.Transition.ID) == "" {
		return 0, nil, badRequest("Missing 'transition' identifier")
	}
	if err := s.checkWriteLimit(rec); err != nil {
		return 0, nil, err
	}
	if err := s.transition(rec, rawID(up.Transition.ID), &up, c.user.AccountID); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, nil
}

// transition moves the issue along one available edge, with the fields of
// its screen; a required screen field left unset is a 400 (R16).
func (s *Server) transition(rec *record, id string, up *issueUpdate, author string) *apiError {
	var tr *Transition
	for _, t := range available(rec.cur) {
		if t.ID == id {
			tr = &t
		}
	}
	if tr == nil {
		return badRequest(fmt.Sprintf("Transition id '%s' is not valid for this issue.", id))
	}
	onScreen := func(f string) bool {
		return slices.ContainsFunc(tr.Screen, func(x TransitionField) bool { return x.FieldID == f })
	}
	next := rec.cur.clone()
	app, errs := s.apply(next, up, modeTransition, onScreen)
	for _, f := range tr.Screen {
		if f.Required && unset(next, f.FieldID) && errs[f.FieldID] == "" {
			errs[f.FieldID] = fmt.Sprintf("%s is required.", s.fieldName(f.FieldID))
		}
	}
	if len(errs) > 0 {
		return fieldErrors(errs)
	}
	next.status = rec.cur.project.status(tr.To)
	if tr.Resolution != "" && next.resolution == "" {
		next.resolution = tr.Resolution
	}
	if next.status.Category != CategoryDone {
		next.resolution = ""
	}
	switch {
	case next.resolution == "":
		next.resolved = timeZero
	case rec.cur.resolution == "" || rec.cur.resolved.IsZero():
		next.resolved = s.clock()
	}
	for _, body := range app.comments {
		s.newComment(next, body, author, nil)
	}
	s.commit(rec, next, author, len(app.comments) > 0)
	return nil
}
