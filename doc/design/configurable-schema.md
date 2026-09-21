# Story: Configurable schema for Jira and Linear (`6555e36`)

**Outcome:** hierarchy, statuses with categories, priority, estimate, dates,
assignee, typed relations and iterations are all config, with `jira` and
`linear` presets proving both native models fit. Tooling keys off
kinds/categories, never names.

**Tasks:** `7c90fbd` where the schema lives · `3556569` the config entity
(designed in `config-entity.md`) · `bb9e89e` schema engine ·
`5b09ee1` SetField op · `c090f9b` typed relations · `59fed1c` presets ·
`aba17f4` iterations.

**Status:** design, awaiting approval.

**Scope:** one team, one repository, one Jira project, matching the Jira
bridge's existing "one bridge = one project" assumption. Sprint planning
(`cd41e40`) draws its pool from this store alone; no project dimension
exists in fields, queries or UIs, and none is planned.

## What the code says

Four findings. The first contradicts a task's stated plan, so it comes first.

**1. Do not bump `formatVersion`. It would orphan the store, and it buys
nothing.**

`5b09ee1` says "new dag op type, formatVersion bump". The bump is the part to
drop. Each operation pack records its format version as a tree entry, and the
reader rejects any mismatch outright — `operation_pack.go:233`,
`if version != def.FormatVersion { return NewErrInvalidFormat(...) }`. There is
no range, no upgrade path: bumping 4→5 makes every pack already written
unreadable, which is all 74 of our issues. Teaching the reader to accept both
means editing `entity/dag`, which is pristine.

And it is unnecessary, because adding an operation type is already compatible
in both directions. An unrecognized type falls through
`operationUnmarshaler`'s `default` to `dag.UnknownOperation`, which applies as
a no-op, skips validation, and re-marshals its original bytes verbatim
(`entity/dag/op_unknown.go`). So a new binary reads old packs natively, and an
old binary reading a `SetField` op ignores it and preserves it rather than
corrupting it. Format versions are for changes that break that property. This
one does not.

**2. Removing `common.Status` is a 23-file, 10-package change with a UI tail.**

`common.Status` reaches `entities/bug` (6 files), `api/graphql` (5),
`query` (3), `cache` (2), all three bridges (5), `termui`, and `commands/bug`.
It is a GraphQL enum with hand-written `MarshalGQL`, and 27 webui TypeScript
files mention status. `5b09ee1` says open/closed "goes away"; doing that in one
change means touching every one of those at once, including a frontend nobody
has looked at yet.

**3. The read surfaces are `Snapshot`, `BugExcerpt` and `query`.**
`bug.Snapshot` has `Status`, `Title`, `Labels`, `Comments`, actors, timeline.
`BugExcerpt` mirrors the queryable subset, and `query.Parse` maps `status:open`
through `common.StatusFromString`. Anything that becomes a schema field has to
appear in all three, or it is invisible to filtering and to every UI.

**4. Our own tracker encodes its schema as labels, and nothing owns undoing
that.** The `type:`/`story:`/`phase:`/`area:`/`prio:` taxonomy in AGENTS.md
exists precisely because git-bug is flat. Once fields exist, those labels are
a duplicate model that will drift. No task covers the migration — see Gaps.

## Decisions

### D1 — The schema is a CRDT entity in `refs/work/*` (`7c90fbd`)

Decided: the config entity, not the worktree file and not the plain blob I
first argued for. Two things settle it.

**The machinery is not paid for by the schema alone.** The flow catalogue
(`b511c63`) goes in the same place — flows are essentially named aliases, and
they want exactly what the schema wants: to travel with the tracker rather than
with a branch, to be editable by several people, and to merge when two of them
edit different entries. A blob gives last-writer-wins over the whole document,
so two people adding two different flows means one of them silently loses an
edit. Per-key operations mean both survive. Amortized across schema *and*
flows, the op types stop looking like overhead and start looking like the
point.

**And my "Jira sync makes the log noisy" argument was wrong.** A sync that
writes only when it finds a real difference emits one operation per actual Jira
configuration change — someone added a status, someone renamed a priority.
That is not noise, that is precisely the audit trail worth having, with the
author attached. The noisy version only exists if the sync writes
unconditionally, which is a bug, not a property of the design. There is one
real residue: Jira's config endpoints can return unstable ordering, and
unmapped fields would produce diffs against nothing. So `69b7be0` normalizes
before comparing — sort by id, ignore what we do not map — which is an
implementation rule, not an architectural objection.

