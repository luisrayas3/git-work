# Config entities (`3556569`)

**Outcome:** every configurable thing in the tracker,
a field, an issue type, a relation type, a flow, a view,
is its own CRDT entity under a `refs/work-*` namespace,
so team configuration travels with the tracker,
merges at the boundary the dag already provides,
and shows who changed what.

**Serves:** `6555e36` (schema, step 1 of its order of work),
`17e1d0a` (flows, `b511c63`),
`3df330f` (views `f37603c`).
`7c90fbd` decided that config is CRDT entities rather than a file;
this document is the entities themselves.

**Status:** design, awaiting approval.
Revised 2026-09-22 from a one-entity draft;
see "Why not one entity" at the end.

## What the code offers

Three facts from `entity/dag` shape everything below.

**1. An entity is the merge unit, and inside it the order is total.**
Two clones editing two different entities never conflict;
`dag.read` sorts one entity's operation packs by lamport edit time,
then by pack id
(`entity/dag/entity.go:207`),
so "the last operation on this attribute wins" is deterministic across clones
without any code of ours.
"Last" means last in lamport order, not wall-clock order:
a clone that commits after a week offline
orders *before* everything committed online since.
Issues already behave this way;
config inherits it.

**2. Operations are validated on read, and a failure makes the entity unreadable.**
`read` calls `opp.Validate()` on every pack,
which calls `Validate()` on every operation.
Whatever an operation's `Validate` checks
must hold for every operation ever written,
or history disappears.
This is why semantic rules cannot live on the operation.

**3. The cache is generic, and so are fetch, push and merge.**
`cache.SubCache` takes any `dag` entity
and gives it the on-disk excerpt file,
the ref-hash staleness diff (`d591cb3`),
the observers the ref watcher drives (`63c68d1`),
and a place in `Fetch`, `Push` and `MergeAll`,
which iterate the subcaches by namespace
(`cache/repo_cache_common.go:85`).
A new entity type costs an excerpt, a cached wrapper and a constructor,
not a second synchronisation path.

## Decisions

### E1 — One entity per configurable thing

A field, an issue type, a relation type, a view and a flow
are each **one entity**, with a content-derived id like everything else,
in a namespace per kind (`483dbe2`):
fields, types and relations under `refs/work-schema/<id>`,
because they form one whole that is imported, exported and reconciled together;
views under `refs/work-views/<id>`; flows under `refs/work-flows/<id>`.
One entity type, one `dag.Definition` per namespace, one subcache each;
the merge tiers run identities, then schema, then issues and iterations, then views and flows.
Automation is out of scope: a flow runs when invoked and nothing in git-work fires on its own,
so there is no rule kind, no trigger attribute and no `refs/work-rules` (`47b8430` closed).

This is the dag used as designed.
The entity boundary is the coarse merge unit:
two people adding two flows are two entities and never meet,
exactly like two people opening two issues.
Inside one entity, per-attribute and per-item operations (E2)
give the fine unit:
two people adding two statuses to the same field both keep them.

There is **no singleton**.
Nothing has to be found by "the one entity of kind X";
the schema is the set of field, type and relation entities,
read by listing the namespace.
The one failure that remains is two clones defining the same key
before either pushes (E7).

One Go entity type serves all kinds.
The kind is fixed in the create operation
and phase 4 adds kinds, not entity types.

### E2 — Six operations, generic across kinds

```
Create      {kind, key}                 kind and key are immutable; the id derives from this op
SetAttr     {name, value JSON}          one attribute, last in dag order wins
RemoveAttr  {name}
SetItem     {id, value JSON}            one member of the entity's item set, same rule
RemoveItem  {id}
SetArchived {bool}                      soft removal; refs cannot be deleted across clones
```

plus dag's own `NoOp` and `SetMetadata`, as the issue entity uses them.

An entity's state is its kind, its key,
a map of attributes,
a map of items,
and the archived flag.
Items are the collection a kind carries:
a field's enum values,
a type's allowed parents.
Flows and views carry their document as attributes;
their owning tasks say which.

Why generic:
sections arrive at different times,
schema in phase 2 and flows and views in phase 4,
and with kind-specific operations each would be a new op type
that old binaries read as `dag.UnknownOperation` and skip.
With generic operations an old binary reads a `view` entity in full,
lists it and syncs it,
and only its typed projection ignores it.
Two structural `Validate`s instead of a dozen is the other half,
and fact 2 says structural is all they may ever be.

Typed access is in Go:
`schema.Field`, `schema.Value`, `schema.Type`, `schema.Relation`
are projections of a snapshot of the matching kind,
and `cache.ConfigCache` exposes typed helpers
that emit these operations.
Nobody outside the entity package writes an attribute name by hand.

