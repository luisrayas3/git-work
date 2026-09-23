# Config entities (`3556569`)

**Outcome:** every configurable thing in the tracker,
an issue type, a field, a flow,
is its own CRDT entity under a `refs/work-*` namespace,
so team configuration travels with the tracker,
merges at the boundary the dag already provides,
and shows who changed what.

**Serves:** `6555e36` (schema, step 1 of its order of work),
`17e1d0a` (flows, `b511c63`),
`3df330f` (saved views, which are flows, `f37603c`).
`7c90fbd` decided that config is CRDT entities rather than a file;
this document is the entities themselves.

**Status:** design, awaiting approval.
Revised 2026-09-22 from a one-entity draft;
see "Why not one entity" at the end.
Revised 2026-09-23 (`e7e58f2`, `d56e6f1`, `0740bf3`):
per-type fields, three shapes, four operations, no roles,
flows as Starlark scripts, and import as the only way from the tree into the refs.

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

An issue type, a field and a flow
are each **one entity**, with a content-derived id like everything else,
in a namespace per shape (`483dbe2`, `d56e6f1`):
types and fields under `refs/work-schema/<id>`,
because they form one whole that is imported, exported and reconciled together;
flows under `refs/work-flows/<id>`.
One entity type, one `dag.Definition` per namespace, one subcache each;
the merge tiers run identities, then schema, then issues, then flows.
Automation is out of scope: a flow runs when invoked and nothing in git-work fires on its own,
so there is no rule shape, no trigger attribute and no `refs/work-rules` (`47b8430` closed).
There is no `view` shape either:
a saved view is a flow whose script returns a render spec,
and `refs/work-views` exists only if a live view ever needs what a flow cannot express (`d56e6f1`).

**Refs are the runtime source of truth; the tree is for authoring** (`0740bf3`).
`schema.yaml` and the `.star` files in the working tree are reviewed and merged by git,
and reach `refs/work-schema` and `refs/work-flows` only through
`git work schema import` and `git work flow import` (E9).
Nothing reads the tree at runtime,
so a checkout can never disagree with the store,
which a flow that assumes the schema of its own commit would.

This is the dag used as designed.
The entity boundary is the coarse merge unit:
two people adding two flows are two entities and never meet,
exactly like two people opening two issues.
Inside one entity, per-attribute operations (E2)
give the fine unit:
two people adding two statuses to the same field both keep them.

There is **no singleton**.
Nothing has to be found by "the one entity of kind X";
the schema is the set of type and field entities,
read by listing the namespace.
The one failure that remains is two clones defining the same key
before either pushes (E7).

One storage form, the document, serves all three shapes;
what differs between them is meaning, consumer and namespace, not storage.
A `type` uses three attributes and a `flow` mostly one;
the document does not care, any more than a map cares how many keys it holds.
The shape is fixed in the create operation
and later phases add shapes, not storage forms.

### E2 — Four operations, generic across shapes

The discriminator is called **shape**, not kind (2026-09-23):
`kind` is a field's data type and nothing else uses the word.

```
Create      {shape, key}       shape and key are immutable; the id derives from this op
Set         {name, value JSON} one attribute, last in dag order wins
Remove      {name}
SetArchived {bool}             soft removal; refs cannot be deleted across clones
```

plus dag's own `NoOp` and `SetMetadata`, as the issue entity uses them.

An entity's state is its kind, its key,
a map of attributes with last-writer-wins per attribute,
and the archived flag.
Nothing else: a plain document.
Collections a shape carries, a field's enum values,
are attributes with a prefixed name, `values/<id>`,
because a map with per-key merge is exactly what a per-item collection was
(`d56e6f1` folded the earlier item map into this one).
Nothing in the operations is specific to a shape;
Luis's test for "generic" is that a member used by one shape only is not generic,
which is why the earlier `type` member of `Create` is gone
and lives in the key instead.

