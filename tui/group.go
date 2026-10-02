package tui

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/schema"
)

// A grabbed row carried past the edge of its group enters the next one, and
// the drop writes the `group_by` field (2026-10-02,
// doc/design/terminal-renderer.md, Rank).
//
// It is the board's rule, which the board has had since `0740bf3`: a card
// carried into another column is dropped there, and the drop sets the field
// the columns are of. A group is a column drawn the other way, so a list's
// sections, a gantt's row groups and a board's swimlanes all move the same
// way, and one drop is still one commit — the rank and the field in one
// `IssueSet`.
//
// What a group is on the screen is the cell its rows draw, so what the drop
// writes is the value a row already in that group holds: the enum's value
// id, the relation's or the identity's id, the bool. `(none)` is the group
// of the rows with no value at all, and writes null.

// crossing is the group a grabbed row has been carried into: the header it
// is drawn under now, and the value its `group_by` field takes at the drop.
type crossing struct {
	group string
	value issue.Value
}

// crossRefusal is why a grabbed row may not change group, "" when it may.
//
// `type` is not a field a move gets to change: it decides which fields an
// issue has, so dragging it across a lane would be a schema change by
// accident. A set-valued field is not one value, so a drag cannot say which
// item it meant — the same reason space on a `multi-relation` rings — and
// `issue add`/`remove` is the answer there as it is everywhere else.
func crossRefusal(repo *cache.RepoCache, typeKey, groupBy string) string {
	if groupBy == schema.TypeKey {
		return "type cannot be changed"
	}
	kind, known := fieldKind(repo, typeKey, groupBy)
	if !known {
		return groupBy + " is not a field of " + typeKey
	}
	if kind.IsMulti() {
		return groupBy + ": use git work issue add/remove"
	}
	return ""
}

// groupValue is what a drop into a group writes: the value a row already in
// it holds, as it is stored, and null for `(none)`.
func groupValue(raw any, group string) issue.Value {
	if group == noGroup {
		return issue.MustValue(nil)
	}
	return issue.MustValue(raw)
}
