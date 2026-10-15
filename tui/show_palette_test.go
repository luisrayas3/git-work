package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/host"
)

// TestPaletteListsShowsOwn: a show page's palette has expand, link and
// unlink, and no other page's has (app.md A1, A6, A7).
func TestPaletteListsShowsOwn(t *testing.T) {
	repo := testRepo(t)
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	stack := stackOf(page, 140, 50)
	run(stack, "esc")
	drawn := drawnStack(stack)
	for _, item := range []string{"e expand", "l link", "u unlink"} {
		require.Contains(t, drawn, item)
	}

	stack = stackOf(list(t, repo, ""), 140, 50)
	run(stack, "esc")
	require.NotContains(t, drawnStack(stack), "l link")
}

// TestPaletteLink: `l` is the links row's picker from anywhere on the page:
// a relation by typing, then the issue; an inverse is written on the other
// issue, and the status names it (A6, L5).
func TestPaletteLink(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})
	other := newTyped(t, repo, "story", map[string]any{"title": "a blocking story"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story"})

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	stack := stackOf(page, 140, 50)
	page.focusStop(stopBox) // not on the links row: anywhere on the page

	run(stack, "esc")
	run(stack, "l")
	require.NotNil(t, page.editor, "esc l opens the relations")
	narrowAndChoose(page, "blo")
	require.NotNil(t, page.editor, "blo is blocks, which opens its members")
	narrowAndChoose(page, "blocked task")
	require.Contains(t, string(fieldsOf(t, repo, story)["blocks"]), one)

	// alt+l is the same key; an inverse writes the other issue
	send(page, "alt+l")
	narrowAndChoose(page, "blocked_by")
	narrowAndChoose(page, "a blocking")
	require.Contains(t, string(fieldsOf(t, repo, other)["blocks"]), story)
	require.Contains(t, page.status, human(other)+".blocks += "+human(story))
	require.Contains(t, linksLine(page), "2 · blocks 1 · blocked_by 1")
}

// TestPaletteUnlink: `u` picks one of the issue's links, stored or inverse,
// and removes it, one write; an inverse's on the other issue (A6).
func TestPaletteUnlink(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "blocks": []string{one}})
	other := newTyped(t, repo, "story", map[string]any{"title": "a blocking story", "blocks": []string{story}})
	lonely := newTyped(t, repo, "story", map[string]any{"title": "alone"})

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)

	send(page, "alt+u")
	require.NotNil(t, page.editor)
	drawn := plainView(page)
	require.Contains(t, drawn, "blocked task")
	require.Contains(t, drawn, "a blocking story")

	// the inverse: written on the other issue
	narrowAndChoose(page, "blocked_by")
	require.Nil(t, page.editor)
	require.NotContains(t, string(fieldsOf(t, repo, other)["blocks"]), story)
	require.Contains(t, page.status, human(other)+".blocks -= "+human(story))

	// the stored one: written on this issue
	send(page, "alt+u")
	narrowAndChoose(page, "blocked task")
	require.NotContains(t, string(fieldsOf(t, repo, story)["blocks"]), one)
	require.Contains(t, page.status, human(story)+".blocks -= "+human(one))
	require.Contains(t, linksLine(page), "—")

	// nothing to remove rings, and says so
	page, err = showCall(t, repo, `{"id":"`+lonely+`"}`)
	require.NoError(t, err)
	rings(t, page, "alt+u")
	require.Contains(t, page.status, "no link to remove")
}

// TestPaletteExpand: `e` toggles what the type's spec expands, for this page
// and the pages it opens; nothing is written, and the first line spells the
// changed spec as part of the call (A7).
func TestPaletteExpand(t *testing.T) {
	repo := testRepo(t)
	one := newTyped(t, repo, "task", map[string]any{"title": "blocked task"})
	story := newTyped(t, repo, "story", map[string]any{"title": "the story", "blocks": []string{one}})
	child := newTyped(t, repo, "task", map[string]any{"title": "a child", "parent": story})
	before, err := host.IssueLog(repo, story)
	require.NoError(t, err)

	page, err := showCall(t, repo, `{"id":"`+story+`"}`)
	require.NoError(t, err)
	require.NotEmpty(t, page.defaulted, "the built-in spec, drawn dim")
	require.Len(t, page.tables, 1, "links: the blocks table")

	send(page, "alt+e")
	require.NotNil(t, page.editor)
	drawn := plainView(page)
	for _, name := range []string{"parent", "blocks", "children", "blocked_by", "links"} {
		require.Contains(t, drawn, name)
	}
	require.Contains(t, lineWith(page, "a table per link relation"), "● expanded")

	// children on: a table of its own, the picker still open
	narrowAndChoose(page, "children")
	require.NotNil(t, page.editor, "several toggles in one go")
	require.Empty(t, page.defaulted, "the call's own now")
	require.JSONEq(t, `{"story":{"expand":["links","children"]}}`, string(page.call.Raw("shows")))
	require.Len(t, page.tables, 2)
	first := strings.Split(plainView(page), "\n")[0]
	require.Contains(t, first, `"children"`, "the first line is the call that drew the page")

	// links off, then done
	narrowAndChoose(page, "links")
	require.JSONEq(t, `{"story":{"expand":["children"]}}`, string(page.call.Raw("shows")))
	send(page, "esc")
	require.Nil(t, page.editor)
	require.Len(t, page.tables, 1)
	require.Equal(t, "children", page.tables[0].layer.Relation)

	// the pages it opens carry the same shows
	shown, err := showOf(repo, page.call.Raw("shows"), child)
	require.NoError(t, err)
	require.JSONEq(t, string(page.call.Raw("shows")), string(shown.call.Raw("shows")))

	// a spec's layer is kept as given; the last element out drops expand
	page, err = specCall(t, repo, story, `{"fields":["status"],"expand":{"relation":"children","fields":["status"]}}`)
	require.NoError(t, err)
	send(page, "alt+e")
	narrowAndChoose(page, "blocks")
	require.JSONEq(t, `{"story":{"fields":["status"],"expand":[{"relation":"children","fields":["status"]},"blocks"]}}`,
		string(page.call.Raw("shows")))
	narrowAndChoose(page, "children")
	narrowAndChoose(page, "blocks")
	require.JSONEq(t, `{"story":{"fields":["status"]}}`, string(page.call.Raw("shows")))
	require.Empty(t, page.tables)

	after, err := host.IssueLog(repo, story)
	require.NoError(t, err)
	require.Len(t, after, len(before), "nothing is written")
}
