# Empty swimlanes: `empty_groups` on the board

**Outcome:** a board grouped by a field can draw the swimlanes no card falls in yet,
so a lane is somewhere to put work, not only a heading over work that exists.

**Serves:** story `2298f37` (the board).

**Status:** proposed 2026-10-09.

## The workflow that needs it

A board of one initiative's tasks, a column per status and a swimlane per epic.
An epic with no tasks yet has no lane today,
so it cannot be seen on the board and nothing can be dropped or created into it.
The columns already work this way round:
every status the schema lists is a column, empty or not,
and an empty column is reached on its `+ (new)` card.
The lanes should be able to do the same.

What counts as "every epic" has to follow the board's query.
Filtered to initiative X and grouped by epic,
the board draws X's epics, never the epics of another initiative,
even though the query selects tasks and names no epic at all.

## E1 — `empty_groups`, a bool on the board

An optional argument on `git work view board`:
`'{"columns":"status","group_by":"parent","empty_groups":true}'`.
Off, the board is as today: a lane per value the cards have.
On, the board adds a lane for each value the field could hold here that no card has,
by the rules below,
each with a `+ (new)` card in every column,
so `↓` reaches it and `Enter` creates an issue already filed under it.

A bool rather than a list of values,
because the case is "show me the rest", not "show me these",
and the columns' `values` remains the model for an explicit list if one is ever wanted.

## E2 — which values, by the field's kind

- **`enum`, `ordinal-enum`, `multi-enum`:**
  every value the schema lists for the field,
  over the types on the board as the columns read them.
- **`bool`:** `false`, then `true`.
- **`relation`, `multi-relation`:** the siblings of the lanes the cards already have (E3).
- **Anything else** (`text`, `number`, `date`, `identity`):
  there is no set of values to draw, so `empty_groups` changes nothing.
  `--help` says so.
  `identity` could mean every user, and is left out until someone needs it.

`(none)` stays as it is:
a lane, last, only when a card has no value,
the way the `(none)` column is drawn only when a card has no value.

## E3 — a relation's empty lanes are the siblings of the lanes present

A relation's possible values are issues,
and "every issue the field may point at" is the whole store,
which is wrong the moment the query narrows the board.
The query cannot be read for its scope, being a jq program over issues of another type,
so the scope is read from the lanes it produced.

The lanes the cards fall in are issues: call them the present lanes.
Each holds a value for the same field key, `group_by`, read from the store:
an epic's own `parent` is its initiative.
Those values are the anchors.
The empty lanes are the other issues whose `group_by` value is one of the anchors,
of a type the field's `target_types` allow for a type on the board,
unarchived unless the board's `include_archive` says otherwise.

So with tasks under initiative X, grouped by `parent`:
the present lanes are the epics that have tasks,
their `parent` values are X,
and the empty lanes are X's other epics.
An epic under initiative Y is never an anchor's child, so it never appears.

The rule names no field — it reuses the key the board was given —
so it keeps the no-field-roles rule.
It does not reach past one level:
an initiative with no epic that has a card on the board adds nothing,
because nothing on the board anchors it.

**A present lane with no value for the field** (an epic with no initiative,
a story where stories have no parent)
anchors on "no value":
its siblings are the target issues that have none either.
Grouped by story, with stories at the top of the hierarchy,
that is every unarchived story, done ones included.
This is the rule applied consistently, and the query cannot narrow it,
since the stories are not in the query's output;
see Q1.

## E4 — order is the values' own, never the cards'

A lane's place does not depend on whether, or where, the query put a card in it.
Lanes are ordered by the natural order of the value they stand for,
and the order the cards first appear in is only a tie-breaker after it:

- **`enum`, `ordinal-enum`, `multi-enum`:** schema order,
  over the types on the board as the columns read them;
  a value the schema does not list follows the listed ones.
- **`bool`:** `false`, then `true`.
- **`relation`, `multi-relation`:** the lane issues in `(rank, …)`,
  the order every view gives issues;
  the unranked follow, as everywhere, keeping the order they first appear in.
  A `multi-relation` lane sorts by the first issue it names.
- **`number`, `date`:** ascending.
- **`text`, `identity`:** by the drawn label (an identity by name).
- **`(none)`:** last.

Where two lanes tie — two unranked issues, two values the schema does not list —
first-seen order breaks the tie, so the order is still stable across a refresh.

This holds with `empty_groups` off as well:
it replaces today's first-seen order for every board,
so that switching `empty_groups` on adds lanes and moves none.
The order code is shared by every view since `395ecd42`,
so the list's, the gantt's and the matrix's `group_by` take the same order;
see Q2.

## E5 — what stays the same

- A drop into a lane writes the lane's value, and a ghost in it is prefilled with it, as today.
  An empty lane has no card to read the stored value from,
  so a lane carries its stored value (the enum id, the issue's full id) itself;
  the label stays what it draws.
- `type` as `group_by` is refused for a drop as today, and gets no empty lanes:
  a type is not a value a drop writes.
- The status line's counts are of cards; an empty lane adds none.
- A refresh recomputes the lanes, so an empty lane fills in place
  when an issue lands in it, and a lane that empties stays.

## Q1 — open

The top-level case in E3 (grouped by an issue with no parent of its own)
draws every such issue, done or not.
That is right for a board grouped by epic in a store of one initiative,
and noisy for this repository grouped by story, where most stories are done.
Proposed answer: accept it for now, since `empty_groups` is opt-in,
and narrow it later if it bites
(a `group_query`, a jq program over the lane issues, would be the general fix).

## Q2 — open

E4 changes the group order of the list, the gantt and the matrix too,
from first seen to the values' own.
Proposed answer: yes, one rule for every view,
because the order code is shared and a group's place should not depend on the view.
The matrix's rows and columns already follow it (`row_values`, else schema order).

## Out of scope

- The same argument on the list, the gantt and the matrix.
  The group code is shared since `395ecd42`, so it is cheap to add there later;
  the matrix already has `row_values`.
- An explicit list of lanes (a `group_values`).
