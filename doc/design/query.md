# Query

A query is a jq program,
run by the one evaluator in `query/jq` (gojq)
wherever a program is taken:
`git work issue list PROGRAM`, `issue log PROGRAM`,
a view's `query`, a layer's, a side table's,
and the Starlark mirror of each.
This document covers what a program runs over,
what it is bound with,
the rows it may hand a view,
how it reads the past,
and search.
The view kinds and their arguments, nesting included, are [views](views.md);
drawing, the grab and editing a query on the palette are [tui](tui.md);
the report flow that reads the past is [flows](flows.md);
the excerpts the input is built from are [store](store.md);
the operations a replay applies are [issue](issue.md);
the command shapes are [cli](cli.md).

## The input

### The input is the unarchived issues

The array a program runs over is every issue whose `archived` is not true,
as excerpts, oldest first,
so a program that does not sort reads the same twice.
`host.IssueListInput` builds it,
and `host.IssueListInputAt` is the same at a time;
every program reads its issues from there,
so the rule has one home and no query has to carry the filter.

`.` keeps its meaning, the whole input;
what the input is changed.
Something is hidden, deliberately, and reachable with one word:
a filter every author must remember is worse than a default every author can lift.
The case where it bites is a Jira-bound store,
where the sync consolidates two copies of one issue by archiving one,
so a query that forgot the filter would report every row twice.

An issue named by id is that issue, archived or not:
`issue get ID` and `issue log ID` take no switch,
and no writer hides an archived issue.
`archived` is an ordinary, replicated field that `get` shows and `log` records.

A flow may carry its own `select(.fields.archived != true)`:
a select on a value that is never true is a no-op.

### The switch is `include_archive`

One name, in each place the one-to-one rule puts it:

| Where | Spelling |
| --- | --- |
| `git work issue [list] [PROGRAM]` | `--include-archive` |
| `git work issue log [ID\|PROGRAM]` | `--include-archive`, acting on the PROGRAM form alone |
| `git work issue search TEXT` | `--include-archive` |
| `work.issue.list(program, at=None, include_archive=False)` | the keyword |
| `work.issue.log(id, from_=None, to=None, include_archive=False)` | the keyword |
| `work.issue.search(text, include_archive=False)` | the keyword |
| every view kind that takes `query` | `"include_archive": true`, a defaulted argument, false |
| `git work schema [export]` | `--include-archive`, for the archived types, fields and values |
| `work.schema.export(include_archive=False)` | the keyword |

A boolean, false by default, that says what it does:
*include* the archived in the input.
Not `--all`, because `.` already means all of the input;
not `--archived`, because it reads as *only* the archived.

A reader that wants history whole passes it:
the report flow reads every issue at both ends of its window
and every operation in it with `include_archive=True`,
or an issue archived during the window would lose its last operations.

### The default program is the order alone

An empty program is `view.DefaultQuery`:

```
sort_by(.edit_time.lamport, .edit_time.timestamp) | reverse
```

last edited first.
`git work issue` with no program and a view with no `query`
read the same constant, so they show the same issues.
It selects nothing, because the input already did.
"Mine" would be a friendlier default and cannot be this one:
Go names no assignee field, so "mine" is a flow's to write.

### Layers and pickers follow the input

Every other reader of "the issues" leaves the archived out by the same rule.
A layer's `query` runs over that row's own candidate children,
taken from the same input with the same switch;
a layer accepts `include_archive` beside its `query`
and, unlike `query`, inherits it:
a layer that does not name it takes the layer above's, the first the call's.
`query` differs per layer because the children can be of other types;
whether the archived show is one choice for the whole view.
A show's side table takes it per table ([views](views.md)).

A relation's picker is the finder over search,
which leaves the archived out too,
and lists the field's current value whatever its state,
since the picker opens on what the cell showed.
The picker takes no switch: nobody assigns an archived issue on purpose.

## The schema in a program

### `$schema` is bound on every run

