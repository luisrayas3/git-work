package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// listGhosts are a list's ghosts by the key of the row they stand under, ""
// for the roots', each with its group: what the drawn tree's scopes are.
func listGhosts(p *listPage) map[string][]*listRow {
	out := map[string][]*listRow{}
	for index := range p.nodes {
		if node := &p.nodes[index]; node.ghost {
			out[node.parent] = append(out[node.parent], &p.rows[index])
		}
	}
	return out
}

// ghostGroups is the groups of the ghosts under one parent.
func ghostGroups(p *listPage, parent string) []string {
	var out []string
	for index := range p.nodes {
		if node := &p.nodes[index]; node.ghost && node.parent == parent {
			out = append(out, node.group)
		}
	}
	return out
}

// withTheme gives the story a field no other type has, so that a draft can
// carry something a child gives up (C9's drop).
func withTheme(t *testing.T, repo *cache.RepoCache) {
	t.Helper()
	doc, err := schema.ParseDocument([]byte(`
types:
  story:
    fields:
      theme: {kind: text, name: Theme}
`))
	require.NoError(t, err)
	_, _, err = host.SchemaImport(repo, doc, false, false)
	require.NoError(t, err)
}

// TestChildScopeGhosts: every scope ends in a ghost (C8): the roots, and the
// child table of every opened parent, one per group of a level that groups,
// drawn at the children's indent, each opening the creator with the parent
// through the layer's relation, the type the scope's rows share and the
// group's value. Counts never count a ghost.
func TestChildScopeGhosts(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	one := newTyped(t, repo, "task", map[string]any{"title": "one", "parent": story, "status": "to-do"})
	newTyped(t, repo, "task", map[string]any{"title": "two", "parent": story, "status": "in-progress"})

	page := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","open":true,`+
		`"expand":{"relation":"children","group_by":"status","fields":["title","status"]}}`)
	ghosts := listGhosts(page)
	require.Len(t, ghosts[""], 1, "the roots' ghost")
	require.Len(t, ghosts[story], 2, "one per group of the child table")
	require.ElementsMatch(t, []string{"to-do", "in-progress"}, ghostGroups(page, story))
	require.Contains(t, plainView(page), "3 issues", "the ghosts are not issues")

	// each child ghost stands at the foot of its group, at the children's
	// indent, and opens the creator with the parent, the type and the group
	page.putCursorOn(one)
	send(page, "down")
	require.True(t, page.node().ghost, "below the last of a group, its ghost")
	require.Equal(t, story, page.node().parent)
	require.Equal(t, 1+indentWidth, colOf(lineWith(page, "›"), ghostPrefix), "drawn at the child table's indent")
	created := opened(t, page, "enter")
	require.Equal(t, issue.StringValue(story), created.fields["parent"], "the relation's stored key on the child")
	require.Equal(t, "task", created.typeKey(), "the scope's rows are tasks")
	require.Equal(t, issue.StringValue("to-do"), created.fields["status"], "the group's value")
	require.Nil(t, created.link, "stored on the child: nothing to write after")

	// the creator's issue lands under the parent, the cursor on it
	made, err := host.IssueNew(repo, host.IssueDocument{Fields: copyFields(map[string]issue.Value{
		schema.TypeKey: issue.StringValue("task"), schema.TitleKey: issue.StringValue("made"),
		"parent": issue.StringValue(story), "status": issue.StringValue("to-do")})})
	require.NoError(t, err)
	page.Update(createdMsg{id: made.String()})
	require.Equal(t, made.String(), page.currentId())
	require.Equal(t, story, page.node().parent)
	require.Equal(t, "created "+made.Human(), page.status)
}

