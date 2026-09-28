package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
)

// Mapping is the seam between the mapping and the engine (JS7, JS17):
// compiled once per run by Compile, pure, and in local terms on its
// local side, so that "did it change" is byte equality.
type Mapping struct {
	project, projectId string
	types              map[string]*typeMap
	byIssueType        map[string]string // Jira issue type id -> type key
	request            []string
}

type typeMap struct {
	key, issueType string
	fields         map[string]*fieldMap // mapped keys, title and type excepted
	keys           []string             // every mapped key, title and type first
	status         string               // the key aliased `status`
	screen         map[string]bool      // Jira field ids on the create screen
}

type fieldMap struct {
	key, ref string
	kind     schema.Kind
	field    *schema.Field
	jira     jiraapi.FieldSchema // a custom field's
	link     string              // link type id, for link:<id>
	toJira   map[string]string   // value id -> Jira id
	fromJira map[string]string
	excluded map[string]bool // Norm of the names of values aliased "": never synced
}

// NewIssue is the body of POST /issue: jiraapi.Client.CreateIssue's arguments.
type NewIssue struct {
	Fields     map[string]any
	Properties []jiraapi.Property
}

// maxSummary is Jira's limit on a summary, in runes.
const maxSummary = 255

// Request is the fields= of every GET /issue.
func (m *Mapping) Request() []string { return slices.Clone(m.request) }

// LocalType is the type an issue type maps to; an unmapped one is skipped.
func (m *Mapping) LocalType(issueTypeId string) (string, bool) {
	k, ok := m.byIssueType[issueTypeId]
	return k, ok
}

// IssueType is the issue type a type maps to; a local-only one is never exported.
func (m *Mapping) IssueType(typeKey string) (string, bool) {
	if tm, ok := m.types[typeKey]; ok {
		return tm.issueType, true
	}
	return "", false
}

// Keys lists the mapped keys of a type, title and type included.
func (m *Mapping) Keys(typeKey string) []string {
	if tm, ok := m.types[typeKey]; ok {
		return slices.Clone(tm.keys)
	}
	return nil
}

// Multi says a mapped key merges item-wise (JS10).
func (m *Mapping) Multi(typeKey, key string) bool {
	if tm, ok := m.types[typeKey]; ok {
		if fm, ok := tm.fields[key]; ok {
			return fm.kind.IsMulti()
		}
	}
	return false
}

// Canceled is the status a Gone issue is set to (JS19): the first value in
// the canceled category of its type's status field.
func (m *Mapping) Canceled(typeKey string) (issue.Value, bool) {
	tm, ok := m.types[typeKey]
	if !ok || tm.status == "" {
		return nil, false
	}
	vs := tm.fields[tm.status].field.ValuesInCategory(schema.CategoryCanceled)
	if len(vs) == 0 {
		return nil, false
	}
	return issue.StringValue(vs[0].Id), true
}

// Users are the accounts an issue and its comments name, in order of first
// appearance, for the engine to ensure as identities before it converts (JS16).
func (m *Mapping) Users(ri *jiraapi.Issue, cs []jiraapi.Comment) []jiraapi.User {
	var out []jiraapi.User
	seen := map[string]bool{}
	add := func(u *jiraapi.User) {
		if u != nil && u.AccountID != "" && !seen[u.AccountID] {
			seen[u.AccountID] = true
			out = append(out, *u)
		}
	}
	for _, id := range []string{"reporter", "creator", refAssignee} {
		var u jiraapi.User
		if ok, _ := ri.Decode(id, &u); ok {
			add(&u)
		}
	}
	var it jiraapi.IssueType
	if ok, _ := ri.Decode("issuetype", &it); ok {
		if tm, ok := m.types[m.byIssueType[it.ID]]; ok {
			for _, key := range tm.keys[2:] {
				if fm := tm.fields[key]; fm.kind == schema.KindIdentity && fm.ref != refAssignee {
					var u jiraapi.User
					if ok, _ := ri.Decode(fm.ref, &u); ok {
						add(&u)
					}
				}
			}
		}
	}
	for _, c := range cs {
		add(c.Author)
		add(c.UpdateAuthor)
	}
	return out
}

