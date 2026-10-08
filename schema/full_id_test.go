package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFullIdValue(t *testing.T) {
	c := testChecker(t)
	full := func(key, value string) string {
		return string(c.FullIdValue("task", key, json.RawMessage(value)))
	}

	// a relation's alias is the issue it names
	require.Equal(t, `"t1"`, full("parent", `"PROJ-1"`))
	require.Equal(t, `["e1","t1"]`, full("blocks", `["e1","PROJ-1"]`))
	// what is already an id is left byte for byte
	require.Equal(t, `[ "e1" ]`, full("blocks", `[ "e1" ]`))
	// what does not resolve is left, for the check to refuse
	require.Equal(t, `"nope"`, full("parent", `"nope"`))
	require.Equal(t, `null`, full("parent", `null`))
	// a field that is no relation, or no field of the type, is never looked up
	require.Equal(t, `["PROJ-1"]`, full("tags", `["PROJ-1"]`))
	require.Equal(t, `"PROJ-1"`, string(c.FullIdValue("epic", "parent", json.RawMessage(`"PROJ-1"`))))

	item := func(key, value string) string {
		return string(c.FullIdItem("task", key, json.RawMessage(value)))
	}
	require.Equal(t, `"t1"`, item("blocks", `"PROJ-1"`))
	require.Equal(t, `"PROJ-1"`, item("tags", `"PROJ-1"`))
	// a single relation has no items: add refuses it, nothing is resolved
	require.Equal(t, `"PROJ-1"`, item("parent", `"PROJ-1"`))
}

func TestFullIdItemWithoutASchema(t *testing.T) {
	s, err := Compile(nil)
	require.NoError(t, err)
	c := &Checker{Schema: s, Resolver: hexResolver{}}

	// the bootstrap guess: four hex digits or more naming one issue
	require.Equal(t, `"abcd0123"`, string(c.FullIdItem("", "blocks", json.RawMessage(`"abcd"`))))
	require.Equal(t, `"abc"`, string(c.FullIdItem("", "blocks", json.RawMessage(`"abc"`))))
	require.Equal(t, `"PROJ-1"`, string(c.FullIdItem("", "blocks", json.RawMessage(`"PROJ-1"`))))
	// and a set, whose kind nothing knows, is written as given
	require.Equal(t, `"abcd"`, string(c.FullIdValue("", "parent", json.RawMessage(`"abcd"`))))
}

// hexResolver knows one issue, abcd0123, by any prefix or by PROJ-1.
type hexResolver struct{ testResolver }

func (hexResolver) IssueId(ref string) (string, error) {
	if ref == "PROJ-1" || strings.HasPrefix("abcd0123", ref) {
		return "abcd0123", nil
	}
	return testResolver{}.IssueId(ref)
}
