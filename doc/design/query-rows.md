# Rows a query makes: a row's key, the issue it stands for, and the children it lists

**Outcome:** a view's query can draw rows that are not one-to-one with issues —
the same issue twice, under two groups,
or a row standing for a pair such as *a person's share of an epic* —
and can say what nests under each,
so a nested list draws the cross the query computed,
not only the tree the store's relations draw.

**Serves:** story `d1c547a`.
It reverses one sentence of `terminal-renderer.md` (Query):
"What is given up is rows that are not issues …
every target workflow is a workflow over issues."
The workflow below is over issues, and it still needs rows that are not.

**Status:** approved 2026-10-08, its open questions settled in R5 and below.

## The workflow that needs it

A sprint page by person.
Under each person, one row per epic they own or hold an open story in,
sorted by the epic's initiative;
under each of those rows, that person's open stories in that epic, and nobody else's;
and a `(none)` row for the person's stories with no epic.
The page opens unfolded.

```
Ada
  Initiative 1  Big epic 1         in-progress
    Story about a girl
  Initiative 1  Big epic 2         to-do
    Story about a boy
    Story about a boy and a girl
  Initiative 2  Do something cool  in-progress
    Some story
  (none)
    A story with no epic
Grace
  Initiative 1  Big epic 1         in-progress
    Grace's story in big epic 1
```

`Big epic 1` is drawn twice, once per person,
and each copy has different children.
Neither is possible today, for three reasons,
the first two of them the issue id doing a second job:

1. **One issue, one row.**
   `nest` draws an issue once, under the first matched row that reaches it,
   and `treeOrder`, the fold state and the cursor's return across a refresh
   are all keyed by issue id.
2. **Children are the store's.**
   A row's children are read off the relation index by the row's id;
   a layer's `query` narrows them but sees only the candidates,
   never the row they are under,
   so it cannot keep one person's stories.
3. **A tree opens folded**, and only `Z` opens it.

The ghost already settled that a row can have an identity that is not an issue's
(`ghostId`, `create.md` C6):
a `+`-prefixed id no issue can have,
because the drawing order keys rows by id.
This design gives a query the same thing.

## R1 — `key` is a row's identity on the screen; `id` stays the issue it acts on

A row the query returns may carry a top-level `key`, a string.
Absent, the key is the `id`, and no existing view changes.

| Concern                                                         | Reads |
| :-------------------------------------------------------------- | :---- |
| drawing order, siblings, parent links (`treeRow`)               | `key` |
| fold state, `open`, the cursor across a refresh                 | `key` |
| `Enter` (show), `Space` (edit), copy of the id, rank writes     | `id`  |
| a link cell's target                                            | `id`  |

So `{"id": "<epic>", "key": "<epic>@<person>", "fields": {…}}`
opens and edits the epic,
and is folded, unfolded and returned to on its own.
Keys are unique among one view's rows;
a query that repeats one is refused, naming the key,
as a malformed row is refused today.
A key cannot start with `+`, the ghost's prefix.

The cells are the row's fields as the query shaped them, as today:
a query that writes `.fields.assignee` to the person draws that person,
and `Space` on that cell edits the epic's stored assignee —
the current rule for a shaped row, unchanged.

## R2 — a row with no `id` stands for nothing

`id` is optional when `key` is present.
Such a row draws its `fields` like any other
and is a row the cursor can stand on:
`Enter` and `Space` on a cell ring, copy copies nothing,
and `Space` on its id grabs it, the keyed row's drag (R5) —
otherwise the ghost's behavior, minus the creator.
It is how the `(none)` row above exists:
`{"key": "none@<person>", "fields": {"title": "(none)", "assignee": "<person>"}, "children": […]}`.

## R3 — `children` on a row lists what nests under it

A row may carry a top-level `children`, an array of issue ids or of rows.
When it does, those are the row's candidate children,
in place of what the layer's `relation` reads off the store for it.
An id is read from the store, as a relation's target is;
a row is taken as the query shaped it, its own `key` and `children` included,
so a query can build every level of the tree itself.

The layer below still applies to them:
its `fields`, `details`, `group_by` and `rank`,
and its `query`, which narrows listed children as it narrows a relation's.
A layer's `relation` becomes optional:
it is required only where some row at the level above does not list its children.

`children` is the word `expand` and show's `children` already use
for what nests under a row.
It is top-level, beside `id` and `fields`, never a field,
so it cannot collide with a schema's `children` inverse.

With R1 and R3, an issue reached twice is drawn twice, once per key:
`nest`'s "an issue shows once, under the first matched row that reaches it"
becomes "a key shows once".
An issue reached twice through the store's relations, with no keys anywhere,
still shows once, since its key is its id.

## R4 — `open` unfolds the tree when it is drawn

A call argument on the list and the gantt:
`true` opens every parent,
a number opens that many levels from the roots,
absent or `false` is today's folded summary.
It sets the fold state the view starts with, by key;
`Z`, `Space` on the arrow and `Tab` work on it as before,
and a refresh keeps the person's folds, not the argument's.

## R5 — a keyed row reorders for this view only (settled 2026-10-08)

A row whose `key` is not its `id`, or that has no `id`, stands for a share of an issue,
not the issue, so writing the issue's `rank` would reorder every row standing for it.
Such a row is still grabbed with `Space` and moved with `↑`/`↓`,
but the drop writes nothing:
it records the scope's order, by key, in the view instance,
and that order holds across refreshes until the view is quit,
a key it does not know keeping its `(rank, id)` place after the ones it does.
Nothing persists between invocations; the status line says `order kept for this view`.
A row keyed by its own id drags and writes `rank` as today,
and a drop of a keyed row outside its group still rings (Out of scope).

## The workflow's call

```python
work.view.list(
    query = PAIRS,  # one row per (person, epic): id the epic, key
                    # epic@person, assignee shaped to the person,
                    # children that person's open stories in it;
                    # one (none) row per person
    fields = ["parent", "title", "status"],
    group_by = "assignee",
    expand = {"fields": ["status", "priority", "title"]},
    open = True,
)
```

One level of nesting:
the pair is the row, so there is no person level
and no filtering by the parent row.

## Out of scope

- **Board and matrix.**
  Neither nests, so `children` means nothing there,
  and a matrix sums issues, so a key repeating an issue would count it twice.
  Both ignore `key` and `children`, and drop a row with no `id`.
- **A drag across groups for a keyed row.**
  Dropping a pair row in another person's group would write the epic's `assignee`,
  which is not what moving a share means;
  for a row whose `key` is not its `id`, a drop outside its group rings.
- **A layer query that sees its parent row** (`$parent`).
  R3 makes it unnecessary here; it can come later on its own.

## Settled questions (2026-10-08)

1. A repeated key is refused, naming the key (R1).
2. A keyed row's drag is an order kept by the view instance and never written (R5).
3. `open` is a call argument (R4), not a layer key.
