package jira

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
)

// system is the alias system name this package reads and writes (JS2).
const system = "jira"

// Field references of the fixed table (JS7); a link type is linkRef.
const (
	refStatus   = "status"
	refPriority = "priority"
	refAssignee = "assignee"
	refLabels   = "labels"
	refDue      = "duedate"
	refParent   = "parent"
	linkPrefix  = "link:"
)

// Derive returns current ⊕ Jira (JS5): the document with Jira's types,
// fields and values adopted or added, Jira's names applied to what is
// aliased, and nothing removed, archived or re-kinded. It is pure and a
// fixpoint: deriving again from its import changes nothing.
//
// The error is a mis-binding: the document maps issue types, none of which
// the project has.
func Derive(current *schema.Document, p *Project) (*schema.Document, []Note, error) {
	doc := copyDocument(current)
	if err := checkBinding(typeAliases(doc), p); err != nil {
		return nil, nil, err
	}
	d := &deriver{p: p, doc: doc, mapped: map[string]string{}}
	d.types()
	for _, it := range p.IssueTypes {
		if key, ok := d.mapped[it.Id]; ok {
			d.fields(key, it)
		}
	}
	return d.doc, d.notes, nil
}

type deriver struct {
	p      *Project
	doc    *schema.Document
	mapped map[string]string // issue type id -> type key
	notes  []Note
}

func (d *deriver) note(level Level, key, format string, args ...any) {
	d.notes = append(d.notes, Note{Level: level, Key: key, Message: fmt.Sprintf(format, args...)})
}

// candidate is one local entity as matching sees it (JS6).
type candidate struct {
	key, name string
	aliases   map[string]string
	fits      bool // same kind, for a field
}

// match finds the local entity for one Jira entity, first rule that applies:
// the alias equals its id; a name match aliased "" excludes it; an unaliased
// name match of the same kind is adopted. tableKey is the fixed table's key,
// which an adopted preset keeps. claimed are the entities an earlier Jira
// entity took, which is how the lower id wins.
func match(cs []candidate, jiraId, jiraName, tableKey string, claimed map[string]bool) (key string, adopted, excluded bool) {
	for _, c := range cs {
		if a, ok := c.aliases[system]; ok && a != "" && a == jiraId {
			return c.key, false, false
		}
	}
	n := normName(jiraName)
	found := ""
	for _, c := range cs {
		same := c.key == tableKey || n != "" && (normName(c.key) == n || normName(c.name) == n)
		if !same || claimed[c.key] {
			continue
		}
		a, aliased := c.aliases[system]
		switch {
		case aliased && a == "":
			return "", false, true
		case !aliased && c.fits && found == "":
			found = c.key
		}
	}
	return found, found != "", false
}

func setAlias(aliases *map[string]string, id string) {
	if *aliases == nil {
		*aliases = map[string]string{}
	}
	(*aliases)[system] = id
}

// types matches every issue type, in (level desc, id) order.
func (d *deriver) types() {
	var cs []candidate
	for _, key := range d.doc.Types.Keys() {
		t, _ := d.doc.Types.Get(key)
		cs = append(cs, candidate{key: key, name: t.Name, aliases: t.Aliases, fits: true})
	}
	claimed := map[string]bool{}
	for _, it := range d.p.IssueTypes {
		key, adopted, excluded := match(cs, it.Id, it.Name, "", claimed)
		if excluded {
			continue
		}
		if key == "" {
			key = keyFor(it.Name, it.Id, "t-", func(k string) bool { _, ok := d.doc.Types.Get(k); return ok })
			t := schema.TypeDoc{Name: it.Name, Description: it.Description}
			setAlias(&t.Aliases, it.Id)
			d.doc.SetType(key, t)
			d.note(LevelInfo, key, "new type for Jira issue type %s (%s)", it.Name, it.Id)
		} else {
			t, _ := d.doc.Types.Get(key)
			if adopted {
				d.note(LevelInfo, key, "adopts Jira issue type %s (%s)", it.Name, it.Id)
			} else if t.Name != it.Name {
				d.note(LevelInfo, key, "renamed %q after Jira", it.Name)
			}
			t.Name = it.Name
			if it.Description != "" {
				t.Description = it.Description
			}
			setAlias(&t.Aliases, it.Id)
			d.doc.SetType(key, t)
		}
		claimed[key] = true
		d.mapped[it.Id] = key
	}
	for _, key := range d.doc.Types.Keys() {
		t, _ := d.doc.Types.Get(key)
		switch a, ok := t.Aliases[system]; {
		case !ok:
			d.note(LevelInfo, key, "local-only type: never exported")
		case a != "" && !claimed[key]:
			d.note(LevelWarn, key, "dead alias: %s has no issue type %s", d.p.Key, a)
		}
	}
}