`schema log` renders operations by kind and key,
so "Luis added value `qa` to field `status`" is what a reader sees.

### E3 — What each kind carries

```
kind      key        attributes                                   items
field     <key>      kind, name, description, ordinal, on,        enum values: <id> -> {name, ordinal,
                     role, on_open, on_close, freeform, …         category, description, color}
type      <id>       name, ordinal, description                   allowed parents: <type id> -> {}
relation  <key>      name, inverse, cardinality (one|many)        —
flow      <name>     owned by `b511c63`                           —
view      <name>     owned by `f37603c`                           —
```

Notes on the field attributes:

- `kind` is the field kind from `bb9e89e`'s fixed list.
  It is set at creation and never changes;
  a `SetAttr` that would change it is refused (E6).
- `on` says which entity the field applies to, `issue` or `iteration`,
  since iterations are the same entity as issues in a second namespace
  and take fields the same way (`configurable-schema.md` D5).
- `role` is a value from a small closed set,
  `assignee`, `estimate`, `start`, `target`, `due`,
  so that the Gantt finds its start and end dates
  and the planning view its estimate
  by kind and role, never by key.
  The set belongs to `bb9e89e`.
- `on_open` and `on_close` on the status field
  name the values the `open` and `close` verbs map to.
- `freeform` on `multi-enum` allows values outside the item list;
  Jira's labels are freeform, ours too.

A relation entity declares both directions:
`inverse` is the name the derived side reads as
(`parent` reads as `child` from the other side, `blocks` as `blocked-by`),
never a key an issue can write (`configurable-schema.md` D4).
On the issue, the relation is a field under the relation's key,
of kind `relation` when the cardinality is one and `multi-relation` otherwise;
the relation entity is what tells the engine that the string it holds is an issue id.

A field key must also pass the issue entity's structural check,
`^[a-z][a-z0-9_-]*$`, the stricter of the two slugs;
the entity's check is frozen, and can only ever be loosened.

Keys and ids are slugs,
`[a-z0-9][a-z0-9_.-]*`, at most 64 characters.
Display names are free text in attributes,
which is what lets a Jira rename change one attribute and no history.

### E4 — Built-in fields are code; entities override their configurable parts

`title` (text), `type`, `status` (enum with category) and `archived` (bool)
exist in every schema,
because tooling cannot function without them
(`configurable-schema.md` D2).
They are defined in code, in `schema.Builtins`, with defaults:
status has values `open` (unstarted) and `closed` (completed).

A field entity whose key is a built-in
**overrides the configurable parts** of that built-in:
its values, name, `on_open` and `on_close`.
It cannot change the kind and cannot be archived (E6).
`schema init jira` therefore creates a `status` entity carrying Jira's statuses,
and a repository with no config entities at all
runs on the built-ins alone,
which is the `base` schema of D7:
not a file, the defaults in code.

### E5 — Order is an attribute called `ordinal`

Every enum value, every type and every field carries an integer `ordinal`;
sort by `(ordinal, id)`.
Presets number by tens
so a human inserting one value between two needs one operation, not a renumber.
When there is no gap, `schema import` renumbers the list;
inside one entity that is many `SetItem`s in one pack and one commit,
so unlike issue rank (`441dcbb`) a renumber here is atomic
and needs no fractional scheme.
Field order across entities is not atomic,
and does not need to be:
a half-applied reorder of fields is still a valid order.

D1 said "ordinal" for values and "rank" for types.
Both are `ordinal`;
`rank` is the issue field kind from `441dcbb`.

### E6 — Structural rules on the operation, semantic rules in `Update`

`Create.Validate` checks the kind is a slug and the key is a slug.
`SetAttr.Validate` and `SetItem.Validate` check
a slug name or id,
and a value that is valid JSON under 64 KiB and `text.Safe`.
**These rules are frozen** the day the first entity is written;
fact 2 says why.
They do not check that the kind or attribute is one this binary knows,
so a newer binary's entities read on an older one.

Everything that can change lives at write time,
in `cache.ConfigCache.Update`:

```go
func (c *ConfigCache) Update(fn func(current *config.Snapshot) ([]config.Change, error)) error
```

`Update` takes the store's write lock,
re-reads the entity (the `rebaseStaged` guarantee, `2a51f66`),
compiles it,
calls `fn` on that fresh snapshot,
validates the snapshot the changes would produce,
appends the operations and commits: one pack, one commit.
Computing the changes inside the lock means
`reconcile(desired, current)` (E9) diffs against what is actually there,
so it changes exactly what still differs
instead of redoing or undoing someone else's edit.

