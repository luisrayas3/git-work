# Story: Configurable schema for Jira and Linear (`6555e36`)

**Outcome:** hierarchy, statuses with categories, priority, estimate, dates,
assignee, typed relations and iterations are all config,
with `jira` and `linear` presets proving both native models fit.
Tooling keys off kinds and categories; flows bind the fields they need by key.

**Tasks:** `7c90fbd` where the schema lives ·
`3556569` the config entities (designed in `config-entity.md`) ·
`bb9e89e` schema engine ·
`5b09ee1` the owned issue entity ·
`c090f9b` typed relations ·
`bf6f392` the one-time store migration ·
`87a48c1` iterations ·
`59fed1c` presets.

**Status:** design, awaiting approval.
Revised 2026-09-22:
the issue model is ours and the store is migrated once,
rather than kept compatible with git-bug's operations
(see D2, D3 and "What changed").

**Scope:** one team, one repository, one Jira project,
matching the Jira bridge's existing "one bridge = one project" assumption.
Sprint planning (`cd41e40`) draws its pool from this store alone;
no project dimension exists in fields, queries or UIs,
and none is planned.

## What the code says

Four findings.

**1. `formatVersion` is a hard gate, which cuts both ways.**
Each operation pack records its format version as a tree entry,
and the reader rejects any mismatch outright
(`entity/dag/operation_pack.go:233`,
`if version != def.FormatVersion { return NewErrInvalidFormat(...) }`).
There is no range and no upgrade path.
An unrecognised operation *type*, by contrast, is harmless:
it falls through `operationUnmarshaler`'s `default` to `dag.UnknownOperation`,
which applies as a no-op, skips validation
and re-marshals its original bytes verbatim
(`entity/dag/op_unknown.go`).

So there are two coherent strategies and no third.
*Compatible evolution* adds operation types, never bumps the version,
and keeps every old operation readable forever.
*Migrate once* rewrites the store into a new format in one pass,
bumps the version,
and lets the gate refuse old binaries instead of letting them misread.
The first draft of this design chose the first;
D2 chooses the second.

**2. `common.Status` reaches 23 files in 10 packages.**
`entities/bug` (6 files), `api/graphql` (5), `query` (3), `cache` (2),
all three bridges (5), `termui` and `commands/bug`.
Every one of those is being rewritten or removed by another story
(`e8d6426`, `84dfbde`, `938434e`, `8ade811`),
so there is no caller to protect by keeping the type,
only work to avoid doing twice.

**3. The read surfaces are `Snapshot`, `BugExcerpt` and `query`.**
`bug.Snapshot` has `Status`, `Title`, `Labels`, `Comments`, actors, timeline.
`BugExcerpt` mirrors the queryable subset,
and `query.Parse` maps `status:open` through `common.StatusFromString`.
Anything that becomes a schema field has to appear in all three,
or it is invisible to filtering and to every UI.

**4. Our own tracker encodes its schema as labels.**
The `type:`/`story:`/`phase:`/`area:`/`prio:` taxonomy in AGENTS.md
exists precisely because git-bug is flat.
Once fields exist, those labels are a duplicate model that will drift.
`bf6f392` undoes it, in the same pass as the format migration.

## Decisions

### D1 — Configuration is CRDT entities under `refs/work-*` namespaces (`7c90fbd`, `483dbe2`)

Decided: entities, not the worktree file and not a plain blob.
Two things settle it.

**The machinery is not paid for by the schema alone.**
The flow catalogue (`b511c63`), saved views included (`f37603c`),
want exactly what the schema wants:
to travel with the tracker rather than with a branch,
to be editable by several people,
and to merge when two of them edit different things.
A blob gives last-writer-wins over the whole document,
so two people adding two different flows means one silently loses.
Entities and per-item operations mean both survive.

**And "Jira sync makes the log noisy" was wrong.**
A sync that writes only when it finds a real difference
emits one operation per actual Jira configuration change,
with the author attached.
That is the audit trail worth having, not noise.
The residue is real:
Jira's config endpoints can return unstable ordering,
so `69b7be0` normalises before comparing.

**Shape.**
One entity per issue type, field and flow,
each with a content-derived id under a namespace per shape, `refs/work-schema/<id>` for types and fields,
`refs/work-flows/<id>` for flows (`483dbe2`, `d56e6f1`)
and its kind fixed in its create operation.
The entity boundary is the coarse merge unit;
inside a field, enum values are per-item operations,
so two people adding two statuses both keep them.
There is no singleton.
Order is an integer `ordinal` attribute, never a list position,
so two concurrent inserts do not fight over index 3.
The whole of it is `config-entity.md`.

