package jira

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/util/sorted"
)

// ---- step 4: the Jira writes ----

// write performs plan's remote changes, each independently, and records in
// b2 each scalar key and text written (I2: the second merge then imports
// Jira's normal form). A set's base stays: the second merge against it
// reaches the merged set on both sides. pairs are the comments it created,
// by Jira id; wrote says anything landed. Only a run failure is returned.
func (e *engine) write(ri *jiraapi.Issue, remote Doc, plan mergePlan, b2 *Base, line *Line) (pairs map[string]entity.Id, wrote bool, err error) {
	writes, skips := e.m.toWrites(remote.Type, plan.Remote, ri, e.ix)
	line.Pending = append(line.Pending, skips...)
	failed := map[string]bool{}
	for _, s := range skips {
		failed[s.Key] = true
	}
	pend := func(key, reason string) {
		failed[key] = true
		line.Pending = append(line.Pending, Skip{Key: key, Reason: reason})
	}
	// try is one request: a run failure stops, an issue failure is pending
	try := func(key string, err error) error {
		switch {
		case err == nil:
			wrote = true
		case runFatal(err):
			return err
		default:
			pend(key, err.Error())
		}
		return nil
	}

	// one PUT with every edit; per-field refusals are retried once without them
	var edits []jiraWrite
	for _, w := range writes {
		if w.Kind == writeEdit {
			edits = append(edits, w)
		}
	}
	for retried := false; len(edits) > 0; retried = true {
		fields, update := map[string]any{}, map[string][]jiraapi.Op{}
		for _, w := range edits {
			if w.Update != nil {
				update[w.Field] = append(update[w.Field], w.Update...)
			} else {
				fields[w.Field] = w.Set
			}
		}
		err := e.c.EditIssue(e.ctx, ri.ID, fields, update)
		var apiErr *jiraapi.Error
		if err == nil || runFatal(err) || retried || !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
			for _, w := range edits {
				if err := try(w.Key, err); err != nil {
					return nil, wrote, err
				}
			}
			break
		}
		var rest []jiraWrite
		for _, w := range edits {
			if msg, bad := apiErr.Fields[w.Field]; bad {
				pend(w.Key, "Jira refused "+w.Field+": "+msg)
			} else {
				rest = append(rest, w)
			}
		}
		if len(rest) == len(edits) { // Jira named nothing we sent: the edit fails
			for _, w := range edits {
				pend(w.Key, err.Error())
			}
			break
		}
		edits = rest
	}

	for _, w := range writes {
		switch w.Kind {
		case writeTransition:
			reason, err := e.transition(ri, w.Status)
			if err == nil && reason != "" {
				pend(w.Key, reason)
				continue
			}
			if err := try(w.Key, err); err != nil {
				return nil, wrote, err
			}
		case writeLink:
			for _, l := range w.Add {
				if err := try(w.Key, e.c.CreateIssueLink(e.ctx, l.LinkType, l.Source, l.Destination)); err != nil {
					return nil, wrote, err
				}
			}
			for _, id := range w.Remove {
				if err := try(w.Key, e.c.DeleteIssueLink(e.ctx, id)); err != nil {
					return nil, wrote, err
				}
			}
		}
	}

	for _, ch := range plan.Remote {
		if !failed[ch.Key] && ch.Set != nil {
			b2.Fields[ch.Key] = form(ch.Key, ch.Set)
		}
	}

	pairs = map[string]entity.Id{}
	for _, cw := range plan.Comments {
		adf := jiraapi.TextToADF(cw.Text)
		if cw.JiraId == "" {
			c, err := e.c.AddComment(e.ctx, ri.ID, adf, jiraapi.Property{Key: PropertyKey, Value: map[string]string{"op": cw.Op.String()}})
			if err := try(cw.key(), err); err != nil {
				return nil, wrote, err
			}
			if err == nil {
				pairs[c.ID] = cw.Op
			}
			continue
		}
		_, err := e.c.UpdateComment(e.ctx, ri.ID, cw.JiraId, adf)
		if err := try(cw.key(), err); err != nil {
			return nil, wrote, err
		}
		if err == nil {
			if b2.Comments == nil {
				b2.Comments = map[string]string{}
			}
			b2.Comments[cw.JiraId] = digest(cw.Text)
		}
	}
	line.exports(plan, remote, failed)
	return pairs, wrote, nil
}

// transition moves the issue to statusId: the transition whose target it
// is, a required resolution filled with its first allowed value, and one
// retry on 409 after re-reading (JS13 step 4, C9). A reason is pending.
func (e *engine) transition(ri *jiraapi.Issue, statusId string) (string, error) {
	for try := 0; ; try++ {
		ts, err := e.c.Transitions(e.ctx, ri.ID)
		if err != nil {
			return "", err
		}
		var t *jiraapi.Transition
		for i := range ts {
			if ts[i].To.ID == statusId {
				t = &ts[i]
				break
			}
		}
		if t == nil {
			return fmt.Sprintf("no transition from %s to %s", orId(e.m.fromIssue(ri, nil, e.ix).Status, "the current status"), e.statusName(statusId)), nil
		}
		fields := map[string]any{}
		for _, id := range sorted.Keys(t.Fields) {
			f := t.Fields[id]
			if !f.Required || f.HasDefaultValue {
				continue
			}
			if id == "resolution" && len(f.AllowedValues) > 0 {
				var o struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(f.AllowedValues[0], &o)
				fields[id] = map[string]string{"id": o.ID}
				continue
			}
			return fmt.Sprintf("the transition to %s requires %s", e.statusName(statusId), orId(f.Name, id)), nil
		}
		err = e.c.DoTransition(e.ctx, ri.ID, t.ID, fields)
		if jiraapi.StatusCode(err) == 409 && try == 0 {
			continue
		}
		return "", err
	}
}

func orId(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func (e *engine) statusName(id string) string {
	for _, it := range e.p.IssueTypes {
		for _, s := range it.Statuses {
			if s.ID == id {
				return s.Name
			}
		}
	}
	return "status " + id
}
