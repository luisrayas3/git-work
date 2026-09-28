package jira

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/repository"
)

var t0 = time.Date(2026, 9, 28, 14, 3, 0, 0, time.UTC)

func str(s string) issue.Value { return issue.StringValue(s) }

func set(items ...string) issue.Value {
	vs := make([]issue.Value, len(items))
	for i, s := range items {
		vs[i] = str(s)
	}
	return issue.ItemsValue(vs)
}

func isMulti(k string) bool { return k == "labels" }

// doc is a document holding the given fields and body, lossless.
func doc(body string, kv ...any) Doc {
	d := Doc{Id: "10001", Key: "PROJ-1", Updated: t0, Type: "task", Fields: map[string]issue.Value{BodyKey: str(body)}}
	for i := 0; i < len(kv); i += 2 {
		d.Fields[kv[i].(string)] = kv[i+1].(issue.Value)
	}
	return d
}

func base(body string, kv ...any) *Base {
	d := doc(body, kv...)
	d.Fields[BodyKey] = form(BodyKey, str(body))
	return &Base{V: 1, Id: d.Id, Key: d.Key, Updated: t0.Add(-time.Hour), Fields: d.Fields}
}

func TestMergeScalars(t *testing.T) {
	cases := []struct {
		name     string
		b        *Base
		l, r     issue.Value
		local    issue.Value // LocalSet value, nil for none
		remote   issue.Value // Remote Set, nil for none
		conflict bool
		base     issue.Value // nil: unchanged
	}{
		{name: "M1 Jira-only edit", b: base("", "status", str("to-do")), l: str("to-do"), r: str("done"), local: str("done"), base: str("done")},
		{name: "M2 local-only edit", b: base("", "status", str("to-do")), l: str("done"), r: str("to-do"), remote: str("done")},
		{name: "M3 double edit", b: base("", "status", str("to-do")), l: str("in-review"), r: str("done"), local: str("done"), conflict: true, base: str("done")},
		{name: "M3 local clear against Jira edit", b: base("", "status", str("to-do")), l: issue.Value("null"), r: str("done"), local: str("done"), conflict: true, base: str("done")},
		{name: "M4 same edit", b: base("", "status", str("to-do")), l: str("done"), r: str("done"), base: str("done")},
		{name: "M6 no base, local null", b: base(""), l: issue.Value("null"), r: str("high"), local: str("high"), base: str("high")},
		{name: "M6 no base, absent locally", b: base(""), l: nil, r: str("high"), local: str("high"), base: str("high")},
		{name: "M6 no base, differs", b: base(""), l: str("low"), r: str("high"), local: str("high"), conflict: true, base: str("high")},
		{name: "M6 no base, converged", b: base(""), l: str("high"), r: str("high"), base: str("high")},
		{name: "M6 nil base", b: nil, l: str("low"), r: str("high"), local: str("high"), conflict: true, base: str("high")},
		{name: "canonical JSON compares", b: base("", "estimate", issue.Value(`3`)), l: issue.Value(` 3`), r: issue.Value(`3`), base: issue.Value(`3`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key := "status"
			if c.b == nil || len(c.b.Fields) == 0 {
				key = "priority"
			} else if _, ok := c.b.Fields["estimate"]; ok {
				key = "estimate"
			}
			l, r := doc(""), doc("", key, c.r)
			if c.l != nil {
				l.Fields[key] = c.l
			}
			p := merge(c.b, l, r, isMulti, true)

			var gotLocal, gotRemote issue.Value
			for _, lc := range p.Local {
				require.Equal(t, localSet, lc.Kind)
				require.Equal(t, "", lc.Author, "field imports are the runner's")
				require.Equal(t, t0, lc.At, "at Jira's updated")
				gotLocal = lc.Value
			}
			for _, ch := range p.Remote {
				gotRemote = ch.Set
			}
			require.Equal(t, string(canonOrNil(c.local)), string(canonOrNil(gotLocal)))
			require.Equal(t, string(canonOrNil(c.remote)), string(canonOrNil(gotRemote)))
			require.Equal(t, c.conflict, len(p.Conflicts) == 1, "%v", p.Conflicts)
			require.Equal(t, "10001", p.Base.Id)
			require.Equal(t, t0, p.Base.Updated)
			if c.base != nil {
				require.JSONEq(t, string(c.base), string(p.Base.Fields[key]))
			} else if c.b != nil {
				require.Equal(t, string(c.b.Fields[key]), string(p.Base.Fields[key]))
			}
			require.Empty(t, p.Pending)
		})
	}
}

