package jira

import (
	"sort"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
)

// BodyKey names comment #0 wherever a key is expected: a Change for the
// description, a conflict, a report member.
const BodyKey = "body"

// CommentKey is the key of a comment's conflict or pending entry.
const CommentKey = "comment"

// typeKey is the built-in type field: import-only in v1 (JS18).
const typeKey = "type"

// LocalKind is what one LocalChange commits.
type LocalKind int

const (
	LocalSet         LocalKind = iota // SetField Key = Value
	LocalAdd                          // AddValue Key, Value
	LocalRemove                       // RemoveValue Key, Value
	LocalEditBody                     // EditComment of #0 to Text
	LocalAddComment                   // AddComment Text, metadata jira-comment-id = JiraId
	LocalEditComment                  // EditComment Op to Text
	LocalPairComment                  // SetMetadata jira-comment-id = JiraId on Op, no text
)

// LocalChange is one operation Merge wants committed locally.
type LocalChange struct {
	Kind   LocalKind
	Key    string
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

	// type first (JS18), then the other keys in order.
	keys := make([]string, 0, len(remote.Fields))
	for k := range remote.Fields {
		if k != typeKey && !skipped[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if _, ok := remote.Fields[typeKey]; ok && !skipped[typeKey] {
		keys = append([]string{typeKey}, keys...)
	}

	for _, k := range keys {
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
				nb.Fields[k] = canon(issue.ItemsValue(merged))
			} else {
				exportOr(k, func() { p.Remote = append(p.Remote, Change{Key: k, Add: rAdd, Remove: rRem}) })
			}
			continue
		}

		switch {
		case same(lv, rv):
		case hasBase && same(lv, bv):
			p.Local = append(p.Local, LocalChange{Kind: LocalSet, Key: k, Value: canon(rv), At: remote.Updated})
		case hasBase && same(rv, bv):
			if k == typeKey {
				pending(k, reasonTypeChange)
			} else {
				exportOr(k, func() { p.Remote = append(p.Remote, Change{Key: k, Set: canon(lv)}) })
			}
			continue // the base stays until Jira holds l
		default: // all three differ, or no base (JS9's last rows)
			p.Local = append(p.Local, LocalChange{Kind: LocalSet, Key: k, Value: canon(rv), At: remote.Updated})
			if hasBase || !issue.IsNull(canon(lv)) {
				p.Conflicts = append(p.Conflicts, Conflict{Key: k, Local: canon(lv), Jira: canon(rv)})
			}
		}
		nb.Fields[k] = canon(rv)
	}

	// the description (JS11)
	{
		bd, ld, rd := b.Body, Digest(local.Body.Text), Digest(remote.Body.Text)
		switch {
		case ld == rd:
			nb.Body = rd
		case bd != "" && ld == bd:
			p.Local = append(p.Local, LocalChange{Kind: LocalEditBody, Key: BodyKey, Text: remote.Body.Text, At: remote.Updated})
			nb.Body = rd
		case bd != "" && rd == bd:
			if !remote.Body.Lossless {
				pending(BodyKey, reasonLossy)
			} else {
				exportOr(BodyKey, func() {
					p.Remote = append(p.Remote, Change{Key: BodyKey, Set: issue.StringValue(local.Body.Text)})
				})
			}
		default:
			p.Local = append(p.Local, LocalChange{Kind: LocalEditBody, Key: BodyKey, Text: remote.Body.Text, At: remote.Updated})
			if bd != "" || ld != Digest("") {
				p.Conflicts = append(p.Conflicts, Conflict{Key: BodyKey, Local: issue.StringValue(ld), Jira: issue.StringValue(rd)})
			}
			nb.Body = rd
		}
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
		if lc.Op != "" {
			byOp[lc.Op] = lc
		}
	}
	done := map[entity.Id]bool{}

	for _, rc := range remote.Comments {
		rd := Digest(rc.Text.Text)
		if lc, ok := byJira[rc.JiraId]; ok {
			done[lc.Op] = true
			bd, hasBase := b.Comments[rc.JiraId]
			hasBase = hasBase && bd != ""
			ld := Digest(lc.Text.Text)
			edit := LocalChange{Kind: LocalEditComment, Key: CommentKey, Op: lc.Op, JiraId: rc.JiraId,
				Text: rc.Text.Text, Author: rc.Editor, At: rc.Edited}
			switch {
			case ld == rd:
			case hasBase && ld == bd:
				p.Local = append(p.Local, edit)
			case hasBase && rd == bd:
				if !rc.Text.Lossless {
					pending(CommentKey+":"+rc.JiraId, reasonLossy)
				} else {
					exportOr(CommentKey+":"+rc.JiraId, func() {
						p.Comments = append(p.Comments, CommentWrite{Op: lc.Op, JiraId: rc.JiraId, Text: lc.Text.Text})
					})
				}
				continue
			default:
				p.Local = append(p.Local, edit)
				p.Conflicts = append(p.Conflicts, Conflict{Key: CommentKey, Comment: rc.JiraId,
					Local: issue.StringValue(ld), Jira: issue.StringValue(rd)})
			}
			nb.Comments[rc.JiraId] = rd
			continue
		}
		if rc.Op != "" {
			if lc, ok := byOp[rc.Op]; ok {
				// our own export whose pairing never committed (JS12's crash row)
				done[lc.Op] = true
				if lc.JiraId == "" && !lc.Note {
					p.Local = append(p.Local, LocalChange{Kind: LocalPairComment, Key: CommentKey, Op: lc.Op, JiraId: rc.JiraId})
					nb.Comments[rc.JiraId] = rd
				}
				continue
			}
		}
		p.Local = append(p.Local, LocalChange{Kind: LocalAddComment, Key: CommentKey, JiraId: rc.JiraId,
			Text: rc.Text.Text, Author: rc.Author, At: rc.At})
		nb.Comments[rc.JiraId] = rd
	}

	for _, lc := range local.Comments {
		if done[lc.Op] || lc.Note {
			continue
		}
		if lc.JiraId == "" {
			exportOr(CommentKey, func() { p.Comments = append(p.Comments, CommentWrite{Op: lc.Op, Text: lc.Text.Text}) })
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
		p.Local = append(p.Local, LocalChange{Kind: LocalEditComment, Key: CommentKey, Op: lc.Op, JiraId: lc.JiraId,
			Text: Tombstone(remote.Updated)})
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

func IsTombstone(text string) bool {
	return len(text) >= len(TombstonePrefix) && text[:len(TombstonePrefix)] == TombstonePrefix
}

// mergeSet is JS10: an item survives when neither side removed it or either
// side added it. It returns the merged set and the diffs against l and r.
func mergeSet(b, l, r issue.Value) (merged, lAdd, lRem, rAdd, rRem []issue.Value) {
	bs, ls, rs := itemSet(b), itemSet(l), itemSet(r)
	all := map[string]issue.Value{}
	for _, s := range []map[string]issue.Value{bs, ls, rs} {
		for k, v := range s {
			all[k] = v
		}
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, inB := bs[k]
		_, inL := ls[k]
		_, inR := rs[k]
		keep := inL || inR
		if inB {
			keep = inL && inR
		}
		if keep {
			merged = append(merged, all[k])
		}
		switch {
		case keep && !inL:
			lAdd = append(lAdd, all[k])
		case !keep && inL:
			lRem = append(lRem, all[k])
		}
		switch {
		case keep && !inR:
			rAdd = append(rAdd, all[k])
		case !keep && inR:
			rRem = append(rRem, all[k])
		}
	}
	return
}

func itemSet(v issue.Value) map[string]issue.Value {
	items, _ := issue.Items(canon(v))
	s := make(map[string]issue.Value, len(items))
	for _, it := range items {
		c := canon(it)
		s[string(c)] = c
	}
	return s
}
