package jira

import (
	"fmt"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
)

// ---- creates, links and imports (JS15) ----

// create exports a local issue with no jira-id: journal on disk, POST with
// the built-ins, then the ordinary merge from a create base. A journal entry
// says an earlier POST may have landed: the issue is looked for by its
// property first, and created again only once the entry has settled.
func (e *engine) create(ic *cache.IssueCache) error {
	id := ic.Id()
	e.done[id] = true
	snap := ic.Snapshot()
	line := Line{Issue: id, Action: ActionCreated}
	if reason := e.unexportable(snap.Fields); reason != "" {
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: "*", Reason: reason})
		e.report(line)
		return nil
	}
	if t, ok := e.st.Creating[id]; ok {
		jid, err := e.findCreated(id)
		if err != nil {
			return e.fail(line, err)
		}
		if jid != "" {
			return e.linkCreated(ic, jid)
		}
		if e.now().Sub(t) < e.opts.Settle {
			line.Action = ActionSkipped
			line.Pending = append(line.Pending, Skip{Key: "*", Retry: true,
				Reason: "a create at " + t.Format(time.RFC3339) + " may have landed; waiting for Jira's search to show it"})
			e.report(line)
			return nil
		}
	}
	typ, _ := issue.String(snap.Fields[typeKey])
	local := e.m.Local(snap, typ)
	body, sent, skips := e.m.Create(local, id, e.ix)
	line.Pending = append(line.Pending, skips...)
	if e.opts.DryRun {
		line.DryRun = true
		for _, k := range sent {
			line.exported(k, local.Fields[k])
		}
		e.report(line)
		return nil
	}
	e.st.Creating[id] = e.now()
	if err := e.persist(); err != nil {
		return err
	}
	ref, err := e.c.CreateIssue(e.ctx, body.Fields, body.Properties)
	if err != nil {
		if refused(err) {
			delete(e.st.Creating, id)
		}
		return e.fail(line, err)
	}
	e.ix.AddIssue(ref.ID, id)
	e.grew = true
	line.Jira = ref.Key
	for _, k := range sent {
		line.exported(k, local.Fields[k])
	}
	ri, cs, err := e.read(ref.ID)
	if err != nil {
		return e.fail(line, err)
	}
	return e.resume(ic, ri, cs, local, sent, line)
}

// resume continues a create whose POST landed, from its create base.
func (e *engine) resume(ic *cache.IssueCache, ri *jiraapi.Issue, cs []jiraapi.Comment, local Doc, sent []string, line Line) error {
	line.Jira = ri.Key
	b := createBase(local, e.m.FromJira(ri, cs, e.ix), sent)
	return e.converge(ic, b, ri, cs, map[string]string{MetaId: ri.ID, MetaAlias: ri.Key}, line)
}

// refused says Jira answered a write and did not make it: a 4xx other than
// a timeout or a rate limit. Anything else may have landed.
func refused(err error) bool {
	s := jiraapi.StatusCode(err)
	return s >= 400 && s < 500 && s != 408 && s != 429
}

// findCreated is the Jira issue whose property names id among those created
// since the oldest journal entry − Overlap, the lower id of two; one search
// per run answers every entry.
func (e *engine) findCreated(id entity.Id) (string, error) {
	if e.created == nil {
		loc, err := e.location()
		if err != nil {
			return "", err
		}
		var since time.Time
		for _, t := range e.st.Creating {
			if since.IsZero() || t.Before(since) {
				since = t
			}
		}
		hits, err := e.search("project = " + jiraapi.JQLQuote(e.p.Key) + " AND created >= " +
			jiraapi.JQLTime(since.Add(-e.opts.Overlap), loc) + " ORDER BY created ASC, id ASC")
		if err != nil {
			return "", err
		}
		e.created = lowestByProperty(hits)
	}
	return e.created[id], nil
}

// createBase is JS15's: a sent key's base is local, so Jira's normal form
// imports; another is Jira's when local holds a value, so it is written now,
// and local's when not, so a Jira default imports.
func createBase(local, remote Doc, sent []string) *Base {
	b := &Base{V: baseVersion, Id: remote.Id, Key: remote.Key, Fields: map[string]issue.Value{}, Body: Digest(local.Body.Text)}
	carried := map[string]bool{}
	for _, k := range sent {
		carried[k] = true
	}
	for k, rv := range remote.Fields {
		lv := canon(local.Fields[k])
		if carried[k] || issue.IsNull(lv) || string(lv) == "[]" {
			b.Fields[k] = lv
		} else {
			b.Fields[k] = canon(rv)
		}
	}
	return b
}

