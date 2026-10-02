package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/util/sorted"
)

// Values of MetaNote and of Base.Gone.
const (
	NoteConflict     = "conflict"
	NoteDeleted      = "deleted"
	NoteConsolidated = "consolidated"

	GoneDeleted = "deleted"
	GoneMoved   = "moved"
)

// Base is what both sides held after the last sync (JS8), in local terms.
type Base struct {
	V        int                    `json:"v"`
	Id       string                 `json:"id"`
	Key      string                 `json:"key"`
	Updated  time.Time              `json:"updated"`
	Fields   map[string]issue.Value `json:"fields"` // each key's form: the body's digest
	Comments map[string]string      `json:"comments,omitempty"`
	Retry    []string               `json:"retry,omitempty"`
	Gone     string                 `json:"gone,omitempty"`
	// Sent is each key written and not yet seen in a GET, as its form (a
	// comment's key is comment:<Jira id>, its value the text's digest);
	// Wrote is Jira's clock just before the last of those writes (I2).
	Sent  map[string]issue.Value `json:"sent,omitempty"`
	Wrote time.Time              `json:"wrote,omitzero"`
	// Fresh is a create not merged yet: a key with no base takes JS15's
	// create rule on the first GET.
	Fresh bool `json:"fresh,omitempty"`
}

// unknownBase is the base of a key whose write Jira answered and then does
// not show: it equals neither side, so the merge takes Jira's value with a
// conflict note (I2). unknownComment is the same for a comment's digest.
var unknownBase = issue.Value(`{"jira-sync":"unconfirmed"}`)

const unknownComment = "unconfirmed"

const baseVersion = 1

// marshal is the marker's metadata value; values are compacted, so two equal
// bases marshal to equal bytes.
func (b *Base) marshal() string {
	c := b.clone()
	for k, v := range c.Fields {
		c.Fields[k] = canon(v)
	}
	for k, v := range c.Sent {
		c.Sent[k] = canon(v)
	}
	c.Updated, c.Wrote = c.Updated.UTC(), c.Wrote.UTC()
	data, err := json.Marshal(c)
	if err != nil {
		panic(err) // every member is plain JSON
	}
	return string(data)
}

// equal reports whether recording o over b would say nothing new. Updated
// alone is not news: Jira bumps it for what the mapping does not cover, and
// for a link's other end, and a marker for it would be a commit per bump
// (E1); the cost is a GET while the overlap re-returns the issue.
func (b *Base) equal(o *Base) bool {
	if b == nil || o == nil {
		return b == o
	}
	c := o.clone()
	c.Updated = b.Updated
	return b.marshal() == c.marshal()
}

func (b *Base) clone() *Base {
	c := *b
	c.Fields = maps.Clone(b.Fields)
	if c.Fields == nil {
		c.Fields = map[string]issue.Value{}
	}
	c.Comments = maps.Clone(b.Comments)
	c.Retry = slices.Clone(b.Retry)
	c.Sent = maps.Clone(b.Sent)
	return &c
}

// settled says nothing waits on Jira: no Retry, no Sent, not Fresh.
func (b *Base) settled() bool { return len(b.Retry) == 0 && len(b.Sent) == 0 && !b.Fresh }

// young says the issue is a create whose first GET may not see it yet.
func (b *Base) young(now time.Time, window time.Duration) bool {
	return b.Fresh && now.Sub(b.Wrote) < window
}

// fresh is JS15's create base, for the keys a create's marker leaves
// without one: local's when local holds nothing, so a Jira default imports;
// Jira's otherwise, so the local value is written now.
func (b *Base) fresh(local, remote Doc) {
	for k, rv := range remote.Fields {
		_, based := b.Fields[k]
		if _, sent := b.Sent[k]; based || sent {
			continue
		}
		if lv := form(k, local.Fields[k]); issue.IsNull(lv) || string(lv) == "[]" {
			b.Fields[k] = lv
		} else {
			b.Fields[k] = form(k, rv)
		}
	}
	b.Fresh = false
}

