package jira

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
)

// ---- one issue (JS13) ----

// read is step 2: the database copy, its git-work property and its
// comments, with identities ensured.
func (e *engine) read(idOrKey string) (*jiraapi.Issue, []jiraapi.Comment, error) {
	ri, err := e.c.GetIssue(e.ctx, idOrKey, e.m.requestFields(), nil, PropertyKey)
	if err != nil {
		return nil, nil, err
	}
	cs, err := e.c.Comments(e.ctx, ri.ID)
	if err != nil {
		return nil, nil, err
	}
	e.ensureUsers(e.m.users(ri, cs))
	return ri, cs, nil
}

// ensureUsers is JS16, each identity its own commit, never inside Update;
// the Index holds every identity carrying an account.
func (e *engine) ensureUsers(us []jiraapi.User) {
	for _, u := range us {
		if _, ok := e.ix.User(u.AccountID); ok || u.AccountID == "" || e.opts.DryRun {
			continue
		}
		login := ""
		if u.DisplayName == "" {
			login = u.AccountID
		}
		created, err := e.repo.Identities().NewRaw(u.DisplayName, u.EmailAddress, login, "", nil,
			map[string]string{MetaAccountId: u.AccountID})
		if err == nil {
			e.ix.addUser(u.AccountID, created.Id())
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

func (e *engine) multi(typ string) func(string) bool {
	return func(k string) bool { return e.m.multi(typ, k) }
}

// syncLinked syncs an issue whose create op carries jira-id.
func (e *engine) syncLinked(ic *cache.IssueCache) error {
	id := ic.Id()
	e.done[id] = true
	b, problems := CurrentBase(ic.Snapshot())
	line := Line{Issue: id, Jira: b.Key, Action: ActionUpdated}
	for _, p := range problems {
		line.Pending = append(line.Pending, Skip{Key: MetaSync, Reason: p})
	}
	ri, cs, err := e.read(b.Id)
	gone := ""
	switch {
	case jiraapi.StatusCode(err) == 404 && b.young(e.now(), e.opts.Settle):
		line.Action = ActionSkipped
		line.Pending = append(line.Pending, Skip{Key: "*", Retry: true, Reason: "Jira does not show the issue created at " + b.Wrote.Format(time.RFC3339) + " yet"})
		e.report(line)
		return nil
	case jiraapi.StatusCode(err) == 404:
		gone = "not in Jira: deleted, or hidden from the sync's account"
	case err != nil:
		return e.fail(line, err)
	case !e.p.owns(ri.Key):
		gone = "moved to " + ri.Key
	}
	if gone != "" {
		// Gone, but only under --full, whose Gone pass then decides (JS19)
		e.missing[b.Id] = true
		if !e.opts.Full {
			line.Action = ActionSkipped
			line.Pending = append(line.Pending, Skip{Key: "*", Reason: gone + "; --full marks it gone"})
			e.report(line)
		}
		return nil
	}
	line.Jira = ri.Key
	return e.converge(ic, b, ri, cs, nil, nil, line)
}

// converge is JS13 steps 3–6 for an issue on both sides: b is the base (nil
// for none), meta the create-op metadata step 6 adds for a new link, echo
// the keys of b.Sent that R answers (a create's, read right after its
// POST), line what the caller has to report already.
func (e *engine) converge(ic *cache.IssueCache, b *Base, ri *jiraapi.Issue, cs []jiraapi.Comment, meta map[string]string, echo map[string]bool, line Line) error {
	remote := e.m.fromIssue(ri, cs, e.ix)
	local := e.m.Local(ic.Snapshot(), remote.Type)
	b, remote = e.settle(b, local, remote, echo)
	plan1 := merge(b, local, remote, e.multi(remote.Type), true)
	if e.opts.DryRun {
		line.DryRun = true
		line.record(plan1.Local, remote)
		line.exports(plan1, remote, nil)
		line.Conflicts, line.Pending = plan1.Conflicts, append(line.Pending, plan1.Pending...)
		e.report(line)
		return nil
	}

	b2 := &Base{V: baseVersion, Fields: map[string]issue.Value{}}
	if b != nil {
		b2 = b.clone()
	}
	pairs, wrote, err := e.write(ri, remote, plan1, b2, &line)
	if err != nil {
		return e.fail(line, err)
	}
	if !wrote {
		return e.commit(ic, b2, ri, cs, meta, nil, line)
	}
	// step 5: what Jira answered is committed even when the re-read fails,
	// against R, where every written key is still pending (I2)
	w := &written{pairs: pairs, echo: map[string]bool{}}
	for k := range b2.Sent {
		if b == nil || b.Sent[k] == nil {
			w.echo[k] = true
		}
	}
	ri2, cs2, err := e.read(ri.ID)
	if err != nil {
		ri2, cs2, w.readErr, w.echo = ri, cs, err, nil
	}
	return e.commit(ic, b2, ri2, cs2, meta, w, line)
}

// written is what step 4 leaves step 6: the comments it created, paired from
// their 201s; the keys it wrote, which R′ answers; a failed re-read.
type written struct {
	pairs   map[string]entity.Id
	echo    map[string]bool
	readErr error
}

// settle is the base a merge against remote starts from: a create's first
// base (fresh), and each key written before resolved by confirm, its Skips
// on remote.
func (e *engine) settle(b *Base, local, remote Doc, echo map[string]bool) (*Base, Doc) {
	if b == nil || !b.Fresh && len(b.Sent) == 0 {
		return b, remote
	}
	b = b.clone()
	if b.Fresh {
		b.fresh(local, remote)
	}
	remote.Skip = append(slices.Clone(remote.Skip), confirm(b, remote, e.multi(remote.Type), echo, e.now(), e.opts.Settle)...)
	return b, remote
}

// restore sets key's base in b2 back to prior's (none for a nil prior).
func restore(b2, prior *Base, key string) {
	if prior == nil {
		prior = &Base{}
	}
	if v, ok := prior.Fields[key]; ok {
		b2.Fields[key] = v
	} else {
		delete(b2.Fields, key)
	}
}

// commit is step 6: the second merge, decided under the lock on the fresh
// issue, with nothing exported; its local changes, note, pairings and marker
// are one commit. w is step 4's (nil when nothing was written): its
// comments are paired whether or not R′ lists them, and a failed re-read is
// reported once the rest is committed. line is step 4's; Update calls the
// closure exactly once.
func (e *engine) commit(ic *cache.IssueCache, b2 *Base, ri *jiraapi.Issue, cs []jiraapi.Comment,
	meta map[string]string, w *written, line Line) error {
	if w == nil {
		w = &written{}
	}
	decided := line
	var retry bool
	err := ic.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		r := e.m.fromIssue(ri, cs, e.ix)
		local := e.m.Local(snap, r.Type)
		var paired []localChange
		byOp := map[entity.Id]string{}
		for jid, op := range w.pairs {
			byOp[op] = jid
		}
		for i := range local.Comments {
			if c := &local.Comments[i]; c.JiraId == "" && byOp[c.Op] != "" {
				c.JiraId = byOp[c.Op]
				paired = append(paired, localChange{Kind: localPairComment, Op: c.Op, JiraId: c.JiraId})
			}
		}
		b, r := e.settle(b2, local, r, w.echo)
		plan := merge(b, local, r, e.multi(r.Type), false)
		plan.Local = append(plan.Local, paired...)
		typ, _ := issue.String(snap.Fields[typeKey])
		decided.Pending = append(decided.Pending, e.admit(&plan, typ, b)...)
		ops := e.ops(snap, plan, &decided, r)
		if len(meta) > 0 {
			ops = append(ops, issue.NewSetMetadataOp(e.me, e.now().Unix(), snap.Operations[0].Id(), meta))
		}
		if cur, _ := CurrentBase(snap); !plan.Base.equal(cur) {
			ops = append(ops, issue.NewNoOpOp(e.me, e.now().Unix(), map[string]string{MetaSync: plan.Base.marshal()}))
		}
		decided.Conflicts = append(decided.Conflicts, plan.Conflicts...)
		// a key step 4 already reported pending keeps step 4's reason
		known := map[string]bool{}
		for _, s := range line.Pending {
			known[s.Key] = true
		}
		for _, s := range plan.Pending {
			if !known[s.Key] {
				decided.Pending = append(decided.Pending, s)
			}
		}
		retry = len(plan.Base.Retry) > 0
		return ops, nil
	})
	if err != nil {
		return e.fail(line, err)
	}
	if w.readErr != nil {
		return e.fail(decided, w.readErr)
	}
	if retry {
		e.retry = append(e.retry, ic.Id())
	}
	// Seen marks a converged issue only: a pending one stays a candidate (I1)
	if len(decided.Pending) == 0 {
		e.st.Seen[ic.Id()] = ic.EditLamportTime()
	} else {
		delete(e.st.Seen, ic.Id())
	}
	e.report(decided)
	return nil
}

// admit drops from plan.Local each field change the schema refuses, against
// the type the plan leaves the issue with: the key keeps its prior base and
// is retried (JS13 step 6), so Update's own check never refuses the batch.
func (e *engine) admit(plan *mergePlan, typ string, prior *Base) []Skip {
	for _, lc := range plan.Local {
		if lc.Kind == localSet && lc.Key == typeKey {
			typ, _ = issue.String(lc.Value)
		}
	}
	var skips []Skip
	refused := map[string]bool{}
	for _, lc := range plan.Local {
		var err error
		switch {
		case !lc.isField():
		case lc.Kind == localSet:
			err = e.checker.CheckFields(typ, map[string]json.RawMessage{lc.Key: json.RawMessage(lc.Value)})
		default:
			err = e.checker.CheckItems(typ, map[string][]json.RawMessage{lc.Key: {json.RawMessage(lc.Value)}})
		}
		if err == nil || refused[lc.Key] {
			continue
		}
		refused[lc.Key] = true
		restore(&plan.Base, prior, lc.Key)
		plan.Base.Retry = append(plan.Base.Retry, lc.Key)
		skips = append(skips, Skip{Key: lc.Key, Reason: err.Error(), Retry: true})
	}
	kept := plan.Local[:0]
	for _, lc := range plan.Local {
		if !lc.isField() || !refused[lc.Key] {
			kept = append(kept, lc)
		}
	}
	plan.Local = kept
	return skips
}

// ops turns plan's local changes into operations and reports them.
func (e *engine) ops(snap *issue.Snapshot, plan mergePlan, line *Line, remote Doc) []issue.Operation {
	now := e.now()
	unix := func(lc localChange) int64 {
		if lc.At.IsZero() {
			return now.Unix()
		}
		return lc.At.Unix()
	}
	line.record(plan.Local, remote)
	var ops []issue.Operation
	for _, lc := range plan.Local {
		a, t := e.author(lc.Author), unix(lc)
		switch lc.Kind {
		case localSet:
			if lc.Key == BodyKey {
				text, _ := issue.String(lc.Value)
				ops = append(ops, issue.NewEditCommentOp(a, t, snap.Operations[0].Id(), text, nil))
			} else {
				ops = append(ops, issue.NewSetFieldOp(a, t, lc.Key, lc.Value))
			}
		case localAdd:
			ops = append(ops, issue.NewAddValueOp(a, t, lc.Key, lc.Value))
		case localRemove:
			ops = append(ops, issue.NewRemoveValueOp(a, t, lc.Key, lc.Value))
		case localAddComment:
			op := issue.NewAddCommentOp(a, t, lc.Text, nil)
			op.SetMetadata(MetaCommentId, lc.JiraId)
			ops = append(ops, op)
		case localEditComment, localTombstone:
			ops = append(ops, issue.NewEditCommentOp(a, t, lc.Op, lc.Text, nil))
		case localPairComment:
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

// conflictNote is JS21's note, one per issue per run.
func conflictNote(now time.Time, cs []Conflict) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Jira sync, %s: Jira's values replaced local edits.\n", now.UTC().Format("2006-01-02 15:04 MST"))
	for _, c := range cs {
		switch c.Key {
		case BodyKey:
			sb.WriteString("- body: the local description edit is kept in its history\n")
		case commentKey:
			fmt.Fprintf(&sb, "- comment %s: the local edit is kept in its history\n", c.Comment)
		default:
			fmt.Fprintf(&sb, "- %s: local %s -> Jira %s\n", c.Key, c.Local, c.Jira)
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// changeValue is what a multi key holds once a Change applies to cur.
func changeValue(ch change, cur issue.Value) issue.Value {
	if ch.Set != nil {
		return ch.Set
	}
	s := setOf(cur)
	for _, it := range ch.Add {
		s.add(it)
	}
	for _, it := range ch.Remove {
		delete(s, string(canon(it)))
	}
	return s.value()
}