// Local is the snapshot in local terms for typeKey's mapped keys (JS1):
// values compacted, sets sorted, an unset scalar null and an unset set [].
// The type is the snapshot's own, so that a local type change shows.
func (m *Mapping) Local(snap *issue.Snapshot, typeKey string) Doc {
	doc := Doc{Type: typeKey, Fields: map[string]issue.Value{}}
	if tm, ok := m.types[typeKey]; ok {
		for _, key := range tm.keys {
			doc.Fields[key] = canonical(snap.Fields[key], m.Multi(typeKey, key))
		}
	}
	if len(snap.Comments) == 0 {
		return doc
	}
	doc.Body = Text{Text: snap.Comments[0].Message, Lossless: true}
	for _, c := range snap.Comments[1:] {
		lc := Comment{Op: c.TargetId(), Text: Text{Text: c.Message, Lossless: true}}
		if c.Author != nil {
			lc.Author = c.Author.Id().String()
		}
		for _, op := range snap.Operations {
			switch {
			case op.Id() == c.TargetId():
				lc.At = op.Time()
				lc.JiraId, _ = op.GetMetadata(MetaCommentId)
				_, lc.Note = op.GetMetadata(MetaNote)
			case isEditOf(op, c.TargetId()):
				lc.Edited, lc.Editor = op.Time(), op.Author().Id().String()
			}
		}
		doc.Comments = append(doc.Comments, lc)
	}
	return doc
}

func isEditOf(op dag.Operation, target entity.Id) bool {
	edit, ok := op.(*issue.EditCommentOperation)
	return ok && edit.Target == target
}

// canonical is a stored value as the merge compares it (JS7).
func canonical(v issue.Value, multi bool) issue.Value {
	if multi {
		items, _ := issue.Items(v)
		return sortedItems(items)
	}
	if v == nil || issue.IsNull(v) {
		return issue.Value("null")
	}
	var buf bytes.Buffer
	if json.Compact(&buf, v) != nil {
		return v
	}
	return buf.Bytes()
}

// sortedItems is a set by compacted bytes, the order AddValue keeps; never null.
func sortedItems(items []issue.Value) issue.Value {
	cs := make([]issue.Value, 0, len(items))
	for _, it := range items {
		cs = append(cs, canonical(it, false))
	}
	slices.SortFunc(cs, func(a, b issue.Value) int { return bytes.Compare(a, b) })
	cs = slices.CompactFunc(cs, func(a, b issue.Value) bool { return bytes.Equal(a, b) })
	return issue.ItemsValue(cs)
}

var null = issue.Value("null")

// FromJira converts one issue and its comments (JS7, JS11, JS17): canonical
// values, texts through ADFToText, out-of-project relations dropped, and
// what cannot convert now in Skip. An issue of an unmapped type has no Type
// and no fields.
func (m *Mapping) FromJira(ri *jiraapi.Issue, cs []jiraapi.Comment, ix *Index) Doc {
	doc := Doc{Id: ri.ID, Key: ri.Key, Fields: map[string]issue.Value{}}
	var (
		summary   string
		updated   jiraapi.Time
		issueType struct{ ID string }
	)
	_ = json.Unmarshal(ri.Fields["summary"], &summary)
	_ = json.Unmarshal(ri.Fields["updated"], &updated)
	_ = json.Unmarshal(ri.Fields["issuetype"], &issueType)
	doc.Updated = updated.UTC()
	tm, ok := m.types[m.byIssueType[issueType.ID]]
	if !ok {
		return doc
	}
	doc.Type = tm.key
	doc.Fields[schema.TitleKey] = issue.StringValue(summary)
	doc.Fields[schema.TypeKey] = issue.StringValue(tm.key)
	text, lossless := jiraapi.ADFToText(ri.Fields["description"])
	doc.Body = Text{Text: text, Lossless: lossless}

	for _, key := range tm.keys[2:] {
		fm := tm.fields[key]
		v, skip, drop := m.fromJira(ri, fm, ix)
		switch {
		case drop:
		case skip != nil:
			doc.Skip = append(doc.Skip, *skip)
		default:
			doc.Fields[key] = v
		}
	}

	for _, c := range cs {
		rc := Comment{JiraId: c.ID, At: c.Created.UTC()}
		rc.Text.Text, rc.Text.Lossless = jiraapi.ADFToText(c.Body)
		if raw, ok := c.Property(PropertyKey); ok {
			var p struct {
				Op entity.Id `json:"op"`
			}
			if json.Unmarshal(raw, &p) == nil {
				rc.Op = p.Op
			}
		}
		if c.Author != nil {
			rc.Author = c.Author.AccountID
		}
		if c.Updated.After(c.Created.Time) {
			rc.Edited = c.Updated.UTC()
			if c.UpdateAuthor != nil {
				rc.Editor = c.UpdateAuthor.AccountID
			}
		}
		doc.Comments = append(doc.Comments, rc)
	}
	return doc
}