**Review still happens on a file.**
`git work schema export > schema.yaml`, edit, `git work schema import schema.yaml`,
where import diffs the desired document against the current entities
and emits the minimal operations,
computed inside the write lock against a re-read.
That is the same `reconcile(desired, current)` the Jira sync calls.

### D2 — The issue entity is ours; the store is migrated once

`entities/bug` was never in the pristine seven,
but the first draft treated its on-disk format as if it were:
every old operation kept readable, `SetStatusOp` reinterpreted,
`Snapshot.Status` kept as a projection.
That is the compatible-evolution strategy of finding 1,
and it carries git-bug's model,
with title, status and labels as special struct members,
for as long as the store exists.

Decided instead:
**own the model and migrate the store once.**

- The package becomes `entities/issue`, package `issue`,
  and stops pretending upstream fixes cherry-pick into it.
  `entity/dag` and the rest of the seven stay pristine.
- The snapshot has a **structural core** and a **fields map**.
  Structural, because it is what the operation log is made of:
  id, author, timestamps, comments, timeline, actors and participants,
  and the fields map itself.
  Everything an issue *has* is a field, relations to other issues included.
- Three fields are **built in**, present on every type and unremovable,
  because tooling cannot function without them:
  `title` (text), `type` (a type id) and `archived` (bool).
  Status is **not** built in (`d56e6f1`, 2026-09-23):
  it is a preset field of kind `enum` on every work type,
  and everything keyed on categories, hiding done work, `status:open`, the weekly report,
  Jira's resolution, applies where a type has one and degrades to "never done" where it does not.
  Archived is the guaranteed default-listing filter (`4d61ebe`):
  status says how work ended, archived whether it is still worth looking at.
- Everything else is preset config, per type (`e7e58f2`):
  status, assignee, priority, estimate, dates, iteration membership.
  There are **no field roles** (`d56e6f1`):
  a flow's script names the fields it needs when it calls the host API,
  so the Gantt flow tells the timeline command which date is start and which is end,
  and presets ship their flows with the keys of their own fields.
  Labels stop being special and become a freeform `multi-enum` in the presets,
  which is what they are in Jira.
- Operations: `Create`, `SetField{key, value}`, `AddValue{key, item}`, `RemoveValue{key, item}`,
  `AddComment`, `EditComment`, plus dag's `NoOp` and `SetMetadata`.
  `SetField` replaces a field whole, last writer wins in the dag's order.
  `AddValue` and `RemoveValue` act on one item of a list-valued field with set semantics,
  because two people adding two labels, two reviewers or two blocking issues concurrently
  must both win, which a list-valued `SetField` can not give.
  git-bug's label change had the same shape for the same reason.
  A concurrent `SetField` and `AddValue` on one field resolve in lamport order,
  so a replace that lands last means replace;
  that is `replace` versus `add` on the array in JSON Patch terms.
  `SetTitleOp`, `SetStatusOp` and `LabelChangeOp` do not exist in the new format.
- `formatVersion` goes to 5, and the gate in finding 1 refuses old binaries.

**Sequencing (2026-09-22).**
`entities/issue` is built as a **peer** of `entities/bug`, not a rewrite in place:
`git work bug` keeps serving the tracker's store while `git work issue` grows,
so the tracker never stops working during the change,
and `entities/bug` is deleted only after this repository has migrated.
The two formats can not share a namespace,
because the version gate rejects the other's packs,
so the new entity lives in `refs/work-issues/*`, its final home (`483dbe2`),
and `entities/bug` took back the typename `bug`
(in-process only; two subcaches can not share one).
Entity ids do not depend on the namespace.
The entity validates shape only
(keys `^[a-z][a-z0-9_-]*$`, values valid JSON, a title that is a non-empty line and can not be cleared),
so it does not wait for the schema:
kind and value checks plug into the cache's write path when `bb9e89e` lands.

**Why the migration is cheap enough to choose.**
One team, no external clones, and the namespace move already did this once
(`misc/migrate`, `c4a4afe`).
Entity ids survive if the create operation's bytes are unchanged,
and comment ids survive because an operation's id hashes the operation, not the pack.
The script replays each issue,
emitting `SetField status` for each `SetStatusOp`,
`SetField labels` for each `LabelChangeOp`
and `SetField title` for each `SetTitleOp`,
with the original author, time and nonce,
and everyone re-pulls.
That is `bf6f392`, merged with the label migration it was already doing.
Two more things the replay has to get right:
lamport times, which `dag.Entity.Commit` takes from the repository clocks,
so the packs are replayed in global lamport order
with the target clocks witnessed to one below each original time;
and the namespaces: `refs/issues/*` is deleted locally and on origin once replayed,
and identities move to `refs/work-identities/*` by editing three ref-name constants
in `entities/identity`, the one listed exception to the pristine seven (`483dbe2`),
in the same change that deletes `entities/bug`.