**Ref shape.** Entity ids are content-derived from the create operation, so
there is no such thing as an entity with the fixed id `schema`. The namespace
`work` holds **one** config entity at `refs/work/<id>`, found by listing the
namespace; schema, flows, views and rules are sections of it, not entities of
their own. (An earlier draft tagged one entity per kind; `config-entity.md`
E1 says why one is better: views reference fields and rules reference
statuses, so they want one consistent snapshot.)

Two clones that both initialize produce two, which cannot be prevented, only
handled: the winner is the oldest by creation lamport time, ties broken by
lowest id, and the loser is *reported* rather than silently ignored. Silent
selection is how a team ends up with two schemas and no idea why half their
issues fail validation.

**Operations are per-key**, so concurrent edits to different things both
survive, and only genuine conflicts on the same key need resolving — which
dag's existing ordering (lamport time, then op id) already does
deterministically across clones. The key is a path and the wire carries two
operations, `SetEntry{path, value}` and `RemoveEntry{path}`; the typed
vocabulary this draft first listed (`DefineField`, `AddEnumValue`,
`DefineFlow`, …) is the Go API on top, emitting those two on well-known paths
(`config-entity.md` E2, E3):

```
fields/<key>               fields/<key>/values/<id>
types/<id>                 relations/<key>
flows/<name>               views/<name>               rules/<name>
```

**Order inside an enum is an attribute, not a position.** Enum values and
issue types both carry an integer `ordinal`, with ties broken by id
(`config-entity.md` E4; "rank" is the issue field kind from `441dcbb`, so
types do not also use the word). List positions would have two concurrent
inserts fighting over index 3; attributes make that a non-event.

**Review still happens on a file.** `git work schema export > schema.yaml`,
edit it, `git work schema import schema.yaml` — and import does not overwrite
anything. It diffs the desired document against the current schema and emits
the minimal set of operations. That is the same `reconcile(desired, current)`
function the Jira sync calls, which is the second time this design gets to use
one mechanism twice. Import computes that diff *inside* the write lock against
a freshly re-read entity, so it changes exactly what still differs
(`config-entity.md` E6).

**No entity is not an error.** Every repository today has none; that state
reads as the embedded `base` preset (`status` open/closed, freeform `labels`),
compiled in memory and never written, so nothing changes until someone runs
`git work schema init <preset>` (`config-entity.md` E7).

### D2 — Add op types, keep every existing one

`SetFieldOp` and `AddRelationOp`/`RemoveRelationOp` join the existing codes in
`entities/bug/operation.go`. `CreateOp`, `SetTitleOp`, `AddCommentOp`,
`EditCommentOp`, `LabelChangeOp`, `SetStatusOp`, `SetMetadataOp` all stay,
readable forever.

**Values are stored by stable id, never by display name.** A status is
`{id: "in-review", name: "In Review", category: started}` in the schema, and an
op stores `in-review`. Renaming it in Jira then changes one line of config
instead of requiring history to be rewritten — which, with content-addressed
operations, it cannot be.

Field kinds, fixed as `bb9e89e` specifies, plus the two this design adds:

| Kind | Used by | Value in the op |
| --- | --- | --- |
| `enum-with-category` | status | value id |
| `ordinal-enum` | priority | value id |
| `text` | free text fields | string |
| `number` | estimate, story points | float64 |
| `date` | start, target, due | RFC 3339 |
| `identity` | assignee | `entity.Id` of an identity |
| `multi-enum` | components, fix versions, labels | list of value ids |
| `multi-identity` | reviewers, watchers | list of identity ids |
| `relation` | parent, blocks, … | see D4 |
| `iteration` | sprint, cycle | `entity.Id` of an iteration (D5) |

`multi-enum` and `multi-identity` are additions to `bb9e89e`'s list, which is
single-valued throughout. Without them Jira's components, fix versions and
every multi-select custom field land in labels, which flattens away which field
a value came from. `labels` then becomes the built-in instance of `multi-enum`
rather than a second multi-select mechanism beside it.

