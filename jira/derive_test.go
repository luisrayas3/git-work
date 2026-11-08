package jira

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/schema"
)

// store is the schema import path without a repository: the whole document
// validated, Reconcile against the entries, the changes applied as the
// config entities would take them, and Compile to read it back.
type store struct {
	entries []schema.Entry
	n       int
}

func (s *store) importDoc(t *testing.T, doc *schema.Document) []schema.Change {
	t.Helper()
	var known []string
	for _, e := range s.entries {
		if e.Shape == config.ShapeType {
			known = append(known, e.Key)
		}
	}
	require.NoError(t, doc.Validate(known))
	changes, err := schema.Reconcile(doc, s.entries, false)
	require.NoError(t, err)
	after, err := schema.Compile(schema.Apply(s.entries, changes))
	require.NoError(t, err)
	// what the import leaves meets
	// (doc/design/schema.md, "A synced type's targets sync to the same system")
	require.NoError(t, after.CheckRetypeSystems(), "a synced type's targets sync too")
	s.apply(changes)
	return changes
}

func (s *store) apply(changes []schema.Change) {
	for _, c := range changes {
		switch c.Action {
		case schema.ActionCreate:
			s.n++
			attrs := map[string]config.Value{}
			for k, v := range c.Set {
				attrs[k] = v
			}
			s.entries = append(s.entries, schema.Entry{Id: entity.Id(fmt.Sprintf("%064d", s.n)),
				Shape: c.Shape, Key: c.Key, Attributes: attrs, Archived: c.Archive != nil && *c.Archive})
		case schema.ActionUpdate:
			i := slices.IndexFunc(s.entries, func(e schema.Entry) bool { return e.Id == c.Id })
			for k, v := range c.Set {
				s.entries[i].Attributes[k] = v
			}
			for _, k := range c.Remove {
				delete(s.entries[i].Attributes, k)
			}
			if c.Archive != nil {
				s.entries[i].Archived = *c.Archive
			}
		case schema.ActionArchive:
			i := slices.IndexFunc(s.entries, func(e schema.Entry) bool { return e.Id == c.Id })
			s.entries[i].Archived = true
		}
	}
}

func (s *store) schema(t *testing.T) *schema.Schema {
	t.Helper()
	sc, err := schema.Compile(s.entries)
	require.NoError(t, err)
	require.Empty(t, sc.Problems)
	return sc
}

func (s *store) export(t *testing.T) *schema.Document { return schema.Export(s.schema(t)) }

// starts are the three starting schemas of the test plan.
func starts(t *testing.T) map[string]*schema.Document {
	preset, err := schema.Preset("jira")
	require.NoError(t, err)
	return map[string]*schema.Document{"empty": schema.NewDocument(), "preset": preset, "repo": customized(t)}
}

// customized is the jira preset as a team grows it: a type of its own,
// `decision`, a local-only field, `area`, on three types, and an `allocation`
// type relating two of the preset's. Derive has to leave all of it alone.
func customized(t *testing.T) *schema.Document {
	t.Helper()
	doc, err := schema.Preset("jira")
	require.NoError(t, err)
	area := schema.FieldDoc{Kind: "multi-enum", Name: "Area",
		Description: "The parts of the code base the work touches.",
		Values:      []schema.ValueDoc{{Id: "core", Name: "Core"}, {Id: "cli", Name: "CLI"}}}
	task, ok := doc.Types.Get("task")
	require.True(t, ok)
	field := func(key string) schema.FieldDoc {
		f, ok := task.Fields.Get(key)
		require.True(t, ok, key)
		return f
	}

	decision := schema.TypeDoc{Name: "Decision", Description: "A design call and its reasoning."}
	for _, key := range []string{"status", "priority", "assignee", "labels"} {
		decision.SetField(key, field(key))
	}
	decision.SetField("parent", schema.FieldDoc{Kind: "relation", Name: "Parent",
		Inverse: "children", TargetTypes: []string{"epic", "story", "task"}})
	decision.SetField("blocks", field("blocks"))
	decision.SetField("area", area)

	allocation := schema.TypeDoc{Name: "Allocation",
		Description: "How much of one person's iteration goes to one piece of work."}
	allocation.SetField("iteration", schema.FieldDoc{Kind: "relation", Name: "Iteration",
		Inverse: "allocations", TargetTypes: []string{"iteration"}})
	allocation.SetField("work", schema.FieldDoc{Kind: "relation", Name: "Work",
		Inverse: "staffing", TargetTypes: []string{"epic", "story"}})
	allocation.SetField("assignee", field("assignee"))
	allocation.SetField("points", schema.FieldDoc{Kind: "number", Name: "Points"})

	for _, key := range []string{"story", "task"} {
		typeDoc, ok := doc.Types.Get(key)
		require.True(t, ok)
		typeDoc.SetField("area", area)
		doc.SetType(key, typeDoc)
	}
	doc.SetType("decision", decision)
	doc.SetType("allocation", allocation)
	require.NoError(t, doc.Validate(nil))
	return doc
}

