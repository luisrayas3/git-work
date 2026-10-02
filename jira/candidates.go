package jira

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
)

// ---- candidates (JS20) ----

type hit struct {
	id, key   string
	updated   time.Time
	created   time.Time
	prop      entity.Id // the git-work property
	mapped    bool      // of an issue type the schema maps
	refetched bool      // a failed hit of an earlier run, read by GET
}

// local is one scan of the excerpts.
type local struct {
	linked   []entity.Id // jira-id set, and the Index's winner for it
	losers   []entity.Id // jira-id set, another issue the Index's winner for it: consolidated (JS27)
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
				// A2: never two local issues exporting to one Jira issue;
				// an archived one is consolidated already (JS27)
				l.losers = append(l.losers, id)
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
			if winner, _ := e.ix.Issue(md[MetaId]); winner != id {
				err = e.consolidate(ic, winner)
			} else {
				err = e.syncLinked(ic)
			}
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
	// before the search, so the loser's last writes return its Jira issue
	// as a hit of this run, and the winner imports them now (JS27)
	for _, id := range l.losers {
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		winner, _ := e.ix.Issue(l.byId[id].CreateMetadata[MetaId])
		if err := stop(e.consolidate(ic, winner)); err != nil {
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
	err := e.c.SearchJQL(e.ctx, jiraapi.Search{JQL: jql, Fields: []string{"updated", "created", "issuetype"}, Properties: []string{PropertyKey}, MaxResults: 100},
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
	if _, err := ri.Decode("created", &u); err == nil {
		h.created = u.UTC()
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
		ri, err := e.c.GetIssue(e.ctx, id, []string{"updated", "created", "issuetype", "project"}, nil, PropertyKey)
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
			// A2: another clone's export; importing it would duplicate the
			// entity, unless the person's bound says it is lost (JS27)
			if e.adoptable(h.created) {
				return e.importIssue(h.id, h.prop)
			}
			e.orphan(h)
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
	return e.importIssue(h.id, "")
}

// adoptable says an issue created from an absent entity is old enough
// to import anyway: the person's bound, on Jira's clock (JS27).
func (e *engine) adoptable(created time.Time) bool {
	return e.opts.Adopt != nil && !created.IsZero() && e.now().Sub(created) >= *e.opts.Adopt
}

// orphan records and reports a hit skipped for an absent entity (JS27),
// so a child waiting on it can name the cause this run and the next.
func (e *engine) orphan(h hit) {
	o := Orphan{Key: h.key, From: h.prop, Created: h.created}
	e.st.Orphans[h.id] = o
	e.ix.absent[h.id] = o
	days := int(e.now().Sub(h.created).Hours() / 24)
	e.report(Line{Jira: h.key, Action: ActionSkipped,
		Pending: []Skip{{Key: "*", Reason: fmt.Sprintf("created in Jira %d days ago from issue %s, which this clone has not pulled; pull first, or --adopt %dd takes it",
			days, h.prop.Human(), days)}}})
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

// ---- consolidation (JS27) ----

// consolidate archives a second local copy of one Jira issue
// into the copy that reached Jira first:
// the loser is synced one last time, so what it holds for Jira reaches Jira,
// then archived;
// the winner takes the loser's local-only values where it has none,
// and a note names the loser;
// and every relation naming the loser is pointed at the winner,
// so the next merge of each finds `l == r`.
// A loser already archived is consolidated already.
func (e *engine) consolidate(loser *cache.IssueCache, winner entity.Id) error {
	id := loser.Id()
	e.done[id] = true
	if isArchived(loser.Snapshot().Fields) {
		return nil
	}
	key, _ := loser.Snapshot().GetCreateMetadata(MetaAlias)
	line := Line{Issue: id, Jira: key, Action: ActionConsolidated}
	line.Pending = append(line.Pending, Skip{Key: "*", Reason: "a second copy of " + key + "; " + winner.Human() + " reached Jira first"})
	if e.opts.DryRun {
		line.DryRun = true
		e.report(line)
		return nil
	}
	if err := e.syncLinked(loser); err != nil {
		return err
	}
	wc, err := e.repo.Issues().Resolve(winner)
	if err != nil {
		return e.fail(line, err)
	}
	now := e.now()
	err = loser.Update(func(*issue.Snapshot) ([]issue.Operation, error) {
		return []issue.Operation{issue.NewSetFieldOp(e.me, now.Unix(), schema.ArchivedKey, issue.MustValue(true))}, nil
	})
	if err != nil {
		return e.fail(line, err)
	}
	ls := loser.Snapshot()
	err = wc.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		ops, kept := e.fill(snap, ls, now)
		for _, k := range kept {
			line.imported(k, ls.Fields[k])
		}
		text := fmt.Sprintf("Jira sync, %s: %s was also tracked as %s, which reached Jira later; it is archived and its history kept.",
			now.Format("2006-01-02 15:04 MST"), key, id.Human())
		note := issue.NewAddCommentOp(e.me, now.Unix(), text, nil)
		note.SetMetadata(MetaNote, NoteConsolidated)
		return append(ops, note), nil
	})
	if err != nil {
		return e.fail(line, err)
	}
	if err := e.repoint(id, winner, now); err != nil {
		return e.fail(line, err)
	}
	e.report(line)
	return nil
}

// fill is the winner's gaps taken from the loser, local-only fields only:
// the missing-base rule with the winner in Jira's seat,
// a scalar it lacks, the union of a set (JS10).
// Mapped fields reach it through Jira.
func (e *engine) fill(winner, loser *issue.Snapshot, now time.Time) (ops []issue.Operation, kept []string) {
	wt, _ := issue.String(winner.Fields[typeKey])
	lt, _ := issue.String(loser.Fields[typeKey])
	mapped := map[string]bool{issue.TitleKey: true, typeKey: true, schema.ArchivedKey: true}
	for _, k := range e.m.keys(lt) {
		mapped[k] = true
	}
	for _, k := range sorted.Keys(loser.Fields) {
		lv := loser.Fields[k]
		if mapped[k] || issue.IsNull(lv) {
			continue
		}
		f, ok := e.schema.Field(wt, k)
		if !ok {
			continue
		}
		wv, has := winner.Fields[k]
		switch {
		case f.Kind.IsMulti():
			have := setOf(wv)
			for _, it := range sorted.Keys(setOf(lv)) {
				if _, ok := have[it]; !ok {
					ops = append(ops, issue.NewAddValueOp(e.me, now.Unix(), k, issue.Value(it)))
					kept = append(kept, k)
				}
			}
		case !has || issue.IsNull(wv):
			ops = append(ops, issue.NewSetFieldOp(e.me, now.Unix(), k, lv))
			kept = append(kept, k)
		}
	}
	return ops, slices.Compact(kept)
}

// repoint sets every relation that names the loser to the winner,
// one commit per issue.
func (e *engine) repoint(loser, winner entity.Id, now time.Time) error {
	from, to := issue.StringValue(loser.String()), issue.StringValue(winner.String())
	for _, id := range e.repo.Issues().AllIds() {
		ex, err := e.repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return err
		}
		typ, _ := issue.String(ex.Fields[typeKey])
		var ops []issue.Operation
		for _, k := range sorted.Keys(ex.Fields) {
			f, ok := e.schema.Field(typ, k)
			switch {
			case !ok || !f.Kind.IsRelation():
			case f.Kind.IsMulti():
				if _, has := setOf(ex.Fields[k])[string(from)]; has {
					ops = append(ops, issue.NewRemoveValueOp(e.me, now.Unix(), k, from),
						issue.NewAddValueOp(e.me, now.Unix(), k, to))
				}
			case same(ex.Fields[k], from):
				ops = append(ops, issue.NewSetFieldOp(e.me, now.Unix(), k, to))
			}
		}
		if len(ops) == 0 {
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		if err := ic.Update(func(*issue.Snapshot) ([]issue.Operation, error) { return ops, nil }); err != nil {
			return err
		}
	}
	return nil
}