Manual **rank** was a third gap, decided in `441dcbb`: a LexoRank-style
fractional index. A drag computes a key strictly between its neighbours, so it
touches one issue and no renumbering happens. The property that makes it
conflict-free is the sort: **(rank, entity id), never rank alone** — two people
dragging into the same slot independently derive the same midpoint key, and the
tie breaks deterministically by id, so both drags survive and every clone
agrees. The encoding lands with `5b09ee1`; the board that uses it comes with
`f32ea71`.

A `bool` kind is a likely fourth, pending `4d61ebe` (archived), which leans
toward archived being a built-in boolean field rather than a status category —
archiving says whether an issue is still worth looking at, while the category
set says how the work ended.

Categories are fixed and closed: `backlog`, `unstarted`, `started`,
`completed`, `canceled`. Every tool keys off these.

### D3 — Status becomes a field, in two steps, with the old op as its past

`SetStatusOp` is not deleted and not migrated. It is **reinterpreted**: when
compiling a snapshot, a `SetStatusOperation` sets the status field to the
schema's designated open or closed value — the `on_open` and `on_close`
attributes of the status field (`config-entity.md` E3). Our 74 issues keep working with no
data migration, and the compatibility shim is perhaps thirty lines in a package
we own.

Given finding 2, the removal is staged:

1. **Field model underneath, projection on top.** `Snapshot.Fields` and
   `BugExcerpt.Fields` land; `Snapshot.Status` stays as a derived value —
   `closed` when the status field's category is `completed` or `canceled`,
   `open` otherwise. GraphQL, the webui, the bridges and `status:open` keep
   working untouched, because from where they sit nothing changed.
2. **Callers move to categories** one surface at a time, and `Snapshot.Status`
   is deleted when the last one is gone.

Step 2 is not this story's to finish. The webui half belongs with `f32ea71`,
which rewrites those views anyway; doing it now means editing code that is
about to be replaced.

This is the one place I am proposing to deliver less than the task's words
(`5b09ee1`: "`common.Status` open/closed goes away"). The model change is
complete; the cleanup is sequenced behind it.

### D4 — Relations store one side; the inverse is derived (`c090f9b`)

`AddRelation{type, target}` on the issue that "owns" the statement, per
AGENTS.md: no multi-entity commit, so nothing is written to the other side.

The inverse lives in the cache. `BugExcerpt` carries its relations, all
excerpts are in memory, so "children of X" is a scan of the excerpt map built
into a reverse index at load and maintained incrementally on update. This is
also why D1's schema needs the relation *types* before the cache can index
them.

- **Cardinality** is enforced at write time only: setting a second parent
  replaces the first (it is `replace`, not `add`, in JSON-Patch terms).
- **Dangling targets render.** An id pointing at an issue that was removed, or
  not yet pulled, shows as the short id, not an error and not a crash. With
  eventually-consistent cross-entity links this is a normal state, not a
  corruption.
- **Allowed-parent rules** come from the type list: the schema declares an
  ordered list of issue types, and which types may parent which. That is the
  whole of hierarchy — Linear's Initiative > Project > Issue > sub-issue and
  Jira's Initiative > Epic > Story/Task > Sub-task are both just config.

### D5 — Iterations are a first-class entity (`aba17f4`)

Following the task's own second comment: sprint planning needs dates,
membership, capacity and carry-over, which an opaque text field cannot carry.

A new entity in its own namespace, `refs/iterations`, on the same `dag`
machinery and the same `SubCache` as issues, with ops `Create`, `SetDates`,
`SetCapacity`, `SetState`. Issues reference it through a cardinality-1
relation of kind `iteration`. Jira sprints and Linear cycles both map onto it.

The consequence to state plainly: **closing an iteration is not atomic.**
Marking it closed and moving N unfinished issues to the next one is N+1
commits, and a crash halfway leaves some issues moved. The order that makes
that recoverable is: create the next iteration, move the issues, then close the
old one — so the intermediate state is "some issues already in the next
sprint", which reads correctly and is idempotent to retry. A close that
happened first would leave issues stranded in a closed iteration.

### D6 — Validate on write, stay lenient on read

A schema is a statement about what may be written now, never about what was
written before. Operations are immutable and content-addressed, so a schema
that rejected existing data would make history unreadable — the opposite of
what a tracker is for.

So: writes are validated and rejected with actionable errors, per `bb9e89e` —
`status "done" is not in the schema; valid values: todo, in-progress,
in-review, shipped` — because agents are the main caller and an agent can act
on that. Reads keep values the schema no longer knows, and surface them as
unknown rather than dropping them. A field removed from the schema stops being
settable; it does not vanish from the issues that have it.

