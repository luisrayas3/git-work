package jira

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
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
	required       map[string]bool      // Jira field ids a create must carry (no default)
}

type fieldMap struct {
	key, ref string
	kind     schema.Kind
	field    *schema.Field
	jira     jiraapi.FieldSchema // a custom field's
	link     string              // link type id, for link:<id>
	toJira   map[string]string   // value id -> Jira id
	fromJira map[string]string
	excluded map[string]bool // normName of the names of values aliased "": never synced
}

// newIssue is the body of POST /issue: jiraapi.Client.CreateIssue's arguments.
type newIssue struct {
	Fields     map[string]any
	Properties []jiraapi.Property
}

// maxSummary is Jira's limit on a summary, in runes.
const maxSummary = 255

// requestFields is the fields= of every GET /issue.
func (m *Mapping) requestFields() []string { return slices.Clone(m.request) }

// localType is the type an issue type maps to; an unmapped one is skipped.
func (m *Mapping) localType(issueTypeId string) (string, bool) {
	k, ok := m.byIssueType[issueTypeId]
	return k, ok
}

// issueType is the issue type a type maps to; a local-only one is never exported.
func (m *Mapping) issueType(typeKey string) (string, bool) {
	if tm, ok := m.types[typeKey]; ok {
		return tm.issueType, true
	}
	return "", false
}

// keys lists the mapped keys of a type, title and type included.
func (m *Mapping) keys(typeKey string) []string {
	if tm, ok := m.types[typeKey]; ok {
		return slices.Clone(tm.keys)
	}
	return nil
}

// multi says a mapped key merges item-wise (JS10).
func (m *Mapping) multi(typeKey, key string) bool {
	if tm, ok := m.types[typeKey]; ok {
		if fm, ok := tm.fields[key]; ok {
			return fm.kind.IsMulti()
		}
	}
	return false
}

// canceled is the status a Gone issue is set to (JS19): the first value in
// the canceled category of its type's status field.
func (m *Mapping) canceled(typeKey string) (key string, v issue.Value, ok bool) {
	tm, ok := m.types[typeKey]
	if !ok || tm.status == "" {
		return "", nil, false
	}
	vs := tm.fields[tm.status].field.ValuesInCategory(schema.CategoryCanceled)
	if len(vs) == 0 {
		return "", nil, false
	}
	return tm.status, issue.StringValue(vs[0].Id), true
}

// users are the accounts an issue and its comments name, in order of first
// appearance, for the engine to ensure as identities before it converts (JS16).
func (m *Mapping) users(ri *jiraapi.Issue, cs []jiraapi.Comment) []jiraapi.User {
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
	doc := Doc{Type: typeKey, Fields: map[string]issue.Value{BodyKey: issue.StringValue("")}}
	if tm, ok := m.types[typeKey]; ok {
		for _, key := range tm.keys {
			doc.Fields[key] = canonical(snap.Fields[key], m.multi(typeKey, key))
		}
	}
	if len(snap.Comments) == 0 {
		return doc
	}
	doc.Fields[BodyKey] = issue.StringValue(snap.Comments[0].Message)
	// one pass over the operations: each comment's creation and last edit
	byOp := map[entity.Id]*docComment{}
	at := func(id entity.Id) *docComment {
		if byOp[id] == nil {
			byOp[id] = &docComment{}
		}
		return byOp[id]
	}
	for _, op := range snap.Operations {
		if edit, ok := op.(*issue.EditCommentOperation); ok {
			c := at(edit.Target)
			c.Edited, c.Editor = op.Time(), op.Author().Id().String()
			continue
		}
		c := at(op.Id())
		c.At = op.Time()
		c.JiraId, _ = op.GetMetadata(MetaCommentId)
		_, c.Note = op.GetMetadata(MetaNote)
	}
	for _, sc := range snap.Comments[1:] {
		lc := *at(sc.TargetId())
		lc.Op, lc.Text = sc.TargetId(), docText{Text: sc.Message, Lossless: true}
		if sc.Author != nil {
			lc.Author = sc.Author.Id().String()
		}
		doc.Comments = append(doc.Comments, lc)
	}
	return doc
}

