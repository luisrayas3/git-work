package schema

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/entities/config"
	"github.com/git-bug/git-bug/entity"
)

// applyChanges is what the import command does to the store, in a map:
// enough to reconcile twice and compare.
func applyChanges(t *testing.T, current []Entry, changes []Change) []Entry {
	t.Helper()

	index := map[string]Entry{}
	var order []string
	for _, e := range current {
		key := string(e.Shape) + " " + e.Key
		index[key] = e
		order = append(order, key)
	}

	for _, change := range changes {
		key := string(change.Shape) + " " + change.Key
		switch change.Action {
		case ActionCreate:
			attributes := map[string]config.Value{}
			for name, value := range change.Set {
				attributes[name] = value
			}
			index[key] = Entry{
				Id:         entity.Id(fmt.Sprintf("%040x", len(order)+1)),
				Shape:      change.Shape,
				Key:        change.Key,
				Attributes: attributes,
			}
			order = append(order, key)
		case ActionUpdate:
			entry := index[key]
			for name, value := range change.Set {
				entry.Attributes[name] = value
			}
			for _, name := range change.Remove {
				delete(entry.Attributes, name)
			}
			index[key] = entry
		case ActionArchive:
			delete(index, key)
			for at, k := range order {
				if k == key {
					order = append(order[:at], order[at+1:]...)
					break
				}
			}
		}
	}

	out := make([]Entry, 0, len(index))
	for _, key := range order {
		out = append(out, index[key])
	}
	return out
}

func TestReconcileCreates(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)

	changes, err := Reconcile(doc, nil, false)
	require.NoError(t, err)

	// two types and four fields, types first so a field's type exists
	require.Len(t, changes, 6)
	require.Equal(t, config.ShapeType, changes[0].Shape)
	require.Equal(t, "epic", changes[0].Key)
	require.Equal(t, ActionCreate, changes[0].Action)
	require.Equal(t, config.ShapeType, changes[1].Shape)
	require.Equal(t, "story", changes[1].Key)
	require.Equal(t, config.ShapeField, changes[2].Shape)
	require.Equal(t, "epic/status", changes[2].Key)

	// ordinals are numbered by tens, and the values carry their own
	require.JSONEq(t, `10`, string(changes[0].Set[AttrOrdinal]))
	require.JSONEq(t, `20`, string(changes[1].Set[AttrOrdinal]))
	require.JSONEq(t, `{"name":"To Do","ordinal":10,"category":"unstarted"}`,
		string(changes[2].Set["values/to-do"]))
	require.JSONEq(t, `{}`, string(changes[5].Set["target_types/epic"]))
}

func TestReconcileIsANoOpTwice(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)

	changes, err := Reconcile(doc, nil, false)
	require.NoError(t, err)
	entries := applyChanges(t, nil, changes)

	again, err := Reconcile(doc, entries, true)
	require.NoError(t, err)
	require.Empty(t, again, "importing what is already there writes nothing")
}

func TestReconcileExportRoundTrip(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)

	changes, err := Reconcile(doc, nil, false)
	require.NoError(t, err)
	entries := applyChanges(t, nil, changes)

	compiled, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, compiled.Problems)

	exported := Export(compiled)
	again, err := Reconcile(exported, entries, true)
	require.NoError(t, err)
	require.Empty(t, again, "export | import emits zero operations")
}

func TestReconcileUpdatesOneValue(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)
	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))

	edited, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    fields:
      status:
        kind: enum
        name: Status
        values:
          - {id: to-do,       name: Not Started, category: unstarted}
          - {id: in-progress, name: In Progress, category: started}
          - {id: done,        name: Done,        category: completed}
      zebra: {kind: text, name: Zebra}
