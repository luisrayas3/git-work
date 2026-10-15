# Link fields: one `links` row on show, the links themselves beside it

**Outcome:** a schema field can be marked a **link** —
`blocks`, `clones`, `relates-to`, the fields a Jira link type becomes —
and show draws every link field of the issue as **one row**, `links`,
a count of what the issue is linked to,
with the links themselves as side tables, one per relation that has any.
An issue with no links spends one row on them, not one row per link type.

**Serves:** task `2f84ef5`, under story `087decb` (`app.md`).

**Status:** approved 2026-10-15.
Revised the same day to fit `app.md`:
L4's tables are the built-in default of a type's show rather than drawn on every show,
and L5 edits on `space` and offers the inverses.

## The problem

The Jira sync maps every link type to a `multi-relation` on every mapped type
(`jira-sync.md`, the mapping table: "link type … all mapped types").
A Jira site carries its own link types and those of the apps installed on it,
so nine or ten is ordinary,
and a type with a dozen real fields gets as many link fields again.
Most of them are empty on most issues:
show draws them all, the fields table is half links,
and the fields that matter scroll past them.

They cannot be removed per type.
A Jira link type is site-wide, so no project setting drops it from one issue type,
and archiving the derived field locally does not hold:
the next sync derives it again and `schema import` creates it anew,
a second entity under the key the archived one held.

The fields are not the problem; drawing each as a row is.
A query that wants `.fields.blocks` should keep having it,
and a flow that plans by `blocks` should keep naming it.

## L1 — `link`, a bool on a relation field

A field entity takes `link: true`:

```yaml
types:
  story:
    fields:
      blocks:
        kind: multi-relation
        name: Blocks
        inverse: is-blocked-by
        link: true
```

- **Only a `relation` or `multi-relation` takes it**;
  on any other kind it is refused at import, naming the field and its kind.
- **The inverse is a link when its stored side is**:
  `is-blocked-by` has no entity of its own to mark (`configurable-schema.md`, D4).
- **It changes no storage, no write and no query.**
  `issue get`, `issue set`, the excerpt a program reads,
  and the check every write goes through are what they are without it.
  It is read by the views alone.
- **It is not a field role.**
  Go names no field by it;
  any relation can be marked, and `parent` could be, though nothing marks it.

A bool on the field rather than a group entity holding field keys,
because a field is either drawn as a link or as itself,
and the mark then merges, archives and travels with the field it describes.

## L2 — The Jira sync marks the link types

Derivation sets `link: true` on every field it derives from a link type,
`aliases.jira: link:<id>`, and on nothing else.
An existing store gets the mark on its next sync, as an update of each such field.
The `jira` preset marks its link fields the same way.

A field defined by hand is a link only when its definition says so.

## L3 — Show draws one `links` row

On show, the link fields are **not rows of the fields table**.
In their place is **one row, `links`**,
at the position the first link field would have had in schema order.
Its value is the count, then each relation with any entry and its count,
stored relations first in schema order, then inverses:

```
›status          implementing
 priority        high
 assignee        Ada Lovelace
 links           3 · blocks 1 · implements 1 · is-blocked-by 1
 due             2026-11-30
```

With no link in either direction the value is `—`,
so the row says the issue has none rather than leaving it in doubt.

A call's `fields` that **names a link field** draws that field as a row, as today,
and leaves it out of the `links` row's count,
so a flow can keep `blocks` in the table.
`fields` may name **`links`** to place the row, or leave it out to drop it;
with `fields` absent the row is drawn as above.

`links` is a word of the views, not a field:
the schema refuses it as a field key,
so a view never has to choose between the two.

## L4 — The links are side tables, by default

The issues the `links` row counts are drawn as **side tables**
(`show-side-table.md`), **one per relation that has any entry**,
stored relations first in schema order, then inverses,
each headed by the relation's name, as a call's own tables are.

They are **the built-in spec's**, so every show draws them unless told otherwise:
`{"expand": ["links"]}` is what a type gets when neither the call's `shows`
nor the type's `show` in the schema names it (`app.md` A7).
Today a bare show draws each link field's targets in its fields table;
with those rows gone, the side tables are where the same issues are read,
so a show that says nothing draws them.
A type whose spec's `expand` leaves `links` out draws none,
and the `links` row still says how many there are;
the palette's `e` (`app.md` A7) brings them back for the page in hand.
An issue with no links draws no table and no `(none)`:
the `links` row already says so.

