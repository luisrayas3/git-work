# Allocations, and the matrix view (`3289ec1`)

**Outcome:** a team can say
*how much of whom goes to which work in which iteration*,
record it in the store it already has,
and read it back as a matrix —
rows of one axis, columns of another, a number in each cell —
without a new entity, a new storage semantics,
or a view that only knows about allocations.

**Serves:** `3289ec1` (the story),
`0655187` (the type, the kind and the renderer),
`87a48c1` (iterations are issues, capacity is a field),
`2298f37` and `00a63d9` (the board and the gantt, whose third sibling this is).

**Status:** decided 2026-10-02 (Luis),
the schema and the matrix kind built the same day.
Approval of this document was the orchestrator's, in Luis's place,
because he had left for the day.

## A1 — An allocation is an issue type

An allocation is an **issue of type `allocation`**.
Its fields are relations to its axes plus a number:
in this repository `iteration`, `work`, `assignee` and `points`.

The reasons are the ones already on the record.
Iterations are issues and capacity is a field, not first class (`87a48c1`),
and an allocation is the same shape of thing one level down:
a small fact about a sprint, a piece of work and a person.
Making it an issue buys the whole machine at no cost —
ids, aliases, comments, the op-based CRDT, the write lock,
`git work issue` as its CLI, jq as its query language,
and every view that reads issues.
It adds **no storage semantics**: no new value kind, no new ref namespace,
no merge rule anybody has to reason about.
And it leaves the axes to the team:
*which* relations an allocation carries is a type somebody writes,
so a shop that allocates to epics, or to stories,
or to a customer and a quarter instead of a sprint,
writes that type and the same renderer draws it.

Two alternatives were rejected.

- **A structured map field on the parent** —
  `{"luis": {"sprint-12": 3}}` on the epic.
  It needs a new value kind, it is last-writer-wins over the whole map,
  so two people allocating two different people in one sprint lose one of them,
  and it is unqueryable: jq over the excerpts would have to walk into it,
  and no view could draw it without knowing its shape.
- **A new config-entity shape**, `allocation` beside `type`, `field` and `flow`.
  It is outside *everything is an issue*,
  it would need its own ref namespace, its own CLI tree and its own merge rules,
  and the thing it would be storing is data, not configuration.

An allocation is **not** a relationship the CRDT has to make atomic.
It names its axes by `entity.Id`, like every other cross-issue relationship
(AGENTS.md: there is no atomic multi-entity commit),
and it is eventually consistent, which is what a plan is anyway.

## A2 — An allocation has no status

The `allocation` type in this repository carries
`iteration`, `work`, `assignee` and `points`, and nothing else:
no `status`, no `priority`, no `area`.

An allocation is a statement of plan, not a piece of work.
*Where does it stand* has no answer:
it is true, or it is archived, and `archive` is already
the replicated removal every entity has.
A status would be worse than dead weight —
it would put allocations into every "open work" listing in the repository,
`git work issue 'map(select(.fields.status != "done"))'` included,
and make `overview` and `board` report planning rows as work in flight.
An allocation does carry the `rank` every type is born with
(`configurable-schema.md` D8) and ignores it:
a matrix's order is its axes' order, there is nothing to drag,
and a null rank costs nothing.

`title`, `type`, `archived` and `rank` are built in on every type
(`d56e6f1`, `e524644`),
so an allocation has a title if someone writes one.
Nothing reads it: the matrix draws the axes and the sum.

The two relations take **two inverse names**, `allocations` and `staffing`,
because an inverse name is unique within the type that holds it
(`schema/document.go`), and both would otherwise read `allocations`.
So a sprint's derived side is `allocations`
and a story's is `staffing`, which is what it is:
who is on this work.

## A3 — The presets do not ship `allocation`

`git work schema init jira` and `init linear` create no `allocation` type.
Only this repository's own `schema.yaml` has one.

The presets are the **native models** of Jira and Linear (`configurable-schema.md` D7):
that is what makes them worth shipping,
and what makes a bound clone's schema reconcilable against a real instance.
Neither tracker has an allocation issue type.
Shipping one would put our invention inside the thing whose whole claim
is that it is not our invention,
and `git work jira sync` would then carry a type
that no Jira project can hold (Jira mapping is explicitly not required here).

Nothing is lost by leaving it out, because **the matrix kind needs no particular type**.
It sums a number field over any issue set, split by any two fields,
so `points per sprint per story` over `allocation` issues
and `estimate per sprint per story` over ordinary tasks
are the same call with different arguments.
This repository's `schema.yaml` is the worked example a team copies.

