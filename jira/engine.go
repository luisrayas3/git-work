package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
)

// Options steer one run (JS22).
type Options struct {
	Ids                         []entity.Id // only these issues: no search, no cursor, no Gone
	DryRun, Full, AcceptDeletes bool
	Overlap                     time.Duration // default 5m
	Settle                      time.Duration // default 15m: how long an unanswered create stays in doubt (JS15)
	MaxDeletes                  int           // default 10
}

// ErrDeletesHeld stops a --full run that found more missing issues than
// MaxDeletes: a permission change looks exactly like a mass delete (JS19).
var ErrDeletesHeld = errors.New("jira: too many issues missing from Jira; none marked (--accept-deletes marks them)")

// Sync converges the store and the project, one issue at a time (JS13–JS24).
// A failure of one issue is its line and the run goes on; a failure of the
// run (JS23) stops it and is returned, after the summary. st is updated in
// place; the caller saves it unless DryRun.
func Sync(ctx context.Context, repo *cache.RepoCache, c *jiraapi.Client, p *Project, m *Mapping,
	st *State, opts Options, emit func(Line)) (Summary, error) {
	if opts.Overlap == 0 {
		opts.Overlap = 5 * time.Minute
	}
	if opts.Settle == 0 {
		opts.Settle = 15 * time.Minute
	}
	if opts.MaxDeletes == 0 {
		opts.MaxDeletes = 10
	}
	e := &engine{ctx: ctx, repo: repo, c: c, p: p, m: m, st: st, opts: opts, emit: emit,
		authors: map[string]identity.Interface{}, done: map[entity.Id]bool{}, cursor: st.Cursor}

	err := e.setup()
	if err == nil {
		if len(opts.Ids) > 0 {
			err = e.runIds()
		} else {
			err = e.runAll()
		}
	}
	if err == nil {
		err = e.repass()
	}
	if len(opts.Ids) == 0 && !opts.DryRun {
		st.Cursor = e.cursor.UTC()
	}
	e.sum.Cursor = st.Cursor
	emit(Line{Summary: &e.sum})
	return e.sum, err
}

type engine struct {
	ctx  context.Context
	repo *cache.RepoCache
	c    *jiraapi.Client
	p    *Project
	m    *Mapping
	st   *State
	opts Options
	emit func(Line)

	ix      *Index
	me      *cache.IdentityCache
	checker *schema.Checker
	authors map[string]identity.Interface // accountId -> identity, this run
	done    map[entity.Id]bool
	retry   []entity.Id // issues left with Retry keys, re-run once at the end
	grew    bool        // the run imported or created an issue
	cursor  time.Time
	sum     Summary
	created map[entity.Id]string // in-doubt creates found by the created search (JS15); nil until searched
}

// now is Jira's clock, from the Date of its last response, so journal times
// compare with Jira's created without client skew; the fallback is local.
func (e *engine) now() time.Time {
	if d := e.c.ServerDate(); !d.IsZero() {
		return d.UTC()
	}
	return time.Now().UTC()
}

// persist saves the state now: the create journal must be on disk before
// the POST it guards (JS15).
func (e *engine) persist() error {
	if err := e.st.Save(e.repo.LocalStorage()); err != nil {
		return fatal{fmt.Errorf("jira: saving the create journal: %w", err)}
	}
	return nil
}

// fatal marks an error that stops the run (JS23).
type fatal struct{ error }

func (f fatal) Unwrap() error { return f.error }

// runFatal is JS23's split: 401, 429 and 5xx after the client's retries, and
// transport errors stop the run; anything else fails one issue.
func runFatal(err error) bool {
	var f fatal
	if errors.As(err, &f) {
		return true
	}
	var apiErr *jiraapi.Error
	if errors.As(err, &apiErr) {
		s := apiErr.StatusCode
		return s == 401 || s == 429 || s >= 500
	}
	var ue *url.Error
	var ne net.Error
	return errors.As(err, &ue) || errors.As(err, &ne) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (e *engine) setup() error {
	me, _, err := e.repo.EnsureUserIdentity()
	if err != nil {
		return err
	}
	e.me = me
	if e.ix, err = NewIndex(e.repo, e.m); err != nil {
		return err
	}
	// JS16: the token's account is the runner's, unless an identity has it
	if acc := e.p.Me.AccountID; acc != "" && !e.opts.DryRun {
		if _, ok := e.ix.User(acc); !ok {
			me.SetMetadata(MetaAccountId, acc)
			if err := me.Commit(); err != nil {
				return err
			}
			e.ix.AddUser(acc, me.Id())
		}
	}
	e.checker, err = e.repo.Checker()
	return err
}

// ---- candidates (JS20) ----

type hit struct {
	id, key string
	updated time.Time
	prop    entity.Id // the git-work property
	mapped  bool      // of an issue type the schema maps
}

// local is one scan of the excerpts.
type local struct {
	linked   []entity.Id // jira-id set
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
	if _, ok := e.m.IssueType(typ); !ok {
		return "type " + typ + " is local-only"
	}
	if isArchived(fields) {
		return "archived issues are not exported"
	}
	return ""
}

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
		if err != nil {
			return err
		}
	}
	return nil
}