// fromJira converts one field. drop is an out-of-project relation, which
// the merge must not see at all (JS17).
func (m *Mapping) fromJira(ri *jiraapi.Issue, fm *fieldMap, ix *Index) (v issue.Value, skip *Skip, drop bool) {
	retry := func(format string, args ...any) (issue.Value, *Skip, bool) {
		return nil, &Skip{Key: fm.key, Reason: fmt.Sprintf(format, args...), Retry: true}, false
	}
	fail := func(err error) (issue.Value, *Skip, bool) {
		return nil, &Skip{Key: fm.key, Reason: err.Error()}, false
	}
	enum := func(id, name string) (issue.Value, *Skip, bool) {
		if local, ok := fm.fromJira[id]; ok {
			return issue.StringValue(local), nil, false
		}
		if n := Norm(name); n != "" && fm.excluded[n] {
			return nil, &Skip{Key: fm.key, Reason: fmt.Sprintf("Jira %s %q is excluded from sync", fm.ref, name)}, false
		}
		return retry("Jira %s %q (%s) is not a value of %s", fm.ref, name, id, fm.key)
	}
	user := func(u *jiraapi.User) (issue.Value, *Skip, bool) {
		if u == nil {
			return null, nil, false
		}
		if id, ok := ix.User(u.AccountID); ok {
			return issue.StringValue(id.String()), nil, false
		}
		return retry("no identity for Jira account %s yet", u.AccountID)
	}

	if fm.link != "" {
		var links []jiraapi.IssueLink
		if _, err := ri.Decode("issuelinks", &links); err != nil {
			return fail(err)
		}
		var items []issue.Value
		for _, l := range links {
			// the relation is stored on the source: viewing A, {outwardIssue: B} is A → B (C1)
			if l.Type.ID != fm.link || l.OutwardIssue == nil || !m.syncable(*l.OutwardIssue, ix) {
				continue
			}
			id, ok := ix.Issue(l.OutwardIssue.ID)
			if !ok {
				return retry("%s is not imported yet", l.OutwardIssue.Key)
			}
			items = append(items, issue.StringValue(id.String()))
		}
		return sortedItems(items), nil, false
	}

	raw, ok := ri.Fields[fm.ref]
	if !ok || issue.IsNull(raw) {
		if fm.kind.IsMulti() {
			return issue.Value("[]"), nil, false
		}
		return null, nil, false
	}
	decode := func(into any) error {
		if err := json.Unmarshal(raw, into); err != nil {
			return fmt.Errorf("Jira %s: %v", fm.ref, err)
		}
		return nil
	}

	switch fm.kind {
	case schema.KindEnum, schema.KindOrdinalEnum:
		var o struct{ ID, Name, Value string }
		if err := decode(&o); err != nil {
			return fail(err)
		}
		return enum(o.ID, orElse(o.Name, o.Value))
	case schema.KindMultiEnum:
		if fm.ref == refLabels {
			var labels []string
			if err := decode(&labels); err != nil {
				return fail(err)
			}
			items := make([]issue.Value, len(labels))
			for i, l := range labels {
				items[i] = issue.StringValue(l)
			}
			return sortedItems(items), nil, false
		}
		var os []struct{ ID, Value string }
		if err := decode(&os); err != nil {
			return fail(err)
		}
		var items []issue.Value
		for _, o := range os {
			v, skip, _ := enum(o.ID, o.Value)
			if skip != nil {
				return nil, skip, false
			}
			items = append(items, v)
		}
		return sortedItems(items), nil, false
	case schema.KindIdentity:
		var u jiraapi.User
		if err := decode(&u); err != nil {
			return fail(err)
		}
		return user(&u)
	case schema.KindRelation:
		var ref jiraapi.IssueRef
		if err := decode(&ref); err != nil {
			return fail(err)
		}
		if !m.syncable(ref, ix) {
			return nil, nil, true
		}
		if id, ok := ix.Issue(ref.ID); ok {
			return issue.StringValue(id.String()), nil, false
		}
		return retry("%s is not imported yet", ref.Key)
	case schema.KindNumber:
		var f float64
		if err := decode(&f); err != nil {
			return fail(err)
		}
		return issue.Value(strconv.FormatFloat(f, 'f', -1, 64)), nil, false
	case schema.KindDate:
		var s string
		if err := decode(&s); err != nil {
			return fail(err)
		}
		if !fm.isDatetime() {
			return issue.StringValue(s), nil, false
		}
		t, err := jiraapi.ParseTime(s)
		if err != nil {
			return fail(fmt.Errorf("Jira %s: %v", fm.ref, err))
		}
		return issue.StringValue(t.UTC().Format(time.RFC3339)), nil, false
	case schema.KindText:
		var s string
		if err := decode(&s); err != nil {
			return fail(err)
		}
		return issue.StringValue(s), nil, false
	}
	return fail(fmt.Errorf("kind %s does not sync", fm.kind))
}

