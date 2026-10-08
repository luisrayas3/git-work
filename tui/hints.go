package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/git-bug/git-bug/schema"
)

// The status line names the keys that act where the cursor is
// (doc/design/terminal-renderer.md, Navigation and editing, 2026-10-02).
//
// Every kind builds its line out of the same pairs, `enter` and `space`
// first, so that the two keys that do everything never have to be looked up
// in the help. The keys are spelled the way the standard family spells them,
// lower case, because `?` is where the vim and the emacs spellings live and
// three spellings in one line read as noise to all three.
//
// Nothing here promises a key the page does not act on in that state: where
// a key rings the bell — space on a sum, on a column that is not a field of
// the row's type, on a set-valued field — its pair is left out rather than
// listed.

// hint is one key and what it does under the cursor.
type hint struct {
	key    string
	action string
}

// helpHint is the one pair that is on every line, in every state: the help
// is where the rest is.
const helpHint = "? keys"

// hintText is the pairs, joined, with no help key: what a hint reads as
// where it is drawn somewhere other than the status line.
func hintText(pairs ...hint) string {
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, pair.key+": "+pair.action)
	}
	return strings.Join(parts, " · ")
}

// hints is the status line's left half: the pairs, then the help.
func hints(pairs ...hint) string {
	text := hintText(pairs...)
	if text == "" {
		return helpHint
	}
	return text + " · " + helpHint
}

// bottomLine is the page's last line: two places, one line
// (doc/design/terminal-renderer.md, Navigation and editing, 2026-10-02).
//
// The hints are on the left, always, because what the keys do here is the one
// thing that is true of every press. The last action's message — with the
// issue count beside it where the kind has one — is right-aligned, and it
// stays there until the next action replaces it. The message had the whole
// line until then, so an edit took the hints over until the next key cleared
// it, which is the opposite of what a hint is for.
//
// Where the window cannot hold both, the hints win and the right side is cut
// from its left: a count, and the end of a refusal, are what is read there,
// and the opening words of a long one are the part worth losing.
func bottomLine(hints, right string, width int) string {
	if right == "" {
		return fit(hints, width)
	}
	if width <= 0 {
		return hints + "  " + right
	}

	left := fit(hints, width)
	// one space at least between the two, so they never read as one
	room := width - ansi.StringWidth(left) - 1
	if room <= 0 {
		return left
	}
	right = fitRight(right, room)
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	return left + strings.Repeat(" ", gap) + right
}

// lastAction is the bottom line's right side: what the page just did, and the
// count beside it where the kind has one, either alone where the other is
// empty.
func lastAction(status, count string) string {
	switch {
	case status == "":
		return count
	case count == "":
		return status
	}
	return status + " · " + count
}

// filterHints is the line while `/` is open, on every kind: the narrowing is
// kept or dropped, and nothing else is read.
func filterHints() string {
	return hints(hint{"enter", "keep"}, hint{"esc", "clear"})
}

// grabHints is the line while something is grabbed: how it moves, then the
// drop and the way back. The moves come first here, because the grab is the
// state and the directions are what it is for.
func grabHints(moves ...hint) string {
	return hints(append(moves, hint{"space", "drop"}, hint{"esc", "put back"})...)
}

// boxHints are the comment box's keys on show, in the status line, the one
// place they are drawn: the footer under the text that repeated them is gone
// (2026-10-08).
func boxHints(typing bool) []hint {
	if typing {
		return []hint{{"enter", "send"}, {"alt+enter", "newline"}, {"esc", "leave"}}
	}
	return []hint{{"space", "type"}, {"tab", "skip"}}
}

// descHints are the description editor's keys on show, in the status line
// and in the footer under it, which is one list so the two never disagree.
//
// They are the box's keys, except that what enter does is write and not
// send: the description is one text being rewritten, not one more comment.
func descHints() []hint {
	return []hint{{"enter", "write"}, {"alt+enter", "newline"}, {"esc", "leave"}}
}

// foldHints are the tree's keys on the row under the cursor, nothing on a
// row with no children.
func foldHints(node *treeRow) []hint {
	if node == nil || node.children == 0 {
		return nil
	}
	fold := hint{"space", "fold"}
	if node.folded {
		fold = hint{"space", "unfold"}
	}
	// tab unfolds on its way in, so it is true of a folded row too
	return []hint{fold, {"tab", "into children"}}
}

// openHint is what enter does on a cell: a link goes to the issue it names,
// and anything else opens the row's own.
func openHint(linked bool) hint {
	if linked {
		return hint{"enter", "go to"}
	}
	return hint{"enter", "open"}
}

// editHint is what space does to a cell of this kind, and whether it does
// anything at all.
//
// It mirrors `editable`, which is the authority: a bool flips, the kinds
// with a widget open it, and everything else — a set-valued field, a kind
// this renderer does not edit, a column that is not a field of the row's
// type — rings the bell and is left out of the line.
func editHint(kind schema.Kind, known bool) (hint, bool) {
	if !known {
		return hint{}, false
	}
	switch kind {
	case schema.KindBool:
		return hint{"space", "flip"}, true
	case schema.KindRelation:
		return hint{"space", "change"}, true
	case schema.KindEnum, schema.KindOrdinalEnum, schema.KindIdentity,
		schema.KindText, schema.KindNumber, schema.KindDate:
		return hint{"space", "edit"}, true
	}
	return hint{}, false
}

// hints is the status line while an editor is open: its own keys, since the
// page under it reads nothing until it closes.
func (e *editor) hints() string {
	switch {
	case e.picker == nil:
		return hints(hint{"enter", "write"}, hint{"esc", "cancel"})
	case e.picker.narrowing != nil:
		return filterHints()
	}
	return hints(hint{"enter", "write"}, hint{"esc", "cancel"}, hint{"/", "narrow"})
}
