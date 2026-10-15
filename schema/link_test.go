package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const linkDocument = `
types:
  epic:
    fields:
      blocks: {kind: multi-relation, inverse: blocked-by, link: true}
      parent: {kind: relation, inverse: children}
  story:
    fields:
      status: {kind: enum, values: [{id: open}]}
      clones: {kind: relation, inverse: is-cloned-by, link: true, target_types: [story]}
      blocks: {kind: multi-relation, inverse: blocked-by, link: true}
      relates: {kind: multi-relation, link: true}
`

func compileDocument(t *testing.T, src string) ([]Entry, *Schema) {
	t.Helper()
	doc, err := ParseDocument([]byte(src))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(nil))
	changes, err := Reconcile(doc, nil, false)
	require.NoError(t, err)
	entries := applyChanges(t, nil, changes)
	compiled, err := Compile(entries)
	require.NoError(t, err)
	require.Empty(t, compiled.Problems)
	return entries, compiled
}

// TestLinkRoundTrips: the mark is a field attribute like inverse, written by
// import, read back by the compile, and exported, so export | import is a
// no-op (link-fields.md, L1, L6).
func TestLinkRoundTrips(t *testing.T) {
	entries, compiled := compileDocument(t, linkDocument)

	blocks, _ := compiled.Field("story", "blocks")
	require.True(t, blocks.Link)
	parent, _ := compiled.Field("epic", "parent")
	require.False(t, parent.Link)

	exported := Export(compiled)
	require.Contains(t, exported.String(), "link: true")
	again, err := Reconcile(exported, entries, true)
	require.NoError(t, err)
	require.Empty(t, again, "export | import emits zero operations")

	// unmarking is a removal of the attribute, one update
	unmarked, err := ParseDocument([]byte(`
types:
  story:
    fields:
      status: {kind: enum, values: [{id: open}]}
      clones: {kind: relation, inverse: is-cloned-by, target_types: [story]}
      blocks: {kind: multi-relation, inverse: blocked-by, link: true}
      relates: {kind: multi-relation, link: true}
`))
	require.NoError(t, err)
	changes, err := Reconcile(unmarked, entries, false)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, []string{AttrLink}, changes[0].Remove)
}

// TestLinkValidate: only a relation takes the mark, a built-in none, and
// `links` is reserved as a field key and as an inverse (L1, L3).
func TestLinkValidate(t *testing.T) {
	cases := map[string][2]string{
		"a link on a text": {"types:\n  epic:\n    fields:\n      note: {kind: text, link: true}\n",
			"field epic/note: link is for relation kinds, not text"},
		"a link on a built-in": {"types:\n  epic:\n    fields:\n      title: {kind: text, link: true}\n",
			"only its name and description are configurable"},
		"links as a field key": {"types:\n  epic:\n    fields:\n      links: {kind: multi-relation}\n",
			"field epic/links: links is reserved"},
		"links as an inverse": {"types:\n  epic:\n    fields:\n      blocks: {kind: multi-relation, inverse: links}\n",
			"inverse links is reserved"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(c[0]))
			require.NoError(t, err)
			require.ErrorContains(t, doc.Validate(nil), c[1])
		})
	}
}

// TestLinks: a type's link relations are its stored link fields in schema
// order, then the inverses of the link fields that may point at it, an
// inverse once however many types store it.
func TestLinks(t *testing.T) {
	_, s := compileDocument(t, linkDocument)

	names := func(typeKey string) (out []string) {
		for _, link := range s.Links(typeKey) {
			out = append(out, link.Name)
		}
		return out
	}

	// the inverse of clones targets stories alone, and parent is no link
	require.Equal(t, []string{"blocks", "blocked-by"}, names("epic"))
	epic := s.Links("epic")
	require.True(t, epic[0].Stored)
	require.False(t, epic[1].Stored)
	require.Len(t, epic[1].Sources, 2, "both types store blocks")

	require.Equal(t, []string{"clones", "blocks", "relates", "blocked-by", "is-cloned-by"}, names("story"))
	require.True(t, s.HasLinks())
}