// derived imports a starting schema, derives against one site, and imports
// the result: the state `git work jira schema | git work schema import -` leaves.
func derived(t *testing.T, start *schema.Document, p *Project) (*store, *schema.Document, []Note, []schema.Change) {
	t.Helper()
	s := &store{}
	s.importDoc(t, start)
	doc, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	changes := s.importDoc(t, doc)
	return s, doc, notes, changes
}

func marshal(t *testing.T, doc *schema.Document) string {
	t.Helper()
	out, err := doc.Marshal("yaml")
	require.NoError(t, err)
	return string(out)
}

// a golden per starting schema and site, then a fixpoint
// (doc/design/jira.md, "Derive, then the schema import").
func TestDerive(t *testing.T) {
	for siteName := range sites {
		p, _ := discover(t, siteName)
		for startName, start := range starts(t) {
			t.Run(startName+"-"+siteName, func(t *testing.T) {
				s, doc, notes, changes := derived(t, start, p)

				var b strings.Builder
				b.WriteString(marshal(t, doc))
				b.WriteString("\n# notes\n")
				for _, n := range notes {
					fmt.Fprintf(&b, "# %s %s: %s\n", n.Level, n.Key, n.Message)
				}
				b.WriteString("\n# changes\n")
				for _, c := range changes {
					line, err := json.Marshal(c)
					require.NoError(t, err)
					fmt.Fprintf(&b, "# %s\n", line)
				}
				checkGolden(t, "derive-"+startName+"-"+siteName+".yaml", []byte(b.String()))

				again, _, err := Derive(s.export(t), p)
				require.NoError(t, err)
				require.Equal(t, marshal(t, doc), marshal(t, again), "Derive is a fixpoint")
				require.Empty(t, s.importDoc(t, again), "a second derivation writes nothing")

				_, _, err = Compile(s.schema(t), p)
				require.NoError(t, err)
			})
		}
	}
}

func typeDoc(t *testing.T, doc *schema.Document, key string) schema.TypeDoc {
	t.Helper()
	td, ok := doc.Types.Get(key)
	require.True(t, ok, "type %s", key)
	return td
}

func fieldDoc(t *testing.T, doc *schema.Document, typeKey, key string) schema.FieldDoc {
	t.Helper()
	f, ok := typeDoc(t, doc, typeKey).Fields.Get(key)
	require.True(t, ok, "field %s/%s", typeKey, key)
	return f
}

func valueDoc(t *testing.T, f schema.FieldDoc, id string) schema.ValueDoc {
	t.Helper()
	i := slices.IndexFunc(f.Values, func(v schema.ValueDoc) bool { return v.Id == id })
	require.GreaterOrEqual(t, i, 0, "value %s", id)
	return f.Values[i]
}

func hasNote(notes []Note, level Level, key, part string) bool {
	return slices.ContainsFunc(notes, func(n Note) bool {
		return n.Level == level && n.Key == key && strings.Contains(n.Message, part)
	})
}

func setStatus(p *Project, id string, fn func(*jiraapi.Status)) {
	for i := range p.IssueTypes {
		for j := range p.IssueTypes[i].Statuses {
			if p.IssueTypes[i].Statuses[j].ID == id {
				fn(&p.IssueTypes[i].Statuses[j])
			}
		}
	}
}