**A field belongs to exactly one type** (decided 2026-09-23, `e7e58f2`).
Its key is `<type>/<field>`, so `task/status` and `epic/status`
are two entities with the same field key
and, if the team wants, different values;
the issue stores `status` and the engine resolves the entity by the issue's type.
This is Jira's model, field configurations and workflows per issue type,
so the sync instantiates per type instead of inferring what is shared.
Sharing is an authoring concern:
the schema YAML uses anchors, and one definition reconciles into one entity per type.
A `template` shape can come later if anchors are not enough.
Relations are fields of kind `relation` or `multi-relation` (D4),
so `task/parent` carries its own `target_types`
and there is no allowed-parents list anywhere.
Built-ins are code and apply to every type;
a per-type entity with a built-in key overrides its configurable parts (E4).
A flow's attributes are `script`, the Starlark source, and `description`
(`0740bf3`, corrected 2026-09-23).
The script is **exactly one function definition** and nothing else
(Luis, 2026-09-23):
its name is the flow's key,
its docstring is the description, which import mirrors into the attribute
so that listing flows never parses a script,
and its parameters are the flow's arguments, defaults included.
Import reads all three from the syntax tree without executing anything,
and rejects a file with a second statement, a `load`, or no `def`;
helpers are nested functions and the host modules are the only globals.
Starlark has no annotation syntax,
so a parameter's type is inferred from its default
and a parameter without one is a required string;
declaring richer types is the **future step** `args` was.
Which fields a flow reads is not an attribute:
the script passes field keys to the generic host functions itself,
`view.gantt(items, start="start_date", end="due_date")`,
and that is where the field roles of an earlier draft went.
Last-writer-wins on the whole script is right here,
because the only writer is `flow import`
and the text merge already happened in git.

Why generic:
shapes arrive at different times,
schema in phase 2 and flows in phase 4,
and with shape-specific operations each would be a new op type
that old binaries read as `dag.UnknownOperation` and skip.
With generic operations an old binary reads a `flow` entity in full,
lists it and syncs it,
and only its typed projection ignores it.
Two structural `Validate`s instead of a dozen is the other half,
and fact 2 says structural is all they may ever be.

Typed access is in Go:
`schema.Type`, `schema.Field`, `schema.Value`
are projections of a snapshot of the matching shape,
and `cache.ConfigCache` exposes typed helpers
that emit these operations.
Nobody outside the entity package writes an attribute name by hand.

`schema log` renders operations by shape and key,
so "Luis added value `qa` to field `task/status`" is what a reader sees.

### E3 — What each shape carries

```
shape   key             attributes
type    <type>          name, ordinal, description
field   <type>/<field>  kind, name, description, ordinal, freeform,
                        values/<id> -> {name, ordinal, category, description, color}   enum kinds
                        inverse, target_types/<type> -> {}                               relation kinds
flow    <flow>          script (one Starlark def named <flow>), description (its docstring)
```

**Set- and map-valued attributes are one attribute per member.**
`values/<id>`, `target_types/<type>`:
adding a member is `Set`, removing it is `Remove`,
and two people adding two members concurrently both keep theirs,
which one attribute holding a list could not give.
Export folds them back into lists and maps, so the YAML is unchanged.
Two people editing the *same* member concurrently resolve last-writer-wins on that member,
a rename against a recategorisation of `values/qa`, say,
which is the same property the earlier item map had.

Notes on the field attributes:

- `kind` is the field kind from `bb9e89e`'s fixed list.
  It is set at creation and never changes;
  a `Set` that would change it is refused (E6).
- the type is part of the key, not an attribute:
  iterations are a type, so their dates and capacity are the `iteration` type's fields
  (`configurable-schema.md` D5).
- there are **no roles** and no `on_open`/`on_close` (`d56e6f1`).
  What a field is *for* is the consumer's business:
  the Gantt flow's script calls `view.gantt(items, start="start_date", end="due_date")`,
  the planning flow's sums `story_points`,
  a close flow's sets the status value it wants, or takes it as an arg.
  Presets ship their flows with the keys of their own fields.
  A role on the field would be one consumer's choice stored on the schema,
  and a second attribute next to `key` that schema users would confuse with it.
- `freeform` on `multi-enum` allows values outside the value list;
  Jira's labels are freeform, ours too.