// syncable says a related issue can be, or is, a local issue: one of
// another project, or of an issue type the schema does not map or excludes,
// never imports, and a relation to it is dropped (JS17).
func (m *Mapping) syncable(ref jiraapi.IssueRef, ix *Index) bool {
	if _, ok := ix.Issue(ref.ID); ok {
		return true
	}
	if prefix, _, _ := strings.Cut(ref.Key, "-"); ref.Key != "" && prefix != m.project {
		return false
	}
	if ref.Fields != nil && ref.Fields.IssueType != nil {
		_, mapped := m.byIssueType[ref.Fields.IssueType.ID]
		return mapped
	}
	return true
}

// ToJira turns merged changes into writes, one per local key; remote gives
// the link ids a removal deletes. A value Jira cannot hold is a Skip
// (JS17), retried only when its target is about to be exported.
func (m *Mapping) ToJira(typeKey string, ch []Change, remote *jiraapi.Issue, ix *Index) ([]Write, []Skip) {
	tm, ok := m.types[typeKey]
	if !ok {
		var skips []Skip
		for _, c := range ch {
			skips = append(skips, Skip{Key: c.Key, Reason: "type " + typeKey + " is local-only"})
		}
		return nil, skips
	}
	var writes []Write
	var skips []Skip
	for _, c := range ch {
		w, skip := m.toJira(tm, c, remote, ix)
		if skip != nil {
			skips = append(skips, *skip)
		}
		if w.Key != "" {
			writes = append(writes, w) // a link write can go with a skip of some items
		}
	}
	return writes, skips
}