// location is the zone JQL literals are read in (JS20).
func (e *engine) location() (*time.Location, error) {
	loc, err := e.p.Me.Location()
	if err != nil || e.p.Me.TimeZone == "" {
		return nil, fatal{fmt.Errorf("jira: the account's time zone %q does not load; JQL dates would be misread (JS20)", e.p.Me.TimeZone)}
	}
	return loc, nil
}

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
		if err := e.linkRequest(ic); err != nil {
			return err
		}
	}

	jql := "project = " + jiraapi.JQLQuote(e.p.Key)
	if !e.opts.Full {
		if lb, ok := e.lowerBound(l); ok {
			jql += " AND updated >= " + jiraapi.JQLTime(lb, loc)
		}
	}
	jql += " ORDER BY updated ASC, id ASC"
	hits, err := e.search(jql)
	if err != nil {
		return err
	}
	if err := e.runHits(hits, l); err != nil {
		return err
	}

	for _, id := range l.linked {
		if e.done[id] {
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		snap := ic.Snapshot()
		b, _ := CurrentBase(snap)
		if len(b.Retry) == 0 && !e.changed(ic, snap, b) {
			continue
		}
		if b.Gone != "" && !e.opts.Full {
			e.report(Line{Issue: id, Jira: b.Key, Action: ActionSkipped,
				Pending: []Skip{{Key: "*", Reason: "the Jira issue is gone (" + b.Gone + "); local edits are not exported"}}})
			continue
		}
		if b.Gone != "" && e.opts.Full {
			continue // the Gone pass below decides
		}
		if err := e.syncLinked(ic); err != nil {
			return err
		}
	}

	// a journal entry of an issue no longer to create is stale
	for id := range e.st.Creating {
		if ex, ok := l.byId[id]; !ok || ex.CreateMetadata[MetaId] != "" {
			delete(e.st.Creating, id)
		}
	}
	for _, id := range l.creates {
		if e.done[id] {
			continue
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		if err := e.create(ic); err != nil {
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
				ri := &page.Issues[i]
				h := hit{id: ri.ID, key: ri.Key}
				var u jiraapi.Time
				if _, err := ri.Decode("updated", &u); err == nil {
					h.updated = u.UTC()
				}
				var it jiraapi.IssueType
				if _, err := ri.Decode("issuetype", &it); err == nil {
					_, h.mapped = e.m.LocalType(it.ID)
				}
				h.prop = property(ri)
				hits = append(hits, h)
			}
			return nil
		})
	if err != nil {
		return nil, fatal{fmt.Errorf("jira: search: %w", err)}
	}
	return hits, nil
}

func property(ri *jiraapi.Issue) entity.Id {
	var v struct {
		Id entity.Id `json:"id"`
	}
	if raw, ok := ri.Properties[PropertyKey]; ok {
		_ = json.Unmarshal(raw, &v)
	}
	return v.Id
}

// runHits handles the search hits in order, and moves the cursor to the
// first one not synced, else past the last (JS20).
func (e *engine) runHits(hits []hit, l *local) error {
	// JS15: of two Jira issues naming one entity, the lower id links
	owner := map[entity.Id]string{}
	for _, h := range hits {
		if h.prop == "" {
			continue
		}
		if cur, ok := owner[h.prop]; !ok || lessId(h.id, cur) {
			owner[h.prop] = h.id
		}
	}

	var firstBad time.Time
	for i, h := range hits {
		failed, err := e.runHit(h, owner, l)
		if err != nil {
			if firstBad.IsZero() {
				firstBad = h.updated
			}
			e.moveCursor(hits[:i], firstBad)
			return err
		}
		if failed && firstBad.IsZero() {
			firstBad = h.updated
		}
	}
	e.moveCursor(hits, firstBad)
	return nil
}

func (e *engine) moveCursor(seen []hit, firstBad time.Time) {
	if !firstBad.IsZero() {
		e.cursor = firstBad
		return
	}
	for _, h := range seen {
		if h.updated.After(e.cursor) {
			e.cursor = h.updated
		}
	}
}

func (e *engine) runHit(h hit, owner map[entity.Id]string, l *local) (failed bool, err error) {
	before := e.sum.Failed
	defer func() { failed = e.sum.Failed > before }()

	if id, ok := e.ix.Issue(h.id); ok {
		if e.done[id] {
			return false, nil
		}
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return false, err
		}
		snap := ic.Snapshot()
		b, _ := CurrentBase(snap)
		// JS13 step 1: our own echo, or an overlap re-seeing a synced issue
		if !e.opts.Full && b != nil && h.updated.Equal(b.Updated) && len(b.Retry) == 0 && b.Gone == "" && !e.changed(ic, snap, b) {
			e.done[id] = true
			e.sum.Skipped++
			return false, nil
		}
		return false, e.syncLinked(ic)
	}

	if h.prop != "" {
		ex, ok := l.byId[h.prop]
		if !ok {
			// A2: another clone's export; importing it would duplicate the entity
			e.report(Line{Jira: h.key, Action: ActionSkipped,
				Pending: []Skip{{Key: "*", Reason: "created in Jira from issue " + h.prop.Human() + ", which this clone has not pulled; pull first"}}})
			return false, nil
		}
		{
			if owner[h.prop] != h.id || ex.CreateMetadata[MetaId] != "" {
				e.report(Line{Issue: h.prop, Jira: h.key, Action: ActionSkipped,
					Pending: []Skip{{Key: "*", Reason: "a second Jira issue names this issue; only the lower id is linked"}}})
				return false, nil
			}
			ic, err := e.repo.Issues().Resolve(h.prop)
			if err != nil {
				return false, err
			}
			return false, e.linkCreated(ic, h.id)
		}
	}
	if !h.mapped {
		// after Derive, an unmapped type is one the schema excludes: silent
		e.sum.Skipped++
		return false, nil
	}
	return false, e.importIssue(h.id)
}