Semantic validation, rejected with the thing and the valid alternatives named,
per `bb9e89e`:

- `kind` is in the fixed list and never changes;
- `category` is one of `backlog unstarted started completed canceled`;
- `ordinal` is an integer, `on` and `role` come from their closed sets;
- `on_open` names a value whose category is not `completed` or `canceled`,
  `on_close` one whose category is;
- items exist only on kinds that have them;
- a built-in's kind is not changed and a built-in is not archived;
- `inverse` names are unique across relation entities and collide with no key;
- flow and view attributes are checked by a validator their owning package registers;
  a kind with no validator is accepted as written.

Cross-entity references, `allowed_parents` naming a type
or a view naming a field,
are checked against the excerpts at write time
and **tolerated on read**:
a dangling reference is reported in `Snapshot.Problems`, never a failure,
the same rule D4 gives dangling relation targets.
Reads never fail on what was written before (D6).

### E7 — Duplicate keys

Two clones creating a field `phase` before either pushes
produce two entities with the same kind and key.
`RepoCacheConfig.Current(kind, key)` picks the one with the lowest creation lamport time,
ties by lowest id,
from the excerpts alone.
The other is ignored deterministically
and every `schema` command prints it on stderr.

Unlike a duplicate singleton, this has a real remedy:
`schema import` of the loser's exported values folds them into the winner,
and `schema rm` **archives** the loser,
which is an operation and so reaches every clone.
Nothing needs a ref deleted.

### E8 — The cache layer

- `cache.RepoCacheConfig` wraps `SubCache[*config.Item, *ConfigExcerpt, *ConfigCache]`,
  cache file `cache/config`.
  `maxLoaded` is unbounded for this subcache;
  a few dozen small entities stay in memory.
- `ConfigExcerpt`: id, kind, key, archived, ordinal, create and edit lamport times.
  Enough to list, to pick a duplicate's winner and to order,
  without loading.
- `ConfigCache` embeds `CachedEntityBase` and adds `Update` (E6)
  and the typed helpers that call it.
- Resolvers gain `&ConfigCache{}` and `&ConfigExcerpt{}`.
- `MergeAll` runs identities, then config, then issues and iterations.
  Issue merges do not consult the schema (D6),
  so this ordering costs nothing and keeps a door open.
- `Fetch` and `Push` pick up the `refs/work-*` config namespaces from the subcache list unchanged,
  so `git work push` and `pull` carry configuration with no new code.
- The ref watcher already refreshes every subcache (`cache/watcher.go:114`),
  so a status someone else adds appears in an open GUI as a new column.
- `RepoCache.Schema()` compiles the built-ins plus every unarchived field, type and relation entity
  into one `*schema.Schema`,
  memoised by the set of config ref hashes,
  rebuilt on update and refresh.
  Issue-write validation (`5b09ee1`, `bb9e89e`) calls it from `cache`,
  which keeps `entities/issue` free of any import of `schema` or `config`.

### E9 — Export, import and `reconcile`

The document a human edits is a view over the entities:

```yaml
preset: jira
fields:
  status:
    kind: enum-with-category
    name: Status
    on_open: to-do
    on_close: done
    values:
      - {id: to-do,       name: To Do,       category: unstarted}
      - {id: in-progress, name: In Progress, category: started}
      - {id: in-review,   name: In Review,   category: started}
      - {id: done,        name: Done,        category: completed}
  priority:
    kind: ordinal-enum
    values:
      - {id: highest, name: Highest}
      - {id: high,    name: High}
      - {id: medium,  name: Medium}
  capacity:
    kind: number
    on: iteration
types:
  - {id: epic,  name: Epic,  allowed_parents: [initiative]}
  - {id: story, name: Story, allowed_parents: [epic]}
relations:
  parent: {inverse: child, cardinality: one}
  blocks: {inverse: blocked-by, cardinality: many}
```

List position *is* the order in the file;
`ordinal` is never written by hand.
Export is deterministic
(sections in a fixed order, then `(ordinal, id)`),
so `export | import` emits zero operations,
which is the round-trip test.
`--format json` gives the same document to agents (`e8d6426`).

`reconcile(desired, current) []EntityChange` is one function,
used by import, by `schema init` (desired = preset, current = nothing)
and by the Jira sync (`69b7be0`):

1. for each section **present** in the document,
   match entries to unarchived entities by kind and key;
2. create an entity for each entry with no match,
   archive each unmatched entity,
   and for each match emit `SetAttr`/`SetItem`/`RemoveAttr`/`RemoveItem`
   for every attribute or item whose canonical JSON differs;
   sections absent from the document are untouched,
   so a file that only says `fields:` cannot archive anyone's views;