## A4 — A `matrix` view kind

`git work view matrix KWARGS` and `work.view.matrix(**kwargs)`,
the same call twice over through `host.View`,
parsed against the one table in package `view` like every other kind.

| Argument | Tier | Kind | Meaning |
| --- | --- | --- | --- |
| `rows` | required | field key | the field whose values are the rows |
| `columns` | required | field key | the field whose values are the columns |
| `value` | feature | field key | the number field summed in a cell; absent, a cell counts issues |
| `row_values` | defaulted | strings | the row values, in order; the axis's own order by default |
| `column_values` | defaulted | strings | the column values, in order |
| `group_by` | feature | field key | the field whose value starts a new block of rows |
| `query` | defaulted | query | the jq program the issues come from |

`value` is a **feature** and not a defaulted argument
because its absence is a different view, not a different default:
with no number named, the matrix counts,
which is the right answer for *how many issues per status per assignee*
and needs no field to exist at all.

There is no `rank` argument: the axes own the order, and there is nothing to drag.
There is no `totals` switch: the totals are always drawn (A7).

## A5 — The order of an axis

Both axes follow one rule, which is the board's rule
(`terminal-renderer.md`, Board) applied twice:

1. the values `row_values` / `column_values` name, in that order, when given;
2. otherwise, for an `enum` or `ordinal-enum` axis, the field's **schema order**,
   read off the types the issues on the matrix have —
   every type owns its own field (`e7e58f2`),
   so an iteration's statuses are not the rows of a matrix of tasks;
3. then, trailing, every value the data has that step 1 or 2 did not place,
   in the order of the rule below,
   because a matrix that silently drops issues
   is worse than one with a ragged edge;
4. then `(none)`, last, always, as every other kind puts it last.

The order in step 3, and the whole order of an axis that is not an enum,
is **the label the axis draws, then the id**.
A relation axis is drawn as the issue it names —
short id and title, as `linkText` draws a relation everywhere else —
so it is ordered by that title, with the issue's id as the tie-break;
an `identity` axis is drawn by name and ordered by it;
a `date` axis sorts right because RFC 3339 is lexicographic;
and two labels that are both numbers compare as numbers,
so a `number` axis does not put 10 before 2.

Ordering a relation axis by the query's order was the alternative,
and it is what the list and the board do.
It is wrong here: the query returns allocations, not sprints,
so an axis value's place would be wherever its first allocation happened to land,
and `sort_by` in jq — the escape hatch everywhere else —
can only reach the allocation's own fields, which hold the sprint's *hash*.
Sorting by the drawn label is the order a person reads off the screen,
and `row_values` / `column_values` with explicit ids
is the escape hatch when the titles do not sort the way the team thinks.

**A multi-valued axis double-counts, by construction.**
An issue whose `area` is `["cli","tui"]`
appears in the `cli` row and in the `tui` row,
and the totals row counts it twice.
That is the honest reading of *area by sprint*;
the alternative — dividing the number between the values —
invents data the store does not have.

## A6 — A cell is a sum, or a count

Each cell holds the issues whose `rows` value is that row
and whose `columns` value is that column,
and draws the sum of their `value` field, or their number when `value` is absent.
Numbers are drawn plainly — `3`, not `3.0`, and `2.5` as `2.5` —
and right-aligned, because a column of numbers is read down its last digit.
A `value` that is not a number is not summed;
it contributes nothing rather than an error,
because one bad cell must not take the matrix down.

**A cell with no issues is blank, not `0`.**
A cell whose issues sum to zero draws `0`.
The difference is the one a planner is looking for:
nothing allocated, against allocated and spent.

## A7 — Totals, dim

A matrix has a totals row along the bottom, a totals column down the right,
and the grand total in the corner, all of them dim,
because they are derived and the data is what the eye should land on.
With `group_by`, each block of rows ends in its own subtotal row
and the last row of the matrix is the grand total.

Totals are of **what is drawn**:
a filtered matrix (A9) totals the rows and columns it is showing.
A number that does not add up to the numbers above it
would be the one way a matrix can lie.

## A8 — `group_by` is blocks of rows, not a matrix each

A bound `group_by` splits the rows into blocks,
each under a group header line, `(none)` last,
sharing **one column header and one column layout**.

