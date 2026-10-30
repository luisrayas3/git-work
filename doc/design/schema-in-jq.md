# The schema in jq: `$schema`, `ordinal`, `category`, `done`

**Outcome:** a jq program — on the command line, in a flow, under a view,
in a schema's `show` spec — can order and filter by what the schema says
a field's value means (its place in the field's values, its category)
without being handed a copy of the schema,
so a program written once stays right when the schema's values change.

**Serves:** `a3a8dc9`, the schema in jq.

**Status:** proposed 2026-10-30.

## Problem

A program runs over the issue JSON and nothing else (`query/jq/jq.go`).
An issue holds a value's id, `"priority": "high"`, `"status": "done"`,
and not what the schema says about it:
that `high` comes before `low`, that `done` is a `completed` category.
The rule the schema states, that "is this done" reads the category,
never the name (`schema.yaml`), cannot be written in jq at all.

Flows work around it.
A flow reads `work.schema.export()` in Starlark,
builds a map of each type's closed values and each type's priority order,
and interpolates them into the program with `%s`:

```python
closed = ",".join(['"%s":[%s]' % (key, quoted(done_ids(key))) for key in types])
query = '{%s} as $closed | map(select(.fields.status as $s | $closed[.fields.type] | index($s) | not))' % closed
```

Every flow that sorts by priority or hides closed work repeats that block.
A schema's `show` spec (`app.md` A7) cannot do it at all, since it has no Starlark:
a side table ordered by priority, closed rows last,
has to carry the schema's values written out as literals,
and goes stale the next time a value is added.

## J1 — The schema is in every program, as `$schema`

Every program runs with one variable bound, `$schema`:
the document `git work schema export --format json` prints
(`schema.Export`, `schema/document.go`).

```jq
$schema.types[.fields.type].fields.priority.values | map(.id)
```

It is bound at run, not at compile,
so a program compiled once — a view's `query`, a layer's, a side table's —
reads the schema as it is on each re-run,
and a schema import under an open view takes effect on its next load.
Package `jq` takes the value and knows nothing of the schema package;
`host` supplies it from the cache's loaded schema,
the way it supplies the input.

It is bound everywhere a program runs, because there is one evaluator
and the one-API rule holds:
`git work issue list`, `issue log PROGRAM`, every view's `query`,
a layer's `query`, a side table's `query`,
and the Starlark mirrors of each.
Under `--at` it is today's schema, not the schema then:
the schema has no history the replay reads (`report.md`),
and a value the schema no longer has reads as unknown (J3).

## J2 — Three functions over it

Defined in jq over `$schema`, prepended to every program,
so they are ordinary jq and can be read as such:

| Function      | Input      | Gives                                                                                |
| ------------- | ---------- | ------------------------------------------------------------------------------------ |
| `ordinal(f)`  | an issue   | the position, from 0, of `.fields[f]` in the values of field `f` of the issue's type |
| `category(f)` | an issue   | the category of `.fields[f]`, as the schema declares it                              |
| `done`        | a category | whether work in it is over, either way: `completed` or `canceled`                    |

```jq
sort_by((category("status") | done), ordinal("priority") == null, ordinal("priority"), .fields.title)
```

orders open work first, by priority, unset priorities last, then title,
on any type, with whatever values the schema has.
That is the program a `show` spec's children table wants,
and the one the flows above build by hand.
The parentheses are needed: `|` binds looser than `,`,
so without them every later key runs on the category.

`ordinal` reads any single-valued enum, `enum` as well as `ordinal-enum`:
an `enum`'s values are listed in workflow order too,
and "status after `in-progress`" is a question flows ask.

`done` is `schema.Category.Done` (`schema/kind.go`) said in jq.
It takes the category, not the issue,
so it names no field, and a test pins its set to `Done`'s.

## J3 — Unknown is null, never an error

`ordinal(f)` and `category(f)` are `null`
when the field is unset,
when the issue's type has no field `f`,
when the field has no values (a relation, text),
when the value is not one the schema lists,
and on a `multi-enum`, whose value has no one position.
`null | done` is false.

A program runs over mixed types,
a story's children of two types, say,
and a field one type has and another lacks is the common case, not a mistake;
an error there would make every mixed query guard each call.
The cost is that a misspelled field reads as unset everywhere.
A view could check the literal field names in its queries against the schema
the way it checks `fields`; that is left open (J5).

## J4 — No field roles

Go names no field: the functions take the field as an argument,
and `done` reads a category, which the schema already makes a fixed set.
Nothing here learns that `status` or `priority` is special.

## J5 — Out of scope

- **Names.** A `label(f)` giving a value's display name is the same shape
  and can follow when a program wants it.
- **Rank.** Manual order is already the built-in `rank` field (`rank/`).
- **Checking field names statically** in a query (J3).

## Implementation

- `query/jq`: `Compile` declares `$schema` (`gojq.WithVariables`)
  and puts the J2 definitions before the parsed program's own function definitions,
  so a user's own error positions are unchanged,
  and a program that defines its own `done` or `category` keeps it:
  gojq resolves a name to its last definition, so appended helpers would shadow the program's;
  `Program.Run` and `Run` take the schema value beside the input.
  A declared variable has to be bound on every run,
  so every caller passes it, even one whose program never reads it.
  `selectIssues` (`host/issue_at.go`) only compiles, to tell a program from an id,
  and needs nothing more.
- `host`: `IssueListAt` and the log's PROGRAM form pass `schema.Export` of the loaded schema,
  cached per schema load rather than exported per run.
- `tui`: the layer query (`runLayerQuery`, `tui/nest.go`)
  and the side table query (`tui/show_side.go`) pass it too.
- `host/quickstart.md` gains the three functions and `$schema`
  where it introduces the program; `go generate` for the command docs.
- Tests: each function on a missing field, an unknown value, a `multi-enum`,
  a value of each category; `done` against `Category.Done` for every category;
  a compiled program re-run after a schema import sees the new order.