**Values are stored by stable id, never by display name.**
A status is `{id: "in-review", name: "In Review", category: started}` in the schema,
and an operation stores `in-review`.
Renaming it in Jira then changes one attribute instead of history,
which, with content-addressed operations, cannot be rewritten.

Field kinds, fixed as `bb9e89e` specifies, plus the additions this design settled:

| Kind | Used by | Value in the op |
| --- | --- | --- |
| `text` | title, free text | string |
| `enum` | status, and any closed list of values | value id |
| `ordinal-enum` | priority | value id |
| `bool` | archived | bool |
| `number` | estimate, story points, capacity | float64 |
| `date` | start, target, due | RFC 3339 |
| `identity` | assignee | `entity.Id` of an identity |
| `multi-enum` | labels, components, fix versions | list of value ids |
| `multi-identity` | reviewers, watchers | list of identity ids |
| `rank` | board and backlog order | LexoRank-style string (`441dcbb`) |
| `relation` | parent, iteration, and any cardinality-one link | `entity.Id` of an issue (D4, D5) |
| `multi-relation` | blocks, relates-to | list of issue ids (D4) |

`type` is validated against the type entities rather than a field's values,
the one special case in the engine.
The `multi-*` kinds are the ones `AddValue` and `RemoveValue` apply to.

The kind named `enum-with-category` when this was written is just `enum`
(implemented 2026-09-23):
a category is a property of a *value*, which every enum value may carry,
so a second kind for "an enum whose values have categories"
would have been a kind for a value's optional attribute.
`ordinal-enum` stays a kind of its own because it says the values are ranked,
which is a property of the field.

Manual **rank** (`441dcbb`): a drag computes a key strictly between its neighbours,
so it touches one issue.
Conflict-free because the sort is **(rank, entity id)**, never rank alone:
two people dragging into the same slot derive the same key,
the tie breaks by id, both drags survive.

Categories are fixed and closed:
`backlog`, `unstarted`, `started`, `completed`, `canceled`.
Every tool keys off these.

### D3 — No compatibility shim; the migration is the compatibility

The first draft reinterpreted `SetStatusOp` at compile time
and kept `Snapshot.Status` as a derived open/closed projection,
so GraphQL, the webui, the bridges and `status:open` kept working.
With D2 none of that exists to keep working:
the operations are rewritten by the migration,
`common.Status` is deleted with its 23 files
as each surface is rewritten by its own story,
and `status:open` becomes a shorthand of the query language,
which is jq via gojq over the excerpt JSON (`483dbe2`, `3c9c24d`),
for "category is not `completed` or `canceled`".

The `open` and `close` verbs, if they survive, are flows
with the status value they write in the script or as an arg;
there is no `on_open`/`on_close` on the schema (`d56e6f1`),
and `git work issue set <id> status done` always works.

### D4 — Relations are fields; one side is stored and the inverse is derived (`c090f9b`)

A relation is a field whose kind says the value is an issue id:
`parent` is a `relation` field set with `SetField`,
`blocks` a `multi-relation` field edited with `AddValue` and `RemoveValue`.
The first draft gave relations their own operations and a structural slice on the snapshot,
on the argument that the target could then be validated as an id
and inverses derived without the schema.
Revised 2026-09-22:
the merge property that argument protected, concurrent adds both surviving,
is the property every multi-valued field needs,
so it belongs to the operation set (`AddValue`) and not to relations;
and target validation and inverse derivation are schema lookups by kind,
like every other field's.
Two operations and a snapshot member disappear, and nothing is lost.

One side is stored, on the issue that "owns" the statement,
per AGENTS.md:
no multi-entity commit, so nothing is written to the other side.

The inverse lives in the cache.
The excerpt carries its fields,
all excerpts are in memory,
so "children of X" is a scan of the excerpt map over the fields of relation kind,
built into a reverse index at load and maintained incrementally on update.
This is why the field entities of relation kind have to exist
before the cache can index them:
without the schema, a field holding an id is just a string.

