# The config entity (`3556569`)

**Outcome:** one CRDT entity under `refs/work/*` holds the schema,
the flow catalogue, saved views and automation rules,
so that team configuration travels with the tracker,
merges per key,
and shows who changed what.

**Serves:** `6555e36` (schema, step 1 of its order of work),
`17e1d0a` (flows, `b511c63`),
`3df330f` (views `f37603c`, rules `47b8430`).
Decided in principle by `7c90fbd`;
this document is the entity itself.

**Status:** design, awaiting approval.

## What the code offers

Three facts from `entity/dag` shape everything below.

**1. An entity is an ordered log of operations, and the order is total.**
`dag.read` sorts operation packs by lamport edit time,
then by pack id
(`entity/dag/entity.go:207`).
So "the last operation on this key wins" is deterministic across clones
without any code of ours,
and "last" means last in lamport order,
not wall-clock order:
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

**3. The cache is generic.**
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

### E1 — One entity, not one per kind

D1 in `doc/design/configurable-schema.md` sketched
"config entities at `refs/work/<id>`, each tagged with a kind (`schema`, `flows`)".
This design collapses that to **one entity**,
typename `config`, namespace `work`, one ref.

Views reference schema fields,
rules reference statuses and relation types,
flows reference views.
One entity means one snapshot in which all of those are consistent with each other,
one singleton to find,
one duplicate-creation race to handle (E8),
and one op log to read as the audit trail.
Separate entities would buy independence between sections
that the sections do not want.

The create operation carries no payload;
its nonce, author and time give the entity its id.

### E2 — Two operations on the wire, a typed API in Go

D1 listed one operation type per concept
(`DefineField`, `AddEnumValue`, `DefineFlow`, …).
The entity stores **two**:

```
SetEntry    {path string, value json.RawMessage}
RemoveEntry {path string}
```

plus `Create`, and dag's own `NoOp` and `SetMetadata`,
as `entities/bug` does.

The state is a flat map from path to JSON document.
A path names a leaf such as `fields/status/values/in-review`;
the value is that leaf's whole document.

Why generic on the wire:

- **Sections arrive at different times.**
  Schema in phase 2, flows and views and rules in phase 4.
  With typed operations each new section is a new op type
  that old binaries read as `dag.UnknownOperation` and skip.
  With paths, an old binary keeps `views/…` entries in its map,
  exports them and reconciles them like any other leaf (E10),
  and only its typed projection ignores them.
- **Granularity is a property of the path layout, not of the op set.**
  Deciding that enum values merge independently of their field
  is a question of where the leaves are (E3),
  answered once, without adding operations.
- **Two structural `Validate`s instead of a dozen**,
  and fact 2 above means structural is all they may ever be.

Why typed in Go:
`schema.Field`, `schema.Value`, `schema.Type`, `schema.Relation`
are what every consumer reads,
and `cache.ConfigCache` exposes typed helpers
that emit `SetEntry`/`RemoveEntry` on well-known paths.
Nobody outside the entity package writes a path by hand.

`schema log` renders the operations by section,
so "Luis added status `qa` to `status`" is still what a reader sees.

### E3 — The leaf is the unit of independent edit

A leaf is the smallest thing two people plausibly change independently.
Everything with an identity of its own is a leaf;
a field is split one level further
because its values have identities of their own
and must survive the field's rename.

```
meta/preset                  "jira"
fields/<key>                 {kind, name, description, on_open, on_close, …kind attrs}
fields/<key>/values/<id>     {name, ordinal, category, description, color}
types/<id>                   {name, ordinal, allowed_parents: [<id>…], description}
relations/<key>              {name, inverse, cardinality: one|many}
                             one leaf declares both directions; `inverse` is the
                             derived side's name (D4), never a key of its own
flows/<name>                 opaque here; `b511c63` owns the document
views/<name>                 opaque here; `f37603c` owns the document
rules/<name>                 opaque here; `47b8430` owns the document
```

Renaming the `status` field and adding a status concurrently both survive:
they touch `fields/status` and `fields/status/values/qa`.
Renaming a status and recategorising it concurrently do not:
both touch one leaf, and the later one in dag order wins.
That trade is accepted because a value is also the unit Jira sync writes,
so anything finer would mostly split hairs.

`on_open` and `on_close` on the status field
name the values a legacy `SetStatusOperation` maps to (D3).
The design in D3 called them "the schema's designated open or closed value";
this is where they live.

Keys and ids are slugs:
`[a-z0-9][a-z0-9_.-]*`, at most 64 characters.
Display names are free text in the document,
which is what lets a Jira rename change one leaf and no history (D2).

### E4 — Order is an attribute called `ordinal`

