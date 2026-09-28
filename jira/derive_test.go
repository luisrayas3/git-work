package jira

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
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
				Shape: c.Shape, Key: c.Key, Attributes: attrs})
		case schema.ActionUpdate:
			i := slices.IndexFunc(s.entries, func(e schema.Entry) bool { return e.Id == c.Id })
			for k, v := range c.Set {
				s.entries[i].Attributes[k] = v
			}
			for _, k := range c.Remove {
				delete(s.entries[i].Attributes, k)
			}
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
	data, err := os.ReadFile("../schema.yaml")
	require.NoError(t, err)
	repo, err := schema.ParseDocument(data)
	require.NoError(t, err)
	return map[string]*schema.Document{"empty": schema.NewDocument(), "preset": preset, "repo": repo}
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

// D2 and D3: a golden per starting schema and site, then a fixpoint.
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

// D4: a Jira rename sets one values/<id> per field and keeps the id; an
// added status gets its value and alias; a removed one changes nothing and warns.
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
	require.Equal(t, "3", valueDoc(t, status, "in-progress").Aliases[system], "a dead alias is kept")
	require.True(t, hasNote(notes, LevelWarn, "story/status:in-progress", "dead alias"))
	changes = s.importDoc(t, doc)
	require.Len(t, changes, 1)
	require.Equal(t, "story/status", changes[0].Key)
}

// D6: an entity aliased "" is never adopted, across runs, and nothing
// replaces it.
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

// D7: a person marks Won't Do canceled once and every derivation keeps it;
// Jira moving the status out of the done class resets it and warns.
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

// D8: a schema mapped against the company project refuses the team one,
// in Derive (before anything is imported) and in Compile.
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

// D9: two entities with one alias, which two clones can produce by merging,
// make Compile refuse.
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
