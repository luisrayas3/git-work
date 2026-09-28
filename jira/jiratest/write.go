package jiratest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// issueUpdate is IssueUpdateDetails (jira-api.md §5.1, §5.3, §6.2).
type issueUpdate struct {
	Fields     map[string]json.RawMessage              `json:"fields"`
	Update     map[string][]map[string]json.RawMessage `json:"update"`
	Transition *struct {
		ID json.RawMessage `json:"id"`
	} `json:"transition"`
	Properties []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	} `json:"properties"`
	HistoryMetadata json.RawMessage `json:"historyMetadata"`
}

type writeMode int

const (
	modeCreate writeMode = iota
	modeEdit
	modeTransition
)

// readOnly fields are never settable, whatever the screen.
var readOnly = map[string]bool{
	"status": true, "creator": true, "created": true, "updated": true, "resolutiondate": true,
	"statuscategorychangedate": true, "subtasks": true, "issuelinks": true,
}

// applied is what applying an update produced beyond the state itself.
type applied struct {
	comments []json.RawMessage
}

// apply writes the fields and update of up into st, which the caller has
// cloned; on any error the caller drops st, so a write is all or nothing.
// screen reports whether a field is on the screen of the operation.
func (s *Server) apply(st *issueState, up *issueUpdate, mode writeMode, screen func(string) bool) (applied, map[string]string) {
	var out applied
	errs := map[string]string{}
	settable := func(id string) bool {
		switch {
		case readOnly[id]:
			return false
		case id == "comment":
			return mode != modeCreate
		case id == "resolution":
			return mode == modeTransition && screen(id)
		}
		if f := s.field(id); f != nil && f.Kind == KindRank {
			return false // written through the agile rank API only (jira-api.md §3.1)
		}
		if f := s.field(id); f == nil && !isSystemField(id) {
			return false
		}
		switch mode {
		case modeEdit:
			// PUT does not check screens, only the field context (jira-api.md §5.3).
			return id == "summary" || id == "issuetype" || typeHas(st.typ, id)
		default:
			return screen(id)
		}
	}

	for _, id := range sortedKeys(up.Fields) {
		if mode == modeCreate && (id == "project" || id == "issuetype") {
			continue
		}
		if _, dup := up.Update[id]; dup {
			errs[id] = fmt.Sprintf("Field '%s' cannot be set in both 'fields' and 'update'.", id)
			continue
		}
		if !settable(id) {
			errs[id] = fmt.Sprintf(msgCannotSetFmt, id)
			continue
		}
		if msg := s.setField(st, id, up.Fields[id]); msg != "" {
			errs[id] = msg
		}
	}
	for _, id := range sortedKeys(up.Update) {
		if !settable(id) {
			errs[id] = fmt.Sprintf(msgCannotSetFmt, id)
			continue
		}
		for _, op := range up.Update[id] {
			for _, verb := range sortedKeys(op) {
				if msg := s.updateOp(st, id, verb, op[verb], &out); msg != "" {
					errs[id] = msg
				}
			}
		}
	}
	return out, errs
}

