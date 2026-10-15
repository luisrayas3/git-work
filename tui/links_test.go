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

// The jira preset marks blocks a link, so on every type it draws as the
// links row, and blocked_by, its inverse, is a link too (link-fields.md).

// rowAt is the index of the first fields-table row of a key, or -1.
func rowAt(rows []tableRow, key string) int {
	for at, row := range rows {
		if row.key == key && row.first {
			return at
		}
	}
	return -1
}

// linksLine is the drawn line of the links row, the cursor on it or not.
func linksLine(p page) string {
	for _, line := range strings.Split(plainView(p), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " ›"), "links ") {
			return line
		}
	}
	return ""
}

// narrowAndChoose narrows the open picker to a text and chooses what the
// cursor lands on.
func narrowAndChoose(p page, text string) page {
	// a picker is typed into at once (app.md A5); typeText ends in enter
	return typeText(p, text)
}

// fieldsOf reads an issue's fields as JSON values.
func fieldsOf(t *testing.T, repo *cache.RepoCache, id string) map[string]issue.Value {
	t.Helper()
	snapshot, err := host.IssueSnapshot(repo, id)
	require.NoError(t, err)
	return snapshot.Fields
}

// TestLinksRow: the link fields are one row, links, at the first one's
// place, the count then each relation's, stored first, then inverses; an
// issue with none says so with a dash (L3).
func TestLinksRow(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})
	two := newTyped(t, repo, "task", map[string]any{"title": "other blocked"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "blocks": []string{one, two}})
	blocker := newTyped(t, repo, "story", map[string]any{"title": "the blocker", "blocks": []string{story}})
	lonely := newTyped(t, repo, "story", map[string]any{"title": "alone"})

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	require.Contains(t, linksLine(page), "3 · blocks 2 · blocked_by 1")
	require.Equal(t, -1, rowAt(page.rows, "blocks"), "a link field is not a row of its own")
	require.Greater(t, rowAt(page.rows, schema.LinksKey), rowAt(page.rows, "iteration"),
		"the row is where blocks would be, after iteration in the preset")
	_ = blocker

	page, err = showCall(t, repo, `{"id":"`+lonely+`"}`)
	require.NoError(t, err)
	require.Contains(t, linksLine(page), "—")

	// a call's fields naming a link field draws it, and leaves it out of the
	// count; naming links places the row, and leaving it out drops it
	page, err = showCall(t, repo, `{"id":"`+story+`","fields":["links","blocks"]}`)
	require.NoError(t, err)
	require.Contains(t, linksLine(page), "1 · blocked_by 1")
	require.NotEqual(t, -1, rowAt(page.rows, "blocks"))
	require.Less(t, rowAt(page.rows, schema.LinksKey), rowAt(page.rows, "blocks"))

	page, err = showCall(t, repo, `{"id":"`+story+`","fields":["status"]}`)
	require.NoError(t, err)
	require.Equal(t, -1, rowAt(page.rows, schema.LinksKey))
}

// TestLinksTables: links in show's expand is a table per link relation with
// entries, stored first, then inverses, its layer's fields applying to each;
// enter on the links row goes to the first of them (L4, L5).
func TestLinksTables(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task", "status": "in-progress"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "blocks": []string{one}})
	blocker := newTyped(t, repo, "story", map[string]any{"title": "the blocker", "blocks": []string{story}})

	page, err := showCall(t, repo, `{"id":"`+story+`","expand":[{"relation":"links","fields":["status"]}]}`)
	require.NoError(t, err)
	require.Len(t, page.tables, 2, "blocks and blocked_by, the rest have no entry")
	require.Equal(t, "blocks", page.tables[0].layer.Relation)
	require.Equal(t, "blocked_by", page.tables[1].layer.Relation)
	require.Contains(t, lineWith(page, one[:idWidth]), "in-progress", "the layer's fields")
	require.NotEmpty(t, lineWith(page, blocker[:idWidth]))

	page.row = rowAt(page.rows, schema.LinksKey)
	send(page, "enter")
	require.Equal(t, stopSide, page.current().stop)
	require.Equal(t, one, page.sideRowAt(page.sideCurrent()).id)

	// links with no tables to go to rings
	page, err = showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	page.row = rowAt(page.rows, schema.LinksKey)
	rings(t, page, "enter")

	// the layer is checked as any is: its fields against what a link reaches
	_, err = showCall(t, repo, `{"id":"`+story+`","expand":{"relation":"links","fields":["nope"]}}`)
	require.ErrorContains(t, err, "names field nope")

	// an issue with no links draws no table and no (none)
	lonely := newTyped(t, repo, "story", map[string]any{"title": "alone"})
	page, err = showCall(t, repo, `{"id":"`+lonely+`","expand":["links"]}`)
	require.NoError(t, err)
	require.Empty(t, page.tables)
}

