package jq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	input, err := Input([]map[string]any{
		{"id": "a", "n": 2},
		{"id": "b", "n": 1},
	})
	require.NoError(t, err)

	// one value out: the whole projected array
	values, err := Run(`map(.id)`, input)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"a", "b"}}, values)

	// several values out: one per element
	values, err = Run(`.[] | .id`, input)
	require.NoError(t, err)
	require.Equal(t, []any{"a", "b"}, values)

	// sorting and selecting, the shape the default program has
	values, err = Run(`map(select(.n > 1)) | sort_by(.n) | reverse | map(.id)`, input)
	require.NoError(t, err)
	require.Equal(t, []any{[]any{"a"}}, values)
}

func TestRunErrors(t *testing.T) {
	// a program that does not parse
	_, err := Run(`map(`, nil)
	require.Error(t, err)

	// a program that fails at runtime
	_, err = Run(`.[] | .a`, "not an array")
	require.Error(t, err)
}

func TestInput(t *testing.T) {
	// Input goes through JSON, so a struct reads as its JSON shape
	in, err := Input(struct {
		Id string `json:"id"`
	}{Id: "x"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"id": "x"}, in)
}