- **Cardinality** is enforced at write time only:
  setting a second parent replaces the first
  (it is `replace`, not `add`, in JSON-Patch terms).
- **Dangling targets render.**
  An id pointing at an issue that was removed, or not yet pulled,
  shows as the short id, not an error and not a crash.
- **Hierarchy** is `target_types` on each type's `parent` field:
  `story/parent` may target `[epic]`, `epic/parent` may target `[initiative]`.
  That is the whole of it;
  Linear's Initiative > Project > Issue > sub-issue
  and Jira's Initiative > Epic > Story/Task > Sub-task are both config.

### D5 — Iterations are an issue type, not a namespace (`aba17f4`, `87a48c1`, revised 2026-09-23)

Sprint planning needs dates, membership, capacity and carry-over,
which an opaque text field cannot carry;
`aba17f4` decided on an entity,
and a first revision of this section made it the issue entity in a second namespace.
Revised again:
**an iteration is an issue of type `iteration` in `refs/work-issues`.**
The namespace had survived the collapse of the separate type
without a reason of its own.
What it bought, issue lists that need no filter and a cheap "is this an iteration" check,
is what per-type applicability gives every type anyway,
and Jira's field configurations and workflows are per issue type,
so that mechanism is needed regardless.

- **Fields belong to a type** (`e7e58f2`, 2026-09-23).
  Every field entity is `(type, key)`;
  start and end dates,
  and `capacity` as a `number` field, are the `iteration` type's fields,
  and a team that plans in hours rather than points changes config, not code.
  Status values differ per type the same way, as Jira's per-type workflows do,
  with no special case (`bb9e89e`).
- **Membership is a relation.**
  An issue's iteration is a `relation` field of cardinality one
  whose field entity carries `target_types: [iteration]`;
  the same attribute on `parent` replaces the older allowed-parents list.
  The `iteration` value kind and the field attribute `on` disappear.
- **Containers at other scales**, quarters or program increments for the roadmap,
  are further types, not further namespaces.
- **Flows that list work exclude the type by default**, one line in the preset.
- An iteration still has a title, fields, comments (the retro lives somewhere),
  a timeline and participants, because it is an issue.
  Jira sprints and Linear cycles both map onto it.

The consequence to state plainly: **closing an iteration is not atomic.**
Marking it closed and moving N unfinished issues is N+1 commits.
The order that makes a crash recoverable is:
create the next iteration, move the issues, then close the old one,
so the intermediate state is "some issues already in the next sprint",
which reads correctly and is idempotent to retry.

### D6 — Validate on write, stay lenient on read

A schema is a statement about what may be written now,
never about what was written before.
Operations are immutable and content-addressed,
so a schema that rejected existing data would make history unreadable.

Writes are validated and rejected with actionable errors, per `bb9e89e`:
`status "done" is not in the schema; valid values: todo, in-progress, in-review, shipped`,
because agents are the main caller and an agent can act on that.
Reads keep values the schema no longer knows
and surface them as unknown rather than dropping them.
A field archived in the schema stops being settable;
it does not vanish from the issues that have it.

### D7 — Presets are embedded, and we dogfood `jira`

`jira.yaml` and `linear.yaml` are embedded with `go:embed`
and instantiated as config entities by `git work schema init <preset>`.
A repository with no config entities runs on the built-ins alone
(`config-entity.md` E4);
that is the base schema, and it is code, not a file.
The round-trip table test in `59fed1c` is the acceptance test for the whole engine.

**This repo dogfoods `jira`**, reversing `59fed1c`'s original note.
With the sandbox skipped (`de1d8fb`) the `jira` preset is written from documentation
and verified by nothing;
running it here every day against real issues is the only check it gets
before it meets the company instance.
`linear` earns its place in the round-trip test.

One mapping needs a call during `bf6f392`:
`type:decision` has no native Jira issue type,
so either the preset carries a custom one or decisions become Task plus a marker.
`area:` maps onto Components, which exercises `multi-enum`.

## Order of work

1. `5b09ee1` — the owned issue entity as a peer of `entities/bug`:
   package `issue`, structural core plus fields, `SetField`, `AddValue`/`RemoveValue`
   (relations are fields, so `c090f9b` is mostly schema work),
   its cache and its `git work issue` tree, `formatVersion` 5, in `refs/work-issues/*`.
   First because it needs no schema to validate shape,
   and because the tracker keeps running on `git work bug` meanwhile.
   Started 2026-09-22.
2. `3556569` — the config entities, `schema init`/`export`/`import` on `reconcile`.
   Everything below reads this.
