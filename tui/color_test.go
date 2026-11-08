package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

// switchColorOn sets `color: true` on one type of the live schema, keeping
// everything else the type says.
func switchColorOn(t *testing.T, repo *cache.RepoCache, typeKey string) {
	t.Helper()
	doc, _, err := host.SchemaExport(repo)
	require.NoError(t, err)
	typ, ok := doc.Types.Get(typeKey)
	require.True(t, ok)
	typ.Color = true
	doc.SetType(typeKey, typ)
	_, _, err = host.SchemaImport(repo, doc, false, false)
	require.NoError(t, err)
}

// TestIssueColorIsTheTerminalsPalette: every color maps to an index of the
// 16-color palette, never a 256-color or a truecolor one, and anything else
// draws plain.
func TestIssueColorIsTheTerminalsPalette(t *testing.T) {
	require.Len(t, ansiColors, len(schema.Colors))
	for _, id := range schema.Colors {
		tint, ok := issueColor(id)
		require.True(t, ok, id)
		require.Contains(t, []string{"1", "2", "3", "4", "5", "6", "9", "10", "11", "12", "13", "14"}, ansiColors[id])
		require.Equal(t, lipgloss.Color(ansiColors[id]), tint)
	}
	for _, other := range []any{"orange", "", nil, 3} {
		_, ok := issueColor(other)
		require.False(t, ok, "%v", other)
	}
}

// TestShowDrawsAndEditsTheColor: show's fields table draws a color as its id
// in that color, and space edits it with a picker over the set and a clear
// (doc/design/views.md, "The fields table").
func TestShowDrawsAndEditsTheColor(t *testing.T) {
	repo := testRepo(t)
	switchColorOn(t, repo, "task")
	id := newIssue(t, repo, map[string]any{"title": "one", "color": "cyan"})

	page := show(t, repo, id, []string{"color"})
	require.Equal(t, "color", page.field())
	require.Contains(t, plainView(page), "cyan")
	page = send(page, "tab").(*showPage) // off the row, so it is not reversed
	var line string
	for _, l := range strings.Split(page.View(), "\n") {
		if strings.Contains(ansiPattern.ReplaceAllString(l, ""), "cyan") {
			line = l
		}
	}
	require.Regexp(t, `\x1b\[(\d+;)*36m[^\x1b]*cyan`, line, "drawn in the terminal's cyan: %q", line)

	page = show(t, repo, id, []string{"color"})
	page = send(page, "space").(*showPage)
	require.NotNil(t, page.editor)
	require.Equal(t, schema.KindColor, page.editor.kind)
	var offered []string
	for _, c := range page.editor.picker.items {
		offered = append(offered, c.value)
	}
	require.Equal(t, append(append([]string{}, schema.Colors...), ""), offered, "the set, then the clear")
	page = send(page, "down", "enter").(*showPage)
	require.Equal(t, "bright-red", fieldOf(t, repo, id, "color"), "the one after cyan, where the picker opened")

	page = send(page, "space").(*showPage)
	for range len(schema.Colors) {
		page = send(page, "down").(*showPage)
	}
	send(page, "enter")
	require.Equal(t, "", fieldOf(t, repo, id, "color"), "the last choice clears")
}

// TestColorIsNotEditableOnATypeWithout: a type that does not switch color on
// has no field to edit.
func TestColorIsNotEditableOnATypeWithout(t *testing.T) {
	repo := testRepo(t)
	switchColorOn(t, repo, "story")
	kind, ok := fieldKind(repo, "task", schema.ColorKey)
	require.False(t, ok, "%s", kind)
	kind, ok = fieldKind(repo, "story", schema.ColorKey)
	require.True(t, ok)
	require.Equal(t, schema.KindColor, kind)
}
