package jira

import (
	"strings"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/util/sorted"
)

// BodyKey is comment #0 as a key of Doc.Fields, its text a JSON string:
// the description in Jira. Its base holds the text's digest (form).
const BodyKey = "body"

// CommentKey is the key of a comment's conflict, and the prefix of its
// pending entry: comment:<Jira id>, or comment:<op> before it has one.
const CommentKey = "comment"

// typeKey is the built-in type field: import-only in v1 (JS18).
const typeKey = "type"

// LocalKind is what one LocalChange commits.
type LocalKind int

const (
	LocalSet         LocalKind = iota // SetField Key = Value; EditComment of #0 for BodyKey
	LocalAdd                          // AddValue Key, Value
	LocalRemove                       // RemoveValue Key, Value
	LocalAddComment                   // AddComment Text, metadata jira-comment-id = JiraId
	LocalEditComment                  // EditComment Op to Text
	LocalTombstone                    // EditComment Op to a tombstone Text, by the runner now
	LocalPairComment                  // SetMetadata jira-comment-id = JiraId on Op, no text
)

// isField says a change writes a field, which the schema checks.
func (lc LocalChange) isField() bool {
	return lc.Kind == LocalSet && lc.Key != BodyKey || lc.Kind == LocalAdd || lc.Kind == LocalRemove
}

// LocalChange is one operation Merge wants committed locally.
type LocalChange struct {
	Kind   LocalKind
	Key    string // field kinds
	Value  issue.Value
	Text   string
	Op     entity.Id
	JiraId string
	Author string    // accountId; "" is the runner
	At     time.Time // Jira's updated for field imports; zero is now
}

// CommentWrite is a comment to write to Jira; JiraId "" creates it.
type CommentWrite struct {
	Op     entity.Id
	JiraId string
	Text   string
}

// key is the write's pending key: comment:<Jira id>, or comment:<op>.
func (cw CommentWrite) key() string {
	if cw.JiraId == "" {
		return CommentKey + ":" + cw.Op.Human()
	}
	return CommentKey + ":" + cw.JiraId
}

// Conflict is a double edit Jira won (JS21). Key is a field, "body" or
// "comment"; texts carry digests.
type Conflict struct {
	Key     string      `json:"key"`
	Local   issue.Value `json:"local,omitempty"`
	Jira    issue.Value `json:"jira,omitempty"`
	Comment string      `json:"comment,omitempty"`
}

// Plan is Merge's decision for one issue.
type Plan struct {
	Local     []LocalChange
	Remote    []Change
	Comments  []CommentWrite
	Conflicts []Conflict
	Pending   []Skip
	Base      Base // what to record once Local commits
}

const (
	reasonNotExported = "local change not written to Jira this run"
	reasonLossy       = "the Jira text holds content git-work cannot write back"
	reasonTypeChange  = "change the type in Jira"
	reasonDeleted     = "deleted in Jira"
)

// TombstonePrefix starts the text a comment deleted in Jira is edited to.
const TombstonePrefix = "Deleted in Jira on "

// verdict is JS9's decision for one key.
type verdict int

const (
	agree verdict = iota // l == r: converged
	take                 // import r
	give                 // export l
)

// form is a key's value as it is compared and kept in a base: the digest
// of the body's text (JS11), canonical JSON for any other key.
func form(key string, v issue.Value) issue.Value {
	if key == BodyKey {
		text, _ := issue.String(v)
		return issue.StringValue(Digest(text))
	}
	return canon(v)
}

// decide is JS9 over comparable forms (form, and digests for comments).
// empty is what an unset local compares as: taking over it is no conflict.
func decide(b string, hasBase bool, l, r, empty string) (v verdict, conflict bool) {
	switch {
	case l == r:
		return agree, false
	case hasBase && l == b:
		return take, false
	case hasBase && r == b:
		return give, false
	}
	return take, hasBase || l != empty
}

