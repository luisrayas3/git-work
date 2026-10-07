# Unarchived by default: `include_archive` (`df6ff51`)

**Outcome:** a program — on the command line, in a flow, under a view —
runs over the issues that are not archived
unless its caller asks for the archived back,
so that no query has to remember a filter
and a store holding an archived copy of every issue reads clean.

**Serves:** `df6ff51` (the story).

**Status:** decided 2026-10-07 (Luis); not built.

## Problem

A jq program's input is every issue the store holds, the archived included.
Only the default program leaves them out,
and only when nobody supplied a program:
`DefaultQuery` opens with `map(select(.fields.archived != true))`,
and `.` is the whole array (`view/query.go`).

That rule was chosen so that nothing is hidden from a program.
In use it is a trap.
Every custom query — a `git work issue PROGRAM`,
a flow's `work.issue.list(program)`,
a view's `query` — has to carry the filter itself,
and the one that forgets sees the archived as if they were live.
The case that makes it bite is a Jira-bound store:
the sync consolidates two copies of one issue by archiving one
(`jira-sync.md`, JS25),
so after a consolidation every issue has an archived twin,
and a query that forgets the filter reports every row twice.
Two flows in a real store did exactly that.

## I1 — The input is the unarchived issues

`host.IssueListInput`, the one function that builds the array a program runs over,
and `IssueListInputAt`, which is the same function at a time
and hands a zero `at` to it (`host/issue.go`, `host/issue_at.go`),
leave out every issue whose `archived` is true,
unless they are told to include them.
Every consumer goes through them —
the list, the log's PROGRAM form (`selectIssues` runs the program over the present input),
every view kind's `query` (each page calls `host.IssueList` on its re-run),
and the Starlark mirrors of each —
so the rule has one home and no caller can forget it.

`.` keeps its meaning: the whole input.
What changes is what the input is,
and one switch brings the rest back.
This is the trade against `view/query.go`'s reason,
"so that `.` means the whole array and nothing is hidden from it":
something is now hidden, deliberately, and reachable with one word.
A filter every author must remember is worse than a default every author can lift.

## I2 — The switch is spelled `include_archive`

One name, in each of the three places the one-to-one rule has
(`cli-convention.md`):

| Where | Spelling |
| --- | --- |
| `git work issue [PROGRAM]` | `--include-archive` |
| `git work issue log [ID\|PROGRAM]` | `--include-archive` |
| `git work view KIND KWARGS` | `"include_archive": true`, a defaulted argument on every kind that takes `query` |
| `work.issue.list(program, at=None, include_archive=False)` | the keyword |
| `work.issue.log(id, from_=None, to=None, include_archive=False)` | the keyword |
| `work.view.KIND(..., include_archive=False)` | from the kinds table, as every view argument is |

It is a boolean, false by default, and it says what it does:
*include* the archived in the input.
`--all` was rejected because `.` already means all of the input,
and `--archived` because it reads as *only the archived*.

`issue get ID` is untouched, and so is `issue log ID`:
one issue named by id is that issue,
archived or not, and `get` already prints its `archived` field.
The flag on `issue log` acts on its PROGRAM form alone,
where the program selects from the input.

## I3 — The default program is the order alone

`DefaultQuery` becomes

```
sort_by(.edit_time.lamport, .edit_time.timestamp) | reverse
```

The select is gone because the input already did it.
`git work issue` with no program and a view with no `query`
still show the same issues, through the same constant,
which is what the constant is for.

## I4 — `--at` composes with nothing added

A snapshot at a time carries the `archived` that stood then
(`report.md`, "`--at` is a replay, not a cache"),
so the input filter reads that value and no special case appears:
an issue archived on Wednesday is in Tuesday's input and out of Thursday's,
exactly as the default program behaved,
and `include_archive=True` with `at` is every issue that existed then.

## I5 — Nesting, pickers and children are the same rule

Three places already leave the archived out by a rule of their own:
a layer's `query` runs over "that row's own unarchived children"
(`terminal-renderer.md`, Nesting),
a relation's picker lists the unarchived issues its `target_types` allow,
and show's `children` leave out the archived.
Today the nesting and the children share one filter,
`allIssues` in `tui/nest.go`, which reads `host.IssueListInput`
and then drops the archived again in Go,
and the picker has its own, `relationChoices` in `tui/edit.go`,
which reads the excerpts straight off the cache.
Each is rewritten to take its issues from the same helper with the same switch,
so there is one rule and not three.
A layer spec accepts `include_archive` beside its `query`,
with the same default,
because a layer is a query with a scope and takes what a query takes.
The picker and show's children take no switch:
nobody assigns an archived issue on purpose,
and the opt-in there would be a key with no caller.

## I6 — The callers that want the archived

The only flow in this repository that wants archived issues in its input
is `report`: it reads every issue at both ends of the window
to say which were archived during it,
with `work.issue.list(".", at=…)`,
and it reads every operation in the window with `work.issue.log(".", from_=…, to=…)`,
whose program selects from the present input,
so an issue archived during the window would lose its status path,
its comments and the archive itself from the log.
It passes `include_archive=True` on those three calls and keeps its own
`select(.fields.archived != true)` as the default selection,
because that select is now about the selection and not the input.
`overview` and `board` filter on status categories, not on `archived`,
and gain the archived filter for free.

Any flow elsewhere that carried the filter itself keeps working:
a select on a value that is never true is a no-op.

## What this is not

- Not a change to what `archive` means.
  It is still the replicated removal, still an ordinary field,
  still what `get` shows and `log` records.
- Not a cache or an index. The filter runs on the excerpt in memory,
  on the live value or the replayed one.
- Not a hidden default on `get`, `set`, `comment` or any writer:
  an id names an issue whatever its state.

## Tasks

- The plumbing: `host.IssueListInputAt` takes the switch; `issue` and
  `issue log` grow `--include-archive`; `work.issue.list` and
  `work.issue.log` grow the keyword; `DefaultQuery` loses its select;
  tests pin that a program with no filter sees no archived issue,
  that the switch brings them back, and that `--at` reads the value then.
- The views: `include_archive` in the kinds table on every kind with
  `query`, read by `host.View`, which needs a `Bool` value kind the table
  does not have yet (`view/kinds.go`); a layer spec takes it; the picker and
  show's children read the same helper.
- The flows and the docs: `report` passes the switch where it reads
  everything; `cli-convention.md`, `terminal-renderer.md`,
  `AGENTS.md` and the quickstart say what the input is.
