# What changed since: snapshots in the past, and a report flow

Status: **designed 2026-10-02, reviewed by the orchestrator in the user's place.**
Story `58e95ff`, tasks `09d9d09` (the plumbing) and `2c0c256` (the flow).
The decisions below were settled with the user on 2026-10-02;
the calls this document makes beyond that brief are marked as such.

## Problem

"What changed since Monday?" is the one question the op log can answer
and a snapshot store cannot.
Every issue is a DAG of operations, each stamped with its author's wall clock,
so the history is already there;
nothing reads it.

A weekly status needs three things from it:
what an issue looked like at the start of the window,
what it looks like now,
and what happened in between.

## The primitive is the snapshot, not the diff

Go replays operations into a snapshot at a time.
Starlark compares two snapshots as plain JSON.
There is no `report` command and no `diff` verb.

Two reasons, and they point the same way.

The apply rules belong to `entities/issue`:
`SetField` with a null clears, `AddValue` and `RemoveValue` have set semantics,
`EditComment` rewrites a body in place, `archived` is an ordinary field.
A flow that walked the log and reconstructed a value
would be a second implementation of those rules,
and it would drift the first time an operation is added.
Replaying with the issue package's own `Apply` cannot drift:
it *is* the rule.

What to report is policy.
Which field is the status, which the parent, what counts as "closed",
whether a reassignment is interesting —
none of that is knowable in Go,
because there are no field roles (`f4bac00`, `d56e6f1`).
A Go `report` command would have to invent them.
A flow names the fields it means, which is the project's rule for every other
piece of policy, so the report is a flow.

The split is therefore:
Go answers *what was true then*, which is mechanical;
Starlark decides *what is worth saying*, which is not.

## The cut is each operation's own wall-clock time

An operation carries two clocks: a lamport time, for merge order,
and a unix timestamp, its author's wall clock at the moment of the write.
The window is cut on the second.

"Since Monday" is a human statement about a calendar,
and lamport time is not a calendar:
it is a partial order with no relation to any date a person can name.
Cutting on wall-clock time also makes the report converge.
An operation written on Tuesday and pulled on Friday
lands in Tuesday's window, not Friday's,
so the report of a week re-run after everyone has pushed
gives the same answer to everyone.
A report run before the pull is simply missing data, and says nothing false.

The cost is the cost of trusting a remote clock:
a contributor whose machine is a day off
puts their operations in the wrong window.
That is the same trust `cache.IssuesByCreationTime` already places in it
when the lamport clocks tie,
and the alternative — reporting in lamport order — answers a question nobody asked.

**A tracker commit as the cut is out.**
Git's own `--since` names a commit range,
and that does not exist here:
every issue is its own ref, there is no cross-issue commit,
and `dag.Entity.Commit` writes one ref at a time by design.
There is nothing to name.

## The vocabulary: `--at`, `--from`, `--to`

A point in time is `--at`.
A window is `--from`/`--to`, half-open, `[from, to)`,
so that back-to-back windows neither drop an operation nor count it twice.

`--from`/`--to` rather than git's `--since`/`--until`:
the gantt already calls the ends of a period `start` and `stop`,
`from` and `to` are what any KWARGS object in this tool would use,
and `--since` carries git's own "reachable from" meaning,
which is exactly the meaning that does not apply here.

One TIME grammar on all three flags, in this order of attempts:

- a date, `2026-09-21`, read as local midnight;
- an RFC 3339 timestamp, `2026-09-21T09:00:00-07:00`;
- a duration back from now, `7d`, `2w`, `12h`.

A duration is relative to the moment the command runs,
so `--from 7d` is the last seven days and needs no date arithmetic in a script.
The duration parser is the one `git work jira sync --adopt` already uses —
Go's `time.ParseDuration` with days added —
moved up into `host` so there is one answer to "what is `7d`" in the tool,
with `w` added for weeks, because a weekly report wants to say `1w`.
`commands/jira` keeps its own error wording and calls it (own call).

**An iteration id is not a TIME form.**
Resolving "the current sprint" means reading an issue's `start` and `end` fields,
and which fields those are is policy:
the schema may call them anything, and `iteration` is a type by convention only.
The flow resolves an iteration to two timestamps and passes them down.
Go never learns the word.

## `--at` is a replay, not a cache

`issue.SnapshotAt(ops, at)` takes an issue's operations in the DAG's order,
keeps those whose `Time()` is at or before `at`,
and applies them into a fresh snapshot with the same `Apply` that `Compile` uses.
It lives in `entities/issue` because that is the package that owns the rules;
`Compile` is the special case where nothing is filtered out.

Two consequences fall out of the replay rather than being added to it:

- **An issue created after `at` is absent.**
  Its create operation is filtered out, nothing else can apply without it,
  and `SnapshotAt` returns nil.
  `issue get --at` reports that the issue did not exist yet;
  the list simply leaves it out.
  This is what lets a flow say "created in the window":
  absent at `from`, present at `to`.
- **`archived` is whatever it was then.**
  It is an ordinary field set by an ordinary operation,
  so the replay restores the value that stood at `at`,
  and the list's default program — `select(.fields.archived != true)` —
  filters on that value with no special case anywhere.
  An issue archived on Wednesday is in Tuesday's list and out of Thursday's.

The same holds for every other field, for the title, for the comment bodies
(an `EditComment` after `at` is not applied),
and for the actors and participants, which the operations accumulate.

**Performance: `--at` is linear in the store's operations.**
Every issue is read and replayed once, in memory, with no cache and no index.
For the hundreds of issues this tool is built for that is a few milliseconds.
On a store of many thousands of issues with long histories
it is a full pass per call, and a report that calls it twice pays twice.
That is accepted: the alternative is a persisted historical index,
which is a cache to invalidate for a query that runs weekly.
If it ever matters, the fix is to replay once and emit both snapshots,
not to store anything.