// a Jira rename sets one values/<id> per field and keeps the id; an added
// status gets its value and alias; a removed one is archived, keeping its
// alias, and warns; one Jira holds again is unarchived
// (doc/design/jira.md, "Derive, then the schema import";
// doc/design/jira.md, "What Jira drops is archived").
func TestDeriveJiraChanges(t *testing.T) {
	p, _ := discover(t, "company")
	s, _, _, _ := derived(t, starts(t)["preset"], p)

	p, _ = discover(t, "company")
	setStatus(p, "3", func(st *jiraapi.Status) { st.Name = "Doing" })
	doc, _, err := Derive(s.export(t), p)
	require.NoError(t, err)
	changes := s.importDoc(t, doc)
	require.NotEmpty(t, changes)
	for _, c := range changes {
		require.Equal(t, schema.ActionUpdate, c.Action)
		require.Equal(t, []string{"values/in-progress"}, slices.Sorted(maps.Keys(c.Set)), c.Key)
	}
	require.Equal(t, "Doing", valueDoc(t, fieldDoc(t, s.export(t), "task", "status"), "in-progress").Name)

	p, _ = discover(t, "company")
	s, _, _, _ = derived(t, starts(t)["preset"], p)
	p.IssueTypes[1].Statuses = append(p.IssueTypes[1].Statuses[:1], p.IssueTypes[1].Statuses[2:]...) // story loses In Progress
	p.IssueTypes[1].Statuses = append(p.IssueTypes[1].Statuses, jiraapi.Status{ID: "10009", Name: "Blocked",
		StatusCategory: jiraapi.StatusCategory{Key: "indeterminate"}})
	doc, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	status := fieldDoc(t, doc, "story", "status")
	blocked := valueDoc(t, status, "blocked")
	require.Equal(t, "started", blocked.Category)
	require.Equal(t, "10009", blocked.Aliases[system])
	gone := valueDoc(t, status, "in-progress")
	require.Equal(t, "3", gone.Aliases[system], "a dead alias is kept")
	require.True(t, gone.Archived != nil && *gone.Archived, "a value Jira dropped is archived")
	require.True(t, hasNote(notes, LevelWarn, "story/status:in-progress", "archived"))
	for _, n := range notes {
		if n.Key == "story/status:in-progress" {
			require.Equal(t, &ValueRef{Type: "story", Field: "status", Value: "in-progress"}, n.Archived)
		}
	}
	require.Nil(t, valueDoc(t, fieldDoc(t, doc, "task", "status"), "in-progress").Archived,
		"another type's value is its own")
	changes = s.importDoc(t, doc)
	var fieldChanges []string
	for _, c := range changes {
		if c.Shape == config.ShapeField {
			fieldChanges = append(fieldChanges, c.Key)
		}
	}
	require.Equal(t, []string{"story/status"}, fieldChanges)
	// story's workflow is no longer its level's other types'
	// (doc/design/jira.md, "Retype targets follow Jira's levels and workflows")
	require.Empty(t, *typeDoc(t, doc, "story").RetypeTargets)
	require.NotContains(t, *typeDoc(t, doc, "task").RetypeTargets, "story")

	// archived, it stays in its place, and the next derive writes nothing
	f, ok := s.schema(t).Field("story", "status")
	require.True(t, ok)
	v, ok := f.Value("in-progress")
	require.True(t, ok)
	require.True(t, v.Archived)
	require.NotContains(t, f.ValueIds(), "in-progress")
	again, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again), "a fixpoint")
	require.False(t, hasNote(notes, LevelWarn, "story/status:in-progress", "archived"), "said once")

	// Jira holds it again: unarchived, then a fixpoint
	p, _ = discover(t, "company")
	back, _, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.NotEmpty(t, s.importDoc(t, back))
	v, _ = s.schema(t).Types["story"].Fields["status"].Value("in-progress")
	require.False(t, v.Archived, "Jira is canonical")
	back, _, err = Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, back), "a fixpoint")
}

