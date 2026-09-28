package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
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
// place; the caller saves it at the end unless DryRun.
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
		authors: map[string]identity.Interface{}, done: map[entity.Id]bool{}, missing: map[string]bool{}, cursor: st.Cursor}

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
	missing map[string]bool      // Jira ids a GET found deleted or moved out (JS19)
	created map[entity.Id]string // in-doubt creates found by the created search (JS15); nil until searched
	since   time.Time            // the created search's bound, before Overlap
	doubt   time.Time            // the oldest in-doubt attempt of the run's creates
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

// stop is the run's error from one issue's: only a run failure stops it.
func stop(err error) error {
	if err != nil && runFatal(err) {
		return err
	}
	return nil
}

// now is Jira's clock, from the Date of its last response: create attempts
// then compare with Jira's created, and notes carry Jira's time, with no
// client skew (JS20). The fallback is local.
func (e *engine) now() time.Time {
	if d := e.c.ServerDate(); !d.IsZero() {
		return d.UTC()
	}
	return time.Now().UTC()
}

// location is the zone JQL literals are read in (JS20).
func (e *engine) location() (*time.Location, error) {
	loc, err := e.p.Me.Location()
	if err != nil || e.p.Me.TimeZone == "" {
		return nil, fatal{fmt.Errorf("jira: the account's time zone %q does not load; JQL dates would be misread (JS20)", e.p.Me.TimeZone)}
	}
	return loc, nil
}

func (e *engine) setup() error {
	me, _, err := e.repo.EnsureUserIdentity()
	if err != nil {
		return err
	}
	e.me = me
	if e.ix, err = NewIndex(e.repo); err != nil {
		return err
	}
	// JS16: the token's account is the runner's, unless an identity has it
	if acc := e.p.Me.AccountID; acc != "" && !e.opts.DryRun {
		if _, ok := e.ix.User(acc); !ok {
			me.SetMetadata(MetaAccountId, acc)
			if err := me.Commit(); err != nil {
				return err
			}
			e.ix.addUser(acc, me.Id())
		}
	}
	e.checker, err = e.repo.Checker()
	return err
}

// ---- the report ----

// fail reports one issue's failure and returns err; callers stop the run
// only when it is a run failure (stop).
func (e *engine) fail(line Line, err error) error {
	line.Action, line.Error = ActionFailed, err.Error()
	e.report(line)
	return err
}

func (e *engine) report(l Line) {
	if l.Action == ActionUpdated && !l.moved() && l.Error == "" {
		if len(l.Pending) == 0 {
			e.sum.Unchanged++
			return
		}
		l.Action = ActionPending
	}
	// a key and reason reported twice (step 4 and step 6) is one entry (A8)
	seen := map[[2]string]bool{}
	l.Pending = slices.DeleteFunc(l.Pending, func(s Skip) bool {
		k := [2]string{s.Key, s.Reason}
		defer func() { seen[k] = true }()
		return seen[k]
	})
	e.sum.count(l)
	e.emit(l)
}

// record reports a plan's local changes on its line.
func (l *Line) record(lcs []localChange, remote Doc) {
	for _, lc := range lcs {
		switch lc.Kind {
		case localSet:
			l.imported(lc.Key, lc.Value)
		case localAdd, localRemove:
			l.imported(lc.Key, remote.Fields[lc.Key])
		case localAddComment:
			l.comments().Imported++
		case localEditComment:
			l.comments().Edited++
		case localTombstone:
			l.comments().Tombstoned++
		}
	}
}

// exports reports what plan writes to Jira, but for the keys that failed.
func (l *Line) exports(plan mergePlan, remote Doc, failed map[string]bool) {
	for _, ch := range plan.Remote {
		if !failed[ch.Key] {
			l.exported(ch.Key, changeValue(ch, remote.Fields[ch.Key]))
		}
	}
	for _, cw := range plan.Comments {
		switch {
		case failed[cw.key()]:
		case cw.JiraId == "":
			l.comments().Exported++
		default:
			l.comments().Edited++
		}
	}
}

func (l *Line) imported(key string, v issue.Value) {
	if l.Imported == nil {
		l.Imported = map[string]json.RawMessage{}
	}
	l.Imported[key] = reported(key, v)
}

func (l *Line) exported(key string, v issue.Value) {
	if l.Exported == nil {
		l.Exported = map[string]json.RawMessage{}
	}
	l.Exported[key] = reported(key, v)
}

// reported is a value as a line shows it: the body's first line, at most
// 60 characters, since a description does not fit a line.
func reported(key string, v issue.Value) json.RawMessage {
	if key != BodyKey {
		return json.RawMessage(canon(v))
	}
	text, _ := issue.String(v)
	first, _, cut := strings.Cut(strings.TrimSpace(text), "\n")
	if r := []rune(first); len(r) > 60 {
		first, cut = string(r[:60]), true
	}
	if cut {
		first += "…"
	}
	return json.RawMessage(issue.StringValue(first))
}

func (l *Line) comments() *CommentCounts {
	if l.Comments == nil {
		l.Comments = &CommentCounts{}
	}
	return l.Comments
}