### The excerpt at a time

The list's input is an array of excerpts, and a jq program written for
`git work issue` has to work unchanged under `--at` —
that is the whole point of making `--at` a flag and not a different command.

The excerpt is therefore built from the historical snapshot
into the same `cmdjson.IssueExcerpt` shape the live path produces,
rather than from `cache.IssueExcerpt`, which only exists for the present.
`cache/` is untouched (own call): nothing there is per-time,
and a historical excerpt has no business in a cache keyed by id.

One field cannot be answered honestly: `edit_time.lamport`.
A lamport time is the entity's, not an operation's — operations do not carry one —
so a snapshot at a past time has no last-edit lamport to report.
It is omitted (the field is `omitempty`), and `edit_time.timestamp` is exact.
`create_time` is whole, lamport included: the create operation is the same one.
A program that sorts on `.edit_time.lamport` therefore sorts on nothing under `--at`;
the default program's secondary key, `.edit_time.timestamp`, still orders it.

## The log takes a PROGRAM as well as an ID

`issue log` is the window's third input: the operations themselves.
A report over a hundred issues cannot be a hundred commands,
so the log takes the list's PROGRAM too,
and returns the selected issues' operations ordered by time.

Each entry gains an `issue` field, the id of the issue it belongs to (own call).
It is present whether one issue was selected or many,
because a shape that changes with the argument
is a shape every caller has to branch on.

**Telling an ID from a PROGRAM**:
the argument is resolved as an id prefix or alias first;
if that succeeds it is that issue.
If it does not, the argument is compiled as a jq program.
If *that* fails too, the error names both attempts,
so a mistyped id reads as a mistyped id and not as a jq syntax error.

No `--query` flag: the ambiguity is not real (own call).
An id prefix is lowercase hex, and no bare hex word is a valid jq program;
an alias is a Jira-shaped key such as `PROJ-12`, which is not one either;
and every useful program contains a character hex does not.
A flag would make the common case — one id — pay for a collision that cannot happen.
The one genuine overlap is the empty argument,
which is the default program over every issue, exactly as in the list.

Ordering across issues is by operation time ascending,
ties broken by the issue's id and then by the operation's position in its own issue,
so that two operations written in the same second still print in a fixed order (own call).

## Starlark

One to one, as always:

```python
work.issue.get(id, at="2026-09-21")
work.issue.list(program, at="7d")
work.issue.log(id_or_program, from_="7d", to=None)
```

`from` is a reserved word in Starlark,
so the keyword is `from_`, the same trailing underscore `work.schema.import_` already uses.
It is the second instance of that rule, which makes it a rule rather than a quirk.

`at`, `from_` and `to` take the same TIME strings the flags take,
and `None` means what the absent flag means:
no `at` is now, no `from` is the beginning of time, no `to` is now.

## The report flow

`flows/report.star`, one function, as every flow is:

```python
def report(from_="7d", to=None, iteration=None, query=None)
```

It resolves the window — an `iteration` id reads that issue's `start` and `end`,
otherwise `from_`/`to` go down to the plumbing as the strings they are —
selects the issues with `query` (default: every unarchived issue),
and for each one asks the plumbing three questions:
the snapshot at `from`, the snapshot at `to`, and the operations in between.
Then it compares and prints markdown.

What it says, and why:

- **Created** is absent-at-`from`, present-at-`to`.
  **Closed** is a status whose schema category became `completed` or `canceled`.
  **Archived** is `archived` false becoming true.
  Nothing else is given a name.
- **Every changed field is reported**, `before → after`, by key,
  for an issue that already existed at the start of the window.
  The flow does not know which fields matter, and guessing wrong is worse than
  one extra line.
- **A created issue is described rather than diffed.**
  There is no before to diff against,
  so `before → after` would read `(none) → value` on every field —
  the whole issue written the long way.
  Instead the block line says `created` and one line follows it,
  `with status done, priority high, area [tui], parent 58e95ff …`:
  the fields it holds at the window's end,
  in the schema's order for its type, the empty ones left out.
  Its description is the body of the create operation and not a comment,
  so it is never one of the comments listed under it;
  those are comments someone wrote in the window.
- **The status gets its whole path**, `to-do → done (via in-progress)`,
  read from the operations rather than from the two snapshots,
  because the interesting part of a week is often the states a task passed through.
  This is a field the flow names outright, and it names it because it is
  policy and the flow is where policy lives.
- **Comments in the window** are listed with author, date and first paragraph,
  cut at about 200 characters: a status report is a table of contents, not a transcript.
- **Grouped by `parent`**, the parent's title as the heading, `(none)` last.
  The tracker's own shape is stories with tasks under them,
  and that is the grouping a reader of a weekly status wants.
  `parent` is named by the flow, like `status`.
- **A summary line at the top**: the window, and the counts
  created / closed / changed / commented.

It prints with `print()`, which is standard output (`e2cad657`),
warns on `work.stderr` (`e5b9750b`) — an iteration with no dates is the case that matters —
and returns `None`.
Markdown because the two readers are a human scanning it
and a model summarizing it for management, and markdown suits both.

## What this is not

- Not a narration. The flow emits the deterministic half; an AI writes prose over it.
- Not a burndown. Counts of what changed, not of what remains; the gantt and the board
  are the shape of the work.
- Not incremental. Every run replays from scratch. There is no stored cursor,
  nothing to invalidate, and a re-run of last week's window gives the same answer
  once everyone has pushed.