// an entity aliased "" is never adopted, across runs, and nothing replaces
// it (doc/design/jira.md, "Matching and keys").
func TestDeriveExcluded(t *testing.T) {
	p, _ := discover(t, "company")
	start := starts(t)["preset"]
	bug := typeDoc(t, start, "bug")
	bug.Aliases = map[string]string{system: ""}
	start.SetType("bug", bug)
	task := typeDoc(t, start, "task")
	est, _ := task.Fields.Get("estimate")
	est.Aliases = map[string]string{system: ""}
	task.SetField("estimate", est)
	start.SetType("task", task)

	s, doc, _, _ := derived(t, start, p)
	for _, d := range []*schema.Document{doc, s.export(t)} {
		require.Equal(t, "", typeDoc(t, d, "bug").Aliases[system])
		require.Equal(t, "", fieldDoc(t, d, "task", "estimate").Aliases[system])
		_, ok := d.Types.Get("bug-2")
		require.False(t, ok)
		_, ok = typeDoc(t, d, "task").Fields.Get("estimate-2")
		require.False(t, ok)
	}
	again, _, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again))

	m, _, err := Compile(s.schema(t), p)
	require.NoError(t, err)
	_, ok := m.localType("10004")
	require.False(t, ok, "an excluded type maps nothing")
}

// A type's show spec is the team's, not Jira's: deriving keeps it, and the
// sync's re-import of the result removes nothing.
func TestDeriveKeepsShow(t *testing.T) {
	p, _ := discover(t, "company")
	start := starts(t)["repo"]
	task := typeDoc(t, start, "task")
	show := &schema.ShowDoc{Fields: []string{"status", "area"}, Expand: "children"}
	task.Show = show
	start.SetType("task", task)

	s, doc, _, _ := derived(t, start, p)
	for _, d := range []*schema.Document{doc, s.export(t)} {
		require.Equal(t, show, typeDoc(t, d, "task").Show)
	}
	again, _, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again))
}

// a person marks Won't Do canceled once and every derivation keeps it; Jira
// moving the status out of the done class resets it and warns
// (doc/design/jira.md, "Status categories").
func TestDeriveCategories(t *testing.T) {
	p, _ := discover(t, "company")
	s, doc, _, _ := derived(t, starts(t)["preset"], p)
	require.Equal(t, "completed", valueDoc(t, fieldDoc(t, doc, "task", "status"), "wont-do").Category)

	edited := s.export(t)
	task := typeDoc(t, edited, "task")
	status, _ := task.Fields.Get("status")
	i := slices.IndexFunc(status.Values, func(v schema.ValueDoc) bool { return v.Id == "wont-do" })
	status.Values[i].Category = "canceled"
	task.SetField("status", status)
	edited.SetType("task", task)
	s.importDoc(t, edited)

	again, _, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again))
	require.Equal(t, "canceled", valueDoc(t, fieldDoc(t, s.export(t), "task", "status"), "wont-do").Category)

	m, _, err := Compile(s.schema(t), p)
	require.NoError(t, err)
	_, v, ok := m.canceled("task")
	require.True(t, ok)
	require.JSONEq(t, `"canceled"`, string(v), "the first canceled value, local-only or not")

	p, _ = discover(t, "company")
	setStatus(p, "10003", func(st *jiraapi.Status) { st.StatusCategory.Key = "indeterminate" })
	moved, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Equal(t, "started", valueDoc(t, fieldDoc(t, moved, "task", "status"), "wont-do").Category)
	require.True(t, hasNote(notes, LevelWarn, "task/status:wont-do", "reset to started"))
}

// a schema mapped against the company project refuses the team one, in
// Derive (before anything is imported) and in Compile
// (doc/design/jira.md, "Derive, then the schema import").
func TestBindingRefused(t *testing.T) {
	company, _ := discover(t, "company")
	team, _ := discover(t, "team")
	s, _, _, _ := derived(t, starts(t)["preset"], company)

	_, _, err := Derive(s.export(t), team)
	require.ErrorContains(t, err, "TEAM does not have")
	_, _, err = Compile(s.schema(t), team)
	require.ErrorContains(t, err, "TEAM does not have")

	fresh := &store{}
	fresh.importDoc(t, starts(t)["preset"])
	_, _, err = Compile(fresh.schema(t), company)
	require.ErrorContains(t, err, "no type of the schema is mapped")
}

