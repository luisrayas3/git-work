package jira

import (
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/util/sorted"
)

// ---- candidates (JS20) ----

type hit struct {
	id, key   string
	updated   time.Time
	prop      entity.Id // the git-work property
	mapped    bool      // of an issue type the schema maps
	refetched bool      // a failed hit of an earlier run, read by GET
}

// local is one scan of the excerpts.
type local struct {
	linked   []entity.Id // jira-id set, and the Index's winner for it
	requests []entity.Id // alias:jira and no jira-id (JS15)
	creates  []entity.Id // unlinked, unaliased, of a mapped type, unarchived
	byId     map[entity.Id]*cache.IssueExcerpt
}

func (e *engine) scan() (*local, error) {
	l := &local{byId: map[entity.Id]*cache.IssueExcerpt{}}
	ids := e.repo.Issues().AllIds()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		ex, err := e.repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		l.byId[id] = ex
		jid := ex.CreateMetadata[MetaId]
		switch {
		case jid != "":
			if winner, _ := e.ix.Issue(jid); winner == id {
				l.linked = append(l.linked, id)
			} else if !isArchived(ex.Fields) {
				// A2: never two local issues exporting to one Jira issue
				e.report(Line{Issue: id, Jira: ex.CreateMetadata[MetaAlias], Action: ActionSkipped,
					Pending: []Skip{{Key: "*", Reason: "its Jira issue is also linked to " + winner.Human() + "; archive one of the two (JS25)"}}})
			}
		case ex.CreateMetadata[MetaAlias] != "":
			l.requests = append(l.requests, id)
		case e.unexportable(ex.Fields) == "":
			l.creates = append(l.creates, id)
		}
	}
	return l, nil
}

// unexportable is why an unlinked issue is never created in Jira: v1
// exports every unarchived issue of a mapped type (JS15).
func (e *engine) unexportable(fields map[string]issue.Value) string {
	typ, _ := issue.String(fields[typeKey])
	if _, ok := e.m.issueType(typ); !ok {
		return "type " + typ + " is local-only"
	}
	if isArchived(fields) {
		return "archived issues are not exported"
	}
	return ""
}

// runIds syncs exactly the issues named, each by the path its links say.
func (e *engine) runIds() error {
	for _, id := range e.opts.Ids {
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		md := ic.Snapshot().Operations[0].AllMetadata()
		switch {
		case md[MetaId] != "":
			err = e.syncLinked(ic)
		case md[MetaAlias] != "":
			err = e.linkRequest(ic)
		default:
			err = e.create(ic)
		}
		if err := stop(err); err != nil {
			return err
		}
	}
	return nil
}

// maxFailed bounds the failed hits re-read in one run (A6).
const maxFailed = 100

