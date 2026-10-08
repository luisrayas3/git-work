package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// The ghost: a dim `+` row at the foot of each scope a view draws — a group
// on a list and a gantt, a column of a lane on a board — that opens the
// creator prefilled with the drop's write set, so that the issue it creates
// is already where it was added (doc/design/create.md, C6 and C7).
//
// It is a row the cursor reaches, enter opens the creator, space rings, copy
// copies nothing, and it is never a rank sibling: a grab skips it, a drop
// above it is a drop at the foot, and the rank fills do not count it. The
// filter hides it with the rows it narrows away. No rank is written for the
// issue it creates: a null rank already sorts last, where the ghost stood.

// ghostPrefix starts every ghost's id, which no issue id can start with.
const ghostPrefix = "+"

// ghostLabel is what a ghost draws where a row draws its first field.
const ghostLabel = "(new)"

// ghostId is a ghost's id: unique per scope, because the drawing order keys
// rows by id, and never an issue's.
func ghostId(scope ...string) string {
	return ghostPrefix + strings.Join(scope, "\x00")
}

func isGhost(id string) bool {
	return strings.HasPrefix(id, ghostPrefix)
}

// sharedType is the type every row of a scope has, or "" where they differ
// or there are none: the rows a query produced are the best witness of what
// it selects, since a jq program cannot be inverted (C6).
func sharedType(typeKeys []string) string {
	shared := ""
	for _, typeKey := range typeKeys {
		switch {
		case typeKey == "":
			return ""
		case shared == "":
			shared = typeKey
		case shared != typeKey:
			return ""
		}
	}
	return shared
}

// groupPrefill is what a ghost in a group prefills the `group_by` field
// with: the value a row already in the group holds, as a drop writes it
// (group.go); nothing for (none), and nothing for a set-valued field, which
// a drop refuses too.
func groupPrefill(repo *cache.RepoCache, typeKey, groupBy, group string, raw any) (issue.Value, bool) {
	if groupBy == "" || group == noGroup || groupBy == schema.TypeKey {
		return nil, false
	}
	kind, known := fieldKind(repo, typeKey, groupBy)
	if !known || kind.IsMulti() {
		return nil, false
	}
	value := groupValue(raw, group)
	if issue.IsNull(value) {
		return nil, false
	}
	return value, true
}

// openNew opens the creator over the view, on a draft.
func openNew(repo *cache.RepoCache, doc host.IssueDocument) tea.Cmd {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	call, err := view.Parse(view.KindNew, map[string]json.RawMessage{"doc": raw})
	if err != nil {
		return nil
	}
	created, err := newNewPage(repo, call)
	if err != nil {
		return func() tea.Msg { return statusMsg(err.Error()) }
	}
	return func() tea.Msg { return pushMsg{page: created} }
}

// crossGhost is the step a grabbed root takes over the ghost at the edge of
// its group: the one row between the last of one group and the first of the
// next, so the block swaps places with it and is drawn under the next
// header, which crossGroup relabels it into. It returns where the block is
// now, the same place when no ghost is adjacent.
func crossGhost(rows []treeRow, order []int, at, by int) int {
	end := blockEnd(rows, order, at)
	if by < 0 {
		if at == 0 || !rows[order[at-1]].ghost {
			return at
		}
		ghost := order[at-1]
		copy(order[at-1:end-1], order[at:end])
		order[end-1] = ghost
		return at - 1
	}
	if end >= len(order) || !rows[order[end]].ghost {
		return at
	}
	ghost := order[end]
	copy(order[at+1:end+1], order[at:end])
	order[at] = ghost
	return at + 1
}