3. assign ordinals so that unchanged relative order keeps its numbers,
   a moved or new entry takes a number between its neighbours,
   and a list is renumbered only when no number fits.

Canonical JSON (sorted keys, no whitespace, normalised numbers)
is the comparison,
which is the "normalise before comparing" rule D1 handed `69b7be0`.

Import validates the whole desired document before writing anything,
then writes one entity at a time through `Update`.
An import that touches five fields is five commits;
a failure on the third leaves two applied,
which is the same non-atomicity every multi-entity change in this store has
and reads correctly at every step, since each entity is valid on its own.
`--dry-run` prints the changes per entity and writes nothing,
in the same shape `issue patch --dry-run` prints its operations.

### E10 — Commands

```
git work schema                 show the live schema (alias of export)
git work schema init [preset]   create the preset's entities; refuses if any field entity exists
git work schema export          [--format yaml|json]
git work schema import <file|-> [--dry-run]
git work schema log [key]       config operations, rendered by kind and key
git work schema rm <key>        archive a field, type or relation (E7)
```

`flow` and `view` commands (`b511c63`, `52a2797`)
create and edit entities of their kinds through the same `Update`
and are not part of this task.

Library: `github.com/goccy/go-yaml`, already an indirect dependency,
because its errors carry line and column,
and an import error an agent can act on needs both.

## Package layout

```
entities/config/   Item, Snapshot, the six operations, unmarshaler, actions, resolver
schema/            Builtins; Field, Kind, Value, Category, Type, Relation, Role;
                   projection from snapshots; presets/{jira,linear}.yaml (go:embed);
                   export; reconcile
cache/             config_subcache.go, config_cache.go (Update), config_excerpt.go, Schema()
commands/schema/   the surface in E10
```

`schema` holds types and projection here;
`bb9e89e` adds issue-write validation to the same package.

## Why not one entity

The first draft (2026-09-21) put everything in one entity
as a flat map of paths with per-path last-writer-wins and prefix removes,
arguing that views name fields and flows name statuses
and so want one consistent snapshot.
Consistency across anything in this store is eventual,
and a view naming a renamed field dangles either way,
so the argument was hollow.
What the draft had built was a small key-value CRDT of its own
inside an entity,
when the dag already gives one CRDT per entity
and the entity boundary is exactly the merge unit being simulated.
The one-entity design also needed a singleton,
with a duplicate race no operation could repair.
Many entities need neither.

## What this changes elsewhere

Recorded on the tasks as well, per the working conventions:

- `configurable-schema.md` D1: many entities under `refs/work-schema/*`, `refs/work-views/*` and `refs/work-flows/*`, not one singleton;
  six generic operations, not the enumerated list;
  types carry `ordinal`, not `rank`.
- `b511c63`, `f37603c`, `47b8430`, `52a2797`:
  a flow, a view is an entity of that kind,
  created and edited with the generic operations;
  each owner supplies the attribute set and a validator.
- `bb9e89e`: built-ins in code with configurable overrides;
  the `on`, `role` and `freeform` attributes.
- `87a48c1`: iteration fields are field entities with `on: iteration`.
- `5b09ee1`: `on_open`/`on_close` on the status field are what the open and close verbs write.

## Done when

- `git work schema init jira` creates one `refs/work-schema/*` entity per field, type and relation,
  and `git work schema` prints the preset back;
- `schema export | schema import` emits zero operations;
  editing one value and importing emits one;
- two clones adding two different statuses both keep them after `pull`;
  two clones renaming the same status agree on the winner;
  two clones creating the same field see one warning and one winner;
- a repository with no config entities runs on the built-ins,
  and `RepoCache.Schema()` says so;
- `schema log` names who added `qa` and when;
- `schema import --dry-run` on a file with an unknown kind fails,
  naming the field and the valid kinds.

## Risks

- **Generic operations are easy to abuse.**
  Any kind can be created without a design.
  The rule is that a kind exists only with an owning task and a validator;
  `Problems` will show the first violation.
- **Attribute names are forever.**
  Renaming an attribute is a new name plus a projection that reads both,
  because old operations keep theirs.
  E3 is the decision to get right now.
- **Same-attribute edits are last-writer-wins in lamport order,**
  which surprises an offline editor once.
  Issues carry the same property today.
- **Many small refs.**
  A preset is a few dozen entities and a few dozen refs.
  Git handles thousands of refs per namespace already, in `refs/issues`.
- **Every import is operations, forever.**
  A sync that writes unconditionally would bloat the log;
  `reconcile` emitting only differences is what prevents it,
  and `69b7be0` must go through it.