// fromIssue converts one issue and its comments (JS7, JS11, JS17): canonical
// values, texts through ADFToText, out-of-project relations dropped, and
// what cannot convert now in Skip. An issue of an unmapped type has no Type
// and no fields.
func (m *Mapping) fromIssue(ri *jiraapi.Issue, cs []jiraapi.Comment, ix *Index) Doc {
	doc := Doc{Id: ri.ID, Key: ri.Key, Fields: map[string]issue.Value{}}
	sf, _ := ri.System()
	doc.Updated, doc.Created = sf.Updated.UTC(), sf.Created.UTC()
	if sf.Reporter != nil {
		doc.Reporter = sf.Reporter.AccountID
	}
	if sf.Status != nil {
		doc.Status = sf.Status.Name
	}
	if sf.IssueType == nil {
		return doc
	}
	tm, ok := m.types[m.byIssueType[sf.IssueType.ID]]
	if !ok {
		return doc
	}
	doc.Type = tm.key
	doc.Fields[schema.TitleKey] = issue.StringValue(sf.Summary)
	doc.Fields[schema.TypeKey] = issue.StringValue(tm.key)
	text, lossless := jiraapi.ADFToText(ri.Fields["description"])
	doc.Fields[BodyKey], doc.Lossy = issue.StringValue(text), !lossless

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
		rc := docComment{JiraId: c.ID, At: c.Created.UTC()}
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
		if n := normName(name); n != "" && fm.excluded[n] {
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

// toWrites turns merged changes into writes, one per local key, the
// description included; remote gives the link ids a removal deletes. A value
// Jira cannot hold is a Skip (JS17): pending, and a candidate anyway because
// local differs from the base.
func (m *Mapping) toWrites(typeKey string, ch []change, remote *jiraapi.Issue, ix *Index) ([]jiraWrite, []Skip) {
	var writes []jiraWrite
	var skips []Skip
	tm := m.types[typeKey]
	for _, c := range ch {
		var w jiraWrite
		var skip *Skip
		switch {
		case c.Key == BodyKey:
			text, _ := issue.String(c.Set)
			w = jiraWrite{Key: BodyKey, Kind: writeEdit, Field: "description", Set: jiraapi.TextToADF(text)}
		case tm == nil:
			skip = &Skip{Key: c.Key, Reason: "type " + typeKey + " is local-only"}
		default:
			w, skip = m.toJira(tm, c, remote, ix)
		}
		if skip != nil {
			skips = append(skips, *skip)
		}
		if w.Key != "" {
			writes = append(writes, w) // a link write can go with a skip of some items
		}
	}
	return writes, skips
}

func (m *Mapping) toJira(tm *typeMap, c change, remote *jiraapi.Issue, ix *Index) (jiraWrite, *Skip) {
	no := func(format string, args ...any) (jiraWrite, *Skip) {
		return jiraWrite{}, &Skip{Key: c.Key, Reason: fmt.Sprintf(format, args...)}
	}
	edit := func(field string, v any) (jiraWrite, *Skip) {
		return jiraWrite{Key: c.Key, Kind: writeEdit, Field: field, Set: mustJSON(v)}, nil
	}

	switch c.Key {
	case schema.TitleKey:
		s, _ := issue.String(c.Set)
		switch n := utf8.RuneCountInString(s); {
		case strings.TrimSpace(s) == "":
			return no("Jira requires a summary")
		case n > maxSummary:
			return no("a summary is at most %d characters in Jira, this one is %d", maxSummary, n)
		}
		return edit("summary", s)
	case schema.TypeKey:
		return no("change the type in Jira (JS18)")
	}

	fm, ok := tm.fields[c.Key]
	if !ok {
		return no("%s/%s is local-only", tm.key, c.Key)
	}
	if fm.kind.IsMulti() {
		return m.toJiraItems(fm, c, remote, ix)
	}

	if issue.IsNull(canon(c.Set)) {
		switch fm.ref {
		case refStatus:
			return no("Jira has no issue without a status")
		case refParent:
			return jiraWrite{Key: c.Key, Kind: writeEdit, Field: refParent,
				Update: []jiraapi.Op{jiraapi.OpSet(map[string]bool{"none": true})}}, nil
		}
		return jiraWrite{Key: c.Key, Kind: writeEdit, Field: fm.ref, Set: null}, nil
	}

	switch fm.kind {
	case schema.KindEnum, schema.KindOrdinalEnum:
		id, _ := issue.String(c.Set)
		jid, ok := fm.toJira[id]
		if !ok {
			return no("%s is local-only: Jira has no %s for it", id, fm.ref)
		}
		if fm.ref == refStatus {
			return jiraWrite{Key: c.Key, Kind: writeTransition, Status: jid}, nil
		}
		return edit(fm.ref, map[string]string{"id": jid})
	case schema.KindIdentity:
		id, _ := issue.String(c.Set)
		account, ok := ix.Account(entity.Id(id))
		if !ok {
			return no("identity %s has no Jira account", entity.Id(id).Human())
		}
		return edit(fm.ref, map[string]string{"accountId": account})
	case schema.KindRelation:
		id, _ := issue.String(c.Set)
		jid, ok := ix.jiraIssue(entity.Id(id))
		if !ok {
			return no("%s is not in Jira", entity.Id(id).Human())
		}
		return edit(fm.ref, map[string]string{"id": jid})
	case schema.KindNumber:
		var f float64
		if json.Unmarshal(c.Set, &f) != nil {
			return no("%s is not a number", c.Set)
		}
		return edit(fm.ref, f)
	case schema.KindDate:
		s, _ := issue.String(c.Set)
		if !fm.isDatetime() {
			t, err := time.Parse(jiraapi.DateLayout, s)
			if err != nil {
				return no("%q is not a date Jira takes (%s)", s, jiraapi.DateLayout)
			}
			return edit(fm.ref, t.Format(jiraapi.DateLayout))
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return no("%q is not an RFC 3339 date-time", s)
		}
		return edit(fm.ref, t.UTC().Format("2006-01-02T15:04:05.000-0700"))
	case schema.KindText:
		s, _ := issue.String(c.Set)
		return edit(fm.ref, s)
	}
	return no("kind %s does not sync", fm.kind)
}

// toJiraItems writes a set's added and removed items (JS10): labels and
// multi-selects by update, links by POST and DELETE /issueLink. A link to an
// issue not in Jira is skipped alone; the other items are still written.
func (m *Mapping) toJiraItems(fm *fieldMap, c change, remote *jiraapi.Issue, ix *Index) (jiraWrite, *Skip) {
	if fm.link != "" {
		if remote == nil {
			return jiraWrite{}, &Skip{Key: c.Key, Reason: "links are written once the issue is in Jira"}
		}
		var links []jiraapi.IssueLink
		_, _ = remote.Decode("issuelinks", &links)
		w := jiraWrite{Key: c.Key, Kind: writeLink}
		var missing []string
		for _, item := range c.Add {
			id, _ := issue.String(item)
			jid, ok := ix.jiraIssue(entity.Id(id))
			if !ok {
				missing = append(missing, entity.Id(id).Human())
				continue
			}
			// POST /issueLink's inwardIssue is the source (C1)
			w.Add = append(w.Add, newLink{LinkType: fm.link, Source: remote.ID, Destination: jid})
		}
		for _, item := range c.Remove {
			id, _ := issue.String(item)
			jid, _ := ix.jiraIssue(entity.Id(id))
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
			w = jiraWrite{}
		}
		return w, skip
	}

	w := jiraWrite{Key: c.Key, Kind: writeEdit, Field: fm.ref}
	for i, v := range append(slices.Clone(c.Add), c.Remove...) {
		s, _ := issue.String(v)
		var x any = s
		if fm.ref == refLabels {
			if s == "" || strings.ContainsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }) {
				return jiraWrite{}, &Skip{Key: c.Key, Reason: fmt.Sprintf("Jira labels have no spaces: %q", s)}
			}
		} else if jid, ok := fm.toJira[s]; ok {
			x = map[string]string{"id": jid}
		} else {
			return jiraWrite{}, &Skip{Key: c.Key, Reason: fmt.Sprintf("%s is local-only: Jira has no option for it", s)}
		}
		if i < len(c.Add) {
			w.Update = append(w.Update, jiraapi.OpAdd(x))
		} else {
			w.Update = append(w.Update, jiraapi.OpRemove(x))
		}
	}
	return w, nil
}