// TestLinksRefusedWhereANestingIs: a nested layer is one relation, and links
// is several, so a list's and a gantt's expand refuses it by name (L4).
func TestLinksRefusedWhereANestingIs(t *testing.T) {
	for _, kind := range []string{view.KindList, view.KindGantt} {
		args := map[string]json.RawMessage{
			"expand": json.RawMessage(`{"relation":"children","expand":"links"}`),
			"start":  json.RawMessage(`"due"`), "stop": json.RawMessage(`"due"`),
		}
		if kind == view.KindList {
			delete(args, "start")
			delete(args, "stop")
		}
		_, err := view.Parse(kind, args)
		require.ErrorContains(t, err, "layer 2 names links", kind)
	}
}

// TestLinksEdit: space on the links row picks a relation, stored or inverse,
// then an issue; a stored relation is written on this issue, an inverse on
// the other one, one add or remove each, the status naming the issue
// written (L5).
func TestLinksEdit(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})
	other := newTyped(t, repo, "story", map[string]any{"title": "a blocking story"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})
	newTyped(t, repo, "iteration", map[string]any{"title": "the sprint"})

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	page.row = rowAt(page.rows, schema.LinksKey)

	send(page, "space")
	require.NotNil(t, page.editor)
	drawn := plainView(page)
	require.Contains(t, drawn, "blocks")
	require.Contains(t, drawn, "blocked_by")

	// stored: this issue's blocks gains the task
	narrowAndChoose(page, "blocks")
	require.NotNil(t, page.editor, "the relation opens its members")
	narrowAndChoose(page, "blocked task")
	require.Nil(t, page.editor)
	require.Contains(t, string(fieldsOf(t, repo, story)["blocks"]), one)
	require.Contains(t, page.status, human(story)+".blocks += "+human(one))
	require.Contains(t, linksLine(page), "1 · blocks 1")

	// inverse: the other story's blocks gains this one, and this one is
	// not written
	send(page, "space")
	narrowAndChoose(page, "blocked_by")
	drawn = plainView(page)
	require.Contains(t, drawn, "a blocking story")
	require.NotContains(t, drawn, "the sprint", "only the types that store blocks, which an iteration does not")
	narrowAndChoose(page, "a blocking")
	require.Contains(t, string(fieldsOf(t, repo, other)["blocks"]), story)
	require.Contains(t, page.status, human(other)+".blocks += "+human(story))
	require.Contains(t, linksLine(page), "2 · blocks 1 · blocked_by 1")

	// choosing a member again removes it
	send(page, "space")
	narrowAndChoose(page, "blocks")
	require.Contains(t, plainView(page), "● linked")
	narrowAndChoose(page, "blocked task")
	require.NotContains(t, string(fieldsOf(t, repo, story)["blocks"]), one)
	require.Contains(t, page.status, "-=")
}

// TestLinksOnADraft: new draws the row over the draft's stored link fields,
// with no inverse, and a member chosen goes into the draft (L5).
func TestLinksOnADraft(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})

	page := newView(t, repo, `{"doc":{"fields":{"type":"story","title":"draft","blocks":["`+one+`"]}}}`)
	send(page, "esc") // out of the title's editor
	require.Contains(t, linksLine(page), "1 · blocks 1")
	require.Equal(t, -1, rowAt(page.rows, "blocks"))

	page.focus = stopFields
	page.row = rowAt(page.rows, schema.LinksKey)
	send(page, "space")
	drawn := plainView(page)
	require.NotContains(t, drawn, "blocked_by", "a draft has no inverse")
	narrowAndChoose(page, "blocks")
	narrowAndChoose(page, "blocked task")
	require.False(t, strings.Contains(string(page.fields["blocks"]), one), "a member chosen again leaves the draft")
	require.Contains(t, linksLine(page), "—")
}