// confirm resolves each key of b.Sent against the GET r, before a merge
// (I2). Jira showing the value written confirms it: that is its base. A GET
// older than the write (r.Updated before b.Wrote), within window of Jira's
// clock, may be stale: the key is a Skip{Retry}, so the merge neither
// imports, exports nor re-bases it. Otherwise Jira holds something else.
// For a key of echo, written this run and r the GET that follows the write
// and shows it landed, that is Jira's normal form of ours: the base is the
// value written, so it imports as an ordinary change. For any other, it
// may as well be a later Jira edit, or ours normalised back to the old
// value: the base is unknownBase, and Jira's value imports with a note. A
// set keeps its base in every case, JS10 deciding: Sent only holds it back
// from a stale GET.
func confirm(b *Base, r Doc, multi func(string) bool, echo map[string]bool, now time.Time, window time.Duration) []Skip {
	landed := !r.Updated.Before(b.Wrote)
	stale := !landed && now.Sub(b.Wrote) < window
	var skips []Skip
	for _, k := range sorted.Keys(b.Sent) {
		sent := b.Sent[k]
		var held, shown bool
		var based issue.Value
		if jid, ok := strings.CutPrefix(k, commentKey+":"); ok {
			sd, _ := issue.String(sent)
			for _, c := range r.Comments {
				if c.JiraId == jid {
					shown, held = true, digest(c.Text.Text) == sd
				}
			}
			if b.Comments == nil {
				b.Comments = map[string]string{}
			}
			switch {
			case held || !shown && !stale: // gone after holding ours: deleted in Jira
				b.Comments[jid] = sd
			case landed && echo[k]:
				b.Comments[jid] = sd
			case !stale:
				b.Comments[jid] = unknownComment
			}
		} else {
			m := multi != nil && multi(k)
			held = same(canonical(form(k, r.Fields[k]), m), canonical(sent, m))
			switch {
			case m: // the base stays: against it JS10 reaches the merged set on both sides
			case held, landed && echo[k]:
				based = sent
			case !stale:
				based = unknownBase
			}
			if based != nil {
				b.Fields[k] = based
			}
		}
		if !held && stale {
			skips = append(skips, Skip{Key: k, Retry: true,
				Reason: "Jira does not show the write of " + b.Wrote.UTC().Format(time.RFC3339) + " yet"})
			continue
		}
		delete(b.Sent, k)
	}
	if len(b.Sent) == 0 {
		b.Sent = nil
	}
	return skips
}

// CurrentBase is the base of a linked issue: the marker with the greatest
// Updated, ties to the later in compiled order (JS8). It is nil when the
// issue has no jira-id; a linked issue with no marker has an empty base, so
// every key is a missing base. Undecodable markers are skipped and reported.
func CurrentBase(snap *issue.Snapshot) (*Base, []string) {
	jiraId, ok := snap.GetCreateMetadata(MetaId)
	if !ok || jiraId == "" {
		return nil, nil
	}
	var cur *Base
	var problems []string
	for _, op := range snap.Operations {
		raw, ok := op.GetMetadata(MetaSync)
		if !ok {
			continue
		}
		var b Base
		if err := json.Unmarshal([]byte(raw), &b); err != nil || b.V != baseVersion {
			problems = append(problems, fmt.Sprintf("marker %s: undecodable base, skipped", op.Id().Human()))
			continue
		}
		if cur == nil || !b.Updated.Before(cur.Updated) {
			cur = &b
		}
	}
	if cur == nil {
		cur = &Base{V: baseVersion}
	}
	cur.Id = jiraId
	if cur.Fields == nil {
		cur.Fields = map[string]issue.Value{}
	}
	return cur, problems
}

// digest is the one text comparison (JS11). The normalisation is the one
// TextToADF preserves, so two texts Jira cannot tell apart share a digest.
func digest(text string) string {
	sum := sha256.Sum256([]byte("v1\n" + jiraapi.NormalizeText(text)))
	return "v1:" + hex.EncodeToString(sum[:])
}