The alternative — a whole matrix per group, header and all —
was rejected because the columns are the same axis in every group:
the question `group_by` answers is *how do these groups compare*,
and two matrices that have drifted to different column widths cannot be compared
by looking down the screen.
One header, one set of column positions, a group header between blocks
is also exactly how the list and the board draw a group,
so there is one grouping idea in the renderer and not two.

## A9 — Keys

The cursor is **a cell**: a row and a column.
It starts in the first data cell.
The row under it is washed and the cell itself is reversed,
as a list's row and cell are.

- `←` `→` move a column, `↑` `↓` a row, across group blocks as well;
  `Home` `End` are the ends of the row, the page keys move rows by a page.
- The **totals row and column are cells like any other.**
  The cursor sits on them, and `Enter` there opens the whole row's,
  the whole column's, or — in the corner — the whole matrix's issues.
  This falls out of the drill-down below rather than being built:
  a total is the cell with one clause of its query dropped.
- `Enter` **opens the issues summed into the cell**,
  pushed as an ordinary `listPage`.
  A sum you cannot open is a number you have to trust.
  Its query is the matrix's own query piped into a `select`
  on the cell's axis values:
  `<query> | map(select(.fields.work == "<id>")) | map(select(.fields.iteration == "<id>"))`,
  with `== null` for `(none)`
  and `((.fields.X // []) | index("<id>")) != null` for a multi-valued axis.
  The **predicate**, not the list of ids that happen to be in the cell today:
  the pushed list is a live view like every other,
  and it has to re-run to the same cell after somebody writes.
  Its columns are the title, the two axis fields and the `value` field,
  so the numbers that made the sum are on the screen under it.
  It is a legal `git work view list` call,
  which its own call line says and `C-c` there copies.
- `Space` **rings the bell**: a sum is not a value, and there is nothing to write.
  The status line says so.
  An allocation is edited where every field is edited — on `show`,
  which `Enter` is one more step away from.
- `C-c` / `y` / `M-w` copy the cell's number as drawn; an empty cell rings.
  `M-c` / `Y`, which copies an id on every other kind, copies the cell's
  drill-down command, because a cell has no id
  and what identifies it is exactly that query.
- `/` or `C-s` **narrows the axes, not the issues**:
  a row or a column whose label does not match is dropped.
  Filtering the issues the way a list and a board do
  would quietly change every sum on the screen
  while the totals still read as totals (A7).
- `Esc`, `?`, `C-q` and the call line are the renderer's, unchanged.

## A10 — Layout

Columns keep a **minimum width** and, when they do not all fit,
the matrix scrolls sideways by whole columns to keep the cursor's column on screen,
with `‹` and `›` in the header saying there is more —
the board's rule, for the board's reason (`terminal-renderer.md`, Board).

The **row-header column is sticky**: it does not scroll with them.
A matrix whose row labels have scrolled off the left
is a grid of numbers nobody can read.
The totals column is pinned to the right of the drawn columns
for the same reason, so it is always the last thing on the line.

The header's corner cell reads `rows \ columns` — `work \ iteration` —
so the two axes are named on the screen,
and the call line names them too, like every argument
that the screen does not already spell out.

## A11 — Live, like every other kind

The matrix re-runs its query on a ref-watcher change,
and keeps the cursor on **the same row and column values**, not the same indexes,
so a column appearing because somebody opened a new sprint
does not move what you were about to open.
That is the list's rule (`putCursorOn`) with two axes instead of one.

## The two worked examples

This repository's allocations, once `schema.yaml` is imported:

```sh
git work view matrix '{"rows":"work","columns":"iteration","value":"points",
  "query":"map(select(.fields.type == \"allocation\"))"}'
```

and, with no allocation type anywhere in sight,
the estimate of the tasks of each story in each sprint:

```sh
git work view matrix '{"rows":"parent","columns":"iteration","value":"estimate",
  "query":"map(select(.fields.type == \"task\"))"}'
```

## Deferred

- **The GUI half.** `--gui` errors until the gui process exists (`8b06191`),
  as it does for every other kind; the matrix is one more kind it will draw.
- **Editing in the matrix.** `Space` rings today.
  A cell holding exactly one issue could edit that issue's number in place,
  and a cell holding none could create the allocation the axes describe.
  Both are writes a matrix would be inventing,
  and the deferred *actions injected into views* (`terminal-renderer.md`)
  is where they belong if they are ever wanted.
- **Jira.** Not required (`3289ec1`), and A3 says why the presets stay clean.
