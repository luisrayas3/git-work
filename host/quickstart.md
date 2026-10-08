# git-work

This document explains
how to read, create and update issues,
and where to find everything else.

## What this is

git-work is a project tracker stored in the git repository itself.
It is a fork of `git-bug`, grown into project management
with Jira as a sync backend.
It installs as a git subcommand,
so every command below starts `git work`.

## How it is stored

Every issue is an entity:
a DAG of operations under its own ref, `refs/work-issues/<id>`.
The operations are a CRDT and the refs are ordinary refs,
so `git fetch` and `git push` merge two clones' edits of one issue
without a conflict and without losing either.
An id is a hash.
Wherever a command takes one it also takes an unambiguous prefix,
or an alias — an external key such as a Jira issue key,
recorded on the create operation, which is the one part that never changes.

Four namespaces, one kind of thing each:

- `refs/work-issues/*` — the issues.
- `refs/work-schema/*` — the types and their fields.
- `refs/work-flows/*` — the flows, one Starlark function each.
- `refs/work-users/*` — the identities.

`schema.yaml` and the `.star` files in the working tree are authoring copies,
merged by git and applied by `git work schema import` and `git work flow import`.
Nothing reads them at run time: the refs are the truth.

There is no multi-entity commit.
A relationship between issues — a parent, a dependency —
is a field holding the other issue's id,
so it is eventually consistent, not transactional.

## The command model

`git work issue` is plumbing, written for an agent.
JSON goes in and JSON comes out;
`--format text` on a reader prints it for a human instead.
A document argument is read from standard input when it is `-`.
A writer prints an id or nothing at all, and diagnostics go to stderr.

- `git work issue new DOC` takes one JSON document,
  `{"fields": {…}, "body": "the first comment", "aliases": {"jira": "PROJ-12"}}`,
  and prints the new id.
  `fields.title` and `fields.type` are required;
  every other key of `fields` is a field of that type.
- `git work issue get ID` prints one issue whole:
  id, fields, author, participants, comments.
- `git work issue set ID '{"key": value, …}'` replaces fields,
  one operation per key and one commit however many keys there are.
  `null` clears a field.
- `git work issue add ID '{"key": [item, …]}'`
  and `git work issue remove ID '{"key": [item, …]}'`
  are for the list-valued fields, with set semantics.
- `git work issue comment new ID BODY` prints the new comment's id,
  and `git work issue comment edit COMMENT_ID BODY` rewrites one.
- `git work issue log ID` is the issue's history,
  one JSON object per operation: what changed, by whom, when.
- `git work issue archive ID` is the replicated removal.
  `git work issue rm ID` drops the local ref only, and a pull brings it back.
- `--dry-run` on `set`, `add`, `remove` and `archive`
  prints the operations the call would commit, and writes nothing.

Every write is checked against the schema before anything is committed,
and every problem is reported at once rather than the first one:
an unknown key is refused naming the fields the type does have,
an enum value naming the values the field accepts,
a relation naming the types it may point at.

The listing is a jq program.
`git work issue 'PROGRAM'` runs PROGRAM
over the array of every unarchived issue as an excerpt
(`id`, `human_id`, `create_time`, `edit_time`, `fields`, `author`,
`actors`, `participants`, `comments` (a count) and `metadata`)
and prints what it emits.
With no program the default is all of them, last edited first.
`--include-archive` puts the archived issues back in the input;
a view takes it as `"include_archive": true`,
and `work.issue.list` and `work.issue.log` as `include_archive=True`.

## What changed since

The store is an operation log, so the past is readable.

- `git work issue get ID --at TIME` prints the issue as it stood then,
  replayed from its operations.
  `git work issue 'PROGRAM' --at TIME` runs the program over the issues
  as they stood then;
  an issue created after TIME is absent,
  and `archived` is the value it had at the time.
- `git work issue log ID --from TIME --to TIME`
  prints the operations written in the window, which is half-open,
  `[from, to)`.
  The log also takes the list's program in place of an id,
  and then every selected issue's operations come back,
  each entry naming the issue it belongs to.
- TIME is a date (`2026-09-21`),
  an RFC 3339 time,
  or a duration back from now (`7d`, `2w`, `12h`).