The draft of this section drew them on every show without exception;
`app.md` gave a type a default show of its own,
and the links became that default's rather than a rule beside it.

The tables follow the call's own: after them, in the same column, under the same width rule.

A spec shapes them with **`links` as a name in its `expand`**,
or as a layer, `{"relation": "links", "fields": ["status"]}`.
It stands for the per-issue list of link relations with entries,
so the layer's `query`, `include_archive` and `fields` apply to each table it becomes.
Its place in `expand` is where those tables are drawn.
`links` in a **list's or gantt's `expand`** is refused by name:
a nested layer is one relation, and `links` is several.
That `expand` is the list's nesting, not a spec's (`app.md` A7).

## L5 — Editing a link from the row

`Enter` opens and `space` edits (`terminal-renderer.md`, 2026-10-02), so:

- **`enter` on the `links` row** moves the cursor to the first link table:
  the row is their table of contents.
- **`space` on the `links` row** opens a picker of the link relations,
  **stored and inverse**, each with its count, narrowed as it is typed into (`app.md` A5):

```
blocks          1
clones          0
implements      1
is-blocked-by   1
…
```

Choosing one opens that relation's membership picker, the finder,
and a write there is one `add` or `remove`:

- on a **stored** relation, to this issue's field;
- on an **inverse**, to the other issue's stored field:
  adding `is-blocked-by X` is `issue add X '{"blocks":[this]}'`.
  It is one commit to one entity, the other one,
  and nothing on this side is stored, so nothing has to be atomic
  ("a reverse relation is just a single relation write, just on another issue", Luis, 2026-10-15).
  The finder offers only issues whose type has the stored field,
  the write is checked against that type's schema,
  and the status line names the issue written.

The draft of this section offered stored relations only,
on the grounds that an inverse is a second entity;
it is, but it is the only entity written (`app.md` A6).
`create.md` C8 writes a child stored on its parent the same way.

The palette's `l` and `u` (`app.md` A6) are this picker reached from anywhere on the page.

`new` draws the same row over the draft's stored link fields,
counted as the draft holds them, with the same picker on `space`;
it offers no inverse, since the draft has no id to write into another issue yet.
A draft has no inverses and no side tables (`show-side-table.md`, S7).

## L6 — One call, one check, every surface

`links` in a spec's `expand` and its `fields` is the same on the command line and in Starlark,
checked by `view.CheckSchema` like any other name.
`git work schema` prints `link: true` where it is set;
`--help` on show describes the row and the `links` name.

## Out of scope

- **Storing links differently.**
  One field of `{type, target}` pairs would make the schema shorter
  and every query longer (`.fields.links | map(select(.type == "blocks"))`),
  and would demote `blocks` from a field a flow can name to an entry in a list.
  The fields stay as they are; this design only draws them.
- **One definition shared by every type.**
  A link type is site-wide, and the schema repeats it per type,
  so link types × types field entities.
  Defining a link once for all types is a schema change of its own,
  wanted only if `git work schema` output stays noisy once show stops drawing it.
- **A `links` column on list, board and gantt.**
  A count cell is cheap, but what `Enter` on it opens is a question for those views.
- **Folding fields that are not links**, such as rarely set custom fields.
  The mark is `link` and means a link; a general `group` is a different design.
- **Archiving a derived field.**
  That the next sync recreates it as a second entity (The problem)
  is a defect of the sync, not of the views, and wants its own design.

## Open questions

1. **A local `link: false` against the sync.**
   A store that wants one link type drawn as its own row everywhere
   can name it in each call's `fields` (L3), or unmark the field.
   Whether derivation reasserts `link: true` over a local `false`
   depends on how the sync treats a derived attribute edited locally,
   which this design does not settle.
2. **The name.** `link` reads well on the field (`link: true`);
   `links_group` says more about what a view does with it.
   This draft takes `link`.
3. ~~**Opting out of the tables.**~~
   Settled by the revised L4 (2026-10-15):
   the tables are the default `expand`, so a type or a call that leaves `links` out has none.