func (m *Mapping) toJira(tm *typeMap, c Change, remote *jiraapi.Issue, ix *Index) (Write, *Skip) {
	no := func(retry bool, format string, args ...any) (Write, *Skip) {
		return Write{}, &Skip{Key: c.Key, Reason: fmt.Sprintf(format, args...), Retry: retry}
	}
	edit := func(field string, v any) (Write, *Skip) {
		return Write{Key: c.Key, Kind: WriteEdit, Field: field, Set: mustJSON(v)}, nil
	}

	switch c.Key {
	case schema.TitleKey:
		s, _ := issue.String(c.Set)
		switch n := utf8.RuneCountInString(s); {
		case strings.TrimSpace(s) == "":
			return no(false, "Jira requires a summary")
		case n > maxSummary:
			return no(false, "a summary is at most %d characters in Jira, this one is %d", maxSummary, n)
		}
		return edit("summary", s)
	case schema.TypeKey:
		return no(false, "change the type in Jira (JS18)")
	}

	fm, ok := tm.fields[c.Key]
	if !ok {
		return no(false, "%s/%s is local-only", tm.key, c.Key)
	}

	if fm.kind.IsMulti() {
		return m.toJiraItems(fm, c, remote, ix)
	}

	if issue.IsNull(c.Set) || c.Set == nil {
		switch fm.ref {
		case refStatus:
			return no(false, "Jira has no issue without a status")
		case refParent:
			return Write{Key: c.Key, Kind: WriteEdit, Field: refParent,
				Update: mustJSON([]jiraapi.Op{jiraapi.OpSet(map[string]bool{"none": true})})}, nil
		}
		return Write{Key: c.Key, Kind: WriteEdit, Field: fm.ref, Set: null}, nil
	}

	switch fm.kind {
	case schema.KindEnum, schema.KindOrdinalEnum:
		id, _ := issue.String(c.Set)
		jid, ok := fm.toJira[id]
		if !ok {
			return no(false, "%s is local-only: Jira has no %s for it", id, fm.ref)
		}
		if fm.ref == refStatus {
			return Write{Key: c.Key, Kind: WriteTransition, Status: jid}, nil
		}
		return edit(fm.ref, map[string]string{"id": jid})
	case schema.KindIdentity:
		id, _ := issue.String(c.Set)
		account, ok := ix.Account(entity.Id(id))
		if !ok {
			return no(false, "identity %s has no Jira account", entity.Id(id).Human())
		}
		return edit(fm.ref, map[string]string{"accountId": account})
	case schema.KindRelation:
		id, _ := issue.String(c.Set)
		jid, ok := ix.JiraIssue(entity.Id(id))
		if !ok {
			return no(ix.WillExport(entity.Id(id)), "%s is not in Jira", entity.Id(id).Human())
		}
		return edit(fm.ref, map[string]string{"id": jid})
	case schema.KindNumber:
		var f float64
		if json.Unmarshal(c.Set, &f) != nil {
			return no(false, "%s is not a number", c.Set)
		}
		return edit(fm.ref, f)
	case schema.KindDate:
		s, _ := issue.String(c.Set)
		if !fm.isDatetime() {
			t, err := time.Parse(jiraapi.DateLayout, s)
			if err != nil {
				return no(false, "%q is not a date Jira takes (%s)", s, jiraapi.DateLayout)
			}
			return edit(fm.ref, t.Format(jiraapi.DateLayout))
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return no(false, "%q is not an RFC 3339 date-time", s)
		}
		return edit(fm.ref, t.UTC().Format("2006-01-02T15:04:05.000-0700"))
	case schema.KindText:
		s, _ := issue.String(c.Set)
		return edit(fm.ref, s)
	}
	return no(false, "kind %s does not sync", fm.kind)
}