// TestNoGhostUnderAFold: a folded parent hides its child table, ghost
// included; the fold's count counts issues, never the ghost.
func TestNoGhostUnderAFold(t *testing.T) {
	repo := testRepo(t)
	epic := newTyped(t, repo, "epic", map[string]any{"title": "the epic"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "parent": epic})
	newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"query":"map(select(.fields.type == \"epic\"))","expand":{"relation":"children","expand":0}}`)
	ghosts := listGhosts(page)
	require.Len(t, ghosts[""], 1)
	require.Empty(t, ghosts[epic], "folded: no ghost")
	require.Contains(t, rowOf(page, epic), "▸ 1", "the count is the story, not a ghost")

	// opening the epic: its table's ghost, and none under the folded story
	page.open[epic] = true
	page.rebuild()
	ghosts = listGhosts(page)
	require.Len(t, ghosts[epic], 1)
	require.Empty(t, ghosts[story])
	require.Contains(t, rowOf(page, story), "▸ 1")

	// the story opened, then the epic folded over it: the story's ghost is
	// hidden with it
	page.open[story] = true
	page.rebuild()
	require.Len(t, listGhosts(page)[story], 1)
	page.open[epic] = false
	page.rebuild()
	require.Empty(t, listGhosts(page)[story], "hidden under a fold above")
	require.Empty(t, listGhosts(page)[epic])
}

// TestLeafOpensToAnEmptyTable: a row whose layer below could hold children
// draws a faint `▸ 0`, and unfolds to an empty child table: its header,
// then its ghost, which tab lands on (C8). A row that can have none draws
// no fold cell.
func TestLeafOpensToAnEmptyTable(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	page := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","expand":"children","fields":["title","status"]}`)
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▸ 0")
	at := page.order[page.cursor]
	require.True(t, leafFold(page.nodes[at]), "drawn faint")
	require.Empty(t, listGhosts(page)[story])

	send(page, "tab")
	require.True(t, page.node().ghost, "tab into a leaf lands on its ghost")
	require.Equal(t, story, page.node().parent)
	require.Contains(t, rowOf(page, story), story[:idWidth]+" ▾")
	require.Contains(t, plainView(page), "1 issue", "the count never counts the ghost")

	lines := strings.Split(plainView(page), "\n")
	storyAt := indexOfLine(lines, story[:idWidth])
	require.Contains(t, lines[storyAt+1], "id", "the empty table draws its header")
	require.Contains(t, lines[storyAt+1], "status")
	require.Equal(t, indentWidth+1, colOf(lines[storyAt+1], "id"), "at the children's indent")
	require.Contains(t, lines[storyAt+2], ghostLabel, "then its ghost")

	created := opened(t, page, "enter")
	require.Equal(t, issue.StringValue(story), created.fields["parent"])
	require.Equal(t, "", created.typeKey(), "no rows, and two types store the parent: the type cell")

	// a task under it has no layer below: no fold cell
	task := newTyped(t, repo, "task", map[string]any{"title": "a task", "parent": story})
	page.Update(refreshMsg{})
	require.NotContains(t, rowOf(page, task), "▸")
}

// TestDraftIsEveryGhostsBase: the call's `draft` is the base of every
// ghost's draft (C9), the roots' and every child scope's: then the shared
// type where it names none, then the group, which wins on its key, then the
// relation; a type that cannot hold the child relation gives way, and what
// the resulting type has no field for is dropped.
func TestDraftIsEveryGhostsBase(t *testing.T) {
	repo := testRepo(t)
	withTheme(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "status": "to-do"})
	newTyped(t, repo, "task", map[string]any{"title": "a task", "parent": story})

	page := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","group_by":"status","open":true,`+
		`"expand":"children",`+
		`"draft":{"fields":{"type":"story","theme":"onboarding","priority":"high","status":"backlog"},"body":"why"}}`)

	root := listGhosts(page)[""][0].place
	require.Equal(t, issue.StringValue("story"), root.draft.Fields[schema.TypeKey], "the draft's type")
	require.Equal(t, issue.StringValue("onboarding"), root.draft.Fields["theme"])
	require.Equal(t, issue.StringValue("to-do"), root.draft.Fields["status"], "the group wins on its key")
	require.Equal(t, "why", root.draft.Body)

	child := listGhosts(page)[story][0].place
	require.Equal(t, issue.StringValue("task"), child.draft.Fields[schema.TypeKey],
		"a story cannot be a story's child: the draft's type gives way to the rows'")
	require.Equal(t, issue.StringValue(story), child.draft.Fields["parent"])
	require.Equal(t, issue.StringValue("high"), child.draft.Fields["priority"], "inherited")
	require.Equal(t, issue.StringValue("backlog"), child.draft.Fields["status"], "the child level does not group")
	_, themed := child.draft.Fields["theme"]
	require.False(t, themed, "a task has no theme: dropped")
	require.Equal(t, "why", child.draft.Body)

	// a draft with no type takes the shared one
	plain := list(t, repo, `{"query":"map(select(.fields.type == \"task\"))","draft":{"fields":{"priority":"low"}}}`)
	ghost := listGhosts(plain)[""][0].place
	require.Equal(t, issue.StringValue("task"), ghost.draft.Fields[schema.TypeKey])
	require.Equal(t, issue.StringValue("low"), ghost.draft.Fields["priority"])
}

