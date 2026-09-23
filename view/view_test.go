package view

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func items(n int) []any {
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{
			"id":     "abcdef0123456789",
			"fields": map[string]any{"title": "a title", "status": "open"},
		})
	}
	return out
}

func TestSpecShape(t *testing.T) {
	spec, err := Board(items(2), map[string]string{"columns": "status", "card_title": "title"})
	require.NoError(t, err)

	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "board", got["view"])
	require.Equal(t, map[string]any{"columns": "status", "card_title": "title"}, got["bindings"])
	require.Len(t, got["items"], 2)

	// a spec is recognisable by a renderer, and by the printer
	require.True(t, IsSpec(got))
}

func TestRequiredBindings(t *testing.T) {
	// board needs its columns
	_, err := Board(items(1), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "columns")

	// gantt needs both ends, and says which one is missing
	_, err = Gantt(items(1), map[string]string{"start": "start_date"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "end")

	_, err = Gantt(items(1), map[string]string{"start": "start_date", "end": "due_date"})
	require.NoError(t, err)

	// list requires nothing
	spec, err := List(items(1), nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{}, spec.Bindings)
}

func TestUnknownBindingNamesTheAllowedOnes(t *testing.T) {
	_, err := Board(items(1), map[string]string{"columns": "status", "colums": "status"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "colums")
	// the error names what the view does take
	require.Contains(t, err.Error(), "card_title")
	require.Contains(t, err.Error(), "columns (required)")
}

func TestEmptyBindingIsRefused(t *testing.T) {
	_, err := Board(items(1), map[string]string{"columns": "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

func TestItemsAreIssueShaped(t *testing.T) {
	_, err := List([]any{"not an issue"}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "item 0")

	_, err = List([]any{map[string]any{"fields": map[string]any{}}}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no id")

	_, err = List([]any{map[string]any{"id": "abc"}}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no fields")

	// no items at all is a view of nothing, not an error, and never null
	spec, err := List(nil, nil)
	require.NoError(t, err)
	require.Equal(t, []any{}, spec.Items)
}

func TestUnknownKind(t *testing.T) {
	_, err := Build("burndown", items(1), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "board")
	require.Contains(t, err.Error(), "gantt")
	require.Contains(t, err.Error(), "list")
}

func TestIsSpecRefusesWhatIsNotOne(t *testing.T) {
	require.False(t, IsSpec(nil))
	require.False(t, IsSpec("a string"))
	require.False(t, IsSpec(map[string]any{"view": "burndown", "items": []any{}}))
	require.False(t, IsSpec(map[string]any{"view": "board"}))
	require.True(t, IsSpec(map[string]any{"view": "board", "items": []any{}}))
}
