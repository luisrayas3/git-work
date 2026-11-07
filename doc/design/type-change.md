# A type change is refused where Jira would refuse it, and written where Jira allows it

Status: **approved, 2026-11-07.**
Supersedes JS18's second half (`jira-sync.md`):
a local type change is no longer always pending.

## Problem

JS18 makes `type` import-only.
A local `set type` on an issue linked to Jira commits locally,
passes the schema check,
and then stays pending on every sync with "change the type in Jira".
Local and Jira disagree until someone changes it by hand on one side.

Two kinds of local type change are mixed together there:

- **One Jira refuses outright.**
  Jira's edit can change `issuetype` only within one hierarchy level.
  Epic → Initiative (level 1 → 2) or Task → Sub-task (0 → -1)
  needs a Move in Jira's UI, and some Moves cannot be made at all.
  git-work should refuse it when it is written, not leave it pending forever.
- **One Jira accepts.**
  Story → Task, both level 0, is a plain field edit in Jira.
  Leaving it pending is a gap in the sync, not a policy.

## Decisions

### TC1 — A type says which types its issues may become

A type in the schema gains `retype_targets`,
a list of type keys:

```yaml
types:
  epic:
    aliases: {jira: "10000"}
    retype_targets: []
  story:
    aliases: {jira: "10001"}
    retype_targets: [task, bug]
```

- **Absent** means any type, which is today's behaviour,
  so a schema written before this change loses nothing.
- **An empty list** means the type is fixed once the issue is created.
- A key in the list that is not a type is a schema import error,
  like a `target_types` entry that is not one.
- `retype_targets` is one-way and need not be symmetric;
  derive (TC3) makes it symmetric for Jira types because levels are.

`retype_targets` is a type's property, not a field's,
so it does not give `type` a role:
`type` is already built in,
and this is the schema saying what the built-in field accepts.
It is stored on the type entity, as aliases are.

### TC2 — It is policy, so local writes are refused and pulls are not

`Checker.CheckFields` refuses a `set` whose `type` differs
from the issue's current type
and is not in the current type's `retype_targets`,
naming the types it may become:

```
type: type change from epic to initiative is not allowed; allowed: (none)
```

This goes in the **policy** half of the checker (`pull-schema-check.md`),
not the shape half.
A pull runs the shape check only,
so a type changed by a Move in Jira is still imported
whatever `retype_targets` says: Jira is the authority on what Jira holds.
`issue new` is not a type change and is never checked against `retype_targets`.

The command line and Starlark both reach this through `host` and `cache`,
so `git work issue set` and `work.issue.set` refuse alike,
and `--dry-run` reports the refusal.

### TC3 — Derive fills `retype_targets` from Jira's hierarchy levels

`jira/derive.go` already knows each issue type's `hierarchyLevel`.
For each mapped type it sets `retype_targets`
to the other mapped types of the same level, in key order.
Derive already rewrites a mapped type's name from Jira on every run,
and `retype_targets` is treated the same way:
Jira is canonical for what a Jira type can become,
and a change is reported as a note
(`retype_targets task, bug after Jira levels`).

- A **local-only type** (no Jira alias) is left alone:
  its `retype_targets` is whatever the author wrote.
- A mapped type's `retype_targets` never lists a local-only type.
  A linked issue retyped to a local-only type would never be exported again (`toWrites`),
  which is a silent unlink;
  someone who wants that writes it into the schema by hand
  after the import, and the next derive takes it back out.
- A type whose alias is `""` (stated never-sync) counts as local-only.

The policy is per type, not per issue:
an epic created locally and never synced also cannot become an initiative.
That is the cost of keeping the checker free of Jira state,
since whether an issue is linked lives in the sync's base, not the schema.
The cost is small:
a repository that syncs to Jira has its types derived from Jira,
and the rule is Jira's own.

### TC4 — A local type change is exported, alone and first

When the merge says `give` for `type`
(local changed it, Jira did not),
the sync writes it instead of making it pending:

1. Map the local type to its issue type id.
   A local-only type is pending, as now ("type X is local-only").
2. `PUT /issue/{id}` with `fields.issuetype` and nothing else,
   **before** the edit carrying the other fields.
   The other fields are mapped under a type's field mapping,
   and Jira validates them against the issue's type as it stands,
   so they cannot share the request with the type.
3. On a 2xx, `type` goes into `Sent` as any write does (I2),
   and the issue's remaining remote changes are mapped and written
   under the **new** type's mapping, not `remote.Type`'s.
   Keys the two types share carry over, as JS18 says for an import;
   a key the new type lacks in Jira is pending with the reason it is now
   ("<type>/<key> is local-only").
4. On a refusal, `type` is pending with Jira's own message,
   and the rest of the edit goes ahead under the old type,
   as today.

TC3 means a refusal should be rare,
but levels are not all Jira checks:
a target type under another workflow, or with a required field unset,
can still be refused, and Jira's message says which.
git-work does not read `editmeta` to predict this.
It would cost a request per type change to repeat a check
Jira makes anyway on the write,
and the refusal already lands where the user looks: the sync report's pending list.

### TC5 — A type changed on both sides is a conflict, Jira wins

Unchanged from JS9's table for any scalar:
when both sides changed `type` to different values,
Jira's is taken and the conflict reported.
It is listed here only because, before this change,
the local side could never win a `type` key,
and now it can when only it changed.

## What this does not change

- **Subtasks and parents.**
  A same-level move between two sub-task types keeps the parent.
  Moving across levels is refused by TC2,
  so the parent field never has to be rewritten by a type change.
- **Fields the old type had and the new one lacks.**
  They stay on the issue, as after any type change today;
  readers already tolerate off-schema values.
- **No Move.**
  git-work never calls Jira's bulk-move API.
  A cross-level change is made in Jira and imported.

## Tests

Each test owns its schema (`AGENTS.md`).

- Schema: `retype_targets` round-trips through import and export;
  an unknown key in it is an import error.
- Checker: a refused change names the allowed types;
  absent `retype_targets` allows anything;
  the shape check ignores `retype_targets`.
- Derive: a fake project with levels 2, 1, 0, 0, -1
  gives `retype_targets` lists by level, excludes local-only types,
  and rewrites a hand-widened list with a note.
- Sync (`jiratest`): Story → Task writes `issuetype` alone and first,
  then the rest under Task's mapping;
  a refused type change is pending with Jira's message
  and the other fields still land;
  a pulled Epic → Initiative is imported even though `retype_targets` forbids it locally.
