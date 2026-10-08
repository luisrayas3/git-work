# Show's side table: the issues a named relation reaches, beside the fields

**Outcome:** `git work view show` draws the issues a call names through a stored relation —
the tasks whose `parent` is this story, the issues this one `blocks` —
as a small table to the right of the fields,
a row per issue and a column per field the call asks for,
so the page reads like Jira's issue page with its child issues beside it,
and every cell of that table opens, edits and copies the way a list's cell does.

**Serves:** `bf9bf96` (the task, held until this is approved);
`111e8e9` (views create issues, whose `b9a9b62` puts a ghost under each such table on show,
which is this table's foot, S6);
`2298f37` and `00a63d9`, whose stories are the pages it is wanted on.

**Status:** proposed 2026-10-08, not reviewed.
The argument's shape — `relation_table`, its two forms, no inverse names — was decided 2026-10-08 (Luis);
the rest is still a proposal.
It replaces show's `children` (`terminal-renderer.md`, Show, Children, 2026-09-29),
where a section is rows of the fields table named by a relation or its inverse,
and it assumes show as it stands on trunk since 2026-10-08:
`rank` is not drawn, the box has no hint line under it,
and the page opens with the cursor on the first row of the fields table.

## The page it gives

```
git work view show '{"id":"2298f37","relation_table":[{"type":"task","relation":"parent","fields":["status","assignee"]},{"field":"blocks","fields":["status"]}]}'
story
Board: a live kanban in the terminal and the GUI
────────────────────────────────────────────────
[ ] archived

›status          in-progress            │ task · parent
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

and, in a window too narrow for both, the same tables under the fields, full width:

```
›status          in-progress
 priority        high
 …
 due             2026-10-30

 task · parent
 id       title                         status       assignee
 a1b2c3d  Columns from the schema       done         Luis Rayas
 …
```

## S1 — The argument is `relation_table`, and `children` goes

Show takes `relation_table`, a **list**, one side table per entry, drawn in the list's order:
a story has tasks *and* blocks issues, and those are two tables.
Show's `children` is **removed, with no alias**.
Nothing calls it: no flow in `flows/` passes it (`overview` uses the list's `expand`, a different argument),
and the only mentions are its own documentation —
`terminal-renderer.md` (Show, Children), the kinds table's description,
and the example in `AGENTS.md` (Operating the tracker) —
which change with it, and its code (`view/children.go`, `tui/show_children.go` and their tests), which is replaced.
An alias would keep alive the reading this design drops (S3).

The name says what the argument is — a table, per relation —
where `children` named one use of it and implied a hierarchy that `blocks` is not.

## S2 — An entry is one of two forms, and names the stored field

An entry names **where the relation is stored**, and takes exactly one of two forms:

**(a) `type` + `relation`, both required: read from the child's side.**
The issues of `type` whose stored relation field `relation` — a field of that type —
holds the shown issue's id.
`{"type":"task","relation":"parent"}` on a story is its tasks;
`{"type":"bug","relation":"blocks"}` is the bugs that block this issue.
`type` is required because the field belongs to a type (every field belongs to exactly one, `e7e58f2`):
`task/parent` and `epic/parent` are two entities,
and a name without its type is a guess at which ones are meant.
Two types are two entries, and two tables.

**(b) `field` alone: read from the shown issue's side.**
A stored relation field of the shown issue's own type; the table lists the issues it names.
`{"field":"blocks"}` is the issues this one blocks.
Its type is the shown issue's, so naming one would only be a chance to name the wrong one.

**`field` takes `relation` as well as `multi-relation`** (decided here):
a `relation` field is a table of at most one row.
The fields table already draws that one issue as a link,
but only its title; the table draws the columns the entry asks for —
a story's epic with its `status` and `due` — which the link cannot.
Refusing it would be a rule to learn with nothing gained,
since the field's kind already says how many rows there can be.
(The same holds in form (a): `relation` there may be either kind on the child.)

Anything else is refused, naming the two forms:
`type` without `relation` or the reverse, `field` with either,
and none of the three.

**No inverse names and no guessing.**
`{"relation":"children"}` meant *every type's `parent`*
because `schema.yaml` declares `inverse: children` on each type's `parent` —
a reading the call never shows, of field names it never wrote.
The call now names the stored field and, on the child's side, its type;
the schema's `inverse` is not consulted at all, and neither is any other type's field.
The 2026-09-29 rule "a key that is not a field is read as an inverse" goes,
and with it the `← parent` heading for a relation with no inverse,
since every relation is now named by the field that stores it.

## S3 — The keys beside the form, re-checked

An entry is otherwise a **layer** (Nesting), and keeps the layer keys that mean something on one level:

- **`query`**, a jq program over the entry's candidates — in (a), the unarchived issues of `type` whose `relation` holds this id;
  in (b), the unarchived issues the field names — written as a list's query is: `map(select(.fields.status != "done"))`.
- **`include_archive`**, which brings the archived candidates back.
  In (b) an archived issue the field still names is otherwise left out, as every program's input leaves it (`include-archive.md`).
- **`fields`**, the columns after `id` and `title`, checked against the types the table can hold:
  in (a), `type`; in (b), the field's `target_types`, or every type when it has none,
  a key being accepted when one of them has it, as a layer's keys are.
- **`rank`**, the field the rows are ordered and dragged by, checked the same way; the built-in `rank` by default.

`relation` is not a layer's `relation` any more, because a layer's is read from the row's side and may be an inverse;
here it is the child's stored field and comes only with `type`.
`details`, `group_by` and `expand` are refused by name:
a side column is too narrow for a second line, a section or a tree.

A stored relation of the shown type that a form (b) table draws
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
The heading is **what the entry named**, so the reader sees the call's own words:
in form (a), `type · relation` — `task · parent`, the tasks by their `parent`;
in form (b), the field's key — `blocks` — as the fields table's key column names the same field.
Keys and not display names, because the fields table beside it shows keys,
and two tables of one type by two relations (`task · parent`, `task · blocks`) read apart.
Under it the column header, dim, as a child table's is on a nested list:
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
Fields and tables are read side by side from the top,
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
The page still opens on the first field row; the side table is reached, never landed on.

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
  The `children` rows were never editable, because they were not fields;
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
The members of a form (b) `multi-relation` are in that order too, not in the order they were added,
because a set has no order of its own.

Adding to or removing from a form (b) field is not a key here:
it is the field's own picker, which rings for now (`cdffc6c`).

## S6 — The ghost is the foot of a form (a) table

A dim `+ (new)` row at the foot of each **form (a)** table — the item `b9a9b62` lists as
"one per children section on show", built with whichever of the two lands first.
`Enter` opens `new` prefilled with the drop's write set (`create.md`, C6),
which the entry spells out in full:
`type` set to the entry's `type`,
and the entry's `relation` set to this issue's id —
the id itself for a `relation`, a set holding it for a `multi-relation`.
Nothing is inferred from the rows, because the entry already says both.
Create pops back to this show, the cursor on the new row.

A **form (b)** table has **no ghost**:
the new issue would have to be added to *this* issue's field,
a second commit on a second entity,
and there is no atomic multi-entity commit to make the pair one write.
It comes with a create-then-link, or with the multi-value picker (`cdffc6c`).

## S7 — Live, and never on a draft

The tables are recomputed on every load, which the ref watcher's refresh and the page's own writes both do:
a task created elsewhere with this story as its parent appears.
The cursor is kept by the row's id across a reload,
and lands on the row that took its place when its issue left the table.

`new` takes no `relation_table` and draws no side table.
A draft has no id, so no form (a) table can hold anything,
and its own stored relations, form (b)'s material, are rows of its fields table, edited there.
When a standalone Create replaces the page with show, that show is bare, as any show reached without a call is.

## S8 — One call, one check, every surface

`work.view.show(id=…, relation_table=[…])` is the same object the command takes:
no script-only form, the 1:1 rule held by construction,
and `children=` is refused there as it is on the command, an unknown argument naming the ones show takes.
`git work view show --help` reads the argument's description from the kinds table, which names the two forms.

The shape — one form per entry, the keys each form takes — is checked by `view.Parse`, which has no store.
The names are checked in `host.View`, before a renderer is chosen,
so the command, a flow and the gui refuse a call with the same words before anything draws.
Form (b) needs the **shown issue's type**, so `host.View` resolves `id` first (it already must).
Refused, each naming what exists:

- (a) a `type` that does not exist (naming the types);
  a `relation` that is not a field of that type, or not a relation (naming that type's relation fields);
  a field whose `target_types` leave out the shown issue's type, which could never hold this id
  (naming the target types);
- (b) a `field` that is not a field of the shown issue's type, or not a relation (naming that type's relation fields);
- a `fields` or `rank` key that no type the table can hold has (S3);
- `details`, `group_by` and `expand` on an entry, and a `query` that does not compile.

A query that fails on the data is reported in the status line, as a layer's is, and the table draws `(none)`.

## Out of scope

- **`expand` still takes inverse names.** The list's and the gantt's `expand` read a relation from the row's side
  and accept the schema's inverse, so `flows/overview.star` nests with `{"relation": "children"}`,
  meaning every type's `parent` — exactly the reading S2 drops from show.
  Whether `expand` should follow, with a child-side form `{"type":"task","relation":"parent"}` and a row-side `{"field":…}`,
  is flagged here and not decided: it changes a flow in use and every layer spec in the docs.
- **Show's defaults.** `Enter` from a list opens a bare show, with no side table.
  A type's usual tables (a story's tasks) belong to a flow that calls show,
  not to the schema: there are no field roles to say which relation is the children.
- **Nesting in the side table** (`expand` on an entry) and **sections** (`group_by`).
- **Rows that are not issues** (`query-rows.md`): a side table's query
  narrows its candidates; a `key` or a row `children` it returns is ignored,
  because one table on one issue has no pair to stand for.
- **Membership edits** of a form (b) field from the table: the picker (`cdffc6c`).
- **The GUI's drawing**, which takes the same call when the gui process exists.

## Open questions

1. **The cap.** The taller of the fields table and 12 lines, then an inner scroll.
   Is a fixed cap right, or should it be half the window,
   or should the column never scroll and simply grow, leaving the page's own scroll to cope?
2. **`←`/`→` on the fields table** stop switching tabs when a side table is drawn.
   One rule, cells where there are cells, but the same stop answers `→` two ways depending on the call.
   Is that acceptable, or should `→` from the fields always enter the side table (and ring when there is none)?
3. **Beside by default.** Should an entry be able to ask to be drawn under the fields
   even when it fits beside (a long table), or is the window the only judge?
