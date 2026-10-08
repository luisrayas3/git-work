# The show a view opens: per-type show calls on a list, a gantt and a board

**Outcome:** a view says what `Enter` opens for each type it draws,
so an epic opened from a list of epics opens with its stories beside its fields,
and the step from a row to its page needs no second call.

**Serves:** story `72cb0d3`.
It takes up the first item of `show-side-table.md`'s Out of scope,
"Show's defaults. `Enter` from a list opens a bare show, with no side table.
A type's usual tables belong to a flow that calls show."
It keeps that sentence's rule — the tables are the flow's, not the schema's —
and gives the flow a way to say them,
since a flow that draws a list cannot reach the show its `Enter` opens.

**Status:** approved 2026-10-08, its open questions settled below.

## The workflow that needs it

A list of a sprint's epics, each folding its open stories.
`Enter` on an epic should open it with every story it holds beside its fields,
closed ones included, with their status, priority and assignee,
because the page is where the epic's whole plan is read.
`Enter` on a story opens it as today.

Today the list's `Enter` builds a bare show (`newShowPage` with no `expand`),
and a flow has no hook into it.

## V1 — `show` maps a type to the show call `Enter` opens

An optional argument on the list, the gantt and the board:
an object whose keys are type keys
and whose values are show's KWARGS without `id`.

```python
work.view.list(
    query = EPICS,
    expand = "children",
    show = {"epic": {"expand": {
        "relation": "children",
        "query": "sort_by(.fields.status == \"done\", .fields.title)",
        "fields": ["status", "priority", "assignee"],
    }}},
)
```

`Enter` on a row whose issue is of a listed type opens
`show` with the row's `id` and that entry's arguments;
on a row of a type not listed, a bare show, as today.
The type is the stored issue's, never the row's shaped `fields.type`,
since a query may shape fields and the page shown is the issue's.
A row with no `id` (`query-rows.md`, R2) rings, as it does today.

It applies at every nesting level:
the type picks the entry, not the level.

## V2 — the entry is checked as show's own call is

Each value goes through show's argument table and `view.CheckSchema`
before anything draws, so a bad entry is refused at the call, naming the type:

- a key that is not a type the schema has, naming the types;
- `id` in an entry, which is the row's to give;
- anything show itself refuses: an unknown argument,
  a side table's `details`, `group_by` or nested `expand`,
  a relation no type has, a field the relation's types lack.

## V3 — what `Enter` opens from there carries the map

The show a view opens keeps the view's `show` map,
and every page it pushes keeps it too:
`Enter` on a side table's row,
on a relation cell in the fields table,
and the page the creator opens after Create,
each open the issue they name by the same map.
So drilling down an epic to a story to its tasks
reads the same at every step,
and `Esc` back up the stack returns to pages that still have their tables.

A matrix's `Enter` opens a list; that list inherits the matrix's `show`,
which the matrix takes for that reason and uses for nothing else.

A `git work view show` call made directly takes `show` too (settled 2026-10-08):
its own arguments are the shown issue's,
and its `show` is the map the pages it opens are opened by —
a side table's row, a relation cell, the page after Create —
so drilling down from a direct show reads as it does from a list.
It never applies to the shown issue itself.

## Out of scope

- **A default per type in the schema.**
  `show-side-table.md` already places a type's usual tables with the flow;
  a view that wants them names them.
- **A wildcard entry** for every type not listed.
  No workflow needs it yet; a bare show is what an unlisted type gets.
- **The GUI**, which takes the same call when the gui process exists.

## Settled questions (2026-10-08)

1. The argument is named `show`:
   the kind and the argument never meet in one call.
2. Show takes `show` too (V3), for the pages it opens, never for itself.
3. The board takes it as the list does: `Enter` on a card opens by the map.