### D7 — Presets are embedded, and we dogfood `jira`

`jira.yaml` and `linear.yaml` embedded with `go:embed`, instantiated into
the schema entity by `git work schema init <preset>`. The round-trip table
test in `59fed1c` is the acceptance test for the whole engine.

**This repo dogfoods `jira`**, reversing `59fed1c`'s original note. The
reasoning there — no Jira here, so run `linear` — points the wrong way now that
the sandbox is skipped (`de1d8fb`): the `jira` preset is written from REST API
documentation and verified by nothing. Running it here every day against real
issues is the only check it gets before it meets the company instance. `linear`
keeps earning its place in the round-trip test, which is where the
"both models are representable" claim is actually proved.

One mapping needs a call during `bf6f392`: `type:decision` has no native Jira
issue type, so either the preset carries a custom one or decisions become Task
plus a marker. `area:` maps onto Components, which exercises `multi-enum`.

## Gaps: two tasks that do not exist yet

1. **Migrate our own label taxonomy to fields.** `type:`, `phase:`, `area:`,
   `prio:` and `story:` become the type field, a phase field, an area field,
   the priority field and the parent relation. Without this the tracker keeps
   two models of itself and AGENTS.md documents the wrong one. It is also the
   best possible test of the engine: 74 real issues, migrated by a script,
   diffed.
2. **Implement the iteration entity.** `aba17f4` is a decision task; D5 decides
   it, but nobody is assigned the entity, ops, subcache and resolver that
   follow — a chunk comparable to a third of this story.

I'd add both to `6555e36` before starting, rather than discovering them
mid-flight.

## Order of work

1. `3556569` — the config entity (ops, snapshot, subcache, resolver), the
   YAML shape, `schema init`/`export`/`import` on top of `reconcile`, and the
   `base` fallback; designed in `config-entity.md`. Everything else reads
   this, and the flow catalogue will reuse it.
2. `bb9e89e` — kinds, values, categories, validation, actionable errors. Pure
   library, no entity changes, fully testable on its own.
3. `5b09ee1` — `SetFieldOp`, `Snapshot.Fields`, `BugExcerpt.Fields`, the
   `SetStatusOp` shim and the derived `Status` projection.
4. `c090f9b` — relations, the cache's reverse index, hierarchy rules.
5. New task — migrate this repo's labels to fields; fix AGENTS.md in the same
   change.
6. `aba17f4` + new task — the iteration entity.
7. `59fed1c` — both presets and the round-trip table test, last, because it is
   the acceptance test for all of the above.

## Done when

- `git work schema export` prints the live schema; editing and importing it
  emits the minimal operations rather than overwriting, and the entity's own op
  log shows who changed what, when;
- both presets load, and the `59fed1c` table test round-trips a representative
  issue set through each;
- an issue can carry type, status with category, priority, estimate, dates,
  assignee, a parent and a blocks relation, all schema-driven;
- `git work issue "status is not a thing"` fails with a message naming the
  valid values;
- this repo's own 74 issues are on fields, not labels, and AGENTS.md says so;
- no operation written before this story is unreadable, and no entity id
  changed.

## Risks

- **Kinds are the expensive thing to get wrong.** A missing *value* is
  config; a missing *kind* is a schema-language change that every preset and
  the Jira field mapping have already been written against. That is why
  `multi-enum`, `multi-identity` and rank are settled here rather than when
  something needs them.
- **The engine outgrowing "just configurable enough."** Every real tracker's
  schema system eventually grows formulas and conditional workflows. The fixed
  kind list and the closed category set are the guard; adding a *kind* should
  feel like a design decision, adding a *value* should not.
- **Two models of our own tracker during step 5.** Labels and fields coexist
  until the migration runs. Keep that window short.
- **The unverified `jira` preset.** Written from documentation with no sandbox
  to check it against; expect it to be wrong in small ways until someone points
  it at a real instance.
- **Config operations are a new entity type to maintain.** Ops, snapshot,
  subcache and resolver, carried forever. The flow catalogue is what makes that
  worth it; if flows end up somewhere else, revisit this.
- **Relation indexes and the cache.** The reverse index is memory-only and must
  be rebuilt by the same `refreshFromRefs` path the watcher drives, or a parent
  set by another process shows stale children.