func (e *engine) runAll() error {
	loc, err := e.location()
	if err != nil {
		return err
	}
	l, err := e.scan()
	if err != nil {
		return err
	}

	for _, id := range l.requests {
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		if err := stop(e.linkRequest(ic)); err != nil {
			return err
		}
	}

	jql := "project = " + jiraapi.JQLQuote(e.p.Key)
	if lb, ok := e.lowerBound(l); ok && !e.opts.Full {
		jql += " AND updated >= " + jiraapi.JQLTime(lb, loc)
	}
	failed := sorted.Keys(e.st.Failed)
	hits, err := e.search(jql + " ORDER BY updated ASC, id ASC")
	if err != nil {
		return err
	}
	if hits, err = e.refetch(failed, hits); err != nil {
		return err
	}
	if err := e.runHits(hits, l); err != nil {
		return err
	}

	for _, id := range l.linked {
		if e.done[id] {
			continue
		}
		// B5: an issue unchanged since its last sync is not even read
		if seen, ok := e.st.Seen[id]; ok && seen == l.byId[id].EditLamportTime {
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		snap := ic.Snapshot()
		b, _ := CurrentBase(snap)
		if b.settled() && !e.changed(snap, b) {
			if b.Gone == "" {
				e.st.Seen[id] = ic.EditLamportTime()
			}
			continue
		}
		if b.Gone != "" {
			if !e.opts.Full { // under --full the Gone pass decides
				e.report(Line{Issue: id, Jira: b.Key, Action: ActionSkipped,
					Pending: []Skip{{Key: "*", Reason: "the Jira issue is gone (" + b.Gone + "); local edits are not exported"}}})
			}
			continue
		}
		if err := stop(e.syncLinked(ic)); err != nil {
			return err
		}
	}

	var creates []*cache.IssueCache
	for _, id := range l.creates {
		if e.done[id] {
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		// one created search answers every create in doubt
		if at, ok := e.inDoubt(ic); ok && (e.doubt.IsZero() || at.Before(e.doubt)) {
			e.doubt = at
		}
		creates = append(creates, ic)
	}
	for _, ic := range creates {
		if err := stop(e.create(ic)); err != nil {
			return err
		}
	}

	if e.opts.Full {
		return e.runGone(hits, l)
	}
	return nil
}

// lowerBound is the cursor − Overlap; without a cursor it starts from the
// newest marker in the store (JS20). A create's issue is always later than
// the cursor, which only moves to hits seen before any POST.
func (e *engine) lowerBound(l *local) (time.Time, bool) {
	lb := e.st.Cursor
	if lb.IsZero() {
		for _, id := range l.linked {
			ic, err := e.repo.Issues().Resolve(id)
			if err != nil {
				continue
			}
			if b, _ := CurrentBase(ic.Snapshot()); b != nil && b.Updated.After(lb) {
				lb = b.Updated
			}
		}
		if lb.IsZero() {
			return lb, false
		}
	}
	return lb.Add(-e.opts.Overlap), true
}

func (e *engine) search(jql string) ([]hit, error) {
	var hits []hit
	err := e.c.SearchJQL(e.ctx, jiraapi.Search{JQL: jql, Fields: []string{"updated", "issuetype"}, Properties: []string{PropertyKey}, MaxResults: 100},
		func(page jiraapi.SearchPage) error {
			for i := range page.Issues {
				hits = append(hits, e.hitOf(&page.Issues[i]))
			}
			return nil
		})
	if err != nil {
		return nil, fatal{err}
	}
	return hits, nil
}

func (e *engine) hitOf(ri *jiraapi.Issue) hit {
	h := hit{id: ri.ID, key: ri.Key, prop: propertyOf(ri.Properties[PropertyKey]).Id}
	var u jiraapi.Time
	if _, err := ri.Decode("updated", &u); err == nil {
		h.updated = u.UTC()
	}
	var it jiraapi.IssueType
	if _, err := ri.Decode("issuetype", &it); err == nil {
		_, h.mapped = e.m.localType(it.ID)
	}
	return h
}

// refetch appends to hits the failed hits of earlier runs the search did
// not return, each read by GET: the database (I4), and never named in the
// JQL, because Jira refuses a whole query naming an id it cannot see, and a
// failed hit is most often an issue since deleted or hidden (A6). An id
// that is gone or moved out is dropped; Gone decides about its issue. One
// that fails again is a failed line. At most maxFailed are read, round
// robin from the state's FailedAfter, so every one is read within
// ceil(n/maxFailed) runs.
func (e *engine) refetch(failed []string, hits []hit) ([]hit, error) {
	found := map[string]bool{}
	for _, h := range hits {
		found[h.id] = true
	}
	failed = slices.DeleteFunc(slices.Clone(failed), func(id string) bool { return found[id] })
	start, _ := slices.BinarySearch(failed, e.st.FailedAfter+"\x00")
	failed = append(failed[start:], failed[:start]...)
	for _, id := range failed[:min(len(failed), maxFailed)] {
		e.st.FailedAfter = id
		ri, err := e.c.GetIssue(e.ctx, id, []string{"updated", "issuetype", "project"}, nil, PropertyKey)
		switch {
		case jiraapi.StatusCode(err) == 404:
			delete(e.st.Failed, id)
		case err != nil:
			if err := stop(e.fail(Line{Jira: id}, err)); err != nil {
				return nil, err
			}
		case !e.p.owns(ri.Key):
			delete(e.st.Failed, id)
		default:
			h := e.hitOf(ri)
			h.refetched = true
			hits = append(hits, h)
		}
	}
	return hits, nil
}

// property is the git-work property's value: {"id"} on an issue, {"op"} on
// a comment.
type property struct {
	Id entity.Id `json:"id"`
	Op entity.Id `json:"op"`
}

func propertyOf(raw json.RawMessage) property {
	var p property
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// runHits handles the hits in order. The cursor moves to the greatest
// updated the search reached; a hit that failed is kept in the state's
// Failed and re-read on the next run, so one issue failing forever costs one
// GET, not a window that only grows (JS20, A6).
func (e *engine) runHits(hits []hit, l *local) error {
	owner := lowestByProperty(hits)
	for _, h := range hits {
		err := e.runHit(h, owner, l)
		if !h.refetched && h.updated.After(e.cursor) {
			e.cursor = h.updated
		}
		if err == nil {
			delete(e.st.Failed, h.id)
			continue
		}
		e.st.Failed[h.id] = h.updated
		// unreached hits are after the cursor, or still in Failed
		if err := stop(err); err != nil {
			return err
		}
	}
	return nil
}

// lowestByProperty is JS15's owner: of two Jira issues naming one entity,
// the lower id links.
func lowestByProperty(hits []hit) map[entity.Id]string {
	owner := map[entity.Id]string{}
	for _, h := range hits {
		if cur, ok := owner[h.prop]; h.prop != "" && (!ok || cmpId(h.id, cur) < 0) {
			owner[h.prop] = h.id
		}
	}
	return owner
}

func (e *engine) runHit(h hit, owner map[entity.Id]string, l *local) error {
	if id, ok := e.ix.Issue(h.id); ok {
		if e.done[id] {
			return nil
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		snap := ic.Snapshot()
		b, _ := CurrentBase(snap)
		// JS13 step 1: our own echo, or an overlap re-seeing a synced issue
		if !e.opts.Full && h.updated.Equal(b.Updated) && b.settled() && b.Gone == "" && !e.changed(snap, b) {
			e.done[id] = true
			e.sum.Unchanged++
			return nil
		}
		return e.syncLinked(ic)
	}

	if h.prop != "" {
		ex, ok := l.byId[h.prop]
		switch {
		case !ok:
			// A2: another clone's export; importing it would duplicate the entity
			e.report(Line{Jira: h.key, Action: ActionSkipped,
				Pending: []Skip{{Key: "*", Reason: "created in Jira from issue " + h.prop.Human() + ", which this clone has not pulled; pull first"}}})
			return nil
		case owner[h.prop] != h.id || ex.CreateMetadata[MetaId] != "":
			e.report(Line{Issue: h.prop, Jira: h.key, Action: ActionSkipped,
				Pending: []Skip{{Key: "*", Reason: "a second Jira issue names this issue; only the lower id is linked"}}})
			return nil
		}
		ic, err := e.repo.Issues().Resolve(h.prop)
		if err != nil {
			return err
		}
		return e.linkCreated(ic, h.id)
	}
	if !h.mapped {
		// after Derive, an unmapped type is one the schema excludes: silent
		e.sum.Unchanged++
		return nil
	}
	return e.importIssue(h.id)
}

// changed reports whether the issue differs from its base locally.
func (e *engine) changed(snap *issue.Snapshot, b *Base) bool {
	typ, _ := issue.String(snap.Fields[typeKey])
	l := e.m.Local(snap, typ)
	for k, v := range l.Fields {
		multi := e.m.multi(typ, k)
		if !same(canonical(form(k, v), multi), canonical(b.Fields[k], multi)) {
			return true
		}
	}
	for _, c := range l.Comments {
		bd, ok := b.Comments[c.JiraId]
		switch {
		case c.Note:
		case c.JiraId == "" || !ok:
			return true
		case bd == "":
			if !IsTombstone(c.Text.Text) {
				return true
			}
		case digest(c.Text.Text) != bd:
			return true
		}
	}
	return false
}

// repass re-runs, once, the issues left with Retry keys, when the run added
// issues they might have been waiting for (JS17).
func (e *engine) repass() error {
	if !e.grew || e.opts.DryRun {
		return nil
	}
	ids := slices.Compact(slices.Sorted(slices.Values(e.retry)))
	e.retry = nil
	for _, id := range ids {
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		if err := stop(e.syncLinked(ic)); err != nil {
			return err
		}
	}
	return nil
}
