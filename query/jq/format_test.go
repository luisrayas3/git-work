package jq

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func formatted(t *testing.T, src string, width int) string {
	t.Helper()
	out, err := Format(src, width)
	require.NoError(t, err)
	// what comes out still means what went in
	back, err := Format(out, 1000)
	require.NoError(t, err)
	one, err := Format(src, 1000)
	require.NoError(t, err)
	require.Equal(t, strings.Join(strings.Fields(one), " "), strings.Join(strings.Fields(back), " "), "the layout changed the program")
	return out
}

// TestFormatBreaksTopLevelPipesAlways: a pipeline reads as a pipeline,
// however short.
func TestFormatBreaksTopLevelPipesAlways(t *testing.T) {
	require.Equal(t, "map(select(.fields.archived != true))\n| sort_by(.edit_time.lamport, .edit_time.timestamp)\n| reverse",
		formatted(t, "map(select(.fields.archived != true))\n\t| sort_by(.edit_time.lamport, .edit_time.timestamp)\n\t| reverse", 200))
	require.Equal(t, ".a\n| .b", formatted(t, ".a|.b", 200))
	require.Equal(t, ".", formatted(t, " . ", 200))
}

// TestFormatLeavesWhatFits: inside parentheses nothing breaks that fits.
func TestFormatLeavesWhatFits(t *testing.T) {
	src := `map(select(.fields.title | test("a|b"))) | length`
	require.Equal(t, "map(select(.fields.title | test(\"a|b\")))\n| length", formatted(t, src, 200))
}

// TestFormatBreaksAtTheSeam: what does not fit breaks at its own node, and
// nothing else does.
func TestFormatBreaksAtTheSeam(t *testing.T) {
	src := `map(select(.fields.status != "done" and .fields.priority == "highest") | {id, title: .fields.title})`
	require.Equal(t, strings.Join([]string{
		`map(`,
		`  select(.fields.status != "done" and .fields.priority == "highest")`,
		`  | { id, title: .fields.title }`,
		`)`,
	}, "\n"), formatted(t, src, 72))

	require.Equal(t, strings.Join([]string{
		`map(`,
		`  select(`,
		`    .fields.status != "done"`,
		`    and .fields.priority == "highest"`,
		`  )`,
		`  | { id, title: .fields.title }`,
		`)`,
	}, "\n"), formatted(t, src, 40))

	// an object is one key per line, and a block that opens after the pipe
	// aligns under its own brace
	require.Equal(t, strings.Join([]string{
		`.[]`,
		`| {`,
		`    id,`,
		`    title: .fields.title`,
		`  }`,
	}, "\n"), formatted(t, `.[] | {id, title: .fields.title}`, 26))
}

func TestFormatKeywords(t *testing.T) {
	src := `if .a then "long enough to break the line" elif .b then "second branch" else "third branch" end`
	require.Equal(t, strings.Join([]string{
		`if .a then`,
		`  "long enough to break the line"`,
		`elif .b then`,
		`  "second branch"`,
		`else`,
		`  "third branch"`,
		`end`,
	}, "\n"), formatted(t, src, 40))

	src = `reduce .[] as $x ({count: 0, total: 0}; .count += 1 | .total += $x.amount)`
	require.Equal(t, strings.Join([]string{
		`reduce .[] as $x (`,
		`  { count: 0, total: 0 };`,
		`  .count += 1`,
		`  | .total += $x.amount`,
		`)`,
	}, "\n"), formatted(t, src, 30))

	src = `def done: .fields.status == "done"; def open: done | not; map(select(open))`
	require.Equal(t, strings.Join([]string{
		`def done: .fields.status == "done";`,
		`def open: done | not;`,
		`map(select(open))`,
	}, "\n"), formatted(t, src, 60))

	src = `.[] as $item | try ($item.a | tonumber) catch "not a number at all"`
	require.Equal(t, strings.Join([]string{
		`.[] as $item`,
		`| try ($item.a | tonumber)`,
		`  catch "not a number at all"`,
	}, "\n"), formatted(t, src, 30))
}

// TestFormatCommaChain: items one per line when they do not fit, with the
// comma where a reader expects it.
func TestFormatCommaChain(t *testing.T) {
	src := `[.fields.title, .fields.status, .fields.priority, .fields.assignee]`
	require.Equal(t, strings.Join([]string{
		`[`,
		`  .fields.title,`,
		`  .fields.status,`,
		`  .fields.priority,`,
		`  .fields.assignee`,
		`]`,
	}, "\n"), formatted(t, src, 30))
}

func TestFormatRefusesWhatDoesNotParse(t *testing.T) {
	_, err := Format("map(select(", 80)
	require.Error(t, err)
}
