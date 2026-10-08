# Consolidation re-points every relation, every run (`dda47ef`)

**Outcome:** after two copies of one Jira issue are consolidated,
no unarchived issue names the archived copy in a relation,
on any clone, whatever refused or interrupted the run that archived it,
so that a child hangs off the copy that survives
and the sync has nothing left to call "not in Jira".

**Serves:** `8ade811` (the story); amends `dda47ef`, which built consolidation.

**Status:** decided 2026-10-07 (Luis);
revised the same day (Luis): the sweep runs first over every issue and is the
only re-point, the archive is the last write, and the loser is stamped (R1, R6, R7).
The first draft kept `consolidate`'s own `repoint` and swept after the losers.
Built against the fake.

## Problem

`consolidate` (`jira/candidates.go`) syncs the loser once more,
archives it, fills the winner's gaps, notes the winner,
and last runs `repoint`,
which sets every relation naming the loser to the winner,
one commit per issue.
Four things make that last step unreliable,
and one makes its failure permanent:

1. **`repoint` writes under the policy check.**
   It commits through `IssueCache.Update`,
   which refuses a relation whose target type the field's `target_types` omit.
   The value it moves was admitted by the shape check on pull
   (`pull-schema-check.md`),
   because Jira is the authority on what Jira holds;
   a parent of a type the schema's policy excludes
   is therefore ordinary in a bound store, listed under `off_schema`,
   and re-pointing it fails with
   `field parent: issue … is a epic; parent takes …`.
2. **The first refusal aborts the pass.**
   `repoint` returns on the first error,
   so every issue after it in the iteration keeps the stale relation,
   whatever its own type allows.
3. **The pass runs after the archive.**
   The loser is already archived and the winner already noted
   when `repoint` fails,
   so `consolidate` reports `failed` for an issue whose archive stands.
4. **An archived loser is never visited again.**
   `scan` lists as `losers` only the unarchived second copies,
   and `consolidate` returns at once on an archived one,
   so the next run has nothing to re-point and the stale relations stay.

The iteration is `AllIds()`, map order,
so which issues were re-pointed before the abort differs run to run.

A stale relation is not harmless.
Exporting it asks the `Index` for the loser's Jira id,
which only the winner has,
so the field is skipped with `… is not in Jira` on every run.
Importing repairs it,
because the pull resolves Jira's value to the winner through the `Index`
and writes under the shape check,
but a run bounded by the cursor never re-reads a child Jira did not touch,
so the repair waits for `--full` or for an unrelated edit in Jira.
A nested view, which draws a row's unarchived children,
shows such a child under no parent at all.

## R1 — Re-pointing is an invariant of the store, not a step of one archive

A run holds, after its scan and before its losers and its search:

> No unarchived issue has a relation value naming an issue
> that is not the `Index`'s winner for its own Jira id.

`sweep` enforces it every run, over every issue of the store,
`runAll` and `runIds` alike, from the `Index` alone:
for each unarchived issue, for each relation field its type has,
each value whose issue carries a `jira-id`
and is not `Index.Issue(jira-id)` is replaced by the winner,
a scalar by one `SetField`,
an item of a set by `RemoveValue` then `AddValue`,
all of one issue in one commit.
A value naming an issue with no `jira-id`,
or one that is itself the winner, is left alone;
a value naming an archived issue that has no duplicate
is a legitimate relation to an archived issue and is left alone too.

The sweep runs **first**, after the scan and before the losers loop,
so each loser's last sync and the winner's fill already see relations
naming winners.
It re-points away from a loser whether or not the loser is archived yet:
the `Index` decides who wins, and the archive only follows.
`consolidate` no longer re-points at all (the first draft kept its own
`repoint` call): one home for the write means one place that reports it,
and the sweep is what makes the outcome hold anyway
after a refusal, an interrupted run,
a loser archived by another clone whose own sweep did not reach this store,
or a store consolidated before this design.
`runIds` sweeps every issue too, not only the named ones,
because the invariant is the store's.
The sweep's cost is one pass over in-memory excerpts,
which is the cost of `scan`.

## R2 — Re-pointing writes under the shape check

`sweep` commits through `IssueCache.UpdateShape`,
as the pull does (`pull-schema-check.md`).
The value they write is one the store already holds
for the same field of the same issue;
only the target's identity changes, never its type.
A policy that refuses the new value would have refused the old,
and the old is Jira's.
Values the policy refuses still reach the report under `off_schema`,
one entry per field re-pointed, as a pull's do.

