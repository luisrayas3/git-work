package jira

import (
	"bytes"
	"encoding/json"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/util/sorted"
)

// The one canonical form (JS7): what the merge compares, the base records
// and the report prints.

var null = issue.Value("null")

// canon is a scalar as compared: compacted JSON, absent and null alike.
func canon(v issue.Value) issue.Value {
	if len(v) == 0 {
		return null
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return v
	}
	return buf.Bytes()
}

func same(a, b issue.Value) bool { return bytes.Equal(canon(a), canon(b)) }

// itemSet is a multi value keyed by its canonical items; absent and null are
// the empty set.
type itemSet map[string]issue.Value

func setOf(v issue.Value) itemSet {
	items, _ := issue.Items(canon(v))
	return setOfItems(items)
}

func setOfItems(items []issue.Value) itemSet {
	s := make(itemSet, len(items))
	for _, it := range items {
		s.add(it)
	}
	return s
}

func (s itemSet) add(it issue.Value) { c := canon(it); s[string(c)] = c }

// value is the set as stored: items sorted by canonical bytes, never null.
func (s itemSet) value() issue.Value {
	items := make([]issue.Value, 0, len(s))
	for _, k := range sorted.Keys(s) {
		items = append(items, s[k])
	}
	return issue.ItemsValue(items)
}

// canonical is a stored value as the merge compares it.
func canonical(v issue.Value, multi bool) issue.Value {
	if multi {
		return setOf(v).value()
	}
	return canon(v)
}

// sortedItems is items as a canonical set.
func sortedItems(items []issue.Value) issue.Value { return setOfItems(items).value() }