A field of a relation kind declares both directions:
`inverse` is the name the derived side reads as
(`parent` reads as `child` from the other side, `blocks` as `blocked-by`),
never a key an issue can write (`configurable-schema.md` D4).
`target_types` restricts what the target may be:
`[iteration]` on `story/iteration`,
`[epic]` on `story/parent`.
Cardinality is the kind: `relation` is one, `multi-relation` is many.
The field entity is what tells the engine that the string the issue holds is an issue id.

A field key must also pass the issue entity's structural check,
`^[a-z][a-z0-9_-]*$`, the stricter of the two slugs;
the entity's check is frozen, and can only ever be loosened.

Config keys are slugs with one optional `/`,
`[a-z0-9][a-z0-9_.-]*(/[a-z0-9][a-z0-9_.-]*)?`, at most 64 characters.
Display names are free text in attributes,
which is what lets a Jira rename change one attribute and no history.

### E4 — Built-in fields are code; entities override their configurable parts

`title` (text), `type` (a type id) and `archived` (bool)
exist on every type,
because tooling cannot function without them
(`configurable-schema.md` D2):
`title` to show anything, `type` to find the rest of the schema,
`archived` to know what the default listing hides.
They are defined in code, in `schema.Builtins`.
Status is **not** built in (`d56e6f1`):
it is a preset field of kind `enum-with-category` on every work type,
and behaviour keyed on categories applies where a type has one.

A field entity whose key is a built-in
**overrides the configurable parts** of that built-in:
its name and description.
It cannot change the kind and cannot be archived (E6).
A repository with no config entities at all
runs on the built-ins alone,
which is the `base` schema of D7:
not a file, the defaults in code.

### E5 — Order is an attribute called `ordinal`

Every enum value, every type and every field carries an integer `ordinal`;
sort by `(ordinal, id)`.
Presets number by tens
so a human inserting one value between two needs one operation, not a renumber.
When there is no gap, `schema import` renumbers the list;
inside one entity that is many `Set`s in one pack and one commit,
so unlike issue rank (`441dcbb`) a renumber here is atomic
and needs no fractional scheme.
Field order across entities is not atomic,
and does not need to be:
a half-applied reorder of fields is still a valid order.

D1 said "ordinal" for values and "rank" for types.
Both are `ordinal`;
`rank` is the issue field kind from `441dcbb`.

### E6 — Structural rules on the operation, semantic rules in `Update`

`Create.Validate` checks the kind is a slug and the key is a config key (E3).
`Set.Validate` checks a slug name, with `/` allowed once for `values/<id>`,
and a value that is valid JSON under 64 KiB and `text.Safe`.
**These rules are frozen** the day the first entity is written;
fact 2 says why.
They do not check that the shape or attribute is one this binary knows,
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
- a field's key names an existing type;
- `category` is one of `backlog unstarted started completed canceled`;
- `ordinal` is an integer;
- `values/<id>` exist only on enum kinds, `inverse` and `target_types/<type>` only on relation kinds;
- a built-in's kind is not changed and a built-in is not archived;
- `inverse` names are unique per type and collide with no field key;
- a flow's `script` is one Starlark function definition whose name is the key;
  a shape with no validator is accepted as written.

Cross-entity references, `target_types` naming a type
or, later, a flow's arg default naming a field,
are checked against the excerpts at write time
and **tolerated on read**:
a dangling reference is reported in `Snapshot.Problems`, never a failure,
the same rule D4 gives dangling relation targets.
Reads never fail on what was written before (D6).

### E7 — Duplicate keys

Two clones creating `task/phase` before either pushes
produce two entities with the same shape and key.
`RepoCacheConfig.Current(shape, key)` picks the one with the lowest creation lamport time,
ties by lowest id,
from the excerpts alone.
The other is ignored deterministically
and every `schema` command prints it on stderr.

Unlike a duplicate singleton, this has a real remedy:
`schema import` of the loser's exported values folds them into the winner,
and `schema archive` **archives** the loser,
which is an operation and so reaches every clone.
Nothing needs a ref deleted;
`schema rm` exists, but it deletes the local ref only, like `issue rm`,
and the entity returns on the next pull (E10).

### E8 — The cache layer