Every enum value and every issue type carries an integer `ordinal`;
sort by `(ordinal, id)`.
Presets number by tens
so a human inserting one value between two needs one leaf, not a renumber.
When there is no gap, `schema import` renumbers the whole list,
which is many `SetEntry`s in one pack and one commit,
so unlike issue rank (`441dcbb`) a renumber here is atomic
and needs no fractional scheme.

D1 said "ordinal" for values and "rank" for types.
Both are `ordinal`;
`rank` is the issue field kind from `441dcbb`
and the collision would cost a reader more than the distinction buys.

### E5 — Compile is a replay; the typed view is lenient

```
entries := map[path]json.RawMessage{}
for op in ops, in dag order:
    SetEntry:    entries[op.path] = op.value
    RemoveEntry: delete op.path and every path with prefix op.path + "/"
```

`RemoveEntry{fields/status}` removes the field and its values in one operation.
A `SetEntry{fields/status/values/qa}` that lands *after* it in dag order
leaves a value under a field that no longer exists.
The typed projection **drops entries whose parent is missing or undecodable**
and lists them in `Snapshot.Problems` with a reason,
instead of failing.
`schema export` prints problems on stderr;
nothing else looks at them.
This is D6 applied to the config itself:
reads never fail on what was written before.

Unknown sections stay in `Snapshot.Entries` untouched
and are neither validated nor projected.

### E6 — Structural rules on the operation, semantic rules in `Update`

`SetEntry.Validate` checks:
two to eight slug segments,
a path under 256 bytes,
a value that is valid JSON under 64 KiB and `text.Safe`.
`RemoveEntry.Validate` checks the path alone.
**These rules are frozen** the day the first entity is written;
fact 2 says why.
They do not check section names,
so a newer binary's sections read on an older one.

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
appends the operations and commits.
One pack, one commit:
an import is never half-applied,
because the no-atomic-commit rule is about *many* entities.

Computing the changes inside the lock is the point.
`reconcile(desired, current)` against a `current` that has since moved
would redo or undo someone else's edit;
against the re-read one, it emits exactly the difference that still exists.
Config writes are rare enough that the lock's extra read costs nothing.

Semantic validation, rejected with the field and the valid alternatives named,
per `bb9e89e`:

- `kind` is in the fixed list, and never changes on an existing field
  (remove and define instead; a `SetEntry` that changes it is refused);
- `category` is one of `backlog unstarted started completed canceled`;
- `ordinal` is an integer;
- `on_open` names a value whose category is not `completed` or `canceled`,
  `on_close` one whose category is;
- values exist only under enum kinds;
- `allowed_parents` name types that exist;
  `inverse` names are unique and collide with no relation key,
  since `child` is what `parent` reads as from the other side (D4),
  not a relation anyone can write;
- the built-in fields `status` and `labels` cannot be removed;
- flows, views and rules are checked by a validator their owning package registers;
  a section with no validator is accepted as written.

### E7 — No entity means the `base` schema

Every repository that exists today has no config entity,
and a `git-bug` clone never will.
So the absence of an entity is not an error state;
it is the **`base` preset**:
`status` with values `open` (unstarted) and `closed` (completed),
`on_open: open`, `on_close: closed`,
and `labels` as a freeform `multi-enum`.
`base.yaml` is embedded beside `jira.yaml` and `linear.yaml` (D7),
compiled in memory,
never written.

`RepoCache.Schema()` returns the compiled schema either way,
and no caller distinguishes the two.
Our 74 issues therefore validate before `schema init` and after it,
and after `schema init jira` their `SetStatusOperation`s
read as `to-do` and `done` through `on_open`/`on_close`
with no data migration, as D3 promised.

The freeform attribute on `multi-enum` belongs to `bb9e89e`'s attribute list;
`base` is its first user.

### E8 — The singleton, and duplicates

`RepoCacheConfig.Current()` picks the entity with the lowest creation lamport time,
ties by lowest id,
from the excerpts alone, without loading anything.
Any others are *ignored deterministically and reported*:
every `schema` command prints their ids on stderr.

The realistic remedy is prevention:
`schema init` refuses when an entity already exists locally
and says to `git work pull` first,
because the race is two people initialising before either pushes.
If it happens anyway,
`schema export` of the loser piped into `schema import` folds its entries into the winner,
and `schema rm <id>` drops the local ref.
The ref returns on the next pull until the remote drops it too;
a distributed store cannot delete, only agree to ignore.

### E9 — The cache layer

- `cache.RepoCacheConfig` wraps `SubCache[*config.Config, *ConfigExcerpt, *ConfigCache]`,
  cache file `cache/work`.
- `ConfigExcerpt`: id, create and edit lamport times, `Preset`, entry count.
  Small on purpose; the singleton choice reads it and nothing else does.
- `ConfigCache` embeds `CachedEntityBase` and adds `Update` (E6)
  and the typed helpers that call it.
- Resolvers gain `&ConfigCache{}` and `&ConfigExcerpt{}`.
- `MergeAll` runs identities, then config, then issues.
  Issue merges do not consult the schema (D6),
  so this ordering costs one tiny entity and buys future freedom.