func lessId(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// changed reports whether the issue differs from its base locally; an issue
// whose edit lamport is the one recorded after its last sync is not read.
func (e *engine) changed(ic *cache.IssueCache, snap *issue.Snapshot, b *Base) bool {
	if seen, ok := e.st.Seen[ic.Id()]; ok && seen == ic.EditLamportTime() {
		return false
	}
	typ, _ := issue.String(snap.Fields[typeKey])
	l := e.m.Local(snap, typ)
	ch := localDiffers(l, b, func(k string) bool { return e.m.Multi(typ, k) })
	if !ch {
		e.st.Seen[ic.Id()] = ic.EditLamportTime()
	}
	return ch
}

func localDiffers(l Doc, b *Base, multi func(string) bool) bool {
	for k, v := range l.Fields {
		bv, ok := b.Fields[k]
		switch {
		case multi(k):
			if len(itemSet(v)) != len(itemSet(bv)) {
				return true
			}
			bs := itemSet(bv)
			for it := range itemSet(v) {
				if _, ok := bs[it]; !ok {
					return true
				}
			}
		case !ok:
			if !issue.IsNull(canon(v)) {
				return true
			}
		case !same(v, bv):
			return true
		}
	}
	if Digest(l.Body.Text) != b.Body {
		return true
	}
	for _, c := range l.Comments {
		if c.Note {
			continue
		}
		bd, ok := b.Comments[c.JiraId]
		switch {
		case c.JiraId == "" || !ok:
			return true
		case bd == "":
			if !IsTombstone(c.Text.Text) {
				return true
			}
		case Digest(c.Text.Text) != bd:
			return true
		}
	}
	return false
}

// ---- one issue (JS13) ----

// read is step 2: the database copy and its comments, with identities ensured.
func (e *engine) read(idOrKey string) (*jiraapi.Issue, []jiraapi.Comment, error) {
	ri, err := e.c.GetIssue(e.ctx, idOrKey, e.m.Request(), nil)
	if err != nil {
		return nil, nil, err
	}
	cs, err := e.c.Comments(e.ctx, ri.ID)
	if err != nil {
		return nil, nil, err
	}
	e.ensureUsers(e.m.Users(ri, cs))
	return ri, cs, nil
}

// ensureUsers is JS16, each identity its own commit, never inside Update.
func (e *engine) ensureUsers(us []jiraapi.User) {
	for _, u := range us {
		if u.AccountID == "" {
			continue
		}
		if _, ok := e.ix.User(u.AccountID); ok || e.opts.DryRun {
			continue
		}
		login := ""
		if u.DisplayName == "" {
			login = u.AccountID
		}
		created, err := e.repo.Identities().NewRaw(u.DisplayName, u.EmailAddress, login, "", nil,
			map[string]string{MetaAccountId: u.AccountID})
		if err == nil {
			e.ix.AddUser(u.AccountID, created.Id())
		}
	}
}

func (e *engine) author(accountId string) identity.Interface {
	if accountId == "" {
		return e.me
	}
	if a, ok := e.authors[accountId]; ok {
		return a
	}
	var a identity.Interface = e.me
	if id, ok := e.ix.User(accountId); ok {
		if ic, err := e.repo.Identities().Resolve(id); err == nil {
			a = ic
		}
	}
	e.authors[accountId] = a
	return a
}

// remote is FromJira with the comments this run posted paired to their ops,
// and our own earlier exports paired by author and digest: the client reads
// no comment properties, so a comment posted before a crash is recognised by
// being the token's, with the text of an unpaired local comment (JS12).
func (e *engine) remote(ri *jiraapi.Issue, cs []jiraapi.Comment, snap *issue.Snapshot, local Doc, b *Base, pairs map[string]entity.Id) Doc {
	r := e.m.FromJira(ri, cs, e.ix)
	used := map[entity.Id]bool{}
	for i := range r.Comments {
		if op, ok := pairs[r.Comments[i].JiraId]; ok {
			r.Comments[i].Op, used[op] = op, true
		}
	}
	unpaired := map[string][]entity.Id{}
	for _, lc := range local.Comments {
		if lc.JiraId == "" && !lc.Note && !used[lc.Op] {
			d := Digest(lc.Text.Text)
			unpaired[d] = append(unpaired[d], lc.Op)
		}
	}
	for i := range r.Comments {
		rc := &r.Comments[i]
		if rc.Op != "" || rc.Author != e.p.Me.AccountID {
			continue
		}
		if b != nil {
			if _, known := b.Comments[rc.JiraId]; known {
				continue
			}
		}
		d := Digest(rc.Text.Text)
		if ops := unpaired[d]; len(ops) > 0 {
			rc.Op, unpaired[d] = ops[0], ops[1:]
		}
	}
	return r
}

func (e *engine) multi(typ string) func(string) bool {
	return func(k string) bool { return e.m.Multi(typ, k) }
}

// syncLinked syncs an issue whose create op carries jira-id.
func (e *engine) syncLinked(ic *cache.IssueCache) error {
	id := ic.Id()
	e.done[id] = true
	snap := ic.Snapshot()
	b, problems := CurrentBase(snap)
	line := Line{Issue: id, Jira: b.Key, Action: ActionUpdated}
	for _, p := range problems {
		line.Pending = append(line.Pending, Skip{Key: MetaSync, Reason: p})
	}
	ri, cs, err := e.read(b.Id)
	if err != nil {
		return e.fail(line, err)
	}
	if !strings.HasPrefix(ri.Key, e.p.Key+"-") {
		// moved out of the project: Gone, but only under --full (JS19)
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: "*", Reason: "moved to " + ri.Key + "; --full marks it gone"})
		e.report(line)
		return nil
	}
	line.Jira = ri.Key
	return e.converge(ic, b, ri, cs, nil, line)
}

func (e *engine) fail(line Line, err error) error {
	if runFatal(err) {
		line.Action, line.Error = ActionFailed, err.Error()
		e.report(line)
		return err
	}
	line.Action, line.Error = ActionFailed, err.Error()
	e.report(line)
	return nil
}