// createBody is the POST /issue body for a local issue and the keys it sends,
// which make JS15's create base: project, type, summary, description and the
// property, plus each field the create screen requires, without a default,
// that local holds. Everything else is written by the ordinary merge after.
func (m *Mapping) createBody(local Doc, id entity.Id, ix *Index) (newIssue, []string, []Skip) {
	body := newIssue{
		Fields:     map[string]any{},
		Properties: []jiraapi.Property{{Key: PropertyKey, Value: map[string]string{"id": id.String()}}},
	}
	tm, ok := m.types[local.Type]
	if !ok {
		return body, nil, []Skip{{Key: schema.TypeKey, Reason: "type " + local.Type + " is local-only"}}
	}
	body.Fields["project"] = map[string]string{"id": m.projectId}
	body.Fields["issuetype"] = map[string]string{"id": tm.issueType}
	title, _ := issue.String(local.Fields[schema.TitleKey])
	body.Fields["summary"] = title
	if text, _ := issue.String(local.Fields[BodyKey]); text != "" {
		body.Fields["description"] = jiraapi.TextToADF(text)
	}
	sent := []string{schema.TitleKey, schema.TypeKey, BodyKey}
	var skips []Skip
	for _, key := range tm.keys[2:] {
		fm, v := tm.fields[key], local.Fields[key]
		if !tm.required[fm.ref] || fm.ref == refStatus || fm.link != "" || issue.IsNull(v) || string(v) == "[]" {
			continue
		}
		c := change{Key: key, Set: v}
		if fm.kind.IsMulti() {
			items, _ := issue.Items(v)
			c = change{Key: key, Add: items}
		}
		w, skip := m.toJira(tm, c, nil, ix)
		if skip != nil {
			skips = append(skips, *skip)
			continue
		}
		body.Fields[w.Field] = w.Set
		if w.Update != nil { // a set is stated whole on a create
			var items []any
			for _, op := range w.Update {
				items = append(items, op.Value)
			}
			body.Fields[w.Field] = items
		}
		sent = append(sent, key)
	}
	return body, sent, skips
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