// toJiraItems writes a set's added and removed items (JS10): labels and
// multi-selects by update, links by POST and DELETE /issueLink.
func (m *Mapping) toJiraItems(fm *fieldMap, c Change, remote *jiraapi.Issue, ix *Index) (Write, *Skip) {
	no := func(retry bool, format string, args ...any) (Write, *Skip) {
		return Write{}, &Skip{Key: c.Key, Reason: fmt.Sprintf(format, args...), Retry: retry}
	}
	if fm.link != "" {
		if remote == nil {
			return no(true, "links are written once the issue is in Jira")
		}
		var links []jiraapi.IssueLink
		_, _ = remote.Decode("issuelinks", &links)
		w := Write{Key: c.Key, Kind: WriteLink}
		var missing []string
		for _, item := range c.Add {
			id, _ := issue.String(item)
			jid, ok := ix.JiraIssue(entity.Id(id))
			if !ok {
				missing = append(missing, entity.Id(id).Human())
				continue
			}
			// POST /issueLink's inwardIssue is the source (C1)
			w.Add = append(w.Add, NewLink{LinkType: fm.link, Source: remote.ID, Destination: jid})
		}
		for _, item := range c.Remove {
			id, _ := issue.String(item)
			jid, _ := ix.JiraIssue(entity.Id(id))
			for _, l := range links {
				if l.Type.ID == fm.link && l.OutwardIssue != nil && l.OutwardIssue.ID == jid {
					w.Remove = append(w.Remove, l.ID)
				}
			}
		}
		var skip *Skip
		if len(missing) > 0 {
			skip = &Skip{Key: c.Key, Reason: strings.Join(missing, ", ") + " not in Jira"}
		}
		if len(w.Add)+len(w.Remove) == 0 {
			w = Write{}
		}
		return w, skip
	}

	var ops []jiraapi.Op
	item := func(v issue.Value) (any, *Skip) {
		s, _ := issue.String(v)
		if fm.ref == refLabels {
			if s == "" || strings.ContainsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }) {
				return nil, &Skip{Key: c.Key, Reason: fmt.Sprintf("Jira labels have no spaces: %q", s)}
			}
			return s, nil
		}
		jid, ok := fm.toJira[s]
		if !ok {
			return nil, &Skip{Key: c.Key, Reason: fmt.Sprintf("%s is local-only: Jira has no option for it", s)}
		}
		return map[string]string{"id": jid}, nil
	}
	for _, v := range c.Add {
		x, skip := item(v)
		if skip != nil {
			return Write{}, skip
		}
		ops = append(ops, jiraapi.OpAdd(x))
	}
	for _, v := range c.Remove {
		x, skip := item(v)
		if skip != nil {
			return Write{}, skip
		}
		ops = append(ops, jiraapi.OpRemove(x))
	}
	return Write{Key: c.Key, Kind: WriteEdit, Field: fm.ref, Update: mustJSON(ops)}, nil
}

// Create is the POST /issue body for a local issue and the keys it carries,
// which make JS15's create base; the description always goes with it.
// Status and links cannot be set by a create and are written after it.
func (m *Mapping) Create(local Doc, id entity.Id, ix *Index) (NewIssue, []string, []Skip) {
	body := NewIssue{
		Fields:     map[string]any{},
		Properties: []jiraapi.Property{{Key: PropertyKey, Value: map[string]string{"id": id.String()}}},
	}
	tm, ok := m.types[local.Type]
	if !ok {
		return body, nil, []Skip{{Key: schema.TypeKey, Reason: "type " + local.Type + " is local-only"}}
	}
	body.Fields["project"] = map[string]string{"id": m.projectId}
	body.Fields["issuetype"] = map[string]string{"id": tm.issueType}
	if local.Body.Text != "" {
		body.Fields["description"] = jiraapi.TextToADF(local.Body.Text)
	}
	keys := []string{schema.TypeKey}
	var skips []Skip

	for _, key := range tm.keys {
		v, ok := local.Fields[key]
		if key == schema.TypeKey || !ok || issue.IsNull(v) || string(v) == "[]" {
			continue
		}
		fm := tm.fields[key]
		if fm != nil && (fm.ref == refStatus || fm.link != "" || !tm.screen[fm.ref]) {
			continue
		}
		c := Change{Key: key, Set: v}
		if fm != nil && fm.kind.IsMulti() {
			items, _ := issue.Items(v)
			c = Change{Key: key, Add: items}
		}
		w, skip := m.toJira(tm, c, nil, ix)
		if skip != nil {
			skips = append(skips, *skip)
			continue
		}
		if w.Set != nil {
			body.Fields[w.Field] = w.Set
		} else {
			body.Fields[w.Field] = createItems(fm, v)
		}
		keys = append(keys, key)
	}
	return body, keys, skips
}

// createItems is a set as a create states it: whole, not as updates.
func createItems(fm *fieldMap, v issue.Value) any {
	items, _ := issue.Items(v)
	if fm.ref == refLabels {
		labels := make([]string, 0, len(items))
		for _, it := range items {
			s, _ := issue.String(it)
			labels = append(labels, s)
		}
		return labels
	}
	var out []map[string]string
	for _, it := range items {
		s, _ := issue.String(it)
		out = append(out, map[string]string{"id": fm.toJira[s]})
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
