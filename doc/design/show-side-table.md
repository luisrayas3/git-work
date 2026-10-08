# Show's side table: the issues a relation reaches, beside the fields

**Outcome:** `git work view show` draws the issues a call names through a relation —
a story's tasks, the issues this one blocks —
as a small table to the right of the fields,
a row per issue and a column per field the call asks for,
so the page reads like Jira's issue page with its child issues beside it,
and every cell of that table opens, edits and copies the way a list's cell does.

**Serves:** `bf9bf96` (the task, held until this is approved);
`111e8e9` (views create issues, whose `b9a9b62` puts a ghost under each children section of show,
which is this table's foot);
`2298f37` and `00a63d9`, whose stories are the pages it is wanted on.

**Status:** proposed 2026-10-08, not reviewed.
It revises `terminal-renderer.md` (Show, Children) of 2026-09-29,
where a section is rows of the fields table,
and assumes show's three changes of the same day:
`rank` is no longer a row of the fields table,
the box has no hint line under it,
and the page opens with the cursor on the fields table.

## The page it gives

```
git work view show '{"id":"2298f37","children":[{"relation":"children","fields":["status","assignee"]}]}'
story
Board: a live kanban in the terminal and the GUI
────────────────────────────────────────────────
[ ] archived

›status          in-progress            │ children
 priority        high                   │ id       title                      status       assignee
 area            tui, gui               │ a1b2c3d  Columns from the schema    done         Luis Rayas
 assignee        Luis Rayas             │ 4e5f6a7  Drag across swimlanes      in-progress  Luis Rayas
 parent          9c0d1e2 Terminal views │ 8b9c0d1  The board in the GUI       to-do
 due             2026-10-30             │ + (new)
                                        │
                                        │ blocks
                                        │ id       title                      status
                                        │ 0f1e2d3  Gantt: drag a bar          to-do

╭──────────────────────────────────────────────────────────────────────────────╮
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯
 ╭─────────────╮╭──────────╮╭─────╮
─╯ description ╰┴ comments ┴┴ log ┴──────────────────────────────────────────────
```

and, in a window too narrow for both, the same table under the fields, full width:

```
›status          in-progress
 priority        high
 …
 due             2026-10-30

 children
 id       title                         status       assignee
 a1b2c3d  Columns from the schema       done         Luis Rayas
 …
```

## S1 — The argument is `children`, and an entry is a layer

No new argument and no placement key.
Show's `children` keeps its name and its list
(a story has tasks *and* is blocked by issues: one entry, one table),
and **every entry is a layer**, the object a list's `expand` takes (Nesting),
read from the shown issue as a layer is read from its parent row:

```json
{"id": "2298f37",
 "children": [{"relation": "children", "query": "map(select(.fields.status != \"done\"))",
               "fields": ["status", "assignee"]},
              {"relation": "blocks", "fields": ["status"]}]}
```

The keys an entry takes are a layer's that mean something on one level:
`relation` (required), `query`, `include_archive`, `fields` and `rank`;
and `type`, which show's entries already had and keep,
narrowing the candidates before the query and naming the table (S3).
`details`, `group_by` and `expand` are refused by name:
a side column is too narrow for a second line, a section or a tree,
and refusing them leaves each to be added later on its own merits.

The entry becomes a layer because that is what it has been all along
without the vocabulary:
the issues one relation reaches from one issue, narrowed and drawn as a table.
The list already has a word for that object, its keys and their checks,
and a second spelling of it on show — `type`/`relation`/`fields` there,
`relation`/`query`/`fields` here — is a grammar to learn twice.
It also gives the table a `query`, which the rows form never had,
so *the open tasks* is said the way a list says it.

A placement key (`"side": true`) was the other shape, and it is rejected:
it keeps two drawings of one thing,
and the rows form has nothing the table lacks (S2).
A new argument (`side`, `related`) would sit beside `children`
meaning the same issues with a different look.

## S2 — The table replaces the rows

The 2026-09-29 sections, rows of the fields table under the relation's name,
go. That decision had three reasons, and the table keeps all three:
each child is a place the cursor stands, `Enter` follows it, copy copies its id.
What the rows could not do is columns:
the fields after a title were a `·`-joined string,
which no cell of it can be edited from, aligned with or copied alone,
and a key/value table has no second column to put them in.
A table of issues is what a list draws, so the side table is drawn and worked as one (S5).

Jira's *Child issues* is a table in the issue's body, with a column per field:
this is closer to it than the rows were, not further.

## S3 — A relation is named from the shown issue's side, as `expand` names it

`relation` is resolved against the **shown issue's type** first, the way a layer's relation is resolved against its row:

1. **a stored relation of the shown type** — `blocks`, a `multi-relation`, is the issues this one blocks;
   a `relation` such as `parent` works too and is a table of one row;
2. else **an inverse name** some type's relation gives the other side —
   `children`, read through every `parent` that names this issue, as `childrenIndex` reads it;
   `blocked_by`, through every `blocks`;
3. else, as before this design, **a stored relation on another type** read from the child's side —
   the issues whose `parent` (or other key) names this one, headed `← parent` —
   kept so that a relation with no `inverse` in the schema can still be shown.

Either side is the answer to "a stored `multi-relation`, an inverse, or either":
both, in one word, as `expand` has taken them since it was built.
The one cost is that rule 1 now wins where the child-side reading used to:
`{"relation":"parent"}` on a task meant its subtasks, and now means its own parent.
Where the entry names a `type` that the stored field cannot reach,
the call is refused with the way out —
*story/parent points at epic, not task; the tasks whose parent is this story are `children`* —
so the documented form (`{"type":"task","relation":"parent"}` on a story)
fails loudly rather than drawing the epic.
Without a `type` it changes silently; `flows/` has no such call, and AGENTS.md's example moves to `children`.

A stored relation of the shown type that a side table draws
is **left out of the fields table**, unless `fields` names it:
one issue's `blocks` drawn twice, once as links and once as a table, is noise.

## S4 — Layout

**Beside, when it fits; under, when it does not.**
The fields block is two columns, the fields table on the left and the side column on the right,
between them a dim `│` with a cell of space either side.
The left column is the marker, the 16-cell key, and the values,
as wide as its widest value up to 36 cells (a longer value is cut with `…`; copy takes it whole).
The side column is the rest of the window,
and it is drawn beside only when that rest holds every side table at its natural width
with the title column at least 20 cells.
Otherwise the side tables are drawn **under** the fields table, full width,
before the comment box, in the same stop order (S5), so nothing about the keys changes with the window;
a terminal is resized as often as it is opened, and the page cannot change its keys when it is.

**Each entry is a table with a heading and a header.**
The heading is the relation's name from the shown issue's side —
`children`, `blocks`, `blocked_by`, `← parent` by rule 3 —
with ` · task` after it when the entry names a type, as the sections were named;
under it the column header, dim, as a child table's is on a nested list:
`id`, then `title`, then the entry's `fields` in order.
The id and title are always drawn and never named, being what a row is read by;
`fields` defaults to nothing more, because there are no field roles (`d56e6f1`)
and nothing can tell which field is the status.
Columns are measured over every row of the table, so a refresh moves nothing
that did not change; the title takes the slack.
A person is drawn by name and a relation as the issue it names, as on a list.
Tables are stacked in the call's order with a blank line between.
An entry that reaches nothing is its heading and `(none)`,
because a table that is not drawn reads as one never asked for.

**The side column's first line is the fields table's first line.**
Fields and children are read side by side from the top,
and no field row is pushed to line up with a table.

**Height.** The block is as tall as the taller of its two columns,
up to the taller of the fields table and 12 lines.
Past that the side column scrolls within its window, following the cursor:
the heading and header of the table the cursor is in are kept on the column's first lines,
as a group header is kept on a list,
and a dim `↓ 7 more` closes the window (`↑ 3 more` opens it once scrolled).
A story with forty tasks does not push the comment box and the tabs a screen away,
and the page's own scroll still moves the whole block.
Under the fields, in the narrow drawing, the same cap applies.

## S5 — Cursor and keys: the side table is a stop, and a list

**A stop of its own.** The stops become header, fields, **side**, box, tabs.
`Tab` from the fields lands on the first side row's id, `S-Tab` back,
as `Tab` lands on a child's id on a nested list (2026-10-07).
`↓` past the fields table's last row goes to the box as before;
`↓` past the side table's last row (its ghost, S6) goes to the box too,
and `↑` from its first row to the header.
In the narrow drawing the side stop is under the fields, so `↓` from the last field enters it.

**`←`/`→` walk cells where a stop has columns, and switch tabs where it has one.**
The header has three cells and the side table has columns, so they walk them;
the fields table *with* a side table beside it is the side table's left neighbour:
`→` from a field row lands on the side row drawn on the same line, or the last when there is none,
on its id; `←` from a side row's id goes back to the field row on that line, or the last.
The fields table without a side table, the box and the tabs keep `←`/`→` as the tab keys,
and `C-PgDn`/`C-PgUp` switch tabs from everywhere, as before.
This is one rule — cells where there are cells — rather than a special case for show.

**In the side table every key is a list's**, because it is a list's table:

- `Enter` opens the row's issue as show, on the id or any plain cell;
  on a relation cell it opens the issue that cell names.
  The page opened is a bare show, as a link opens one: the options are the call's.
  `Esc` there comes back here, the cursor on the same row.
- `Space` edits the cell under the cursor with the list's editors —
  status, assignee, any field of the row's type —
  and a column that is not a field of the row's type rings.
  The rows were never editable as the sections, because they were not fields;
  the cells are, because they are the child's own fields.
  Editing the relation that put the row here (a task's `parent`, through its show) moves it out on the reload.
- `Space` on the id **grabs the row**, `↑`/`↓` carry it, and the drop writes the entry's `rank`
  (the built-in `rank` unless the entry names one), one commit, the unranked above it filled first —
  the Rank section's rule for one scope.
  A row cannot leave its table: `←`/`→` while grabbed ring, and so does a carry past the table's edge,
  because which relation a row is reached by is not an order.
  The rank is the child's, so dragging a task here reorders it on every list too,
  which is what a nested layer's drag does already.
- Copy: `C-c`/`y`/`M-w` the cell, `M-c`/`Y` the id.
- `/` or `C-s` narrows the side tables' rows, as a list's filter narrows its rows,
  while the cursor is in them; elsewhere on show it does nothing, as today.

Rows are ordered `(rank, id)`, the unranked keeping the store's order at the end, as a layer's are.
Members of a stored `multi-relation` are in that order too, not in the order they were added,
because a set has no order of its own.

Adding to or removing from a stored `multi-relation` is not a key here:
it is the field's own picker, which rings for now (`cdffc6c`).

## S6 — The ghost is the table's foot, where the relation can be written

A dim `+ (new)` row at the foot of each table — the item `b9a9b62` lists as
"one per children section on show", built with whichever of the two lands first.
`Enter` opens `new` prefilled with the drop's write set (`create.md`, C6):
the stored side of the relation set to this issue's id —
`parent` for `children`, a `blocks` holding this id for `blocked_by` —
and `type` when the entry names one, else when the relation reaches one type, else when every row shares one.
Create pops back to this show, the cursor on the new row.

There is **no ghost under a table read by rule 1**, a stored relation of the shown issue:
the new issue would be added to *this* issue's `blocks`,
a second commit on a second entity,
and there is no atomic multi-entity commit to make the pair one write.
It comes with a create-then-link, or with the multi-value picker.

## S7 — Live, and never on a draft

The tables are recomputed on every load, which the ref watcher's refresh and the page's own writes both do,
as the sections were: a task created elsewhere with this story as its parent appears.
The cursor is kept by the row's id across a reload,
and lands on the row that took its place when its issue left the table.

`new` takes no `children` and draws no side table.
A draft has no id, so no inverse can name it and its children are always none;
its own stored relations are rows of its fields table, edited there.
When a standalone Create replaces the page with show, that show is bare, as any show reached without a call is.

## S8 — One call, one check, every surface

`work.view.show(id=…, children=[…])` is the same object the command takes:
no new name, no script-only form, the 1:1 rule held by construction.
`git work view show --help` reads the argument's description from the kinds table, which says it is a list of layers.

The check stays in `host.View`, before a renderer is chosen,
so the command, a flow and the gui refuse a call with the same words before anything draws.
It needs one thing it did not: the **shown issue's type**,
because rule 1 is read against it, so `host.View` resolves `id` first (it already must).
Refused, each naming what exists:
a relation the shown type does not have, no type gives as an inverse and no type stores;
a `type` the relation cannot reach (with the inverse that would, S3);
a `fields` or `rank` key that is no field of the types the table can hold;
`details`, `group_by` and `expand` on an entry;
and a `query` that does not compile.
A query that fails on the data is reported in the status line, as a layer's is, and the table draws `(none)`.

## Out of scope

- **Show's defaults.** `Enter` from a list opens a bare show, with no side table.
  A type's usual tables (a story's tasks) belong to a flow that calls show,
  not to the schema: there are no field roles to say which relation is the children.
- **Nesting in the side table** (`expand` on an entry) and **sections** (`group_by`).
- **Rows that are not issues** (`query-rows.md`, R1–R3): a side table's query
  narrows the candidates; a `key` or a row `children` it returns is ignored,
  because one table on one issue has no pair to stand for.
- **Membership edits** of a stored `multi-relation` from the table: the picker (`cdffc6c`).
- **The GUI's drawing**, which takes the same call when the gui process exists.

## Open questions

1. **Rule 1 over rule 3.** Reading the shown issue's side first makes show agree with `expand`,
   and silently changes `{"relation":"parent"}` without a `type`.
   Is that acceptable, or should a name that is both a stored field of the shown type and
   a child-side relation be refused as ambiguous, naming both readings?
2. **The cap.** The taller of the fields table and 12 lines, then an inner scroll.
   Is a fixed cap right, or should it be half the window,
   or should the column never scroll and simply grow, leaving the page's own scroll to cope?
3. **`←`/`→` on the fields table** stop switching tabs when a side table is drawn.
   One rule, cells where there are cells, but the same stop answers `→` two ways depending on the call.
   Is that acceptable, or should `→` from the fields always enter the side table (and ring when there is none)?
4. **Beside by default.** Should an entry be able to ask to be drawn under the fields
   even when it fits beside (a long description-like table), or is the window the only judge?