`))
	require.NoError(t, err)

	changes, err := Reconcile(edited, entries, false)
	require.NoError(t, err)
	require.Len(t, changes, 1, "one changed value is one operation on one entity")
	require.Equal(t, ActionUpdate, changes[0].Action)
	require.Equal(t, "epic/status", changes[0].Key)
	require.Len(t, changes[0].Set, 1)
	require.JSONEq(t, `{"name":"Not Started","ordinal":10,"category":"unstarted"}`,
		string(changes[0].Set["values/to-do"]))
	require.Empty(t, changes[0].Remove)
}

func TestReconcileRemovesAnAttribute(t *testing.T) {
	doc, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    fields:
      status: {kind: enum, name: Status, values: [{id: a}, {id: b}]}
`))
	require.NoError(t, err)
	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))

	edited, err := ParseDocument([]byte(`
types:
  epic:
    fields:
      status: {kind: enum, name: Status, values: [{id: a}]}
`))
	require.NoError(t, err)

	changes, err := Reconcile(edited, entries, false)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	// the type lost its name
	require.Equal(t, []string{AttrName}, changes[0].Remove)
	// the field lost a value
	require.Equal(t, []string{"values/b"}, changes[1].Remove)
}

func TestReconcilePruneArchives(t *testing.T) {
	doc, err := ParseDocument([]byte(sampleDocument))
	require.NoError(t, err)
	entries := applyChanges(t, nil, mustReconcile(t, doc, nil, false))

	partial, err := ParseDocument([]byte(`
types:
  epic:
    name: Epic
    fields:
      status:
        kind: enum
        name: Status
        values:
          - {id: to-do,       name: To Do,       category: unstarted}
          - {id: in-progress, name: In Progress, category: started}
          - {id: done,        name: Done,        category: completed}
      zebra: {kind: text, name: Zebra}
`))
	require.NoError(t, err)

	// an upsert leaves what the file does not mention alone
	changes, err := Reconcile(partial, entries, false)
	require.NoError(t, err)
	require.Empty(t, changes)

	// --prune archives it, fields before their type
	changes, err = Reconcile(partial, entries, true)
	require.NoError(t, err)
	require.Len(t, changes, 3)
	require.Equal(t, ActionArchive, changes[0].Action)
	require.Equal(t, "story/parent", changes[0].Key)
	require.Equal(t, "story/status", changes[1].Key)
	require.Equal(t, config.ShapeType, changes[2].Shape)
	require.Equal(t, "story", changes[2].Key)
}

func TestAssignOrdinals(t *testing.T) {
	// nothing known: numbered by tens
	require.Equal(t, map[string]int{"a": 10, "b": 20, "c": 30},
		assignOrdinals([]string{"a", "b", "c"}, nil))

	// unchanged order keeps its numbers
	current := map[string]int{"a": 10, "b": 20, "c": 30}
	require.Equal(t, current, assignOrdinals([]string{"a", "b", "c"}, current))

	// a new entry between two takes a number between them
	out := assignOrdinals([]string{"a", "x", "b"}, current)
	require.Equal(t, 10, out["a"])
	require.Equal(t, 20, out["b"])
	require.Greater(t, out["x"], 10)
	require.Less(t, out["x"], 20)

	// a new entry at the end follows the last
	out = assignOrdinals([]string{"a", "b", "c", "d"}, current)
	require.Equal(t, 40, out["d"])

	// no room between neighbours renumbers the whole list
	tight := map[string]int{"a": 1, "b": 2}
	out = assignOrdinals([]string{"a", "x", "b"}, tight)
	require.Equal(t, map[string]int{"a": 10, "x": 20, "b": 30}, out)

	// a move keeps the longest run and renumbers what moved
	out = assignOrdinals([]string{"c", "a", "b"}, current)
	require.Equal(t, 10, out["a"])
	require.Equal(t, 20, out["b"])
	require.Less(t, out["c"], 10)
}

func mustReconcile(t *testing.T, doc *Document, current []Entry, prune bool) []Change {
	t.Helper()
	changes, err := Reconcile(doc, current, prune)
	require.NoError(t, err)
	return changes
}