func canonOrNil(v issue.Value) issue.Value {
	if v == nil {
		return nil
	}
	return canon(v)
}

// I1: a merge of three equal documents emits nothing.
func TestMergeFixpoint(t *testing.T) {
	b := base("the body", "title", str("T"), "status", str("done"), "labels", set("a", "b"))
	b.Comments = map[string]string{"100": digest("hi")}
	d := doc("the body", "title", str("T"), "status", str("done"), "labels", set("b", "a"))
	l, r := d, d
	l.Comments = []docComment{{JiraId: "100", Op: "op1", Text: docText{"hi", true}}}
	r.Comments = []docComment{{JiraId: "100", Text: docText{"hi", true}}}
	p := merge(b, l, r, isMulti, true)
	require.Empty(t, p.Local)
	require.Empty(t, p.Remote)
	require.Empty(t, p.Comments)
	require.Empty(t, p.Conflicts)
	require.Empty(t, p.Pending)

	// and recording its base again changes nothing
	again := merge(&p.Base, l, r, isMulti, true).Base
	require.True(t, p.Base.equal(&again))
}

func TestMergeSets(t *testing.T) {
	// M5: one add and one remove on each side all survive
	b := base("", "labels", set("keep", "gone-local", "gone-jira"))
	l := doc("", "labels", set("keep", "gone-jira", "new-local"))
	r := doc("", "labels", set("keep", "gone-local", "new-jira"))
	p := merge(b, l, r, isMulti, true)
	require.Empty(t, p.Conflicts)
	var adds, removes []string
	for _, lc := range p.Local {
		s, _ := issue.String(lc.Value)
		if lc.Kind == localAdd {
			adds = append(adds, s)
		} else {
			require.Equal(t, localRemove, lc.Kind)
			removes = append(removes, s)
		}
	}
	require.Equal(t, []string{"new-jira"}, adds)
	require.Equal(t, []string{"gone-jira"}, removes)
	require.Len(t, p.Remote, 1)
	require.Equal(t, []issue.Value{str("new-local")}, p.Remote[0].Add)
	require.Equal(t, []issue.Value{str("gone-local")}, p.Remote[0].Remove)
	require.Equal(t, string(b.Fields["labels"]), string(p.Base.Fields["labels"]), "base waits for the write")

	// M6: a missing base is a union
	p = merge(base(""), doc("", "labels", set("a")), doc("", "labels", set("b")), isMulti, true)
	require.Len(t, p.Local, 1)
	require.Equal(t, localAdd, p.Local[0].Kind)
	require.Equal(t, []issue.Value{str("a")}, p.Remote[0].Add)

	// converged sets record the base
	p = merge(base(""), doc("", "labels", set("a")), doc("", "labels", set("a")), isMulti, true)
	require.Empty(t, p.Local)
	require.JSONEq(t, `["a"]`, string(p.Base.Fields["labels"]))
}

func TestMergeBody(t *testing.T) {
	// Jira edit imported
	p := merge(base("old"), doc("old"), doc("new"), isMulti, true)
	require.Len(t, p.Local, 1)
	require.Equal(t, localChange{Kind: localSet, Key: BodyKey, Value: str("new"), At: t0}, p.Local[0])
	require.Equal(t, form(BodyKey, str("new")), p.Base.Fields[BodyKey])

	// local edit exported
	p = merge(base("old"), doc("mine"), doc("old"), isMulti, true)
	require.Empty(t, p.Local)
	require.Equal(t, []change{{Key: BodyKey, Set: str("mine")}}, p.Remote)
	require.Equal(t, form(BodyKey, str("old")), p.Base.Fields[BodyKey])

	// M7: over a lossy Jira text, pending
	r := doc("old")
	r.Lossy = true
	p = merge(base("old"), doc("mine"), r, isMulti, true)
	require.Empty(t, p.Remote)
	require.Equal(t, []Skip{{Key: BodyKey, Reason: reasonLossy}}, p.Pending)

	// both: Jira wins, noted
	p = merge(base("old"), doc("mine"), doc("theirs"), isMulti, true)
	require.Len(t, p.Local, 1)
	require.Len(t, p.Conflicts, 1)
	require.Equal(t, BodyKey, p.Conflicts[0].Key)

	// normalisation alone is no change
	p = merge(base("a"), doc("a  \r\n\n"), doc("a"), isMulti, true)
	require.Empty(t, p.Local)
	require.Empty(t, p.Remote)
}

