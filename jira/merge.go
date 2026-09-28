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

// commentKey is the key of a comment's conflict, and the prefix of its
// pending entry: comment:<Jira id>, or comment:<op> before it has one.
const commentKey = "comment"

// typeKey is the built-in type field: import-only in v1 (JS18).
const typeKey = "type"

// localKind is what one localChange commits.
type localKind int

const (
	localSet         localKind = iota // SetField Key = Value; EditComment of #0 for BodyKey
	localAdd                          // AddValue Key, Value
	localRemove                       // RemoveValue Key, Value
	localAddComment                   // AddComment Text, metadata jira-comment-id = JiraId
	localEditComment                  // EditComment Op to Text
	localTombstone                    // EditComment Op to a tombstone Text, by the runner now
	localPairComment                  // SetMetadata jira-comment-id = JiraId on Op, no text
)

// isField says a change writes a field, which the schema checks.
func (lc localChange) isField() bool {
	return lc.Kind == localSet && lc.Key != BodyKey || lc.Kind == localAdd || lc.Kind == localRemove
}

// localChange is one operation Merge wants committed locally.
type localChange struct {
	Kind   localKind
	Key    string // field kinds
	Value  issue.Value
	Text   string
	Op     entity.Id
	JiraId string
	Author string    // accountId; "" is the runner
	At     time.Time // Jira's updated for field imports; zero is now
}

// commentWrite is a comment to write to Jira; JiraId "" creates it.
type commentWrite struct {
	Op     entity.Id
	JiraId string
	Text   string
}

// key is the write's pending key: comment:<Jira id>, or comment:<op>.
func (cw commentWrite) key() string {
	if cw.JiraId == "" {
		return commentKey + ":" + cw.Op.Human()
	}
	return commentKey + ":" + cw.JiraId
}

// Conflict is a double edit Jira won (JS21). Key is a field, "body" or
// "comment"; texts are not repeated, the local one is in the history.
type Conflict struct {
	Key     string      `json:"key"`
	Local   issue.Value `json:"local,omitempty"`
	Jira    issue.Value `json:"jira,omitempty"`
	Comment string      `json:"comment,omitempty"`
}

// mergePlan is merge's decision for one issue.
type mergePlan struct {
	Local     []localChange
	Remote    []change
	Comments  []commentWrite
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
		return issue.StringValue(digest(text))
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

// merge decides one issue per key over base b, the local and the remote
// documents (JS9–JS12). It is pure. b nil is an issue never synced. With
// export false every local change is pending instead of in Remote/Comments
// (JS13 step 6). A key in mergePlan.Remote keeps its old base: the engine records
// the value written (I2).
func merge(b *Base, local, remote Doc, multi func(key string) bool, export bool) mergePlan {
	if b == nil {
		b = &Base{V: baseVersion}
	}
	nb := b.clone()
	nb.V, nb.Id, nb.Key, nb.Updated, nb.Gone, nb.Retry = baseVersion, remote.Id, remote.Key, remote.Updated, "", nil
	p := mergePlan{}

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
				p.Local = append(p.Local, localChange{Kind: localAdd, Key: k, Value: it, At: remote.Updated})
			}
			for _, it := range lRem {
				p.Local = append(p.Local, localChange{Kind: localRemove, Key: k, Value: it, At: remote.Updated})
			}
			if len(rAdd)+len(rRem) == 0 {
				nb.Fields[k] = merged
			} else {
				exportOr(k, func() { p.Remote = append(p.Remote, change{Key: k, Add: rAdd, Remove: rRem}) })
			}
			continue
		}

		switch v, conflict := decide(string(canon(bv)), hasBase, string(form(k, lv)), string(form(k, rv)), string(form(k, null))); v {
		case take:
			p.Local = append(p.Local, localChange{Kind: localSet, Key: k, Value: canon(rv), At: remote.Updated})
			if conflict && k == BodyKey {
				p.Conflicts = append(p.Conflicts, Conflict{Key: k}) // texts are in the history, not the report
			} else if conflict {
				p.Conflicts = append(p.Conflicts, Conflict{Key: k, Local: canon(lv), Jira: canon(rv)})
			}
		case give:
			switch {
			case k == typeKey:
				pending(k, reasonTypeChange)
			case k == BodyKey && remote.Lossy:
				pending(k, reasonLossy) // JS11: never overwritten
			default:
				exportOr(k, func() { p.Remote = append(p.Remote, change{Key: k, Set: canon(lv)}) })
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
func mergeComments(p *mergePlan, b, nb *Base, local, remote Doc, pending func(key, reason string), exportOr func(string, func())) {
	if nb.Comments == nil {
		nb.Comments = map[string]string{}
	}
	byJira := map[string]*docComment{}
	byOp := map[entity.Id]*docComment{}
	for i := range local.Comments {
		lc := &local.Comments[i]
		if lc.JiraId != "" {
			byJira[lc.JiraId] = lc
		}
		byOp[lc.Op] = lc
	}
	done := map[entity.Id]bool{}

	for _, rc := range remote.Comments {
		rd := digest(rc.Text.Text)
		key := commentKey + ":" + rc.JiraId
		if lc, ok := byJira[rc.JiraId]; ok {
			done[lc.Op] = true
			bd, hasBase := b.Comments[rc.JiraId]
			ld := digest(lc.Text.Text)
			switch v, conflict := decide(bd, hasBase && bd != "", ld, rd, digest("")); v {
			case take:
				p.Local = append(p.Local, localChange{Kind: localEditComment, Op: lc.Op, JiraId: rc.JiraId,
					Text: rc.Text.Text, Author: rc.Editor, At: rc.Edited})
				if conflict {
					p.Conflicts = append(p.Conflicts, Conflict{Key: commentKey, Comment: rc.JiraId})
				}
			case give:
				if !rc.Text.Lossless {
					pending(key, reasonLossy)
				} else {
					exportOr(key, func() {
						p.Comments = append(p.Comments, commentWrite{Op: lc.Op, JiraId: rc.JiraId, Text: lc.Text.Text})
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
				p.Local = append(p.Local, localChange{Kind: localPairComment, Op: lc.Op, JiraId: rc.JiraId})
				nb.Comments[rc.JiraId] = rd
			}
			continue
		}
		p.Local = append(p.Local, localChange{Kind: localAddComment, JiraId: rc.JiraId,
			Text: rc.Text.Text, Author: rc.Author, At: rc.At})
		nb.Comments[rc.JiraId] = rd
	}

	for _, lc := range local.Comments {
		if done[lc.Op] || lc.Note {
			continue
		}
		if lc.JiraId == "" {
			exportOr(commentKey+":"+lc.Op.Human(), func() { p.Comments = append(p.Comments, commentWrite{Op: lc.Op, Text: lc.Text.Text}) })
			continue
		}
		// paired, and gone from Jira
		bd, hasBase := b.Comments[lc.JiraId]
		if hasBase && bd == "" {
			if !IsTombstone(lc.Text.Text) {
				pending(commentKey+":"+lc.JiraId, reasonDeleted)
			}
			continue
		}
		p.Local = append(p.Local, localChange{Kind: localTombstone, Op: lc.Op, JiraId: lc.JiraId, Text: tombstone(remote.Updated)})
		if hasBase && digest(lc.Text.Text) != bd {
			p.Conflicts = append(p.Conflicts, Conflict{Key: commentKey, Comment: lc.JiraId})
		}
		nb.Comments[lc.JiraId] = ""
	}
	if len(nb.Comments) == 0 {
		nb.Comments = nil
	}
}

// tombstone is the text a comment deleted in Jira is edited to (JS12).
func tombstone(at time.Time) string {
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