- `cache.RepoCacheConfig` wraps `SubCache[*config.Entity, *ConfigExcerpt, *ConfigCache]`,
  one per namespace, cache files `cache/work-schema` and `cache/work-flows`.
  `maxLoaded` is unbounded for this subcache;
  a few dozen small entities stay in memory.
- `ConfigExcerpt`: id, shape, key, archived, ordinal, create and edit lamport times.
  Enough to list, to pick a duplicate's winner and to order,
  without loading.
- `ConfigCache` embeds `CachedEntityBase` and adds `Update` (E6)
  and the typed helpers that call it.
- Resolvers gain `&ConfigCache{}` and `&ConfigExcerpt{}`.
- `MergeAll` runs identities, then config, then issues.
  Issue merges do not consult the schema (D6),
  so this ordering costs nothing and keeps a door open.
- `Fetch` and `Push` pick up the `refs/work-*` config namespaces from the subcache list unchanged,
  so `git work push` and `pull` carry configuration with no new code.
- The ref watcher already refreshes every subcache (`cache/watcher.go:114`),
  so a status someone else adds appears in an open GUI as a new column.
- `RepoCache.Schema()` compiles the built-ins plus every unarchived type and field entity
  into one `*schema.Schema`,
  memoised by the set of config ref hashes,
  rebuilt on update and refresh.
  Issue-write validation (`5b09ee1`, `bb9e89e`) calls it from `cache`,
  which keeps `entities/issue` free of any import of `schema` or `config`.

### E9 — Export, import and `reconcile`

The document a human edits is a view over the entities:

```yaml
preset: jira
shared:
  status: &status
    kind: enum-with-category
    name: Status
    values:
      - {id: to-do,       name: To Do,       category: unstarted}
      - {id: in-progress, name: In Progress, category: started}
      - {id: in-review,   name: In Review,   category: started}
      - {id: done,        name: Done,        category: completed}
  priority: &priority
    kind: ordinal-enum
    values:
      - {id: highest, name: Highest}
      - {id: high,    name: High}
      - {id: medium,  name: Medium}
types:
  epic:
    name: Epic
    fields:
      status:   *status
      priority: *priority
      parent:   {kind: relation, inverse: child, target_types: [initiative]}
  story:
    name: Story
    fields:
      status:    *status
      priority:  *priority
      parent:    {kind: relation, inverse: child, target_types: [epic]}
      iteration: {kind: relation, inverse: issues, target_types: [iteration]}
      blocks:    {kind: multi-relation, inverse: blocked-by}
  iteration:
    name: Sprint
    fields:
      start:    {kind: date}
      end:      {kind: date}
      capacity: {kind: number}
```

`shared` is authoring only, YAML anchors for the human writing the file;
the store holds one `status` entity per type.

Flows are authored the same way, one function per file:

```python
def board(iteration="current"):
    """Kanban of one iteration, a column per status."""
    items = issue.list('map(select(.fields.iteration == "%s"))' % iteration)
    return view.board(items, columns="status", card_title="title")
```