// converge is JS13 steps 3–6 for an issue that exists on both sides.
// meta is create-op metadata step 6 adds (a new link); b nil is no base.
func (e *engine) converge(ic *cache.IssueCache, b *Base, ri *jiraapi.Issue, cs []jiraapi.Comment, meta map[string]string, line Line) error {
	snap := ic.Snapshot()
	typ0, _ := issue.String(snap.Fields[typeKey])
	l0 := e.m.Local(snap, typ0)
	remote := e.remote(ri, cs, snap, l0, b, nil)
	local := e.m.Local(snap, remote.Type)
	multi := e.multi(remote.Type)

	plan1 := Merge(b, local, remote, multi, true)
	if e.opts.DryRun {
		line.DryRun = true
		e.describe(&line, plan1.Local, remote)
		for _, ch := range plan1.Remote {
			line.exported(ch.Key, changeValue(ch, local.Fields[ch.Key]))
		}
		e.countWrites(&line, plan1.Comments)
		line.Conflicts, line.Pending = plan1.Conflicts, append(line.Pending, plan1.Pending...)
		e.report(line)
		return nil
	}

	b2 := &Base{V: baseVersion, Fields: map[string]issue.Value{}}
	if b != nil {
		b2 = b.clone()
	}
	pairs, wrote, err := e.write(ri, remote, local, plan1, b2, &line)
	if err != nil {
		return e.fail(line, err)
	}
	ri2, cs2 := ri, cs
	var unconfirmed []string
	if wrote {
		if ri2, cs2, err = e.read(ri.ID); err != nil {
			return e.fail(line, err)
		}
		unconfirmed = e.unconfirmed(plan1.Remote, b2, remote, e.m.FromJira(ri2, cs2, e.ix))
	}
	return e.commit(ic, b2, ri2, cs2, pairs, meta, unconfirmed, line)
}

// unconfirmed are the keys written this run that the re-read still shows
// with their pre-write value: a stale read, not a Jira edit, so plan₂ must
// not import it over local. They are left alone and retried (I2).
func (e *engine) unconfirmed(written []Change, b2 *Base, r, r2 Doc) []string {
	var keys []string
	for _, ch := range written {
		switch {
		case ch.Set == nil: // a set's base waits for the second merge anyway
		case ch.Key == BodyKey:
			text, _ := issue.String(ch.Set)
			if b2.Body == Digest(text) && Digest(r2.Body.Text) == Digest(r.Body.Text) && Digest(r.Body.Text) != b2.Body {
				keys = append(keys, BodyKey)
			}
		default:
			if v, ok := b2.Fields[ch.Key]; ok && same(v, ch.Set) && same(r2.Fields[ch.Key], r.Fields[ch.Key]) && !same(r.Fields[ch.Key], v) {
				keys = append(keys, ch.Key)
			}
		}
	}
	return keys
}

// commit is step 6: the second merge, decided under the lock on the fresh
// issue, with nothing exported; its local changes, note, pairings and marker
// are one commit.
func (e *engine) commit(ic *cache.IssueCache, b2 *Base, ri *jiraapi.Issue, cs []jiraapi.Comment,
	pairs map[string]entity.Id, meta map[string]string, unconfirmed []string, line Line) error {
	var decided Line
	err := ic.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		decided = line
		typ0, _ := issue.String(snap.Fields[typeKey])
		r := e.remote(ri, cs, snap, e.m.Local(snap, typ0), b2, pairs)
		for _, k := range unconfirmed {
			r.Skip = append(r.Skip, Skip{Key: k, Reason: "Jira does not show this run's write yet", Retry: true})
		}
		local := e.m.Local(snap, r.Type)
		plan := Merge(b2, local, r, e.multi(r.Type), false)
		ops := e.ops(snap, &plan, b2, r, &decided)
		if len(meta) > 0 {
			ops = append(ops, issue.NewSetMetadataOp(e.me, time.Now().Unix(), snap.Operations[0].Id(), meta))
		}
		cur, _ := CurrentBase(snap)
		if !plan.Base.Equal(cur) {
			ops = append(ops, issue.NewNoOpOp(e.me, time.Now().Unix(), map[string]string{MetaSync: plan.Base.Marshal()}))
		}
		decided.Conflicts = append(decided.Conflicts, plan.Conflicts...)
		// a key step 4 already reported pending is not reported twice
		known := map[string]bool{}
		for _, s := range decided.Pending {
			known[s.Key] = true
		}
		for _, s := range plan.Pending {
			if !known[s.Key] {
				decided.Pending = append(decided.Pending, s)
			}
		}
		if len(plan.Base.Retry) > 0 {
			e.retry = append(e.retry, ic.Id())
		}
		return ops, nil
	})
	if err != nil {
		return e.fail(line, err)
	}
	// Seen marks a converged issue only: a pending one stays a candidate (I1)
	if len(decided.Pending) == 0 {
		e.st.Seen[ic.Id()] = ic.EditLamportTime()
	} else {
		delete(e.st.Seen, ic.Id())
	}
	delete(e.st.Creating, ic.Id())
	e.report(decided)
	return nil
}