// TestDraftIsCheckedAtOpen: a draft the planner would refuse is refused
// before a list, a gantt or a board draws, wherever it is opened from.
func TestDraftIsCheckedAtOpen(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	bad := `"draft":{"fields":{"type":"task","status":"nope"}}`
	for kind, kwargs := range map[string]string{
		"list":  `{` + bad + `}`,
		"gantt": `{"start":"start","stop":"stop",` + bad + `}`,
		"board": `{"columns":"status",` + bad + `}`,
	} {
		_, err := pageOf(t, repo, kind, kwargs)
		require.ErrorContains(t, err, "nope", kind)
		require.ErrorContains(t, err, "draft", kind)
	}
}

// TestGanttAndBoardGhostsTakeTheDraft: the gantt's child scopes end in
// ghosts as the list's do, and both start from `draft`; the board's
// column and lane win on their keys.
func TestGanttAndBoardGhostsTakeTheDraft(t *testing.T) {
	repo := testRepo(t)
	withDates(t, repo)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "start": "2026-10-01", "stop": "2026-10-09"})
	newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story, "status": "to-do"})

	chart := gantt(t, repo, `{"start":"start","stop":"stop","query":"map(select(.fields.type == \"story\"))",`+
		`"expand":"children","open":true,"draft":{"fields":{"priority":"high"}}}`)
	var child *bar
	for index := range chart.nodes {
		if chart.nodes[index].ghost && chart.nodes[index].parent == story {
			child = &chart.bars[index]
		}
	}
	require.NotNil(t, child, "the child scope's ghost")
	require.Equal(t, issue.StringValue(story), child.place.draft.Fields["parent"])
	require.Equal(t, issue.StringValue("task"), child.place.draft.Fields[schema.TypeKey])
	require.Equal(t, issue.StringValue("high"), child.place.draft.Fields["priority"])

	cards := board(t, repo, `{"columns":"status","query":"map(select(.fields.type == \"task\"))",`+
		`"draft":{"fields":{"status":"done","priority":"low"}}}`)
	for _, c := range cards.cards {
		if c.ghost && c.value == "to-do" {
			doc := cards.ghostDoc(&c)
			require.Equal(t, issue.StringValue("to-do"), doc.Fields["status"], "the column wins")
			require.Equal(t, issue.StringValue("low"), doc.Fields["priority"])
			require.Equal(t, issue.StringValue("task"), doc.Fields[schema.TypeKey])
		}
	}
}

// TestParentSideGhosts: a relation stored on the parent leaves the draft
// alone, and the opener writes the parent after Create (C8): `issue add`
// for a multi-relation, `issue set` for a single relation, whose ghost reads
// `(new, replacing …)` while it is set. A refused write leaves the issue
// created and says both halves.
func TestParentSideGhosts(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	// blocks is stored on the story, a set: the opener adds to it
	page := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","expand":"blocks","open":true}`)
	require.Contains(t, rowOf(page, story), "▸ 0", "a story can block: a fold cell")
	send(page, "tab")
	require.True(t, page.node().ghost)
	created := opened(t, page, "enter")
	_, holds := created.fields["blocks"]
	require.False(t, holds, "nothing of the relation in the draft")
	require.Equal(t, &parentLink{parent: story, field: "blocks", multi: true}, created.link)

	blocked := newTyped(t, repo, "task", map[string]any{"title": "blocked"})
	page.Update(createdMsg{id: blocked, link: created.link})
	require.Equal(t, []string{blocked}, idsOf(t, repo, story, "blocks"), "issue add on the parent")
	require.Equal(t, blocked, page.currentId(), "the new row under its parent, the cursor on it")
	require.Equal(t, "created "+blocked[:idWidth], page.status)

	// parent is single on a task: set, and its ghost names what it replaces
	old := newTyped(t, repo, "story", map[string]any{"title": "the old story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": old})
	tasks := list(t, repo, `{"query":"map(select(.fields.type == \"task\"))","fields":["title"],"expand":"parent","open":true}`)
	ghost := listGhosts(tasks)[task][0]
	require.Equal(t, "(new, replacing "+old[:idWidth]+")", ghost.cells[schema.TitleKey])
	require.Equal(t, &parentLink{parent: task, field: "parent", replacing: old}, ghost.place.link)
	require.Equal(t, issue.StringValue("story"), ghost.place.draft.Fields[schema.TypeKey], "the row there is a story")

	fresh := newTyped(t, repo, "story", map[string]any{"title": "the new story"})
	tasks.Update(createdMsg{id: fresh, link: ghost.place.link})
	require.Equal(t, `"`+fresh+`"`, field(t, repo, task, "parent"), "issue set on the parent")
	require.Equal(t, `"the old story"`, field(t, repo, old, "title"), "the replaced issue is not touched")

	// the parent's field refuses the type: the issue stays, both halves said
	sub := newTyped(t, repo, "subtask", map[string]any{"title": "wrong type"})
	tasks.Update(createdMsg{id: sub, link: &parentLink{parent: task, field: "parent"}})
	require.True(t, strings.HasPrefix(tasks.status, "created "+sub[:idWidth]+"; not added to "+task[:idWidth]+"'s parent: "), tasks.status)
	_, err := host.IssueSnapshot(repo, sub)
	require.NoError(t, err, "nothing undone")
	require.Equal(t, `"`+fresh+`"`, field(t, repo, task, "parent"))
}

