# Show's side tables: `expand` on show, a table per layer beside the fields

**Outcome:** `git work view show` draws the issues a relation reaches from the shown one —
a story's tasks, the issues this one blocks —
as small tables to the right of the fields,
a row per issue and a column per field the call asks for,
so the page reads like Jira's issue page with its child issues beside it.
The argument is `expand`, the one the list and the gantt take,
so a relation and a layer mean the same thing on every kind.

**Serves:** `bf9bf96` (the task);
`111e8e9` (views create issues, whose `b9a9b62` puts a ghost under each such table on show, S6);
`2298f37` and `00a63d9`, whose stories are the pages it is wanted on.

**Status:** approved 2026-10-08 (Luis).
The argument's shape went through two drafts the same day —
`relation_table`, with a child-side form `type`+`relation` and a shown-side form `field` and no inverse names —
before settling on `expand`, for symmetry with the list and the gantt.
The objection to inverse names was withdrawn with it:
an inverse is declared in the schema (`inverse: children` on each type's `parent`), not assumed.
It replaces show's `children` (`terminal-renderer.md`, Show, Children, 2026-09-29),
where a section was rows of the fields table,
and builds on show as it stands since 2026-10-08:
`rank` is not drawn, the box has no hint line under it,
and the page opens on the first row of the fields table.

## The page it gives

```
git work view show '{"id":"2298f37","expand":[{"relation":"children","fields":["status","assignee"]},{"relation":"blocks","fields":["status"]}]}'
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
```

and, in a window too narrow for both, the same tables under the fields, full width:

```
›status          in-progress
 …
 due             2026-10-30

 children
 id       title                         status       assignee
 a1b2c3d  Columns from the schema       done         Luis Rayas
 …
```

## S1 — The argument is `expand`, and `children` goes

Show takes `expand`: **exactly what the list's and the gantt's `expand` takes** —
a relation name, or a layer object —
**or a list of those**, one side table per element, drawn in the list's order.
A single name or layer is a one-table list.
A story has tasks *and* blocks issues, and those are two tables.

The name is the list's because the object is the list's:
the issues one relation reaches from one row, narrowed and drawn as a table.
A nested list draws that table under a parent row; show draws it beside the one issue it is about.
`flows/overview.star` nests stories with `expand={"relation": "children"}`,
and the same words on show draw the same rows.

Show's `children` is **removed, with no alias**.
No flow passed it; the only mentions were its own documentation —
`terminal-renderer.md` (Show, Children), the kinds table and the example in `AGENTS.md` —
which change with it, and its code (`view/children.go`, `tui/show_children.go`), which is replaced.

## S2 — A relation is read as `expand` reads it

One parser and one check, `view/expand.go`'s, for every kind.
A relation name is either side of a relation:

- **a stored relation field** — `blocks`, a `multi-relation`, is the issues the shown one's `blocks` names;
  a cardinality-one `relation` such as `parent` is a table of at most one row,
  which is not a mistake: the table draws the columns the layer asks for, which the field's link cannot;
- **an inverse the schema declares** — `children` is every issue whose `parent` names the shown one,
  because `schema.yaml` declares `inverse: children` on each type's `parent`;
  `blocked_by` is every issue whose `blocks` does.

Narrowing to one type is the layer's `query`, `map(select(.fields.type == "task"))`:
there is no `type` key, because a list's layer has none.

## S3 — The layer keys a flat table takes

`relation` is required, as it is for a layer with no row above to list its children.
`query`, `include_archive` and `fields` behave as on a layer:

- **`query`** is a jq program over the shown issue's candidates, the unarchived issues the relation reaches;
- **`include_archive`** brings the archived candidates back;
- **`fields`** are the columns after `id` and `title`, checked against the types the relation can reach;

The rows are ordered and dragged by the built-in `rank`, always:
`rank` was a layer key here too until 2026-10-08,
when it left every view as internal (`terminal-renderer.md`, Rank),
and naming it now is an unknown key.

`details`, `group_by` and a nested `expand` (a layer or a number) are **refused by name**:
a side column is too narrow for a second line, a section or a tree.

A stored relation of the shown type that a table draws
is **left out of the fields table**, unless `fields` names it:
one issue's `blocks` drawn twice, once as links and once as a table, is noise.

## S4 — Layout: beside when it fits, under when not

The fields block is two columns, the fields table on the left and the side column on the right,
between them a dim `│` with a cell of space either side.
The left column is the marker, the 16-cell key, and the values,
as wide as its widest value up to 36 cells (a longer value is cut with `…`; copy takes it whole).
The side column is the rest of the window,
and it is drawn beside only when that rest holds every table at its natural width
with the title column at least 20 cells.
Otherwise the tables are drawn **under** the fields table, full width, before the comment box.
**Width alone decides**; an entry has no override,
because which side a table is on says nothing about it, and the keys are the same both ways (S5).

**Each table has a heading and a header.**
The heading is **the relation as the call gave it** — `children`, `blocks` —
the call's own words, as the call line above shows them.
Under it the column header, dim, as a child table's is on a nested list:
`id`, then `title`, then the layer's `fields` in order.
The id and title are always drawn, being what a row is read by;
`fields` defaults to nothing more, because there are no field roles (`d56e6f1`).
Columns are measured over every row of the table, so a refresh moves nothing that did not change;
the title takes the slack.
A person is drawn by name and a relation as the issue it names, as on a list.
Tables are stacked in the call's order with a blank line between.
A table with no row reads `(none)` under its header,
because a table that is not drawn reads as one never asked for.

**The side column's first line is the fields table's first line**, and no field row is pushed to line up with a table.

**Height.** The side column is at most the taller of the fields table and 12 lines.
Past that it scrolls within its window, following the cursor:
the heading and header of the table the cursor is in are kept on the window's first lines,
as a group header is kept on a list,
and a dim last line says how much is out of sight (`↑ 3 · ↓ 7 more`).
A story with forty tasks does not push the comment box and the tabs a screen away.
Under the fields, in the narrow drawing, the same cap applies.

## S5 — Cursor and keys: the side tables are a stop, reached by Tab

The stops are header, fields, **side**, box, tabs.
**`Tab` and `S-Tab` are the only way between the fields and the side tables**, both ways:
`Tab` from the fields lands on the first side row, `S-Tab` from it back on the fields.
`←`/`→` **stay what they are on every stop** —
they walk the header's three cells there, and switch the tab everywhere else, the side tables included —
so that no stop answers them two ways depending on the call.
`C-PgDn`/`C-PgUp` switch tabs from everywhere, as before.

`↑`/`↓` walk within a stop and leave it at its edges, never into the fields' sibling:

- on the fields, `↑` from the first row is the header and `↓` from the last is the box, as before;
- on the side tables, `↑`/`↓` walk every row of every table, ghosts included, top to bottom;
  `↑` from the first is the header and `↓` from the last is the box;
- on the box, `↑` is the fields' last row (the header when there are none), as before.

The fields and the side tables are two columns side by side, so from above and below both are reached the same way,
and which one `↑` from the box lands on is the one it always did.
The page still opens on the first field row.

**In a side table a row is one issue, and the cursor is a row** —
the cells are not walked, `←`/`→` being the tab keys:

- `Enter` opens the row's issue as show — a bare show, as a link opens one — and `Esc` there comes back here, on the same row.
  On the ghost it opens the creator (S6); on `(none)` it rings.
- `Space` **grabs the row**, as `Space` on a list's id does; `↑`/`↓` carry it within its table,
  `Space` or `Enter` drops it, `Esc` puts it back.
  The drop writes the built-in `rank`, one commit, the unranked rows above it in the table ranked first —
  the Rank section's rule for one scope.
  A row cannot leave its table: a carry past its edge stops there, and `←`/`→` ring while a row is grabbed,
  because which relation reaches a row is not an order.
  The rank is the child's, so a drag here reorders it on every list too, as a nested layer's drag does.
  The child's own fields are edited on its page, one `Enter` away.
- Copy (`C-c`/`y`/`M-w`, and `M-c`/`Y`) copies the row's id.
- `/` or `C-s` narrows the side tables' rows to those whose id, title or drawn fields contain the text,
  as a list's filter does; `Enter` keeps it, `Esc` clears it. Elsewhere on show it does nothing, as before.

Rows are ordered `(rank, id)`, the unranked keeping the store's order at the end, as a layer's are.

## S6 — The ghost: under a table over an inverse

A dim `+ (new)` row at the foot of each table **over an inverse** —
the item `b9a9b62` lists as "one per children section on show".
`Enter` opens `new` prefilled with the drop's write set (`create.md`, C6):
the stored field on the child that resolves the inverse set to this issue's id
(`parent` for `children`, a set holding it for a `multi-relation` such as `blocks` under `blocked_by`),
and `type` when the field resolving the inverse belongs to one type.
Only the fields whose `target_types` admit the shown issue's type are counted,
so `children` on a story is `task/parent` and `subtask/parent`, two types and one key:
the page opens on the type cell with `parent` filled.
Where the types disagree on the key, nothing can be prefilled without the type, and there is no ghost.
Create pops back to this show, the cursor on the new row.

A table over **a stored relation of the shown issue** has **no ghost**:
the new issue would have to be added to *this* issue's field,
a second commit on a second entity, and there is no atomic multi-entity commit.

## S7 — Live, and never on a draft

The tables are recomputed on every load, which the ref watcher's refresh and the page's own writes both do.
The cursor is kept by the row's id across a reload,
and stays at its place in the column when its issue left.

`new` takes no `expand` and draws no side table:
a draft has no id, so no inverse can name it,
and its own stored relations are rows of its fields table.
When a standalone Create replaces the page with show, that show is bare, as any show reached without a call is.

## S8 — One call, one check, every surface

`work.view.show(id=…, expand=[…])` is the same object the command takes, through `host.View`:
no script-only form, and `children=` is an unknown argument on both, naming the ones show takes.
`git work view show --help` reads the argument from the kinds table.

The shape — a name, a layer, or a list of them, and the refused keys — is checked by `view.Parse`, which has no store;
the names by `view.CheckSchema` in `host.View`, the same check every layer gets,
before a renderer is chosen, so every surface refuses the same call with the same words before anything draws:
a relation no type has, as a field or an inverse (naming the relations);
a `fields` key no type the relation reaches has;
a `query` that does not compile.
A query that fails on the data is reported in the status line, as a layer's is, and its table is left unnarrowed.

## Out of scope

- **Show's defaults.** `Enter` from a list opens a bare show, with no side table.
  A type's usual tables belong to a flow that calls show.
  Taken up 2026-10-08 by `show-from-a-view.md`:
  a view's `show` maps a type to the show its `Enter` opens, still the flow's and not the schema's.
- **Editing a child's cells in the table.** `←`/`→` are the tab keys, so a cell cannot be reached;
  the child's page is one `Enter` away.
- **Nesting and sections in the side table**, refused by name (S3).
- **Rows that are not issues** (`query-rows.md`): a side table's query narrows its candidates,
  and a `key` or row `children` it returns is ignored.
- **Membership edits** of a stored `multi-relation` from the table: the field's picker (`cdffc6c`).
- **The GUI's drawing**, which takes the same call when the gui process exists.

## Settled questions

The proposal's open questions were settled on approval (Luis, 2026-10-08):
the cap stays the taller of the fields table and 12 lines, then an inner scroll;
`←`/`→` stay tab changes everywhere, and `Tab`/`S-Tab` alone move between the fields and the side tables;
placement is width alone.