- `Fetch` and `Push` pick up `refs/work/*` from the subcache list unchanged,
  so `git work push` and `pull` carry the config with no new code.
- The ref watcher already refreshes every subcache (`cache/watcher.go:114`),
  so a status someone else adds appears in an open GUI as a new column.
- `RepoCache.Schema()` memoises the compiled `*schema.Schema`
  by (entity id, ref hash), invalidated on update and refresh.
  Issue-write validation (`5b09ee1`, `bb9e89e`) calls it from `cache`,
  which keeps `entities/bug` free of any import of `schema` or `config`.

### E10 — Export, import and `reconcile`

The document a human edits:

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
(sections in a fixed order, fields by key, values and types by `(ordinal, id)`),
so `export | import` emits zero operations,
which is the round-trip test.
`--format json` gives the same document to agents (`e8d6426`).

`reconcile(desired, current) []Change` is one function,
used by import, by `schema init` (desired = preset, current = empty)
and by the Jira sync (`69b7be0`):

1. flatten the document to leaves;
2. for each section **present** in the document,
   `SetEntry` every leaf whose canonical JSON differs
   and `RemoveEntry` every current leaf the document lacks;
   sections absent from the document are untouched,
   so a file that only says `fields:` cannot delete anyone's views;
3. assign ordinals so that unchanged relative order keeps its existing numbers,
   a moved or new value takes a number between its neighbours,
   and the list is renumbered only when no number fits.

Canonical JSON (sorted keys, no whitespace, normalised numbers)
is the comparison,
which is the "normalise before comparing" rule D1 handed `69b7be0`.
Unknown sections reconcile generically as `section/<key>` leaves.

`schema import --dry-run` prints the changes and writes nothing,
in the same shape `issue patch --dry-run` prints its operations.

### E11 — Commands

```
git work schema                 show the live schema (alias of export)
git work schema init [preset]   write a preset into a new entity; refuses if one exists
git work schema export          [--format yaml|json]
git work schema import <file|-> [--dry-run]
git work schema log             the entity's operations, rendered by section
git work schema rm <id>         drop a duplicate's local ref (E8)
```

`flow`, `view` and `rule` commands (`b511c63`, `52a2797`)
write their sections through the same `Update`
and are not part of this task.

Library: `github.com/goccy/go-yaml`, already an indirect dependency,
because its errors carry line and column,
and an import error an agent can act on needs both.

## Package layout

```
entities/config/   Config, Snapshot, Create/SetEntry/RemoveEntry, unmarshaler, actions, resolver
schema/            Field, Kind, Value, Category, Type, Relation; decode from entries;
                   presets/{base,jira,linear}.yaml (go:embed); export; reconcile
cache/             config_subcache.go, config_cache.go (Update), config_excerpt.go, Schema()
commands/schema/   the surface in E11
```

`schema` holds types and decoding here;
`bb9e89e` adds issue-write validation to the same package.

## What this changes elsewhere

Recorded on the tasks as well, per the working conventions:

- `configurable-schema.md` D1: one entity, not one per kind;
  two wire operations, not the enumerated list;
  types carry `ordinal`, not `rank`.
- `b511c63`, `f37603c`, `47b8430`:
  `DefineFlow`/`DefineView`/`DefineRule` are `SetEntry` on
  `flows/<name>`, `views/<name>`, `rules/<name>`;
  each owner supplies the document shape and a validator.
- `5b09ee1`: the legacy status mapping reads `on_open`/`on_close` from `fields/status`.
- New: the `base` fallback, and `Update` computing changes inside the write lock.

## Done when

- `git work schema init jira` creates `refs/work/<id>`,
  and `git work schema` prints the preset back;
- `schema export | schema import` emits zero operations;
  editing one value and importing emits one;
- two clones adding two different statuses both keep them after `pull`;
  two clones renaming the same status agree on the winner;
- a repository with no entity behaves exactly as today,
  and `RepoCache.Schema()` returns `base`;
- `schema log` names who added `qa` and when;
- `schema import --dry-run` on a file with an unknown kind fails,
  naming the field and the valid kinds.

## Risks

- **A flat map is easy to abuse.**
  Anything can be stuffed under a new section without a design.
  The rule is that a section exists only with an owning task and a validator;
  `Problems` will show the first violation.
- **Leaf layout is forever.**
  Moving a leaf (say, splitting value attributes) is a new section name
  and a migration, because old operations keep their paths.
  E3 is the decision to get right now.
- **Same-leaf edits are last-writer-wins in lamport order,**
  which surprises an offline editor once.
  Issues carry the same property today.
- **Every import is operations, forever.**
  A sync that writes unconditionally would bloat the log;
  `reconcile` emitting only differences is what prevents it,
  and `69b7be0` must go through it.