The function is the whole declaration:
name, description and arguments come from it,
so the file's name and location do not matter
and `git work flow import` takes any files, directories or stdin,
a scratch file in `/tmp` included (Luis, 2026-09-23).
A directory of `.star` files under the repository is a convention for review,
not something the tool knows about.
Import is an **upsert** of the flows it is given:
create, `Set script`, `Set description`.
Archiving what a directory no longer has is `--prune`, explicit,
the same flag `schema import` uses (E10).
Starlark (`go.starlark.net`) was measured before being chosen (`0740bf3`):
a whole board flow over a thousand issues runs in 3.3 ms,
conversion of the issues into Starlark values included,
and a step cap bounds every run.
The host API a script sees **mirrors the command line one to one**
(Luis, 2026-09-23; the full map is `cli-convention.md`):
every `git work <module> <verb>` is a Starlark function
of the same name under a module of the same name,
taking the same arguments and returning the JSON the command prints,
so `git work issue set ID status done` is `issue.set(id, "status", "done")`
and `git work issue PROGRAM` is `issue.list(program)`.
Starlark has no positional-only parameters,
so a command's arguments are one JSON object of keyword arguments,
`git work flow run board '{"iteration":"current"}'`
is `flow.run("board", iteration="current")`,
and the fixed positionals of `set ID KEY VALUE` are that object spelled out.
Nothing is reachable from a script that is not reachable from the shell,
and the reverse,
so a flow is exactly a shell script that runs in-process.
Rendering is a module like any other, `view`,
because `view.gantt(...)` is an atomic capability git-work provides,
not a surface:
a view function builds a **spec** from items and field bindings,
and a renderer consumes specs.
The terminal renderer is behind `git work view` (`84dfbde`),
the HTTP renderer behind `git work gui` (`8b06191`),
and a renderer that lacks a view type fails at render time naming itself,
so there is never a command that exists for one surface and not the other.
`git work view board '{"columns":"status"}' < items.json`
is `view.board(items, columns="status")`;
in a TTY it opens the interactive view, otherwise it prints the spec,
and `--gui` sends it to the browser.
A view that edits, a kanban drag, calls `issue.set` and re-renders;
there is no second path.
`me()` is the only name with no shell equivalent.

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
   match entries to unarchived entities by shape and key;
2. create an entity for each entry with no match,
   and for each match emit `Set`/`Remove`
   for every attribute whose canonical JSON differs;
   an unmatched entity is archived only under `--prune`,
   so an import is an upsert by default
   and a partial file from anywhere can never archive anyone's work
   (Luis, 2026-09-23);
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

The whole command line, and the rules it follows, is `cli-convention.md`;
the config part of it:

```
git work schema [--format yaml|json]              live schema
git work schema init [PRESET]                     refuses if any field entity exists
git work schema import FILE|- [--prune] [--dry-run]
git work schema export [--format yaml|json]
git work schema log [KEY]                         config operations, rendered by shape and key
git work schema archive KEY                       the replicated removal (E7)
git work schema rm KEY                            local ref only; returns on pull

git work flow                                     names, descriptions, arguments
git work flow run NAME [KWARGS] [--gui] [--format json|text]
git work flow show NAME                           the script
git work flow import FILE|DIR|-... [--prune] [--dry-run]
git work flow export NAME > FILE
git work flow export --all DIR
git work flow log [NAME]
git work flow archive NAME
git work flow rm NAME                             local ref only
```

`rm` and `archive` are different verbs on every tree
because they are different things (Luis, 2026-09-23):
`archive` is an operation and reaches every clone,
`rm` deletes the local ref and the entity returns on the next pull.
The `flow` commands (`b511c63`, `52a2797`) reach the entities through the same `Update`
and are not part of this task;
`schema init` applies a preset's embedded default flows through `flow import`.

Library: `github.com/goccy/go-yaml`, already an indirect dependency,
because its errors carry line and column,
and an import error an agent can act on needs both.

## Package layout

```
entities/config/   Entity, Snapshot, the four operations, unmarshaler, actions, resolver
schema/            Builtins; Type, Field, Kind, Value, Category;
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

- `configurable-schema.md` D1: many entities under `refs/work-schema/*` and `refs/work-flows/*`, not one singleton;
  four generic operations, not the enumerated list;
  types carry `ordinal`, not `rank`.
- `b511c63`, `f37603c`, `52a2797`:
  a flow is an entity of that shape holding one Starlark function,
  applied from any `.star` file by import, run by `git work flow run <name>`,
  and a saved view is a flow whose function returns a `view.*` spec (`0740bf3`).
- `bb9e89e`: three built-ins in code with configurable overrides;
  kinds, categories, `freeform`, `target_types`; no roles, no `on_open`/`on_close`.
- `87a48c1`: iteration fields are the `iteration` type's field entities, and membership a target-typed relation field.
- `c090f9b`: relations are fields of kind `relation`/`multi-relation`, not a config kind.
- `5b09ee1`: the entity guarantees `title`, `type`, `archived`; status is a preset field.

## Done when

- `git work schema init jira` creates one `refs/work-schema/*` entity per type and per (type, field),
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
  Any shape can be created without a design.
  The rule is that a shape exists only with an owning task and a validator;
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