The report is a flow over those three:

```sh
git work flow run report '{"from_":"7d"}'
```

It prints markdown — a summary line, then what was created, closed, changed
and commented on, grouped by parent —
meant for a human to scan or for an AI to summarize.
`'{"iteration":"<id>"}'` takes the window from an iteration's dates instead,
and `'{"query":"PROGRAM"}'` chooses the issues.
`from_` carries a trailing underscore because `from` is a reserved word
in Starlark, the same reason `work.schema.import_` does.

## Where everything else is

- The schema is the authority
  on what a type's fields and available values are:
  `git work schema` prints it as YAML, `--format json` as JSON.
  The live section below summarizes it.
- Any command explains itself: `git work issue set --help`, and so on down the tree.
- Flows are the porcelain, one Starlark function each:
  `git work flow` lists them with their arguments and
  `git work flow run NAME` runs one,
  taking its arguments as one JSON object.
- Views are interactive and need a terminal.
  An agent reads the data with `git work issue 'PROGRAM'` instead.
  There are five kinds — `list`, `show`, `board`, `gantt` and `matrix` —
  each a `git work view KIND KWARGS` command
  taking one JSON object of keyword arguments.
  `git work view list --help` is the argument table,
  and it is the reference for `work.view.list(**kwargs)` too,
  because both parse against the same table.
  A `required` argument has to be named,
  a `defaulted` one has a value already,
  and an `optional` one is off until it is named.
  Most kinds take a `query`, the same jq program the listing takes;
  `group_by` sections the rows by a field, the ones with no value last;
  and `expand` nests the issues one relation reaches under each row,
  as `"children"` or as a layer that says what that level draws.
  On a list and a gantt a row the query makes may carry a top-level `key`,
  its identity on the screen, so one issue can be drawn twice;
  its `id` stays the issue it acts on, and may be left out beside a key;
  and a top-level `children`, the ids or rows nested under it
  in place of what the layer's relation reads.
  A nested view opens folded; `"open":true` opens every parent,
  a number that many levels from the roots.
- Starlark mirrors this command line one to one,
  because both go through the same code.
  `work` is the only predeclared name:
  `git work issue get ID` is `work.issue.get(id)`,
  `git work schema import` is `work.schema.import_(doc)`
  because `import` is a Starlark keyword,
  and this page is `work.quickstart()`.
- Sync: `git work pull` and `git work push` carry every namespace
  over the git remote, `git work sync` does both in one run,
  and `git work jira sync` (or `git work sync --jira`) syncs a bound clone
  with a Jira project, where Jira is canonical.
- `git work user me` is the identity you write as.
  It is settled from git's `user.name` and `user.email` on the first write,
  so there is nothing to set up.

## Basic types

A field's kind says what its value has to be:

- `text` — a string.
- `enum`, `ordinal-enum` — one value id out of the field's list.
- `multi-enum` — an array of those ids.
- `bool` — `true` or `false`.
- `number` — a number.
- `date` — an RFC 3339 string, `"2026-10-02"` or a full timestamp.
- `identity`, `multi-identity` — the id of an identity, as `git work user` lists them.
- `relation`, `multi-relation` — the id of another issue,
  of a type the field's target types allow.
- `rank` — a fractional index for manual order; leave it to a view to write.

`title` (text), `type` (enum), `archived` (bool) and `rank` (rank)
are built in fields on every type and cannot be removed.
A `rank` starts null and stays null until a view's drag writes one.

## Examples

Create a task under a story, and print its id:

```sh
git work issue new '{"fields":{"title":"Rebuild the index on pull","type":"task","status":"to-do","parent":"<story id>"},"body":"Why this is worth doing."}'
```

Close it, then reopen it:

```sh
git work issue set <id> '{"status":"done"}'
git work issue set <id> '{"status":"to-do"}'
```

Comment on it, with the body on standard input:

```sh
git work issue comment new <id> - <<'EOF'
Landed; the index is rebuilt from the ref diff now.
EOF
```

Everything still open, grouped by type:

```sh
git work issue 'map(select(.fields.status != "done")) | group_by(.fields.type)'
```

One story's tasks:

```sh
git work issue 'map(select(.fields.parent == "<story id>"))' --format text
```