func TestMergeSkipAndExport(t *testing.T) {
	// M8: a remote Skip leaves the key alone and retries it
	b := base("", "parent", str("x"))
	l := doc("", "parent", str("y"))
	r := doc("")
	r.Skip = []Skip{{Key: "parent", Reason: "not imported yet", Retry: true}}
	p := merge(b, l, r, isMulti, true)
	require.Empty(t, p.Local)
	require.Empty(t, p.Remote)
	require.Equal(t, r.Skip, p.Pending)
	require.Equal(t, []string{"parent"}, p.Base.Retry)
	require.Equal(t, string(str("x")), string(p.Base.Fields["parent"]))

	// M9: export=false makes every local change pending
	b = base("old", "status", str("to-do"), "labels", set())
	l = doc("mine", "status", str("done"), "labels", set("a"))
	l.Comments = []docComment{{Op: "op1", Text: docText{"new", true}}}
	r = doc("old", "status", str("to-do"), "labels", set())
	p = merge(b, l, r, isMulti, false)
	require.Empty(t, p.Remote)
	require.Empty(t, p.Comments)
	require.Empty(t, p.Local)
	var keys []string
	for _, s := range p.Pending {
		keys = append(keys, s.Key)
	}
	require.ElementsMatch(t, []string{"status", "labels", BodyKey, commentKey + ":op1"}, keys)
}

// M11: Jira normalised our write back to what it held; the second merge
// imports it, and the next run is quiet.
func TestMergeNormalisedWrite(t *testing.T) {
	b := base("", "title", str("T"))
	// we wrote "T  x" (step 6: B'[title] = written); Jira trimmed it to "T x"
	b.Fields["title"] = str("T  x")
	l, r := doc("", "title", str("T  x")), doc("", "title", str("T x"))
	p := merge(b, l, r, isMulti, false)
	require.Len(t, p.Local, 1)
	require.Empty(t, p.Pending)
	require.Empty(t, p.Conflicts)
	l.Fields["title"] = str("T x")
	p = merge(&p.Base, l, r, isMulti, true)
	require.Empty(t, p.Local)
	require.Empty(t, p.Remote)
}

func TestMergeType(t *testing.T) {
	// M12: a Jira type change imports, first
	b := base("", "type", str("task"), "status", str("to-do"))
	p := merge(b, doc("", "type", str("task"), "status", str("to-do")), doc("", "type", str("bug"), "status", str("done")), isMulti, true)
	require.Len(t, p.Local, 2)
	require.Equal(t, "type", p.Local[0].Key)

	// a local type change is pending, never exported
	p = merge(b, doc("", "type", str("bug"), "status", str("to-do")), doc("", "type", str("task"), "status", str("to-do")), isMulti, true)
	require.Empty(t, p.Remote)
	require.Equal(t, []Skip{{Key: "type", Reason: reasonTypeChange}}, p.Pending)
}