// linkCreated links an issue whose POST landed and whose commit did not,
// found by its property (JS15): it resumes as a create.
func (e *engine) linkCreated(ic *cache.IssueCache, jiraId string) error {
	id := ic.Id()
	e.done[id] = true
	line := Line{Issue: id, Action: ActionCreated}
	if other, ok := e.ix.Issue(jiraId); ok && other != id {
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: "*", Reason: "the Jira issue created from it is linked to " + other.Human()})
		e.report(line)
		return nil
	}
	// I4: the search's copy of the property found it; the database decides
	ri, cs, err := e.read(jiraId)
	if err != nil {
		return e.fail(line, err)
	}
	if from := propertyOf(ri.Properties[PropertyKey]).Id; from != id {
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: "*", Reason: "Jira's " + ri.Key + " no longer names this issue in its git-work property"})
		e.report(line)
		return nil
	}
	e.ix.AddIssue(ri.ID, id)
	snap := ic.Snapshot()
	typ, _ := issue.String(snap.Fields[typeKey])
	local := e.m.Local(snap, typ)
	_, sent, _ := e.m.Create(local, id, e.ix)
	return e.resume(ic, ri, cs, local, sent, line)
}

// linkRequest links a local issue created with aliases: {jira: KEY}; it is
// never created, and merged with no base (JS15).
func (e *engine) linkRequest(ic *cache.IssueCache) error {
	id := ic.Id()
	e.done[id] = true
	key, _ := ic.Snapshot().GetCreateMetadata(MetaAlias)
	line := Line{Issue: id, Jira: key, Action: ActionLinked}
	skip := func(reason string) error {
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: MetaAlias, Reason: reason})
		e.report(line)
		return nil
	}
	ri, cs, err := e.read(key)
	if jiraapi.StatusCode(err) == 404 {
		return skip("alias " + key + " names no Jira issue")
	}
	if err != nil {
		return e.fail(line, err)
	}
	if !e.p.Owns(ri.Key) {
		return skip("alias " + key + " names an issue of another project")
	}
	if other, ok := e.ix.Issue(ri.ID); ok && other != id {
		return skip(key + " is already linked to " + other.Human())
	}
	if from := propertyOf(ri.Properties[PropertyKey]).Id; from != "" && from != id {
		return skip(key + " was created from another issue, " + from.Human())
	}
	e.ix.AddIssue(ri.ID, id)
	b := &Base{V: baseVersion, Id: ri.ID, Key: ri.Key, Fields: map[string]issue.Value{}}
	return e.converge(ic, b, ri, cs, map[string]string{MetaId: ri.ID}, line)
}

// importIssue creates a Jira issue locally (B3): the merge against no base
// and an empty local side, whose admitted fields and body are NewRaw's, by
// the reporter at its created, the create op the first marker; comments
// follow in the ordinary step 6.
func (e *engine) importIssue(jiraId string) error {
	line := Line{Action: ActionImported}
	ri, cs, err := e.read(jiraId)
	if err != nil {
		return e.fail(line, err)
	}
	line.Jira = ri.Key
	remote := e.m.FromJira(ri, cs, e.ix)
	if remote.Type == "" {
		e.sum.Skipped++ // after Derive, an unmapped type is one the schema excludes
		return nil
	}
	plan := Merge(nil, Doc{Type: remote.Type, Fields: map[string]issue.Value{}}, remote, e.multi(remote.Type), false)
	line.Pending = append(line.Pending, plan.Pending...)
	if e.opts.DryRun {
		line.DryRun = true
		line.record(plan.Local, remote)
		e.report(line)
		return nil
	}
	line.Pending = append(line.Pending, e.admit(&plan, remote.Type, &Base{})...)

	fields, sets := map[string]issue.Value{}, map[string]itemSet{}
	var head []LocalChange
	for _, lc := range plan.Local {
		switch lc.Kind {
		case LocalSet:
			if lc.Key != issue.TitleKey {
				fields[lc.Key] = lc.Value
			}
		case LocalAdd:
			if sets[lc.Key] == nil {
				sets[lc.Key] = itemSet{}
			}
			sets[lc.Key].add(lc.Value)
		case LocalEditBody:
		default:
			continue // comments: step 6
		}
		head = append(head, lc)
	}
	for k, s := range sets {
		fields[k] = s.value()
	}
	line.record(head, remote)
	plan.Base.Comments = nil
	created := remote.Created
	if created.IsZero() {
		created = e.now()
	}
	title, _ := issue.String(remote.Fields[issue.TitleKey])
	ic, _, err := e.repo.Issues().NewRaw(e.author(remote.Reporter), created.Unix(), title, remote.Body.Text, nil, fields,
		map[string]string{MetaId: ri.ID, MetaAlias: ri.Key, MetaSync: plan.Base.Marshal()})
	if err != nil {
		return e.fail(line, err)
	}
	e.ix.AddIssue(ri.ID, ic.Id())
	e.grew = true
	e.done[ic.Id()] = true
	line.Issue = ic.Id()
	if len(plan.Base.Retry) > 0 {
		e.retry = append(e.retry, ic.Id())
	}
	if len(cs) == 0 {
		if len(plan.Base.Retry) == 0 {
			e.st.Seen[ic.Id()] = ic.EditLamportTime()
		}
		e.report(line)
		return nil
	}
	b, _ := CurrentBase(ic.Snapshot())
	return e.commit(ic, b, ri, cs, nil, nil, nil, line)
}