// ops turns plan's local changes into operations, pre-checking each field
// write; a refused one keeps its old base and is retried (JS13 step 6).
func (e *engine) ops(snap *issue.Snapshot, plan *Plan, prior *Base, remote Doc, line *Line) []issue.Operation {
	now := time.Now()
	unix := func(t time.Time) int64 {
		if t.IsZero() {
			return now.Unix()
		}
		return t.Unix()
	}
	typ, _ := issue.String(snap.Fields[typeKey])
	for _, lc := range plan.Local {
		if lc.Kind == LocalSet && lc.Key == typeKey {
			typ, _ = issue.String(lc.Value)
		}
	}
	refuse := func(key string, err error) {
		if bv, ok := prior.Fields[key]; ok {
			plan.Base.Fields[key] = bv
		} else {
			delete(plan.Base.Fields, key)
		}
		plan.Base.Retry = appendOnce(plan.Base.Retry, key)
		line.Pending = append(line.Pending, Skip{Key: key, Reason: err.Error(), Retry: true})
	}
	refused := map[string]bool{}
	for _, lc := range plan.Local {
		switch lc.Kind {
		case LocalSet:
			if err := e.checker.CheckFields(typ, map[string]json.RawMessage{lc.Key: lc.Value}); err != nil {
				refuse(lc.Key, err)
				refused[lc.Key] = true
			}
		case LocalAdd, LocalRemove:
			if err := e.checker.CheckItems(typ, map[string][]json.RawMessage{lc.Key: {lc.Value}}); err != nil {
				refuse(lc.Key, err)
				refused[lc.Key] = true
			}
		}
	}

	var ops []issue.Operation
	for _, lc := range plan.Local {
		if refused[lc.Key] && lc.Kind <= LocalRemove {
			continue
		}
		a, t := e.author(lc.Author), unix(lc.At)
		switch lc.Kind {
		case LocalSet:
			ops = append(ops, issue.NewSetFieldOp(a, t, lc.Key, lc.Value))
			line.imported(lc.Key, lc.Value)
		case LocalAdd:
			ops = append(ops, issue.NewAddValueOp(a, t, lc.Key, lc.Value))
			line.imported(lc.Key, remote.Fields[lc.Key])
		case LocalRemove:
			ops = append(ops, issue.NewRemoveValueOp(a, t, lc.Key, lc.Value))
			line.imported(lc.Key, remote.Fields[lc.Key])
		case LocalEditBody:
			ops = append(ops, issue.NewEditCommentOp(a, t, snap.Operations[0].Id(), lc.Text, nil))
			line.imported(BodyKey, issue.StringValue(Digest(lc.Text)))
		case LocalAddComment:
			op := issue.NewAddCommentOp(a, t, lc.Text, nil)
			op.SetMetadata(MetaCommentId, lc.JiraId)
			ops = append(ops, op)
			line.comments().Imported++
		case LocalEditComment:
			ops = append(ops, issue.NewEditCommentOp(a, unix(lc.At), lc.Op, lc.Text, nil))
			if IsTombstone(lc.Text) && lc.At.IsZero() {
				line.comments().Tombstoned++
			} else {
				line.comments().Edited++
			}
		case LocalPairComment:
			ops = append(ops, issue.NewSetMetadataOp(e.me, now.Unix(), lc.Op, map[string]string{MetaCommentId: lc.JiraId}))
		}
	}
	if len(plan.Conflicts) > 0 {
		op := issue.NewAddCommentOp(e.me, now.Unix(), conflictNote(now, plan.Conflicts), nil)
		op.SetMetadata(MetaNote, NoteConflict)
		ops = append(ops, op)
	}
	return ops
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// conflictNote is JS21's note, one per issue per run.
func conflictNote(now time.Time, cs []Conflict) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Jira sync, %s: Jira's values replaced local edits.\n", now.UTC().Format("2006-01-02 15:04 MST"))
	for _, c := range cs {
		switch c.Key {
		case BodyKey:
			sb.WriteString("- body: the local description edit is kept in its history\n")
		case CommentKey:
			fmt.Fprintf(&sb, "- comment %s: the local edit is kept in its history\n", c.Comment)
		default:
			fmt.Fprintf(&sb, "- %s: local %s -> Jira %s\n", c.Key, c.Local, c.Jira)
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ---- step 4: the Jira writes ----

// write performs plan's remote changes, each independently, and records in
// b2 each scalar key written (I2: the second merge then imports Jira's normal
// form). A multi key's base stays: the second merge against it reaches the
// merged set on both sides. pairs are the comments it created, by Jira id.
func (e *engine) write(ri *jiraapi.Issue, remote, local Doc, plan Plan, b2 *Base, line *Line) (map[string]entity.Id, bool, error) {
	// the description is a system field of every type, so the engine writes it
	var fields []Change
	var writes []Write
	for _, ch := range plan.Remote {
		if ch.Key == BodyKey {
			text, _ := issue.String(ch.Set)
			writes = append(writes, Write{Key: BodyKey, Kind: WriteEdit, Field: "description", Set: jiraapi.TextToADF(text)})
		} else {
			fields = append(fields, ch)
		}
	}
	more, skips := e.m.ToJira(remote.Type, fields, ri, e.ix)
	writes = append(writes, more...)
	line.Pending = append(line.Pending, skips...)
	failed := map[string]bool{}
	for _, s := range skips {
		failed[s.Key] = true
	}
	pend := func(key, reason string) {
		failed[key] = true
		line.Pending = append(line.Pending, Skip{Key: key, Reason: reason})
	}
	wrote := false
	attempted := map[string]bool{}

	// one PUT with every edit; per-field errors retried once without them
	var edits []Write
	for _, w := range writes {
		attempted[w.Key] = true
		if w.Kind == WriteEdit {
			edits = append(edits, w)
		}
	}
	for try := 0; len(edits) > 0 && try < 2; try++ {
		fields, update := map[string]any{}, map[string][]jiraapi.Op{}
		for _, w := range edits {
			if w.Update != nil {
				update[w.Field] = append(update[w.Field], decodeOps(w.Update)...)
			} else {
				fields[w.Field] = w.Set
			}
		}
		err := e.c.EditIssue(e.ctx, ri.ID, fields, update)
		if err == nil {
			wrote = true
			break
		}
		if runFatal(err) {
			return nil, wrote, err
		}
		var apiErr *jiraapi.Error
		if try == 0 && errors.As(err, &apiErr) && apiErr.StatusCode == 400 && len(apiErr.Fields) > 0 {
			var rest []Write
			for _, w := range edits {
				if msg, bad := apiErr.Fields[w.Field]; bad {
					pend(w.Key, "Jira refused "+w.Field+": "+msg)
				} else {
					rest = append(rest, w)
				}
			}
			edits = rest
			continue
		}
		for _, w := range edits {
			pend(w.Key, err.Error())
		}
		break
	}

	for _, w := range writes {
		switch w.Kind {
		case WriteTransition:
			reason, err := e.transition(ri, w.Status)
			if err != nil {
				if runFatal(err) {
					return nil, wrote, err
				}
				reason = err.Error()
			}
			if reason != "" {
				pend(w.Key, reason)
			} else {
				wrote = true
			}
		case WriteLink:
			for _, l := range w.Add {
				if err := e.c.CreateIssueLink(e.ctx, l.LinkType, l.Source, l.Destination); err != nil {
					if runFatal(err) {
						return nil, wrote, err
					}
					pend(w.Key, err.Error())
				} else {
					wrote = true
				}
			}
			for _, id := range w.Remove {
				if err := e.c.DeleteIssueLink(e.ctx, id); err != nil {
					if runFatal(err) {
						return nil, wrote, err
					}
					pend(w.Key, err.Error())
				} else {
					wrote = true
				}
			}
		}
	}

	for _, ch := range plan.Remote {
		if failed[ch.Key] || !attempted[ch.Key] {
			if !failed[ch.Key] {
				line.Pending = append(line.Pending, Skip{Key: ch.Key, Reason: "no Jira field to write it to"})
			}
			continue
		}
		switch {
		case ch.Key == BodyKey:
			text, _ := issue.String(ch.Set)
			b2.Body = Digest(text)
			line.exported(BodyKey, issue.StringValue(b2.Body))
		case ch.Set != nil:
			b2.Fields[ch.Key] = canon(ch.Set)
			line.exported(ch.Key, ch.Set)
		default:
			line.exported(ch.Key, changeValue(ch, remote.Fields[ch.Key]))
		}
	}

	pairs := map[string]entity.Id{}
	for _, cw := range plan.Comments {
		adf := jiraapi.TextToADF(cw.Text)
		if cw.JiraId == "" {
			c, err := e.c.AddComment(e.ctx, ri.ID, adf,
				jiraapi.Property{Key: PropertyKey, Value: map[string]string{"op": cw.Op.String()}})
			if err != nil {
				if runFatal(err) {
					return nil, wrote, err
				}
				pend(CommentKey, err.Error())
				continue
			}
			wrote = true
			pairs[c.ID] = cw.Op
			line.comments().Exported++
			continue
		}
		if _, err := e.c.UpdateComment(e.ctx, ri.ID, cw.JiraId, adf); err != nil {
			if runFatal(err) {
				return nil, wrote, err
			}
			pend(CommentKey+":"+cw.JiraId, err.Error())
			continue
		}
		wrote = true
		if b2.Comments == nil {
			b2.Comments = map[string]string{}
		}
		b2.Comments[cw.JiraId] = Digest(cw.Text)
		line.comments().Edited++
	}
	return pairs, wrote, nil
}

// decodeOps reads an update.<field> array such as [{"add":"x"}].
func decodeOps(raw json.RawMessage) []jiraapi.Op {
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		var one map[string]json.RawMessage
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		list = []map[string]json.RawMessage{one}
	}
	var ops []jiraapi.Op
	for _, m := range list {
		verbs := make([]string, 0, len(m))
		for v := range m {
			verbs = append(verbs, v)
		}
		sort.Strings(verbs)
		for _, v := range verbs {
			ops = append(ops, jiraapi.Op{Verb: v, Value: m[v]})
		}
	}
	return ops
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
			from := ""
			if sf, err := ri.System(); err == nil && sf.Status != nil {
				from = sf.Status.Name
			}
			return fmt.Sprintf("no transition from %s to %s", orId(from, "the current status"), e.statusName(statusId)), nil
		}
		fields := map[string]any{}
		for id, f := range t.Fields {
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

// ---- creates, links and imports (JS15) ----

// create exports a local issue with no jira-id (JS15). A journal entry
// says an earlier POST may have landed: the issue is looked for by its
// property first, and created again only once the entry has settled.
func (e *engine) create(ic *cache.IssueCache) error {
	id := ic.Id()
	e.done[id] = true
	snap := ic.Snapshot()
	typ, _ := issue.String(snap.Fields[typeKey])
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
	local := e.m.Local(snap, typ)
	body, carried, skips := e.m.Create(local, id, e.ix)
	line.Pending = append(line.Pending, skips...)
	if e.opts.DryRun {
		line.DryRun = true
		for _, k := range carried {
			line.exported(k, local.Fields[k])
		}
		e.report(line)
		return nil
	}
	props := body.Properties
	if !hasProperty(props, PropertyKey) {
		props = append(props, jiraapi.Property{Key: PropertyKey, Value: map[string]string{"id": id.String()}})
	}
	e.st.Creating[id] = e.now()
	if err := e.persist(); err != nil {
		return err
	}
	ref, err := e.c.CreateIssue(e.ctx, body.Fields, props)
	// JS13 step 4, as for a PUT: refused fields are dropped once, and pending
	var apiErr *jiraapi.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 && len(apiErr.Fields) > 0 {
		for f, msg := range apiErr.Fields {
			if _, sent := body.Fields[f]; sent && f != "summary" && f != "issuetype" && f != "project" {
				delete(body.Fields, f)
				line.Pending = append(line.Pending, Skip{Key: f, Reason: "Jira refused " + f + ": " + msg})
			}
		}
		// nothing but the built-ins counts as carried: every other key set
		// locally is then written by the ordinary merge, and a refusal there
		// is pending under its own key
		carried = nil
		ref, err = e.c.CreateIssue(e.ctx, body.Fields, props)
	}
	if err != nil {
		if refused(err) {
			delete(e.st.Creating, id)
		}
		return e.fail(line, err)
	}
	e.ix.AddIssue(ref.ID, id)
	e.grew = true
	line.Jira = ref.Key

	ri, cs, err := e.read(ref.ID)
	if err != nil {
		return e.fail(line, err)
	}
	remote := e.m.FromJira(ri, cs, e.ix)
	b := createBase(local, remote, carried)
	b.Id, b.Key = ri.ID, ri.Key
	for _, k := range carried {
		line.exported(k, local.Fields[k])
	}
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
// per run answers every entry (JS15).
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
		e.created = map[entity.Id]string{}
		for _, h := range hits {
			if cur, ok := e.created[h.prop]; h.prop != "" && (!ok || cmpId(h.id, cur) < 0) {
				e.created[h.prop] = h.id
			}
		}
	}
	return e.created[id], nil
}

func hasProperty(ps []jiraapi.Property, key string) bool {
	for _, p := range ps {
		if p.Key == key {
			return true
		}
	}
	return false
}

// createBase is JS15's: a carried key's base is local (so Jira's normal form
// imports); another is Jira's when local holds a value (so it is written
// now) and local's when not (so a Jira default imports).
func createBase(local, remote Doc, carried []string) *Base {
	b := &Base{V: baseVersion, Fields: map[string]issue.Value{}}
	// the description always goes with the POST
	c := map[string]bool{issue.TitleKey: true, typeKey: true, BodyKey: true}
	for _, k := range carried {
		c[k] = true
	}
	for k, rv := range remote.Fields {
		lv := local.Fields[k]
		switch {
		case c[k] || issue.IsNull(canon(lv)):
			b.Fields[k] = canon(lv)
		default:
			b.Fields[k] = canon(rv)
		}
	}
	b.Body = Digest(local.Body.Text)
	return b
}

// linkCreated links an issue whose POST landed and whose commit did not,
// found by its property (JS15): it resumes as a create.
func (e *engine) linkCreated(ic *cache.IssueCache, jiraId string) error {
	id := ic.Id()
	e.done[id] = true
	line := Line{Issue: id, Action: ActionCreated}
	ri, cs, err := e.read(jiraId)
	if err != nil {
		return e.fail(line, err)
	}
	line.Jira = ri.Key
	e.ix.AddIssue(ri.ID, id)
	snap := ic.Snapshot()
	typ, _ := issue.String(snap.Fields[typeKey])
	local := e.m.Local(snap, typ)
	_, carried, _ := e.m.Create(local, id, e.ix)
	b := createBase(local, e.m.FromJira(ri, cs, e.ix), carried)
	b.Id, b.Key = ri.ID, ri.Key
	return e.converge(ic, b, ri, cs, map[string]string{MetaId: ri.ID, MetaAlias: ri.Key}, line)
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
	if !strings.HasPrefix(ri.Key, e.p.Key+"-") {
		return skip("alias " + key + " names an issue of another project")
	}
	if other, ok := e.ix.Issue(ri.ID); ok && other != id {
		return skip(key + " is already linked to " + other.Human())
	}
	if raw, err := e.c.GetProperty(e.ctx, ri.ID, PropertyKey); err == nil {
		var v struct {
			Id entity.Id `json:"id"`
		}
		if json.Unmarshal(raw, &v) == nil && v.Id != "" && v.Id != id {
			return skip(key + " was created from another issue, " + v.Id.Human())
		}
	}
	e.ix.AddIssue(ri.ID, id)
	b := &Base{V: baseVersion, Id: ri.ID, Key: ri.Key, Fields: map[string]issue.Value{}}
	return e.converge(ic, b, ri, cs, map[string]string{MetaId: ri.ID}, line)
}

// importIssue creates a Jira issue locally: NewRaw by its reporter at its
// created, the create op the first marker; comments follow in step 6.
func (e *engine) importIssue(jiraId string) error {
	line := Line{Action: ActionImported}
	ri, cs, err := e.read(jiraId)
	if err != nil {
		return e.fail(line, err)
	}
	line.Jira = ri.Key
	remote := e.m.FromJira(ri, cs, e.ix)
	if remote.Type == "" {
		// after Derive, an unmapped type is one the schema excludes: silent
		e.sum.Skipped++
		return nil
	}
	sf, _ := ri.System()
	b := &Base{V: baseVersion, Id: ri.ID, Key: ri.Key, Updated: remote.Updated.UTC(), Fields: map[string]issue.Value{}}
	fields := map[string]issue.Value{}
	for _, s := range remote.Skip {
		line.Pending = append(line.Pending, s)
		if s.Retry {
			b.Retry = appendOnce(b.Retry, s.Key)
		}
	}
	keys := make([]string, 0, len(remote.Fields))
	for k := range remote.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := canon(remote.Fields[k])
		if k == issue.TitleKey {
			b.Fields[k] = v
			continue
		}
		if issue.IsNull(v) {
			b.Fields[k] = v
			continue
		}
		if k != typeKey {
			if err := e.checker.CheckFields(remote.Type, map[string]json.RawMessage{k: v}); err != nil {
				line.Pending = append(line.Pending, Skip{Key: k, Reason: err.Error(), Retry: true})
				b.Retry = appendOnce(b.Retry, k)
				continue
			}
		}
		fields[k] = v
		b.Fields[k] = v
		line.imported(k, v)
	}
	b.Body = Digest(remote.Body.Text)
	if remote.Body.Text != "" {
		line.imported(BodyKey, issue.StringValue(b.Body))
	}
	if e.opts.DryRun {
		line.DryRun = true
		line.comments().Imported = len(remote.Comments)
		e.report(line)
		return nil
	}
	title, _ := issue.String(remote.Fields[issue.TitleKey])
	author, created := e.me, time.Now()
	var a identity.Interface = author
	if sf.Reporter != nil {
		a = e.author(sf.Reporter.AccountID)
	}
	if !sf.Created.IsZero() {
		created = sf.Created.Time
	}
	ic, _, err := e.repo.Issues().NewRaw(a, created.Unix(), title, remote.Body.Text, nil, fields,
		map[string]string{MetaId: ri.ID, MetaAlias: ri.Key, MetaSync: b.Marshal()})
	if err != nil {
		return e.fail(line, err)
	}
	e.ix.AddIssue(ri.ID, ic.Id())
	e.grew = true
	e.done[ic.Id()] = true
	line.Issue = ic.Id()
	if len(b.Retry) > 0 {
		e.retry = append(e.retry, ic.Id())
	}
	if len(cs) == 0 {
		e.st.Seen[ic.Id()] = ic.EditLamportTime()
		e.report(line)
		return nil
	}
	b, _ = CurrentBase(ic.Snapshot())
	return e.commit(ic, b, ri, cs, nil, nil, nil, line)
}

// repass re-runs, once, the issues left with Retry keys, when the run added
// issues they might have been waiting for (JS17).
func (e *engine) repass() error {
	if !e.grew || e.opts.DryRun {
		return nil
	}
	ids := e.retry
	e.retry = nil
	seen := map[entity.Id]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		ic, err := e.repo.Issues().Resolve(id)
		if err != nil {
			return err
		}
		if err := e.syncLinked(ic); err != nil {
			return err
		}
	}
	return nil
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
		if present[jid] {
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
			if err := e.fail(Line{Issue: id, Jira: b.Key}, err); err != nil {
				return err
			}
		case !strings.HasPrefix(ri.Key, e.p.Key+"-"):
			found = append(found, gone{ic, b, GoneMoved, ri.Key})
		default:
			if !e.done[id] { // the index lagged: an ordinary sync
				if err := e.syncLinked(ic); err != nil {
					return err
				}
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
		if err := e.markGone(g.ic, g.b, g.kind, g.key); err != nil {
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
		now := time.Now()
		typ, _ := issue.String(snap.Fields[typeKey])
		nb := b.clone()
		nb.Gone = kind
		var ops []issue.Operation
		if v, ok := e.m.Canceled(typ); ok {
			nb.Fields["status"] = canon(v)
			if !same(snap.Fields["status"], v) {
				ops = append(ops, issue.NewSetFieldOp(e.me, now.Unix(), "status", v))
				line.imported("status", v)
			}
		}
		what := "was deleted in Jira, or is hidden from the sync's account"
		if kind == GoneMoved {
			what = "moved to another project, as " + key
		}
		note := issue.NewAddCommentOp(e.me, now.Unix(),
			fmt.Sprintf("Jira sync, %s: the Jira issue %s %s; it is no longer synced.", now.UTC().Format("2006-01-02 15:04 MST"), b.Key, what), nil)
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

// ---- the report ----

func (e *engine) report(l Line) {
	if l.Action == ActionUpdated && !l.touched() {
		e.sum.Skipped++
		return
	}
	e.sum.count(l)
	e.emit(l)
}

func (e *engine) describe(l *Line, lcs []LocalChange, remote Doc) {
	for _, lc := range lcs {
		switch lc.Kind {
		case LocalSet:
			l.imported(lc.Key, lc.Value)
		case LocalAdd, LocalRemove:
			l.imported(lc.Key, remote.Fields[lc.Key])
		case LocalEditBody:
			l.imported(BodyKey, issue.StringValue(Digest(lc.Text)))
		case LocalAddComment:
			l.comments().Imported++
		case LocalEditComment:
			l.comments().Edited++
		}
	}
}

func (e *engine) countWrites(l *Line, cws []CommentWrite) {
	for _, cw := range cws {
		if cw.JiraId == "" {
			l.comments().Exported++
		} else {
			l.comments().Edited++
		}
	}
}

// changeValue is what a multi key holds once a Change applies to cur.
func changeValue(ch Change, cur issue.Value) issue.Value {
	if ch.Set != nil {
		return ch.Set
	}
	s := itemSet(cur)
	for _, it := range ch.Add {
		s[string(canon(it))] = canon(it)
	}
	for _, it := range ch.Remove {
		delete(s, string(canon(it)))
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]issue.Value, len(keys))
	for i, k := range keys {
		items[i] = s[k]
	}
	return issue.ItemsValue(items)
}

func (l *Line) imported(key string, v issue.Value) {
	if l.Imported == nil {
		l.Imported = map[string]json.RawMessage{}
	}
	l.Imported[key] = json.RawMessage(canon(v))
}

func (l *Line) exported(key string, v issue.Value) {
	if l.Exported == nil {
		l.Exported = map[string]json.RawMessage{}
	}
	l.Exported[key] = json.RawMessage(canon(v))
}

func (l *Line) comments() *CommentCounts {
	if l.Comments == nil {
		l.Comments = &CommentCounts{}
	}
	return l.Comments
}