func TestMergeComments(t *testing.T) {
	c := func(jiraId string, op entity.Id, text string) docComment {
		return docComment{JiraId: jiraId, Op: op, Author: "acc", Editor: "ed", At: t0, Edited: t0.Add(time.Minute), Text: docText{text, true}}
	}
	b := base("")
	b.Comments = map[string]string{"1": digest("one"), "2": digest("two"), "3": digest("three"), "4": digest("four"), "5": digest("five"), "6": ""}
	l, r := doc(""), doc("")
	l.Comments = []docComment{
		c("1", "op1", "one"),         // Jira edit → imported
		c("2", "op2", "two, mine"),   // local edit → exported
		c("3", "op3", "three, mine"), // both → Jira's, conflict
		c("4", "op4", "four"),        // deleted in Jira → tombstone
		c("5", "op5", "five, mine"),  // deleted in Jira after a local edit → tombstone, conflict
		c("6", "op6", "six, edited"), // deleted before, edited since → pending
		c("", "op7", "new local"),    // → exported
		c("", "op8", "exported, crashed"),
		{Op: "op9", Text: docText{"a note", true}, Note: true}, // never exported
	}
	r.Comments = []docComment{
		c("1", "", "one, theirs"),
		c("2", "", "two"),
		c("3", "", "three, theirs"),
		c("10", "", "new in Jira"),
		c("11", "op8", "exported, crashed"),
	}
	p := merge(b, l, r, isMulti, true)

	byOp := map[entity.Id]localChange{}
	var added []localChange
	for _, lc := range p.Local {
		if lc.Kind == localAddComment {
			added = append(added, lc)
		} else {
			byOp[lc.Op] = lc
		}
	}
	require.Equal(t, localEditComment, byOp["op1"].Kind)
	require.Equal(t, "one, theirs", byOp["op1"].Text)
	require.Equal(t, "ed", byOp["op1"].Author)
	require.Equal(t, localEditComment, byOp["op3"].Kind)
	require.Equal(t, tombstone(t0), byOp["op4"].Text)
	require.Equal(t, tombstone(t0), byOp["op5"].Text)
	require.Equal(t, localPairComment, byOp["op8"].Kind)
	require.Equal(t, "11", byOp["op8"].JiraId)
	require.NotContains(t, byOp, entity.Id("op2"))
	require.NotContains(t, byOp, entity.Id("op6"))
	require.Len(t, added, 1)
	require.Equal(t, "10", added[0].JiraId)
	require.Equal(t, "acc", added[0].Author)
	require.Equal(t, t0, added[0].At)

	require.ElementsMatch(t, []commentWrite{{Op: "op2", JiraId: "2", Text: "two, mine"}, {Op: "op7", Text: "new local"}}, p.Comments)
	var conflicted []string
	for _, cf := range p.Conflicts {
		conflicted = append(conflicted, cf.Comment)
	}
	require.ElementsMatch(t, []string{"3", "5"}, conflicted)
	require.Equal(t, []Skip{{Key: commentKey + ":6", Reason: reasonDeleted}}, p.Pending)

	require.Equal(t, map[string]string{
		"1": digest("one, theirs"), "2": digest("two"), "3": digest("three, theirs"),
		"4": "", "5": "", "6": "", "10": digest("new in Jira"), "11": digest("exported, crashed"),
	}, p.Base.Comments)
}

// M13: the current base is the marker with the greatest Updated.
func TestCurrentBase(t *testing.T) {
	repo := repository.NewMockRepo()
	me, err := identity.NewIdentity(repo, "Runner", "runner@example.com")
	require.NoError(t, err)
	require.NoError(t, me.Commit(repo))

	i, _, err := issue.Create(me, t0.Unix(), "title", "", nil, nil, nil)
	require.NoError(t, err)
	unlinked := i.Compile()
	b, problems := CurrentBase(unlinked)
	require.Nil(t, b)
	require.Empty(t, problems)

	marker := func(updated time.Time, status string) string {
		return (&Base{V: 1, Updated: updated, Fields: map[string]issue.Value{"status": str(status)}}).marshal()
	}
	i, _, err = issue.Create(me, t0.Unix(), "title", "", nil, nil, map[string]string{
		MetaId: "10001", MetaSync: marker(t0, "first"),
	})
	require.NoError(t, err)
	b, _ = CurrentBase(i.Compile())
	require.Equal(t, "10001", b.Id)
	require.JSONEq(t, `"first"`, string(b.Fields["status"]))

	i.Append(issue.NewNoOpOp(me, t0.Unix(), map[string]string{MetaSync: marker(t0.Add(2*time.Hour), "newest")}))
	i.Append(issue.NewNoOpOp(me, t0.Unix(), map[string]string{MetaSync: marker(t0.Add(time.Hour), "older, later")}))
	i.Append(issue.NewNoOpOp(me, t0.Unix(), map[string]string{MetaSync: `{"v":1,`}))
	b, problems = CurrentBase(i.Compile())
	require.JSONEq(t, `"newest"`, string(b.Fields["status"]))
	require.Len(t, problems, 1)

	// Marshal round-trips
	var back Base
	require.NoError(t, json.Unmarshal([]byte(b.marshal()), &back))
	require.True(t, b.equal(&back))
}

func TestDigest(t *testing.T) {
	require.Equal(t, digest("a\nb"), digest("a  \r\nb\n\n"))
	require.NotEqual(t, digest("a"), digest("b"))
	require.Regexp(t, `^v1:[0-9a-f]{64}$`, digest(""))
}