// ---- Gone (JS19) ----

func (e *engine) runGone(hits []hit, l *local) error {
	present := map[string]bool{}
	for _, h := range hits {
		present[h.id] = true
	}
	type gone struct {
		ic   *cache.IssueCache
		b    *Base
		kind string
		key  string
	}
	var found []gone
	for _, id := range l.linked {
		jid := l.byId[id].CreateMetadata[MetaId]
		if present[jid] && !e.missing[jid] { // a lagging index still shows a deleted or moved issue
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		b, _ := CurrentBase(ic.Snapshot())
		if b.Gone != "" {
			continue
		}
		ri, err := e.c.GetIssue(e.ctx, jid, []string{"project"}, nil)
		switch {
		case jiraapi.StatusCode(err) == 404:
			found = append(found, gone{ic, b, GoneDeleted, b.Key})
		case err != nil:
			if err := stop(e.fail(Line{Issue: id, Jira: b.Key}, err)); err != nil {
				return err
			}
		case !e.p.Owns(ri.Key):
			found = append(found, gone{ic, b, GoneMoved, ri.Key})
		case !e.done[id]: // the index lagged: an ordinary sync
			if err := stop(e.syncLinked(ic)); err != nil {
				return err
			}
		}
	}
	if len(found) > e.opts.MaxDeletes && !e.opts.AcceptDeletes {
		for _, g := range found {
			e.report(Line{Issue: g.ic.Id(), Jira: g.key, Action: ActionSkipped,
				Pending: []Skip{{Key: "*", Reason: "missing from Jira (" + g.kind + "); held, as more than the limit are"}}})
		}
		return ErrDeletesHeld
	}
	for _, g := range found {
		if err := stop(e.markGone(g.ic, g.b, g.kind, g.key)); err != nil {
			return err
		}
	}
	return nil
}

// markGone sets the canceled status, writes the note, and records Gone in
// the marker, whose status is the canceled value: the one exception to I2.
func (e *engine) markGone(ic *cache.IssueCache, b *Base, kind, key string) error {
	e.done[ic.Id()] = true
	line := Line{Issue: ic.Id(), Jira: key, Action: ActionGone}
	if e.opts.DryRun {
		line.DryRun = true
		e.report(line)
		return nil
	}
	err := ic.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		now := e.now()
		typ, _ := issue.String(snap.Fields[typeKey])
		nb := b.clone()
		nb.Gone = kind
		var ops []issue.Operation
		if key, v, ok := e.m.Canceled(typ); ok {
			nb.Fields[key] = canon(v)
			if !same(snap.Fields[key], v) {
				ops = append(ops, issue.NewSetFieldOp(e.me, now.Unix(), key, v))
				line.imported(key, v)
			}
		}
		what := "was deleted in Jira, or is hidden from the sync's account"
		if kind == GoneMoved {
			what = "moved to another project, as " + key
		}
		note := issue.NewAddCommentOp(e.me, now.Unix(),
			fmt.Sprintf("Jira sync, %s: the Jira issue %s %s; it is no longer synced.", now.Format("2006-01-02 15:04 MST"), b.Key, what), nil)
		note.SetMetadata(MetaNote, NoteDeleted)
		ops = append(ops, note, issue.NewNoOpOp(e.me, now.Unix(), map[string]string{MetaSync: nb.Marshal()}))
		return ops, nil
	})
	if err != nil {
		return e.fail(line, err)
	}
	e.st.Seen[ic.Id()] = ic.EditLamportTime()
	e.report(line)
	return nil
}
