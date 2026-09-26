# The store migration

`bf6f392`, run on 2026-09-25.
The tracker's own issues,
written by git-bug in its format
under `refs/issues/*`,
become issues in the owned model
under `refs/work-issues/*`,
and the identities they are signed by
move to `refs/work-users/*`.
This document records what the migration does and why,
so that whoever reads the store's history
finds the seam explained.
`configurable-schema.md` D2 and D3 are the decisions it rests on;
the label mapping is comment 1 of `bf6f392`.

## Decisions

**A copy, not a move** (Luis, 2026-09-25).
`refs/issues/*` and `refs/identities/*` stay where they are,
locally and on origin,
and `git work bug` keeps reading them,
because the terminal renderer behind `git work view list`
had never been tried on real data
and the old command line is the fallback if it falls short.
What the copy costs is one rule,
written into `AGENTS.md`:
the old store is frozen the moment the copy is taken,
so anything `git work bug` writes after that
reaches `refs/issues/*` alone and is lost to the tracker.
Deleting `entities/bug` and everything that only exists to serve it
(`commands/bug`, `termui`, the GraphQL API and the bridges' bug side)
is the round after this one (`860d6e0`),
once the new surface has proven itself;
`f4bac00`'s deletion stands, only later.

**Ids are preserved by construction, and checked.**
An operation's id hashes its JSON,
type code included,
and a `Create`, `AddComment` or `EditComment` operation
built by `entities/issue` from the same inputs
serializes to the same bytes as `entities/bug`'s
(`TestIdsMatchBug`),
so the replay copies the old operation's base
(nonce, timestamp, metadata)
onto the new one and gets the same id back.
The replay asserts it,
for every issue and every comment,
and aborts before writing anything if one differs;
the assertion is the migration's own test on the real store.

**Lamport times are reproduced exactly, through a clock decorator.**
`dag.Entity.Commit` takes both clocks from the repository,
so `configurable-schema.md` D2 planned
to witness each clock to one below the original
and let the increment land on it.
That drifts as soon as two clones ever produced the same time,
which is legal, and the drift compounds.
Instead the replay commits through a `repository.ClockedRepo`
whose `Increment` hands out the original pack's time,
and witnesses the real clock with it,
so the target packs carry the very same create and edit times
and the real clocks end where they must.
The decorator lives in package `migrate`,
above the pristine line.
The packs are replayed in global edit order,
one commit per original pack,
so ordering by edit time is unchanged
and `git work issue log` reads as the history did.

**Every old operation has one meaning in the new model.**

| Old operation | New operations |
| --- | --- |
| `Create` | `Create`, same bytes, plus `SetField type` and `SetField status` in the same pack (see below) |
| `AddComment`, `EditComment` | the same, same bytes |
| `SetTitle` | `SetField title` |
| `SetStatus open` · `closed` | `SetField status "to-do"` · `"done"` |
| `LabelChange` | one operation per label, by prefix (below) |

The label taxonomy `AGENTS.md` documented as a workaround
becomes fields of the schema this repository runs on:

| Label | Field | Value |
| --- | --- | --- |
| `type:story` · `type:task` · `type:decision` | `type` | the same |
| `type:spike` | `type` | `task`, plus the label `spike` |
| `prio:high` · `prio:med` · `prio:low` | `priority` | `high` · `medium` · `low` |
| `phase:X` | `phase` | `X`, an enum of every value the store holds |
| `area:X` | `area` | `X`, a multi-enum |
| `story:PREFIX` | `parent` | the issue whose id the prefix names |
| anything else | `labels` | the label, verbatim |

Two things were undone the same afternoon, by a flow and three archives,
once the list showed the type as a column (Luis, 2026-09-25):
the `Story:`, `Task:`, `Decision:` and `Spike:` prefixes came off 75 titles,
because a title that repeats the type is noise next to the column,
and `phase` was cleared on 82 issues and archived on its three types,
because the parent story already orders the work
and eleven values, five of them retired, said as much.
The migration itself stays lossless; history holds both.

A label added is a `SetField` for a single-valued field
and an `AddValue` for a multi-valued one;
a label removed with nothing added in its place
clears the field (`SetField null`) or removes the value.
A removed `type:` label on its own changes nothing:
a type is never unset.

**Type and status are set in the create pack.**
The new model requires a type at creation
and the old store had none until a label arrived,
fourteen issues never got one;
and git-bug's open was implicit,
an issue never closed has no status operation at all,
where a board needs every card in a column.
The replay appends `SetField type` and `SetField status "to-do"` to the create pack,
with the create operation's author and time,
the type being the first the issue was ever given,
`task` when it never was;
a later change of either replays where its label or status change did.
The create operation itself keeps its bytes,
so the issue id is untouched:
the pack's blob is not part of any id.

**The schema is an authoring file in the tree.**
`bf6f392` planned `schema init jira` in the same pass.
Since then the schema import is an upsert of a document,
and the design says the tree holds authoring files
applied by an explicit import (`0740bf3`),
so this repository's schema is `schema.yaml` at the root:
the `jira` preset,
a `decision` type
(fifteen issues are decisions and Jira has no such type;
a type of its own beats a task with a marker,
because a decision's status is not work),
and `area` and `phase` on the three types that carried the labels.
`git work schema import schema.yaml` precedes `git work migrate`,
and `migrate` refuses to run
naming the keys it needs and does not find.
After the replay every issue's final fields are checked against the schema
the way `git work issue new` would check them,
and every problem is printed;
a problem is a fact about the old data, not a reason to abort.

**Identities move with the three constants.**
`entities/identity` names its refs with three constants,
and changing them is the one recorded exception to the pristine seven
(`483dbe2`, `bf6f392` comment 4, named `work-users` on 2026-09-25).
The migration copies `refs/identities/*` to `refs/work-users/*`
before it reads anything,
because the old issues resolve their authors through the new constants.
Identity clocks are not named after the namespace,
so nothing else moves.
Other clones need no copy:
`git work pull` fetches `refs/work-users/*` like any namespace.

**It runs once, and says so.**
`git work migrate` refuses when `refs/work-issues/*` is not empty,
so a second run, on this clone or any other, does nothing;
it takes the write lock,
because it writes what the cache owns;
and `--dry-run` prints the plan,
one line per issue with the fields it will have,
and writes only the identity copy,
which every command needs after the constants change anyway.
The command leaves with `entities/bug`.

## What is not done here

Nothing on origin is deleted;
`git work push` publishes `refs/work-issues/*` and `refs/work-users/*`
next to what is there.
The old cache file under `.git/git-work/cache/`
stays for as long as `entities/bug` does.
Nesting in the list view, board and gantt, and the deletion round
are sequenced after this in `terminal-renderer.md`.
