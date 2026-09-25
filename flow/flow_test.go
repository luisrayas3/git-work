package flow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMinimal(t *testing.T) {
	def, err := Parse("def board():\n    pass\n")
	require.NoError(t, err)
	require.Equal(t, "board", def.Name)
	require.Equal(t, "", def.Description)
	require.Empty(t, def.Params)
}

func TestParseDocstring(t *testing.T) {
	def, err := Parse(`def board(iteration="current"):
    """Kanban of one iteration, a column per status."""
    return None
`)
	require.NoError(t, err)
	require.Equal(t, "board", def.Name)
	require.Equal(t, "Kanban of one iteration, a column per status.", def.Description)
}

func TestParseDocstringDedented(t *testing.T) {
	def, err := Parse(`def report():
    """Weekly status.

    One paragraph per epic,
    generated from the op log.
    """
    return None
`)
	require.NoError(t, err)
	require.Equal(t,
		"Weekly status.\n\nOne paragraph per epic,\ngenerated from the op log.",
		def.Description)
}

func TestParseNoDocstring(t *testing.T) {
	// a first statement that is not a string is a statement, not a description
	def, err := Parse("def board():\n    x = 1\n    return x\n")
	require.NoError(t, err)
	require.Equal(t, "", def.Description)
}

func TestParseParams(t *testing.T) {
	def, err := Parse(`def plan(epic, iteration="current", limit=10, ratio=1.5, dry=True,
             owner=None, labels=["a","b"], columns={"status":"In progress"}, offset=-2):
    """Plan."""
    return None
`)
	require.NoError(t, err)
	require.Len(t, def.Params, 9)

	require.Equal(t, "epic", def.Params[0].Name)
	require.False(t, def.Params[0].HasDefault)
	require.Nil(t, def.Params[0].Default)

	for _, tc := range []struct {
		at      int
		name    string
		defJSON string
	}{
		{1, "iteration", `"current"`},
		{2, "limit", `10`},
		{3, "ratio", `1.5`},
		{4, "dry", `true`},
		{5, "owner", `null`},
		{6, "labels", `["a","b"]`},
		{7, "columns", `{"status":"In progress"}`},
		{8, "offset", `-2`},
	} {
		param := def.Params[tc.at]
		require.Equal(t, tc.name, param.Name)
		require.True(t, param.HasDefault, tc.name)
		require.JSONEq(t, tc.defJSON, string(param.Default), tc.name)
	}
}

func TestParseRefusals(t *testing.T) {
	for name, src := range map[string]string{
		"empty":           "",
		"comment only":    "# nothing here\n",
		"two defs":        "def a():\n    pass\n\ndef b():\n    pass\n",
		"load":            "load('other.star', 'helper')\n\ndef a():\n    pass\n",
		"bare expression": "def a():\n    pass\n\na()\n",
		"assignment":      "X = 1\n\ndef a():\n    pass\n",
		"no def":          "X = 1\n",
		"leading docstring": `"""a module docstring"""

def a():
    pass
`,
		"args":             "def a(*args):\n    pass\n",
		"kwargs":           "def a(**kwargs):\n    pass\n",
		"computed default": "def a(x=1+1):\n    pass\n",
		"name default":     "def a(x=other):\n    pass\n",
		"call default":     "def a(x=len('ab')):\n    pass\n",
		"non-string key":   "def a(x={1: 'a'}):\n    pass\n",
		"nested computed":  "def a(x=[1, 2+2]):\n    pass\n",
		"upper case name":  "def Board():\n    pass\n",
		"leading digit":    "def _board():\n    pass\n",
		"syntax error":     "def a(:\n",
	} {
		_, err := Parse(src)
		require.Error(t, err, name)
	}
}

func TestParseRefusalsNameTheLine(t *testing.T) {
	_, err := Parse("def a():\n    pass\n\n\ndef b():\n    pass\n")
	require.ErrorContains(t, err, "line 5")

	_, err = Parse("load('other.star', 'helper')\n\ndef a():\n    pass\n")
	require.ErrorContains(t, err, "line 1")
	require.ErrorContains(t, err, "a load")

	_, err = Parse("def a(\n      *args):\n    pass\n")
	require.ErrorContains(t, err, "line 2")
}

func TestParseLongName(t *testing.T) {
	long := "def "
	for range MaxNameLength + 1 {
		long += "a"
	}
	_, err := Parse(long + "():\n    pass\n")
	require.ErrorContains(t, err, "longer than")
}

func TestParseDuplicateParam(t *testing.T) {
	// Starlark's own parser may or may not catch this; the flow refuses it either way.
	_, err := Parse("def a(x, x=1):\n    pass\n")
	require.Error(t, err)
}