Every program runs with `$schema` bound
to the document `git work schema export --include-archive --format json` prints:
the archived types, fields and values too, marked,
since an issue may hold one, and a value keeps its ordinal and category in its archive
([schema, "Export hides what is archived"](schema.md#export-hides-what-is-archived)).

```jq
$schema.types[.fields.type].fields.priority.values | map(.id)
```

An issue holds a value's id, not what the schema says of it;
without this a flow would have to export the schema in Starlark
and interpolate its values into the program,
and a schema's `show` spec, which has no Starlark, could not do it at all.

It is bound at run, not at compile,
so a program compiled once — a view's, a layer's —
reads the schema as it is on each re-run,
and an import under an open view takes effect on its next load.
`host.SchemaValue` loads and exports the schema on every run:
nothing caches a schema load to hang an export on,
and every other read, the write path's included, loads it afresh.
Package `jq` takes the value and knows nothing of package `schema`.

Under `--at` it is today's schema:
the schema has no history the replay reads,
and a value it no longer has reads as unknown.

A declared variable must be bound on every run,
so every caller passes it.
A nil `$schema` is allowed,
where every helper reads null:
a TUI layer whose schema does not load still runs.
The command line and Starlark fail on a schema that does not load,
as any read of it does.

### `ordinal`, `category` and `done`

Three functions over `$schema`, written in jq:

| Function | Input | Gives |
| --- | --- | --- |
| `ordinal(f)` | an issue | the position, from 0, of `.fields[f]` in the values of field `f` of the issue's type |
| `category(f)` | an issue | the category of `.fields[f]`, as the schema declares it |
| `done` | a category | whether work in it is over either way: `completed` or `canceled` |

```jq
sort_by((category("status") | done), ordinal("priority") == null, ordinal("priority"), .fields.title)
```

orders open work first, by priority, unset last,
on any type, with whatever values the schema has.
The parentheses are needed: `|` binds looser than `,`.

`ordinal` reads any single-valued enum, `enum` as well as `ordinal-enum`:
an `enum`'s values are listed in workflow order too.
It is a jq number and prints as `0`.
`done` is `schema.Category.Done` said in jq,
and a test pins the two to one set;
it takes the category, not the issue, so it names no field.

The definitions are put before the program's own,
not parsed with it,
so a program's error positions are its own,
and a program that defines its own `done` or `category` keeps it:
gojq resolves a name to its last definition.

### Unknown is null

`ordinal(f)` and `category(f)` are `null`
when the field is unset,
when the issue's type has no field `f`,
when the field has no values (a relation, text),
when the value is not one the schema lists,
and on a `multi-enum`, whose value has no one position.
`null | done` is false.

A program runs over mixed types,
and a field one type has and another lacks is the common case;
an error there would make every mixed query guard each call.
The cost is that a misspelled field reads as unset everywhere.

### The functions name no field

The field is an argument,
and `done` reads a category, which the schema already makes a fixed set.
Nothing here learns that `status` or `priority` is special.

## Rows a query makes

A view's query may return rows that are not one-to-one with issues:
the same issue under two groups,
or a row standing for a pair such as *a person's share of an epic*,
each with its own children.
`host/rows.go` reads them;
the list and the gantt draw them,
and the board and the matrix do not.

### A row's key

A row may carry a top-level `key`, a string:
its identity on the screen.
Absent, the key is the `id`, and nothing changes.

| Concern | Reads |
| --- | --- |
| drawing order, siblings, parent links | `key` |
| fold state, `open`, the cursor across a refresh | `key` |
| show, edit, copy of the id, rank writes | `id` |
| a link cell's target | `id` |

So `{"id": "<epic>", "key": "<epic>@<person>", "fields": {…}}`
opens and edits the epic,
and is folded and returned to on its own.
Keys are unique among one view's rows, children included;
a repeated key, an empty one,
and one starting with `+`, the ghost's prefix,
are refused, naming the key.
The cells are the row's fields as the query shaped them,
and editing a cell edits the issue's stored value.

Not the issue id alone: it would do two jobs,
and an issue reached twice could be drawn only once.
A key shows once;
an issue reached twice through the store's relations, with no keys anywhere,
still shows once, since its key is its id.

### A row that stands for no issue

`id` is optional beside a `key`.
Such a row draws its `fields` and can be stood on;
show and edit ring, copy copies nothing,
and grabbing it moves it by the view's order.
It is how a `(none)` row exists:
`{"key": "none@<person>", "fields": {"title": "(none)", "type": "epic", "assignee": "<person>"}, "children": […]}`.

It names its type:
`fields.type` must be a type the schema knows, or the row is refused, naming its key,
and its fields are drawn and grouped as that type's.
Not the kind every type agrees on, because a guess across types is no type.

### A level is keyed or plain

A row is keyed when its `key` is not its `id`, or it has no `id`;
a row keyed by its own id is plain.
The rows of one level under one parent —
the roots across all their groups,
the children a row lists,
the children a layer brings under one row —
are all keyed or all plain.
A mixed level is refused, naming one key and one id from it:
the view fails to open, or a refresh says so on the status line.
A keyed level's order is the view's and a plain level's the ranks',
and one scope with two orders has no drop that keeps both.

### A row lists its children

A row may carry a top-level `children`,
an array of issue ids or of rows.
Those are its candidate children,
in place of what the layer's `relation` reads off the store for it.
An id is read from the store;
a row is taken as the query shaped it, its own `key` and `children` included,
so a query can build every level of the tree itself.
The layer below still applies: its `fields`, `details`, `group_by`,
and its `query`, which narrows listed children as it narrows a relation's.
A layer's `relation` is required only where some row above lists no children of its own.

`children` is top-level, beside `id` and `fields`, never a field,
so it cannot collide with a schema's `children` inverse;
it is the word `expand` already uses for what nests under a row.

### `open` unfolds the tree

`open` is a call argument on the list and the gantt:
`true` opens every parent,
a number that many levels from the roots,
absent or `false` the folded summary.
It sets the folds the view starts with, by key;
a refresh keeps the person's folds, not the argument's.
A call argument and not a layer key,
because how the view opens is the call's.

### A keyed row reorders for the view only

A keyed row stands for a share of an issue, or for none,
so writing the issue's `rank` would reorder every row standing for it.
It is grabbed and moved,
but the drop writes nothing:
the view instance records the scope's order by key,
which holds across refreshes until the view is quit,
a key it does not know keeping its `(rank, id)` place after the ones it does.
Nothing persists; the status line says `order kept for this view`.
A keyed row dropped outside its group rings:
writing the epic's `assignee` is not what moving a share means.

### The board and the matrix draw issues

Neither nests, so `children` means nothing there,
and a matrix sums issues, so a repeated issue would count twice.
Both ignore `key` and `children`,
drop a row with no `id`,
and draw an issue two rows stand for once, the first.

## The past

### Go replays, a flow compares

Go replays operations into a snapshot at a time;
Starlark compares snapshots as plain JSON.
There is no `report` command and no `diff` verb.

The apply rules belong to `entities/issue`,
and a flow that walked the log to rebuild a value
would be a second implementation of them, drifting with the first new operation;
replaying with the package's own `Apply` cannot drift.
What is worth reporting is policy,
which field is the status, what counts as closed,
and Go names no field, so the report is a flow ([flows](flows.md)).
Go answers *what was true then*; Starlark decides *what is worth saying*.

### The cut is wall-clock time

A window is cut on each operation's own timestamp,
its author's wall clock at the write, never its lamport time.
"Since Monday" is a statement about a calendar,
and lamport time is a partial order with no relation to a date.
An operation written on Tuesday and pulled on Friday lands in Tuesday's window,
so a window re-run after everyone has pushed gives everyone the same answer;
one run before the pull is missing data and says nothing false.
The cost is trusting a remote clock,
the trust `cache.IssuesByCreationTime` already places in it.
Operations are filtered one by one, not truncated at the first late one,
because wall clocks across clones are not monotonic in DAG order.

Not a tracker commit as the cut:
every issue is its own ref and there is no cross-issue commit to name.

### `--at`, `--from` and `--to`

A point in time is `--at`, on `issue get` and `issue list`;
a window is `--from`/`--to` on `issue log`,
half-open, `[from, to)`,
so back-to-back windows neither drop an operation nor count it twice.
An absent `--at` is now, an absent `--from` the beginning of time,
an absent `--to` the end of it.
Not git's `--since`/`--until`:
`--since` carries git's "reachable from" meaning, which does not apply here,
and `from`/`to` are what any KWARGS object in this tool would say.

In Starlark the keywords are `at`, `from_` and `to`,
taking the same strings, `None` meaning the absent flag.
`from` is a reserved word in Starlark,
so the keyword takes the trailing underscore `work.schema.import_` already carries.

### One TIME grammar

Every TIME argument is tried, in order, as
a date, `2026-09-21`, read as local midnight;
an RFC 3339 timestamp;
a duration back from now, `7d`, `2w`, `12h`.
A duration needs no date arithmetic in a script.
`host.ParseDuration` is Go's with days and weeks added,
and the one parser in the tool:
`jira sync --adopt` calls it too, prefixing only its flag's name to the error.

An iteration id is not a TIME form:
resolving one means reading its start and end fields,
which fields those are is policy,
so a flow resolves it to two timestamps and passes them down.

### `--at` is a replay

`issue.SnapshotAt` keeps an issue's operations whose time is at or before `at`,
in the DAG's order,
and applies them into a fresh snapshot with the same `Apply` `Compile` uses.
Two consequences fall out of the replay rather than being added to it.
An issue created after `at` is absent:
`issue get --at` says it did not exist yet,
and the list leaves it out,
which is what lets a flow say "created in the window".
And `archived` is whatever it was then,
so the input's filter reads that value with no special case:
an issue archived on Wednesday is in Tuesday's input and out of Thursday's,
and `include_archive` with `at` is every issue that existed then.
The same holds for every field, the title,
the comment bodies and the actors.

It is linear in the store's operations:
every issue is read and replayed once, in memory, with no index.
That is milliseconds for the hundreds of issues this tool is built for;
not a persisted historical index,
which is a cache to invalidate for a query that runs weekly.

### The excerpt at a time

A program written for the present has to read `--at` unchanged,
so the input at a time is built from the replayed snapshot
into the same `cmdjson.IssueExcerpt` shape the live input has,
and the cache, which holds the present only, is not involved.
`edit_time.lamport` is omitted:
a lamport time is the entity's, not an operation's,
so there is no honest last-edit lamport for a moment in the past;
`edit_time.timestamp` is exact,
and the default program's second key still orders by it.
`create_time` is whole, lamport included.
The id an issue is drawn by is its present alias:
an id names one issue at every time.

### The log takes a program

`issue log` returns operations, oldest first,
and takes the list's PROGRAM as well as an ID,
since a report over a hundred issues cannot be a hundred commands.
The program selects from the present input;
`--from`/`--to` then cut each selected issue's operations.
Every entry carries `issue`, the id it belongs to,
whether one issue was selected or many:
a shape that changed with the argument is one every caller branches on.

The argument is resolved as an id prefix or alias first,
and what does not resolve is compiled as a program;
if that fails too the error names both,
so a mistyped id reads as a mistyped id.
No `--query` flag:
an id prefix is lowercase hex, no bare hex word is a valid jq program,
and an alias is a Jira-shaped key, which is not one either.
An empty argument is the default program.

Across issues the order is the operation's time,
then the issue's id,
then the operation's place in its own issue,
so two operations written in the same second print in a fixed order.
An empty window is `[]`, never `null`.

## Search

### Search scans the excerpts

`git work issue search TEXT` and `work.issue.search(text)` return
the issues whose id, alias, title or description contain TEXT,
as excerpts, as `issue list` prints them;
the finder calls the same function.
The match is case-insensitive, since a finder is typed into;
the id matches by prefix, as every ID position reads it,
and the rest anywhere.
Empty text matches every issue.

The description is comment 0,
so the excerpt carries the description's text.
A few thousand descriptions of a few KB is a few MB in memory,
a substring scan over it is milliseconds,
and the excerpt is rebuilt when its issue changes,
so there is no second structure to keep coherent.
The text stays out of the JSON `issue list` prints:
a program that wants it reads the issue.

### Search order

The issue an id or alias names first,
then a match in the title,
then a match in the description alone;
within each, last edited first, the default program's order.

## Deliberately absent

- **No static check of field names** in a query against the schema:
  a program runs over mixed types, where a field one type lacks is the common case,
  so an unknown field reads as null rather than failing the query
  ([Unknown is null](#unknown-is-null)).
- **No `$parent`** in a layer query: a row's own `children` makes it unnecessary.