// pageOf opens a list, a gantt or a board the way the renderer does, the
// error being what refuses it.
func pageOf(t *testing.T, repo *cache.RepoCache, kind, kwargs string) (page, error) {
	t.Helper()
	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(kwargs), &values))
	call, err := view.Parse(kind, values)
	require.NoError(t, err)
	switch kind {
	case view.KindGantt:
		return newGanttPage(repo, call)
	case view.KindBoard:
		return newBoardPage(repo, call)
	}
	return newListPage(repo, call)
}

// idsOf is a set field as ids.
func idsOf(t *testing.T, repo *cache.RepoCache, id, key string) []string {
	t.Helper()
	snap, err := host.IssueSnapshot(repo, id)
	require.NoError(t, err)
	ids, _ := issue.Strings(snap.Fields[key])
	return ids
}

// TestShowStoredRelationGhost: a side table over a single relation stored
// on the shown issue has the ghost too, naming what it replaces (C8).
func TestShowStoredRelationGhost(t *testing.T) {
	repo := testRepo(t)
	old := newTyped(t, repo, "story", map[string]any{"title": "the old story"})
	task := newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": old})

	page, err := specCall(t, repo, task, `{"expand":"parent"}`)
	require.NoError(t, err)
	require.NotNil(t, page.tables[0].ghost)
	require.Equal(t, &parentLink{parent: task, field: "parent", replacing: old}, page.tables[0].ghost.link)
	require.Contains(t, plainView(page), "(new, replacing "+old[:idWidth]+")")

	fresh := newTyped(t, repo, "story", map[string]any{"title": "the new story"})
	page.Update(createdMsg{id: fresh, link: page.tables[0].ghost.link})
	require.Equal(t, `"`+fresh+`"`, field(t, repo, task, "parent"))
	require.Equal(t, fresh, page.sideRowAt(page.sideCurrent()).id)
}

// TestOpenEverythingAndFoldAll: fold all opens the rows with children and
// leaves the leaves folded; open everything opens the leaves too, so every
// level's ghost is on the screen; both are on the palette beside each other,
// only where something nests.
func TestOpenEverythingAndFoldAll(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	leaf := newTyped(t, repo, "story", map[string]any{"title": "the leaf story"})
	newTyped(t, repo, "task", map[string]any{"title": "the task", "parent": story})

	page := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","expand":"children"}`)
	send(page, "alt+z")
	require.Contains(t, rowOf(page, story), "▾")
	require.Contains(t, rowOf(page, leaf), "▸ 0", "fold all leaves a leaf folded")
	require.Empty(t, listGhosts(page)[leaf])

	send(page, "alt+Z")
	require.Contains(t, rowOf(page, leaf), "▾", "open everything opens the leaves")
	require.Len(t, listGhosts(page)[leaf], 1)
	require.Len(t, listGhosts(page)[story], 1)

	send(page, "alt+z")
	require.Contains(t, rowOf(page, story), "▸ 1", "fold all again folds every row")
	require.Contains(t, rowOf(page, leaf), "▸ 0")

	// `open` keeps its meaning: the rows with children
	summary := list(t, repo, `{"query":"map(select(.fields.type == \"story\"))","expand":"children","open":true}`)
	require.Contains(t, rowOf(summary, leaf), "▸ 0")

	var letters []string
	for _, entry := range pageCommands(page) {
		letters = append(letters, entry.letter)
	}
	require.Subset(t, letters, []string{"z", "Z"})
	flat := list(t, repo, "")
	for _, entry := range pageCommands(flat) {
		require.NotEqual(t, "Z", entry.letter, "nothing nests: no open everything")
	}
}
