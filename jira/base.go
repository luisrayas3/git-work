package jira

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/jira/jiraapi"
)

// Values of MetaNote and of Base.Gone.
const (
	NoteConflict = "conflict"
	NoteDeleted  = "deleted"

	GoneDeleted = "deleted"
	GoneMoved   = "moved"
)

// Base is what both sides held after the last sync (JS8), in local terms.
type Base struct {
	V        int                    `json:"v"`
	Id       string                 `json:"id"`
	Key      string                 `json:"key"`
	Updated  time.Time              `json:"updated"`
	Fields   map[string]issue.Value `json:"fields"`
	Body     string                 `json:"body"`
	Comments map[string]string      `json:"comments,omitempty"`
	Retry    []string               `json:"retry,omitempty"`
	Gone     string                 `json:"gone,omitempty"`
}

const baseVersion = 1

// Marshal is the marker's metadata value; values are compacted, so two equal
// bases marshal to equal bytes.
func (b *Base) Marshal() string {
	c := b.clone()
	for k, v := range c.Fields {
		c.Fields[k] = canon(v)
	}
	c.Updated = c.Updated.UTC()
	data, err := json.Marshal(c)
	if err != nil {
		panic(err) // every member is plain JSON
	}
	return string(data)
}

// Equal reports whether recording o over b would say nothing new.
func (b *Base) Equal(o *Base) bool {
	if b == nil || o == nil {
		return b == o
	}
	return b.Marshal() == o.Marshal()
}

func (b *Base) clone() *Base {
	c := *b
	c.Fields = make(map[string]issue.Value, len(b.Fields))
	for k, v := range b.Fields {
		c.Fields[k] = v
	}
	if b.Comments != nil {
		c.Comments = make(map[string]string, len(b.Comments))
		for k, v := range b.Comments {
			c.Comments[k] = v
		}
	}
	c.Retry = append([]string(nil), b.Retry...)
	return &c
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

// Digest is the one text comparison (JS11). The normalisation is the one
// TextToADF preserves, so two texts Jira cannot tell apart share a digest.
func Digest(text string) string {
	sum := sha256.Sum256([]byte("v1\n" + jiraapi.NormalizeText(text)))
	return "v1:" + hex.EncodeToString(sum[:])
}

// canon is a value as compared: compacted JSON, absent and null alike.
func canon(v issue.Value) issue.Value {
	if len(v) == 0 {
		return issue.Value("null")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return v
	}
	return buf.Bytes()
}

func same(a, b issue.Value) bool { return bytes.Equal(canon(a), canon(b)) }
