package host

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodeRows(t *testing.T, text string) []any {
	t.Helper()
	var values []any
	require.NoError(t, json.Unmarshal([]byte(text), &values))
	return []any{values}
}

// TestViewRowsTakeAKeyAndChildren: a row is an issue with two top-level keys
// an issue never has, `key` and `children`, and its id may be left out where
// a key is given (doc/design/query-rows.md, R1 to R3).
func TestViewRowsTakeAKeyAndChildren(t *testing.T) {
	rows, ok := ViewRows(decodeRows(t, `[
		{"id":"a","fields":{}},
		{"id":"a","key":"a@ada","fields":{},"children":["b",{"key":"c@ada","fields":{}}]},
		{"key":"none@ada","fields":{"title":"(none)"},"children":[]}
	]`))
	require.True(t, ok)
	require.Len(t, rows, 3)

	require.Equal(t, "a", RowKey(rows[0]), "a key left out is the id")
	require.Equal(t, "a@ada", RowKey(rows[1]))
	require.Equal(t, "a", RowId(rows[1]))
	require.Equal(t, "", RowId(rows[2]), "a row that stands for nothing")
	children, lists := RowChildren(rows[2])
	require.True(t, lists, "an empty list is a list")
	require.Empty(t, children)
	_, lists = RowChildren(rows[0])
	require.False(t, lists)

	// not rows: no id and no key, a key that is not a string, children that
	// are not ids or rows
	for _, text := range []string{
		`[{"fields":{}}]`,
		`[{"key":3,"fields":{}}]`,
		`[{"id":"a","fields":{},"children":"b"}]`,
		`[{"id":"a","fields":{},"children":[3]}]`,
		`[{"id":"a","fields":{},"children":[{"fields":{}}]}]`,
	} {
		_, ok := ViewRows(decodeRows(t, text))
		require.False(t, ok, text)
	}
}

// TestCheckRowKeysRefusesARepeatedKey: keys are unique among a view's rows,
// listed children included, and none starts with the ghost's prefix; two
// rows of one issue with no key between them are the one row they always
// were.
func TestCheckRowKeysRefusesARepeatedKey(t *testing.T) {
	check := func(text string) error {
		rows, ok := ViewRows(decodeRows(t, text))
		require.True(t, ok, text)
		return CheckRowKeys(rows)
	}

	require.NoError(t, check(`[{"id":"a","fields":{}},{"id":"a","fields":{}}]`))
	require.NoError(t, check(`[{"id":"a","key":"a@1","fields":{}},{"id":"a","key":"a@2","fields":{}},{"id":"a","fields":{}}]`))

	require.EqualError(t, check(`[{"key":"x","fields":{}},{"key":"x","fields":{}}]`), "key x is given twice")
	require.EqualError(t, check(`[{"key":"x","fields":{},"children":[{"key":"x","fields":{}}]}]`), "key x is given twice")
	require.EqualError(t, check(`[{"key":"a","fields":{}},{"id":"a","fields":{}}]`), "key a is given twice")
	require.EqualError(t, check(`[{"id":"a","fields":{}},{"key":"a","fields":{}}]`), "key a is given twice")
	require.EqualError(t, check(`[{"key":"+new","fields":{}}]`), "key +new starts with +, which is the ghost's")
}

// TestIssueRowsDropWhatIsNotAnIssue: the board and the matrix draw issues,
// so a row with no id is dropped and an issue two rows stand for is drawn
// once.
func TestIssueRowsDropWhatIsNotAnIssue(t *testing.T) {
	rows := IssueRows(decodeRows(t, `[
		{"id":"a","key":"a@1","fields":{}},
		{"key":"none","fields":{}},
		{"id":"a","key":"a@2","fields":{}},
		{"id":"b","fields":{}}
	]`))
	require.Len(t, rows, 2)
	require.Equal(t, "a@1", RowKey(rows[0]))
	require.Equal(t, "b", RowId(rows[1]))
}