// updateOp is one FieldUpdateOperation.
func (s *Server) updateOp(st *issueState, id, verb string, raw json.RawMessage, out *applied) string {
	switch {
	case verb == "set" && id == "parent":
		var none struct {
			None bool `json:"none"`
		}
		if json.Unmarshal(raw, &none) == nil && none.None {
			return s.setField(st, id, json.RawMessage("null"))
		}
		return s.setField(st, id, raw)
	case verb == "set":
		return s.setField(st, id, raw)
	case id == "labels" && (verb == "add" || verb == "remove"):
		var l string
		if json.Unmarshal(raw, &l) != nil {
			return "The label must be a string."
		}
		if msg := checkLabel(l); msg != "" {
			return msg
		}
		if verb == "add" && !slices.Contains(st.labels, l) {
			st.labels = append(st.labels, l)
			slices.Sort(st.labels)
		}
		if verb == "remove" {
			st.labels = slices.DeleteFunc(st.labels, func(x string) bool { return x == l })
		}
		return ""
	case id == "comment" && verb == "add":
		var body struct {
			Body json.RawMessage `json:"body"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return "INVALID_INPUT"
		}
		doc, msg := s.adfValue(body.Body)
		if msg != "" {
			return msg
		}
		out.comments = append(out.comments, doc)
		return ""
	}
	return fmt.Sprintf("The operation '%s' is not supported for the field '%s'.", verb, id)
}

func checkLabel(l string) string {
	if l == "" || strings.ContainsAny(l, " \t\n") {
		return fmt.Sprintf("The label '%s' contains spaces which is invalid.", l)
	}
	return ""
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// idOrKeyRef reads {"id": …}, {"key": …}, {"name": …} or {"accountId": …}.
type ref struct {
	ID        json.RawMessage `json:"id"`
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Value     string          `json:"value"`
	AccountID string          `json:"accountId"`
}

func (r ref) id() string {
	var s string
	if json.Unmarshal(r.ID, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(r.ID, &n) == nil {
		return n.String()
	}
	return ""
}

func parseRef(raw json.RawMessage) (ref, bool) {
	var r ref
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &r) != nil {
		return r, false
	}
	return r, true
}

// setField writes one field value, or answers the field's error message.
func (s *Server) setField(st *issueState, id string, raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	null := isNull(raw)
	switch id {
	case "summary":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return "The summary must be a string."
		}
		v = strings.TrimSpace(v)
		switch {
		case v == "":
			return "You must specify a summary of the issue."
		case utf8.RuneCountInString(v) > 255:
			return "Summary must be less than 255 characters."
		}
		st.summary = v
	case "description":
		if null {
			st.description = nil
			return ""
		}
		doc, msg := s.adfValue(raw)
		if msg != "" {
			return msg
		}
		if !sameADF(doc, st.description) {
			st.description = doc
		}
	case "priority":
		if null {
			st.priority = ""
			return ""
		}
		r, ok := parseRef(raw)
		p := s.priority(r.id())
		if p == nil && r.Name != "" {
			p = s.priority(r.Name)
		}
		if !ok || p == nil {
			return "Specify the Priority (id or name) in the proper format."
		}
		st.priority = p.ID
	case "assignee", "reporter":
		acc := ""
		if !null {
			r, ok := parseRef(raw)
			acc = r.AccountID
			if acc == "" {
				acc = r.id() // the spec's own examples send {"id"} (jira-api.md §5.1)
			}
			if !ok || s.user(acc) == nil {
				return "Specified user does not exist or you do not have required permissions"
			}
		}
		if id == "assignee" {
			st.assignee = acc
		} else {
			if acc == "" {
				return "Reporter is required."
			}
			st.reporter = acc
		}
	case "labels":
		var ls []string
		if !null && json.Unmarshal(raw, &ls) != nil {
			return "The labels must be an array of strings."
		}
		for _, l := range ls {
			if msg := checkLabel(l); msg != "" {
				return msg
			}
		}
		slices.Sort(ls)
		st.labels = slices.Compact(ls)
	case "duedate":
		return setDate(&st.duedate, raw, null)
	case "parent":
		return s.setParent(st, raw, null)
	case "issuetype":
		return setType(st, raw)
	case "resolution":
		if null {
			st.resolution = ""
			return ""
		}
		r, _ := parseRef(raw)
		res := s.resolution(r.id())
		if res == nil && r.Name != "" {
			res = s.resolution(r.Name)
		}
		if res == nil {
			return "Could not find resolution."
		}
		st.resolution = res.ID
	default:
		return s.setCustom(st, s.field(id), raw, null)
	}
	return ""
}

// setType is an edit's type change, which Jira allows between types of the
// project that share the workflow and the hierarchy level; any other change
// is a move, which the REST API does not offer (Server.Move is the web UI's).
func setType(st *issueState, raw json.RawMessage) string {
	r, ok := parseRef(raw)
	var t *IssueType
	if ok {
		if t = st.project.issueType(r.id()); t == nil && r.Name != "" {
			t = st.project.issueType(r.Name)
		}
	}
	if t == nil || t.HierarchyLevel != st.typ.HierarchyLevel || !reflect.DeepEqual(t.Workflow, st.typ.Workflow) {
		return "The issue type selected is invalid."
	}
	st.typ = t
	return ""
}

func setDate(dst *string, raw json.RawMessage, null bool) string {
	if null {
		*dst = ""
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "Error parsing date string: " + string(raw)
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "Error parsing date string: " + v
	}
	*dst = v
	return ""
}

// setParent sets the unified parent: a sub-task's parent is a base-level
// issue, a base issue's is an epic (jira-api.md §8.4), in the same project.
func (s *Server) setParent(st *issueState, raw json.RawMessage, null bool) string {
	if null {
		if st.typ.Subtask() {
			return "A sub-task must have a parent."
		}
		st.parent = 0
		return ""
	}
	r, ok := parseRef(raw)
	idOrKey := r.Key
	if idOrKey == "" {
		idOrKey = r.id()
	}
	rec := s.lookup(idOrKey)
	if !ok || rec == nil {
		return "Could not find issue by id or key."
	}
	p := rec.cur
	if p.id == st.id || p.typ.HierarchyLevel != st.typ.HierarchyLevel+1 {
		return "Given parent work item does not belong to appropriate hierarchy."
	}
	if p.project != st.project {
		return "The parent must be in the same project."
	}
	st.parent = p.id
	return ""
}

func (s *Server) setCustom(st *issueState, f *CustomField, raw json.RawMessage, null bool) string {
	if null {
		if f.Kind == KindSprint {
			// null leaves the open sprint, closed ones stay (jira-api.md §5.3).
			st.custom[f.ID] = s.closedSprints(st)
			if len(st.custom[f.ID].([]int)) == 0 {
				delete(st.custom, f.ID)
			}
			return ""
		}
		delete(st.custom, f.ID)
		return ""
	}
	switch f.Kind {
	case KindNumber:
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return "Operation value must be a number."
		}
		v, err := n.Float64()
		if err != nil {
			return "Operation value must be a number."
		}
		st.custom[f.ID] = v
	case KindString:
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return "Operation value must be a string"
		}
		st.custom[f.ID] = v
	case KindText:
		doc, msg := s.adfValue(raw)
		if msg != "" {
			return msg
		}
		if old, _ := st.custom[f.ID].(json.RawMessage); !sameADF(doc, old) {
			st.custom[f.ID] = doc
		}
	case KindDate:
		var v string
		if msg := setDate(&v, raw, false); msg != "" {
			return msg
		}
		st.custom[f.ID] = v
	case KindOption:
		r, ok := parseRef(raw)
		for _, o := range f.Options {
			if ok && ((r.id() != "" && o.ID == r.id()) || (r.Value != "" && o.Value == r.Value)) {
				st.custom[f.ID] = o.ID
				return ""
			}
		}
		return fmt.Sprintf("Option value '%s' is not valid", string(raw))
	case KindSprint:
		// Written as one number, never an array (R18).
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return "Number value expected as the Sprint id."
		}
		id, err := strconv.Atoi(n.String())
		sp := st.project.sprint(id)
		if err != nil || sp == nil {
			return fmt.Sprintf("Sprint with id %s does not exist", n.String())
		}
		if sp.State == "closed" {
			return "Issue can be assigned only active or future sprints."
		}
		// Only one open sprint at a time; closed ones stay (jira-api.md §11.3).
		st.custom[f.ID] = append(s.closedSprints(st), id)
	case KindRank:
		return fmt.Sprintf(msgCannotSetFmt, f.ID)
	}
	return ""
}

func (s *Server) closedSprints(st *issueState) []int {
	var out []int
	for _, f := range s.fields {
		if f.Kind != KindSprint {
			continue
		}
		ids, _ := st.custom[f.ID].([]int)
		for _, id := range ids {
			if sp := st.project.sprint(id); sp != nil && sp.State == "closed" {
				out = append(out, id)
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