3. `bb9e89e` — kinds, the three built-ins, categories, `target_types`, validation, actionable errors.
   Pure library, wired into the issue cache's write path.
4. `bf6f392` — the one-time migration:
   old operations to new, labels to fields, ids and lamport times preserved;
   `refs/issues/*` deleted locally and on origin, identities moved to `refs/work-identities/*`;
   `entities/bug`, `commands/bug` and their cache files deleted;
   `schema init jira` in the same pass; AGENTS.md rewritten.
5. `87a48c1` — iterations as a type: per-type field applicability, target-typed relations,
   the date and planning fields in the presets.
6. `59fed1c` — both presets and the round-trip table test, last,
   because it is the acceptance test for all of the above.

## Done when

- `git work schema export` prints the live schema;
  editing and importing it emits the minimal operations,
  and `schema log` shows who changed what;
- both presets load, and the `59fed1c` table test round-trips a representative issue set through each;
- an issue carries type, status with category, priority, estimate, dates, assignee,
  a parent, a blocks relation and an iteration, all schema-driven;
- `git work issue set` with a status not in the schema fails naming the valid values;
- this repo's issues are on fields, not labels, with their ids unchanged,
  and AGENTS.md says so;
- an old binary refuses the migrated store instead of misreading it.

## What changed on 2026-09-22

- D2 and D3 flipped from compatible evolution to migrate once,
  because `entities/bug` was already ours and nothing that reads the old shape survives the other stories.
- D1 went from one config entity to one per configurable thing (`config-entity.md`, "Why not one entity").
- D5 made iterations the same entity as issues and capacity a field.
- 2026-09-23: D5 revised again, iterations are an issue *type* in `refs/work-issues`;
  membership is a target-typed relation,
  and `refs/work-iterations`, the `iteration` kind and the `on` attribute are gone.
- 2026-09-23 (`e7e58f2`): every field and relation entity belongs to exactly one type,
  Jira's model; sharing is YAML anchors at authoring time, and the store is fully instantiated.
  The entity keeps the name `issue`: it is Jira's, Linear's and GitHub's word,
  Jira's own types include Initiative and Epic, and `item` collides with list items.
- 2026-09-23 (`d56e6f1`): built-ins are `title`, `type`, `archived`; status is a preset field.
  No field roles and no `on_open`/`on_close`: flows bind fields by key in their own config.
  The config entity is a plain document with four operations and no item map;
  shapes are `type`, `field` and `flow`, relation folded into field, view folded into flow,
  and `refs/work-views` is gone.
- 2026-09-23 (`0740bf3`): refs are the runtime source of truth for schema and flows;
  `schema.yaml` and `.star` files in the tree reach them only through `import`.
  A flow is one Starlark function over a host API that mirrors the CLI one to one,
  measured at 3.3 ms per thousand issues for a board.
- The first-class set was named: structural core plus four built-in fields;
  labels demoted to a preset field.
- Sequencing: `entities/issue` is built as a peer of `entities/bug`,
  this repository migrates, then `bug` is deleted;
  the peer lives in `refs/work-issues/*` from the start, and the order of work moved it first.
- Namespaces carry the `work-` prefix, one per kind, identities included at migration;
  the query language is jq via gojq over the excerpt JSON;
  external ids such as Jira keys are immutable aliases in create-op metadata, never entity ids;
  views are selection plus presentation, flows selection plus actions run on invocation;
  automation is out of scope, so there are no rules and no triggers (`483dbe2`, `47b8430` closed).
- Relations lost their own operations and became fields of kind `relation` and `multi-relation`;
  per-item `AddValue`/`RemoveValue` operations arrived for every multi-valued field (D2, D4).

## Risks

- **Kinds are the expensive thing to get wrong.**
  A missing *value* is config; a missing *kind* is a schema-language change.
  That is why `bool`, `multi-enum`, `multi-identity`, `rank`, `relation` and `multi-relation` are settled here.
- **The engine outgrowing "just configurable enough."**
  The fixed kind list and the closed category set are the guard.
- **The migration is a flag day.**
  Every clone re-pulls `refs/work-issues/*`,
  and an old binary is locked out by design.
  One team makes this an afternoon; do it once, and do it before Jira import (`33148f2`) multiplies the data.
- **The unverified `jira` preset.**
  Written from documentation; expect it to be wrong in small ways until someone points it at a real instance.
- **Relation indexes and the cache.**
  The reverse index is memory-only and must be rebuilt by the same `refreshFromRefs` path the watcher drives.