// want is one field the fixed table or a create screen maps onto a type.
type want struct {
	ref      string // the field reference, the alias
	key      string // the table's key; "" for a custom field
	name     string // Jira's name
	kind     schema.Kind
	freeform bool
	inverse  string
	targets  []string
	values   []jiraValue
	ordered  bool // values follow Jira's order (priority)
	status   bool // values carry categories
}

type jiraValue struct{ id, name, class string }

// wants lists what maps onto one type, table before custom fields, then by id.
func (d *deriver) wants(it IssueType) []want {
	name := func(id, fallback string) string {
		if f, ok := d.p.field(id); ok && f.Name != "" {
			return f.Name
		}
		return fallback
	}
	on := func(id string) bool { _, ok := it.onScreen(id); return ok }

	var ws []want
	if len(it.Statuses) > 0 {
		w := want{ref: refStatus, key: "status", name: name(refStatus, "Status"), kind: schema.KindEnum, status: true}
		for _, s := range it.Statuses {
			w.values = append(w.values, jiraValue{id: s.ID, name: s.Name, class: s.StatusCategory.Key})
		}
		ws = append(ws, w)
	}
	if on(refPriority) {
		w := want{ref: refPriority, key: "priority", name: name(refPriority, "Priority"), kind: schema.KindOrdinalEnum, ordered: true}
		for _, pr := range d.p.Priorities {
			w.values = append(w.values, jiraValue{id: pr.ID, name: pr.Name})
		}
		ws = append(ws, w)
	}
	if on(refAssignee) {
		ws = append(ws, want{ref: refAssignee, key: "assignee", name: name(refAssignee, "Assignee"), kind: schema.KindIdentity})
	}
	if on(refLabels) {
		ws = append(ws, want{ref: refLabels, key: "labels", name: name(refLabels, "Labels"), kind: schema.KindMultiEnum, freeform: true})
	}
	if on(refDue) {
		ws = append(ws, want{ref: refDue, key: "due", name: name(refDue, "Due date"), kind: schema.KindDate})
	}
	var above []string
	for _, other := range d.p.IssueTypes {
		if key, ok := d.mapped[other.Id]; ok && other.Level == it.Level+1 {
			above = append(above, key)
		}
	}
	if len(above) > 0 {
		ws = append(ws, want{ref: refParent, key: "parent", name: name(refParent, "Parent"), kind: schema.KindRelation,
			inverse: "children", targets: above})
	}
	for _, lt := range d.p.LinkTypes {
		name := lt.Name
		if lt.Outward != "" {
			name = strings.ToUpper(lt.Outward[:1]) + lt.Outward[1:]
		}
		w := want{ref: linkPrefix + lt.ID, key: slug(lt.Outward), name: name, kind: schema.KindMultiRelation}
		if lt.Inward != lt.Outward {
			w.inverse = slug(lt.Inward)
		}
		ws = append(ws, w)
	}
	points := storyPoints(d.p)
	if points != "" && on(points) {
		ws = append(ws, want{ref: points, key: "estimate", name: name(points, "Story points"), kind: schema.KindNumber})
	}

	for _, m := range it.Screen {
		id := metaId(m)
		if !strings.HasPrefix(id, "customfield_") || id == points {
			continue
		}
		kind, ok := customKind(m.Schema)
		if !ok {
			d.note(LevelWarn, d.mapped[it.Id]+"/"+id, "Jira field %s (%s, %s) is not mapped", m.Name, id, customName(m.Schema))
			continue
		}
		w := want{ref: id, name: m.Name, kind: kind}
		if kind.IsEnum() {
			for _, raw := range m.AllowedValues {
				var o struct{ ID, Value, Name string }
				if json.Unmarshal(raw, &o) == nil && o.ID != "" {
					w.values = append(w.values, jiraValue{id: o.ID, name: orElse(o.Value, o.Name)})
				}
			}
		}
		ws = append(ws, w)
	}
	return ws
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func customName(s jiraapi.FieldSchema) string {
	if s.Custom != "" {
		return s.Custom
	}
	if s.Items != "" {
		return s.Type + "(" + s.Items + ")"
	}
	return s.Type
}

// customKind is JS7's table of custom field kinds, by schema.type.
func customKind(s jiraapi.FieldSchema) (schema.Kind, bool) {
	switch {
	case s.Type == "number":
		return schema.KindNumber, true
	case s.Type == "date" || s.Type == "datetime":
		return schema.KindDate, true
	case s.Type == "string" && strings.HasSuffix(s.Custom, ":textfield"):
		return schema.KindText, true
	case s.Type == "option":
		return schema.KindEnum, true
	case s.Type == "array" && s.Items == "option":
		return schema.KindMultiEnum, true
	case s.Type == "user":
		return schema.KindIdentity, true
	}
	return "", false
}

// storyPoints finds the estimate field (JS7): jsw-story-points, else a
// number field named exactly "Story Points" or "Story point estimate".
func storyPoints(p *Project) string {
	for _, f := range p.Fields {
		if f.Schema != nil && strings.HasSuffix(f.Schema.Custom, ":jsw-story-points") {
			return f.ID
		}
	}
	for _, f := range p.Fields {
		n := orElse(f.UntranslatedName, f.Name)
		if f.Custom && f.Schema != nil && f.Schema.Type == "number" && (n == "Story Points" || n == "Story point estimate") {
			return f.ID
		}
	}
	return ""
}

// fields maps every want of one issue type onto its type.
func (d *deriver) fields(typeKey string, it IssueType) {
	t, _ := d.doc.Types.Get(typeKey)
	claimed := map[string]bool{}
	for _, w := range d.wants(it) {
		var cs []candidate
		for _, key := range t.Fields.Keys() {
			if schema.IsBuiltin(key) {
				continue
			}
			f, _ := t.Fields.Get(key)
			cs = append(cs, candidate{key: key, name: f.Name, aliases: f.Aliases, fits: f.Kind == string(w.kind)})
		}
		key, adopted, excluded := match(cs, w.ref, w.name, w.key, claimed)
		if excluded {
			continue
		}
		where := typeKey + "/" + key
		var f schema.FieldDoc
		if key == "" {
			key = keyFor(orElse(w.key, w.name), w.ref, "f-", func(k string) bool { return fieldTaken(&t, k) })
			where = typeKey + "/" + key
			f = schema.FieldDoc{Kind: string(w.kind), Name: w.name}
			if w.key != "" && key != w.key {
				d.note(LevelWarn, where, "%s is taken by a field of another kind or mapping; Jira's %s is %s", w.key, w.name, key)
			} else {
				d.note(LevelInfo, where, "new field for Jira field %s (%s)", w.name, w.ref)
			}
		} else {
			f, _ = t.Fields.Get(key)
			if adopted {
				d.note(LevelInfo, where, "adopts Jira field %s (%s)", w.name, w.ref)
			}
			f.Name = orElse(w.name, f.Name)
		}
		claimed[key] = true
		setAlias(&f.Aliases, w.ref)
		f.Freeform = f.Freeform || w.freeform
		if f.Inverse == "" && w.inverse != "" {
			f.Inverse = keyFor(w.inverse, w.ref, "f-", func(k string) bool { return k == key || fieldTaken(&t, k) })
		}
		for _, target := range w.targets {
			if !slices.Contains(f.TargetTypes, target) {
				f.TargetTypes = append(f.TargetTypes, target)
			}
		}
		slices.Sort(f.TargetTypes) // the store keeps them as a set

		if w.kind.IsEnum() && !w.freeform {
			f.Values = d.values(where, f.Values, w)
		}
		t.SetField(key, f)
	}
	for _, key := range t.Fields.Keys() {
		f, _ := t.Fields.Get(key)
		switch a, ok := f.Aliases[system]; {
		case schema.IsBuiltin(key):
		case !ok:
			d.note(LevelInfo, typeKey+"/"+key, "local-only field: never exported")
		case a != "" && !claimed[key]:
			d.note(LevelWarn, typeKey+"/"+key, "dead alias: Jira field %s is not mapped on %s", a, it.Name)
		}
	}
	d.doc.SetType(typeKey, t)
}

// fieldTaken is a key a new field or inverse cannot take: a field's or an inverse's.
func fieldTaken(t *schema.TypeDoc, k string) bool {
	if _, ok := t.Fields.Get(k); ok {
		return true
	}
	for _, key := range t.Fields.Keys() {
		if f, _ := t.Fields.Get(key); f.Inverse == k {
			return true
		}
	}
	return false
}

// classes rank Jira's status categories (JS7): each allows some local
// categories, and a new value gets the first.
var classes = map[string][]schema.Category{
	"new":           {schema.CategoryUnstarted, schema.CategoryBacklog},
	"undefined":     {schema.CategoryUnstarted, schema.CategoryBacklog},
	"indeterminate": {schema.CategoryStarted},
	"done":          {schema.CategoryCompleted, schema.CategoryCanceled},
}

func classOf(key string) []schema.Category {
	if c, ok := classes[key]; ok {
		return c
	}
	return classes["undefined"]
}

// rankOf orders categories by class: new, indeterminate, done.
func rankOf(c schema.Category) int {
	switch c {
	case schema.CategoryStarted:
		return 1
	case schema.CategoryCompleted, schema.CategoryCanceled:
		return 2
	}
	return 0
}

// values matches Jira's values of one field onto its value list (JS6, JS7):
// matched and local values keep their places, new ones join their class
// (status) or the end, and an ordered field's mapped values follow Jira.
func (d *deriver) values(where string, current []schema.ValueDoc, w want) []schema.ValueDoc {
	vs := make([]schema.ValueDoc, len(current))
	copy(vs, current)
	var cs []candidate
	for _, v := range vs {
		cs = append(cs, candidate{key: v.Id, name: v.Name, aliases: v.Aliases, fits: true})
	}
	index := func(id string) int { return slices.IndexFunc(vs, func(v schema.ValueDoc) bool { return v.Id == id }) }

	claimed := map[string]bool{}
	var order []string // value ids in Jira's order
	for _, jv := range w.values {
		id, adopted, excluded := match(cs, jv.id, jv.name, "", claimed)
		if excluded {
			continue
		}
		at := where + ":" + id
		if id != "" {
			i := index(id)
			v := &vs[i]
			if adopted {
				d.note(LevelInfo, at, "adopts Jira value %s (%s)", jv.name, jv.id)
			} else if v.Name != jv.name {
				d.note(LevelInfo, at, "renamed %q after Jira", jv.name)
			}
			v.Name = jv.name
			setAlias(&v.Aliases, jv.id)
			if allowed := classOf(jv.class); w.status && !slices.Contains(allowed, schema.Category(v.Category)) {
				d.note(LevelWarn, at, "category %s is outside Jira's %s; reset to %s", orElse(v.Category, "(none)"), jv.class, allowed[0])
				v.Category = string(allowed[0])
			}
		} else {
			id = keyFor(jv.name, jv.id, "", func(k string) bool { return index(k) >= 0 })
			v := schema.ValueDoc{Id: id, Name: jv.name}
			setAlias(&v.Aliases, jv.id)
			pos := len(vs)
			if w.status {
				c := classOf(jv.class)[0]
				v.Category = string(c)
				pos = 0
				for i, other := range vs {
					if rankOf(schema.Category(other.Category)) <= rankOf(c) {
						pos = i + 1
					}
				}
			}
			vs = slices.Insert(vs, pos, v)
			d.note(LevelInfo, where+":"+id, "new value for Jira's %s (%s)", jv.name, jv.id)
		}
		claimed[id] = true
		order = append(order, id)
	}

	for _, v := range vs {
		switch a, ok := v.Aliases[system]; {
		case !ok:
			d.note(LevelInfo, where+":"+v.Id, "local-only value: never exported")
		case a != "" && !claimed[v.Id]:
			d.note(LevelWarn, where+":"+v.Id, "dead alias: Jira has no value %s here any more", a)
		}
	}

	if w.ordered {
		vs = jiraOrder(vs, order)
	}
	return vs
}

// jiraOrder puts the mapped values in Jira's order, each local-only value
// staying right after the value it followed (JS7: the order is the value).
func jiraOrder(vs []schema.ValueDoc, order []string) []schema.ValueDoc {
	mapped := map[string]bool{}
	for _, id := range order {
		mapped[id] = true
	}
	var head []schema.ValueDoc
	after := map[string][]schema.ValueDoc{}
	byId := map[string]schema.ValueDoc{}
	last := ""
	for _, v := range vs {
		switch {
		case mapped[v.Id]:
			byId[v.Id] = v
			last = v.Id
		case last == "":
			head = append(head, v)
		default:
			after[last] = append(after[last], v)
		}
	}
	out := head
	for _, id := range order {
		out = append(out, byId[id])
		out = append(out, after[id]...)
	}
	return out
}

// typeAliases lists the non-empty type aliases of a document, sorted.
func typeAliases(doc *schema.Document) []string {
	var out []string
	for _, key := range doc.Types.Keys() {
		t, _ := doc.Types.Get(key)
		if a := t.Aliases[system]; a != "" {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// checkBinding refuses a schema mapped against another project (JS5): it
// maps issue types, and the project has none of them.
func checkBinding(aliases []string, p *Project) error {
	if len(aliases) == 0 {
		return nil
	}
	for _, a := range aliases {
		if _, ok := p.issueType(a); ok {
			return nil
		}
	}
	return fmt.Errorf("the schema maps issue types %s that %s does not have; is this the project the tracker was mapped against?",
		strings.Join(slices.Compact(aliases), ", "), p.Key)
}

func copyDocument(src *schema.Document) *schema.Document {
	out := schema.NewDocument()
	if src == nil {
		return out
	}
	for _, key := range src.Types.Keys() {
		t, _ := src.Types.Get(key)
		c := schema.TypeDoc{Name: t.Name, Description: t.Description, Aliases: maps.Clone(t.Aliases)}
		for _, fk := range t.Fields.Keys() {
			f, _ := t.Fields.Get(fk)
			f.Aliases = maps.Clone(f.Aliases)
			f.TargetTypes = slices.Clone(f.TargetTypes)
			f.Values = slices.Clone(f.Values)
			for i := range f.Values {
				f.Values[i].Aliases = maps.Clone(f.Values[i].Aliases)
			}
			c.SetField(fk, f)
		}
		out.SetType(key, c)
	}
	return out
}