## R3 — One issue's refusal is that issue's line

A relation that cannot be re-pointed,
which after R2 is a write error and not a policy refusal,
is reported as a `failed` line for the issue that holds it,
with the field and the error,
and the pass continues with the next issue.
A re-point error never touches a consolidation's line:
the sweep reports the issue that refused on its own.
The pass iterates issue ids in sorted order,
so two runs over one store report the same lines in the same order.

## R4 — The report names the re-point

Each re-pointed issue is one line,
action `repointed`,
`imported` listing the relation fields that changed,
the same shape a pull's `updated` line has,
because a re-point is a local write the person did not make
and the report is where those are told.
The summary gains `repointed`,
the count of issues the run re-pointed.
A dry run lists what it would re-point and writes nothing.

## R5 — Idempotence

After one run the invariant holds and the sweep finds nothing,
so a store that was consolidated correctly pays the scan and no commit.
A clone that pulls a loser's archive from another clone
sees the invariant false for the relations that clone did not reach
and repairs them on its next run,
after which every clone agrees,
since the winner is a function of immutable history (`firstToSync`).
A consolidation interrupted before its archive is retried whole (R6),
and writes nothing twice.

## R6 — The archive is the last write

`consolidate` syncs the loser once more,
then commits the winner's fill and the note together,
then archives the loser.
A failure anywhere before the archive leaves the loser unarchived,
so the next run's scan lists it again and the consolidation is retried:
the fill is gap-only, so a value already carried over is not carried again,
and the note carries `jira-consolidated-from`, the loser's id,
so a winner already holding the note for this loser is not noted twice.
The metadata key, not the note's text, is what is matched,
because text is for people and may be reworded.
Archiving first, as the first draft did,
made an interruption after it final:
the archived loser is never a loser again,
and its fill and note were lost.

## R7 — The loser is stamped

The archive's commit also sets `jira-consolidated-into`,
the winner's id, on the loser's create operation
(`SetMetadata`, the way a link writes `jira-id`),
one commit with the archive, so a loser is archived and stamped or neither.
Create-op metadata is the excerpt's `metadata`,
so a jq program drops the losers with
`map(select(.metadata["jira-consolidated-into"] == null))`,
which a report over archived issues needs:
a loser is an archive the person did not decide.

## What this is not

- Not a change to who wins. `firstToSync` and the `Index` are untouched.
- Not a change to what consolidation fills or notes,
  only to their order and to the note's metadata (R6).
- Not a write to Jira. A re-point changes the local target of a value
  whose Jira value is unchanged;
  the next export resolves the winner to the same Jira id
  and finds nothing to send.
- Not a cache of the invariant. It is checked from excerpts each run.

## Tests

In `jira/orphan_test.go` beside E26–E28:

- E29 — a loser with a child whose relation the policy refuses:
  the loser is archived and `consolidated`,
  the child is re-pointed and reported `repointed` with `off_schema`,
  the children after it are re-pointed too.
- E30 — a store holding an archived loser and a stale relation,
  no loser in `scan`: the sweep re-points it,
  the line says `repointed`, the summary counts one,
  and the next run writes nothing.
- E31 — a relation to an archived issue that has no duplicate:
  untouched by the sweep.
- E32 — a multi-relation holding the loser among other items:
  the loser's item is replaced, the others and their order kept.
- E33 — `--dry-run`: the lines appear, no commit is made.
- E34 — two runs over one store list the same `repointed` lines
  in the same order.
- E35 — a consolidation whose fill and note committed and whose archive
  did not (simulated by unarchiving the loser):
  the next run archives it, with one note and no second fill.
- E36 — after consolidation the loser's excerpt metadata carries
  `jira-consolidated-into`, the winner's id.

## Tasks

- `sweep` in `jira/candidates.go`, replacing `repoint`:
  `UpdateShape`, sorted ids, per-issue `failed` lines.
- `runAll` calls `sweep` over every issue after the scan
  and before the losers loop;
  `runIds` calls it over every issue before its own.
- `consolidate`: sync, fill and note (once), archive and stamp, in that order;
  `MetaConsolidatedInto` and `MetaConsolidatedFrom` in `jira/types.go`.
- `Line.Action` gains `repointed`; `Summary` gains `Repointed`;
  `--format text` prints both.
- `doc/design/jira-sync.md` JS25 gains the invariant
  and names this document; `AGENTS.md`'s Jira paragraph says
  "every relation naming it is pointed at the survivor, every run".
