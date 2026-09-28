package jira

import (
	"encoding/json"
	"fmt"
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
	ri, err := e.c.GetIssue(e.ctx, idOrKey, e.m.Request(), nil, PropertyKey)
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

// remote is FromJira with the comments this run posted paired to their ops
// even if Jira returned no comment property (JS12).
func (e *engine) remote(ri *jiraapi.Issue, cs []jiraapi.Comment, pairs map[string]entity.Id) Doc {
	r := e.m.FromJira(ri, cs, e.ix)
	for i := range r.Comments {
		if op, ok := pairs[r.Comments[i].JiraId]; ok {
			r.Comments[i].Op = op
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
	b, problems := CurrentBase(ic.Snapshot())
	line := Line{Issue: id, Jira: b.Key, Action: ActionUpdated}
	for _, p := range problems {
		line.Pending = append(line.Pending, Skip{Key: MetaSync, Reason: p})
	}
	ri, cs, err := e.read(b.Id)
	gone := ""
	switch {
	case jiraapi.StatusCode(err) == 404:
		gone = "not in Jira: deleted, or hidden from the sync's account"
	case err != nil:
		return e.fail(line, err)
	case !e.p.Owns(ri.Key):
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
	return e.converge(ic, b, ri, cs, nil, line)
}

// converge is JS13 steps 3–6 for an issue on both sides: b is the base (nil
// for none), meta the create-op metadata step 6 adds for a new link, line
// what the caller has to report already.
func (e *engine) converge(ic *cache.IssueCache, b *Base, ri *jiraapi.Issue, cs []jiraapi.Comment, meta map[string]string, line Line) error {
	remote := e.remote(ri, cs, nil)
	local := e.m.Local(ic.Snapshot(), remote.Type)
	plan1 := Merge(b, local, remote, e.multi(remote.Type), true)
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
	ri2, cs2 := ri, cs
	var unconfirmed []string
	if wrote {
		if ri2, cs2, err = e.read(ri.ID); err != nil {
			return e.fail(line, err)
		}
		unconfirmed = e.unconfirmed(plan1.Remote, b2, remote, e.m.FromJira(ri2, cs2, e.ix))
		// I2: a write Jira does not show yet is not a base; the prior one stays,
		// so if Jira keeps the old value the next run exports again rather than
		// importing the old value over the local edit
		for _, k := range unconfirmed {
			restore(b2, b, k)
		}
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
		default:
			k := ch.Key
			if v, ok := b2.Fields[k]; ok && same(v, form(k, ch.Set)) && same(form(k, r2.Fields[k]), form(k, r.Fields[k])) && !same(form(k, r.Fields[k]), v) {
				keys = append(keys, k)
			}
		}
	}
	return keys
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
// are one commit. line is step 4's; Update calls the closure exactly once.
func (e *engine) commit(ic *cache.IssueCache, b2 *Base, ri *jiraapi.Issue, cs []jiraapi.Comment,
	pairs map[string]entity.Id, meta map[string]string, unconfirmed []string, line Line) error {
	decided := line
	var retry bool
	err := ic.Update(func(snap *issue.Snapshot) ([]issue.Operation, error) {
		r := e.remote(ri, cs, pairs)
		for _, k := range unconfirmed {
			r.Skip = append(r.Skip, Skip{Key: k, Reason: "Jira does not show this run's write yet", Retry: true})
		}
		plan := Merge(b2, e.m.Local(snap, r.Type), r, e.multi(r.Type), false)
		typ, _ := issue.String(snap.Fields[typeKey])
		decided.Pending = append(decided.Pending, e.admit(&plan, typ, b2)...)
		ops := e.ops(snap, plan, &decided, r)
		if len(meta) > 0 {
			ops = append(ops, issue.NewSetMetadataOp(e.me, e.now().Unix(), snap.Operations[0].Id(), meta))
		}
		if cur, _ := CurrentBase(snap); !plan.Base.Equal(cur) {
			ops = append(ops, issue.NewNoOpOp(e.me, e.now().Unix(), map[string]string{MetaSync: plan.Base.Marshal()}))
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
func (e *engine) admit(plan *Plan, typ string, prior *Base) []Skip {
	for _, lc := range plan.Local {
		if lc.Kind == LocalSet && lc.Key == typeKey {
			typ, _ = issue.String(lc.Value)
		}
	}
	var skips []Skip
	refused := map[string]bool{}
	for _, lc := range plan.Local {
		var err error
		switch {
		case !lc.isField():
		case lc.Kind == LocalSet:
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
func (e *engine) ops(snap *issue.Snapshot, plan Plan, line *Line, remote Doc) []issue.Operation {
	now := e.now()
	unix := func(lc LocalChange) int64 {
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
		case LocalSet:
			if lc.Key == BodyKey {
				text, _ := issue.String(lc.Value)
				ops = append(ops, issue.NewEditCommentOp(a, t, snap.Operations[0].Id(), text, nil))
			} else {
				ops = append(ops, issue.NewSetFieldOp(a, t, lc.Key, lc.Value))
			}
		case LocalAdd:
			ops = append(ops, issue.NewAddValueOp(a, t, lc.Key, lc.Value))
		case LocalRemove:
			ops = append(ops, issue.NewRemoveValueOp(a, t, lc.Key, lc.Value))
		case LocalAddComment:
			op := issue.NewAddCommentOp(a, t, lc.Text, nil)
			op.SetMetadata(MetaCommentId, lc.JiraId)
			ops = append(ops, op)
		case LocalEditComment, LocalTombstone:
			ops = append(ops, issue.NewEditCommentOp(a, t, lc.Op, lc.Text, nil))
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

// changeValue is what a multi key holds once a Change applies to cur.
func changeValue(ch Change, cur issue.Value) issue.Value {
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