// two entities with one alias, which two clones can produce by merging, make
// Compile refuse (doc/design/jira.md, "Duplicate aliases are refused").
func TestCompileDuplicateAlias(t *testing.T) {
	p, _ := discover(t, "company")
	for name, attr := range map[string]struct{ key, name, value string }{
		"type":  {"story", "alias_jira", `"10002"`},
		"field": {"task/labels", "alias_jira", `"priority"`},
		"value": {"task/status", "alias_jira/done", `"10003"`},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _, _ := derived(t, starts(t)["preset"], p)
			i := slices.IndexFunc(s.entries, func(e schema.Entry) bool { return e.Key == attr.key })
			s.entries[i].Attributes[attr.name] = config.Value(attr.value)
			_, _, err := Compile(s.schema(t), p)
			require.ErrorContains(t, err, "ambiguous")
		})
	}
}

// D10.
func TestSlug(t *testing.T) {
	for name, want := range map[string]string{
		"In Progress": "in-progress", "To Do": "to-do", "Won't Do": "wont-do", "Won’t Do": "wont-do",
		"Story point estimate": "story-point-estimate", "Café Crème": "cafe-creme", "  --a  b-- ": "a-b",
		"完了": "", "3rd party": "3rd-party",
		strings.Repeat("abcdefghij ", 6): "abcdefghij-abcdefghij-abcdefghij-abcdefghij",
		strings.Repeat("x", 60):          strings.Repeat("x", 48),
	} {
		require.Equal(t, want, slug(name), name)
	}
	for name, want := range map[string]string{"Sub-task": "subtask", "Subtask": "subtask", "SUB_TASK": "subtask", "完了": ""} {
		require.Equal(t, want, normName(name), name)
	}
	none := func(string) bool { return false }
	taken := func(k string) bool { return k == "done" || k == "done-2" }
	require.Equal(t, "jira-10002", keyFor("完了", "10002", "", none))
	require.Equal(t, "t-3rd-party", keyFor("3rd party", "1", "t-", none))
	require.Equal(t, "3rd-party", keyFor("3rd party", "1", "", none), "a value id may start with a digit")
	require.Equal(t, "done-3", keyFor("Done", "1", "", taken))
	require.Equal(t, "title-2", keyFor("Title", "1", "f-", none), "a built-in always collides")
}