// Merge decides one issue per key over base b, the local and the remote
// documents (JS9–JS12). It is pure. b nil is an issue never synced. With
// export false every local change is pending instead of in Remote/Comments
// (JS13 step 6). A key in Plan.Remote keeps its old base: the engine records
// the value written (I2).
func Merge(b *Base, local, remote Doc, multi func(key string) bool, export bool) Plan {
	if b == nil {
		b = &Base{V: baseVersion}
	}
	nb := b.clone()
	nb.V, nb.Id, nb.Key, nb.Updated, nb.Gone, nb.Retry = baseVersion, remote.Id, remote.Key, remote.Updated, "", nil
	p := Plan{}

	skipped := map[string]bool{}
	for _, s := range remote.Skip {
		skipped[s.Key] = true
		p.Pending = append(p.Pending, s)
		if s.Retry {
			nb.Retry = append(nb.Retry, s.Key)
		}
	}
	pending := func(key, reason string) { p.Pending = append(p.Pending, Skip{Key: key, Reason: reason}) }
	exportOr := func(key string, write func()) {
		if export {
			write()
		} else {
			pending(key, reasonNotExported)
		}
	}

	// type first (JS18), then the other keys in order
	keys := sorted.Keys(remote.Fields)
	if _, ok := remote.Fields[typeKey]; ok {
		keys = append([]string{typeKey}, keys...)
	}
	for i, k := range keys {
		if skipped[k] || k == typeKey && i > 0 {
			continue
		}
		bv, hasBase := b.Fields[k]
		lv, rv := local.Fields[k], remote.Fields[k]

		if multi != nil && multi(k) {
			merged, lAdd, lRem, rAdd, rRem := mergeSet(bv, lv, rv)
			for _, it := range lAdd {
				p.Local = append(p.Local, LocalChange{Kind: LocalAdd, Key: k, Value: it, At: remote.Updated})
			}
			for _, it := range lRem {
				p.Local = append(p.Local, LocalChange{Kind: LocalRemove, Key: k, Value: it, At: remote.Updated})
			}
			if len(rAdd)+len(rRem) == 0 {
				nb.Fields[k] = merged
			} else {
				exportOr(k, func() { p.Remote = append(p.Remote, Change{Key: k, Add: rAdd, Remove: rRem}) })
			}
			continue
		}

		switch v, conflict := decide(string(canon(bv)), hasBase, string(form(k, lv)), string(form(k, rv)), string(form(k, null))); v {
		case take:
			p.Local = append(p.Local, LocalChange{Kind: LocalSet, Key: k, Value: canon(rv), At: remote.Updated})
			if conflict {
				p.Conflicts = append(p.Conflicts, Conflict{Key: k, Local: form(k, lv), Jira: form(k, rv)})
			}
		case give:
			switch {
			case k == typeKey:
				pending(k, reasonTypeChange)
			case k == BodyKey && remote.Lossy:
				pending(k, reasonLossy) // JS11: never overwritten
			default:
				exportOr(k, func() { p.Remote = append(p.Remote, Change{Key: k, Set: canon(lv)}) })
			}
			continue // the base stays until Jira holds l
		}
		nb.Fields[k] = form(k, rv)
	}

	mergeComments(&p, b, nb, local, remote, pending, exportOr)
	p.Base = *nb
	return p
}

// mergeComments is JS12's table.
func mergeComments(p *Plan, b, nb *Base, local, remote Doc, pending func(key, reason string), exportOr func(string, func())) {
	if nb.Comments == nil {
		nb.Comments = map[string]string{}
	}
	byJira := map[string]*Comment{}
	byOp := map[entity.Id]*Comment{}
	for i := range local.Comments {
		lc := &local.Comments[i]
		if lc.JiraId != "" {
			byJira[lc.JiraId] = lc
		}
		byOp[lc.Op] = lc
	}
	done := map[entity.Id]bool{}

	for _, rc := range remote.Comments {
		rd := Digest(rc.Text.Text)
		key := CommentKey + ":" + rc.JiraId
		if lc, ok := byJira[rc.JiraId]; ok {
			done[lc.Op] = true
			bd, hasBase := b.Comments[rc.JiraId]
			ld := Digest(lc.Text.Text)
			switch v, conflict := decide(bd, hasBase && bd != "", ld, rd, Digest("")); v {
			case take:
				p.Local = append(p.Local, LocalChange{Kind: LocalEditComment, Op: lc.Op, JiraId: rc.JiraId,
					Text: rc.Text.Text, Author: rc.Editor, At: rc.Edited})
				if conflict {
					p.Conflicts = append(p.Conflicts, Conflict{Key: CommentKey, Comment: rc.JiraId,
						Local: issue.StringValue(ld), Jira: issue.StringValue(rd)})
				}
			case give:
				if !rc.Text.Lossless {
					pending(key, reasonLossy)
				} else {
					exportOr(key, func() {
						p.Comments = append(p.Comments, CommentWrite{Op: lc.Op, JiraId: rc.JiraId, Text: lc.Text.Text})
					})
				}
				continue
			}
			nb.Comments[rc.JiraId] = rd
			continue
		}
		if lc, ok := byOp[rc.Op]; ok && rc.Op != "" {
			// our own export whose pairing never committed (JS12's crash row)
			done[lc.Op] = true
			if lc.JiraId == "" && !lc.Note {
				p.Local = append(p.Local, LocalChange{Kind: LocalPairComment, Op: lc.Op, JiraId: rc.JiraId})
				nb.Comments[rc.JiraId] = rd
			}
			continue
		}
		p.Local = append(p.Local, LocalChange{Kind: LocalAddComment, JiraId: rc.JiraId,
			Text: rc.Text.Text, Author: rc.Author, At: rc.At})
		nb.Comments[rc.JiraId] = rd
	}

	for _, lc := range local.Comments {
		if done[lc.Op] || lc.Note {
			continue
		}
		if lc.JiraId == "" {
			exportOr(CommentKey+":"+lc.Op.Human(), func() { p.Comments = append(p.Comments, CommentWrite{Op: lc.Op, Text: lc.Text.Text}) })
			continue
		}
		// paired, and gone from Jira
		bd, hasBase := b.Comments[lc.JiraId]
		if hasBase && bd == "" {
			if !IsTombstone(lc.Text.Text) {
				pending(CommentKey+":"+lc.JiraId, reasonDeleted)
			}
			continue
		}
		p.Local = append(p.Local, LocalChange{Kind: LocalTombstone, Op: lc.Op, JiraId: lc.JiraId, Text: Tombstone(remote.Updated)})
		if hasBase && Digest(lc.Text.Text) != bd {
			p.Conflicts = append(p.Conflicts, Conflict{Key: CommentKey, Comment: lc.JiraId})
		}
		nb.Comments[lc.JiraId] = ""
	}
	if len(nb.Comments) == 0 {
		nb.Comments = nil
	}
}

// Tombstone is the text a comment deleted in Jira is edited to (JS12).
func Tombstone(at time.Time) string {
	return TombstonePrefix + at.UTC().Format("2006-01-02") + "."
}

func IsTombstone(text string) bool { return strings.HasPrefix(text, TombstonePrefix) }

// mergeSet is JS10: an item survives when neither side removed it or either
// side added it. It returns the merged set and the diffs against l and r.
func mergeSet(b, l, r issue.Value) (merged issue.Value, lAdd, lRem, rAdd, rRem []issue.Value) {
	bs, ls, rs := setOf(b), setOf(l), setOf(r)
	all, keep := itemSet{}, itemSet{}
	for _, s := range []itemSet{bs, ls, rs} {
		for k, v := range s {
			all[k] = v
		}
	}
	for _, k := range sorted.Keys(all) {
		_, inB := bs[k]
		_, inL := ls[k]
		_, inR := rs[k]
		kept := inL || inR
		if inB {
			kept = inL && inR
		}
		if kept {
			keep[k] = all[k]
		}
		switch {
		case kept && !inL:
			lAdd = append(lAdd, all[k])
		case !kept && inL:
			lRem = append(lRem, all[k])
		}
		switch {
		case kept && !inR:
			rAdd = append(rAdd, all[k])
		case !kept && inR:
			rRem = append(rRem, all[k])
		}
	}
	return keep.value(), lAdd, lRem, rAdd, rRem
}