// A type or a field Jira no longer has is archived, saying so once, and
// unarchived when Jira has it again; a field merely off one type's create
// screen keeps the warning (doc/design/jira.md, "What Jira drops is archived").
func TestDeriveArchivesTypesAndFields(t *testing.T) {
	p, _ := discover(t, "company")
	s, _, _, _ := derived(t, starts(t)["preset"], p)

	p, _ = discover(t, "company")
	p.IssueTypes = slices.DeleteFunc(p.IssueTypes, func(it IssueType) bool { return it.Name == "Bug" })
	p.Fields = slices.DeleteFunc(p.Fields, func(f jiraapi.Field) bool { return f.ID == "customfield_10016" })
	for i := range p.IssueTypes {
		p.IssueTypes[i].Screen = slices.DeleteFunc(slices.Clone(p.IssueTypes[i].Screen),
			func(m jiraapi.FieldMeta) bool { return metaId(m) == "customfield_10016" })
		if p.IssueTypes[i].Name == "Task" {
			p.IssueTypes[i].Screen = slices.DeleteFunc(slices.Clone(p.IssueTypes[i].Screen),
				func(m jiraapi.FieldMeta) bool { return metaId(m) == "customfield_10015" })
		}
	}
	doc, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)

	bug := typeDoc(t, doc, "bug")
	require.True(t, bug.Archived != nil && *bug.Archived, "an issue type Jira dropped")
	require.True(t, hasNote(notes, LevelWarn, "bug", "archived: "))
	for _, key := range []string{"story", "task"} {
		f := fieldDoc(t, doc, key, "estimate")
		require.True(t, f.Archived != nil && *f.Archived, "a field Jira no longer has, on %s", key)
		require.True(t, hasNote(notes, LevelWarn, key+"/estimate", "archived: "))
	}
	require.Nil(t, fieldDoc(t, doc, "task", "start-date").Archived, "off one create screen is not gone")
	require.True(t, hasNote(notes, LevelWarn, "task/start-date", "dead alias"))
	for _, n := range notes {
		if n.Key == "bug" {
			require.Equal(t, &ValueRef{Type: "bug"}, n.Archived)
		}
		if n.Key == "task/estimate" {
			require.Equal(t, &ValueRef{Type: "task", Field: "estimate"}, n.Archived)
		}
	}

	require.NotEmpty(t, s.importDoc(t, doc))
	sc := s.schema(t)
	require.True(t, sc.Types["bug"].Archived)
	require.NotContains(t, sc.TypeKeys(), "bug")
	require.True(t, sc.Types["task"].Fields["estimate"].Archived)
	again, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, again), "a fixpoint")
	require.False(t, hasNote(notes, LevelWarn, "bug", "archived"), "said once")

	// Jira has them again
	p, _ = discover(t, "company")
	back, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)
	require.True(t, hasNote(notes, LevelInfo, "bug", "unarchived"))
	require.NotEmpty(t, s.importDoc(t, back))
	sc = s.schema(t)
	require.False(t, sc.Types["bug"].Archived)
	require.False(t, sc.Types["task"].Fields["estimate"].Archived)
	back, _, err = Derive(s.export(t), p)
	require.NoError(t, err)
	require.Empty(t, s.importDoc(t, back), "a fixpoint")
}

// withColour gives the project an option field, Colour, on the Task and
// Story create screens with the options listed for each; nil leaves it off
// that type's create screen.
func withColour(p *Project, task, story []string) {
	const id = "customfield_10077"
	schemaOf := jiraapi.FieldSchema{Type: "option", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:select"}
	p.Fields = append(p.Fields, jiraapi.Field{ID: id, Key: id, Name: "Colour", Custom: true, Schema: &schemaOf})
	options := map[string]string{"1": "Red", "2": "Blue"}
	for i := range p.IssueTypes {
		var ids []string
		switch p.IssueTypes[i].Name {
		case "Task":
			ids = task
		case "Story":
			ids = story
		}
		if ids == nil {
			continue
		}
		m := jiraapi.FieldMeta{FieldID: id, Key: id, Name: "Colour", Schema: schemaOf}
		for _, o := range ids {
			m.AllowedValues = append(m.AllowedValues, json.RawMessage(fmt.Sprintf(`{"id":%q,"value":%q}`, o, options[o])))
		}
		p.IssueTypes[i].Screen = append(slices.Clone(p.IssueTypes[i].Screen), m)
	}
}

// An option missing from a create screen's allowedValues is archived; a
// field off one type's create screen has no allowedValues there, so its
// values stay live and it keeps the dead-alias warning
// (doc/design/jira.md, "What Jira drops is archived").
func TestDeriveOptionsOnlyFromTheScreen(t *testing.T) {
	p, _ := discover(t, "company")
	withColour(p, []string{"1", "2"}, []string{"1", "2"})
	s, _, _, _ := derived(t, starts(t)["preset"], p)

	p, _ = discover(t, "company")
	withColour(p, nil, []string{"1"})
	doc, notes, err := Derive(s.export(t), p)
	require.NoError(t, err)

	task := fieldDoc(t, doc, "task", "colour")
	require.Nil(t, task.Archived, "in /field, so not gone")
	require.Len(t, task.Values, 2)
	for _, v := range task.Values {
		require.Nil(t, v.Archived, "task/colour:%s: off the screen is not gone", v.Id)
	}
	require.True(t, hasNote(notes, LevelWarn, "task/colour", "dead alias"))

	story := fieldDoc(t, doc, "story", "colour")
	blue := valueDoc(t, story, "blue")
	require.True(t, blue.Archived != nil && *blue.Archived, "on the screen and missing")
	require.Nil(t, valueDoc(t, story, "red").Archived)
}
