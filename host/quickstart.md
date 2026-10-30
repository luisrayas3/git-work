# git-work

This document explains
how to read, create and update issues,
and where to find everything else.

## What this is

git-work is a project tracker stored in the git repository itself.
It began as a hard fork of git-bug, whose storage engine it keeps,
and is a project tracker with Jira as a first-class sync backend.
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
A relation such as `parent` takes them too,
and is stored as the full id,
so a query compares `.fields.parent` with the full id.
`human_id` is the id an issue is drawn by:
the short hash, or its Jira key when the clone sets `git-work.display.id` to `jira`.
It is accepted back wherever an id is, but it varies with that setting,
so a program keys on `id`, which never does.

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
The one exception is the bare noun:
`git work issue`, `git work flow` and `git work user` alone
are the human form of their `list`, one line per row,
so an agent lists with `git work issue list` and reads JSON.
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
`git work issue list 'PROGRAM'` runs PROGRAM
over the array of every unarchived issue as an excerpt
(`id`, `human_id`, `create_time`, `edit_time`, `fields`, `author`,
`actors`, `participants`, `comments` (a count) and `metadata`)
and prints what it emits, as JSON.
With no program the default is all of them, last edited first.
The bare `git work issue 'PROGRAM'` takes the same program and flags
and prints one line per issue (id, status, title), for a human.
`--include-archive` puts the archived issues back in the input;
a view takes it as `"include_archive": true`,
and `work.issue.list`, `work.issue.log` and `work.issue.search` as `include_archive=True`.

Every program, the listing's and every view's and layer's `query`,
runs with the schema bound as `$schema`,
the document `git work schema export --format json` prints,
and three functions over it,
so a program orders and filters by what a value means
and stays right when the schema's values change:

- `ordinal(f)` on an issue is the position, from 0,
  of its field `f`'s value in that field's values;
- `category(f)` on an issue is that value's category;
- `done` on a category is whether the work is over, `completed` or `canceled`.

Each is null where the schema says nothing:
an unset field, a type without it, an unlisted value, a multi-enum.
`$schema` is today's schema under `--at` too.
Open work first, by priority, unset last
(the parentheses are needed, since `|` binds looser than `,`):

```sh
git work issue list 'sort_by((category("status") | done), ordinal("priority") == null, ordinal("priority"))'
```

`git work issue search TEXT` finds issues without a program:
the ones whose id (by prefix), alias, title or description contain TEXT,
case ignored, as the same excerpts in one array.
The description is the first comment; the others are not searched.
The issues an id or alias names come first, then title matches,
then description-only matches, each last edited first.
`--format text` prints them one line each.

## What changed since

The store is an operation log, so the past is readable.

- `git work issue get ID --at TIME` prints the issue as it stood then,
  replayed from its operations.
  `git work issue list 'PROGRAM' --at TIME` runs the program over the issues
  as they stood then;
  an issue created after TIME is absent,
  and `archived` is the value it had at the time.
- `git work issue log ID --from TIME --to TIME`
  prints the operations written in the window, which is half-open,
  `[from, to)`.
  The cut is each operation's own clock time,
  so an operation pulled late still lands in the window it was written in.
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

- Any command explains itself: `git work issue set --help`, and so on down the tree.
- The schema is the authority on what a type's fields and values are;
  the live section at the end of this page summarizes it.
- The flows are how this repository is meant to be looked at;
  the live section lists them too.

## The schema

Types and fields are entities under `refs/work-schema`,
so they merge like issues do.

- `git work schema` prints the schema as YAML, `--format json` as JSON.
- `git work schema init [jira|linear]` creates a preset's types and fields,
  and refuses once any field exists.
- `git work schema export > FILE` and `git work schema import FILE`
  round-trip it. Import is an upsert that writes only what differs,
  `--prune` archives what the file leaves out,
  and `--dry-run` prints what it would do.
- A field's key is `TYPE/FIELD`: every field belongs to one type,
  so `task/status` and `epic/status` are two fields with two value lists.
  In the file, YAML anchors share a definition between types.
- A type's `show`, `show: {expand: [children, links]}`, is how show draws its issues
  where a view's `shows` does not say; the import checks it as a view's spec is checked.
- `git work schema log [KEY]` is a key's history,
  and `git work schema archive KEY` removes it, replicated.

Every issue write is checked against the schema, as above;
with no type defined nothing is checked.

## Flows and Starlark

A flow is one Starlark function stored under `refs/work-flows`:
its name is the flow's name, its docstring the description,
its parameters the arguments.

- `git work flow list` lists them, with their arguments.
- `git work flow run NAME [KWARGS]` runs one;
  KWARGS is one JSON object of its arguments, the signature's defaults filling the rest,
  and an unknown key is refused naming the parameters.
  `git work flow run - < FILE` runs a script without storing it.
- `git work flow import FILE|DIR` stores flows: an upsert keyed on the function's name,
  so the file's name never matters, `--prune` archives the flows not given,
  and a file that is not exactly one `def` aborts the whole import.
- `git work flow export NAME` prints a flow's source; `--all DIR` writes every one.
- Bare `git work`, in a terminal, runs the flow named `home`, as `git work flow run home` does;
  with no `home` flow stored, or no terminal, it prints the help.

A script reaches one predeclared name, `work`,
which mirrors this command line one to one, because both go through the same code:
`git work issue get ID` is `work.issue.get(id)`,
`git work issue list PROGRAM --at TIME` is `work.issue.list(program, at=…)`,
`git work view board KWARGS` is `work.view.board(**kwargs)`,
and this page is `work.quickstart()`.
Two names change because they are Starlark keywords:
`git work schema import` is `work.schema.import_(doc)`,
and `--from` is `from_=`.
The bare `git work issue` is the human form of `list` and has no Starlark name.
`print()` is a flow's standard output, a returned value is printed as JSON after it,
and `work.stderr(*values)` writes one line to standard error.
A flow writes through the same path a command does, schema check included.

## Views

Views are interactive and need a terminal; there is no browser interface.
An agent reads the data with `git work issue list 'PROGRAM'` instead,
and opens a view only for a human to look at.

There are seven kinds — `list`, `show`, `board`, `gantt`, `matrix`,
`new`, the form that creates an issue, and `home`, the flows a person starts from —
each a `git work view KIND KWARGS` command
taking one JSON object of keyword arguments (`-` reads it from standard input).
`git work view list --help` is the argument table,
and it is the reference for `work.view.list(**kwargs)` too,
because both parse against the same table.
A `required` argument has to be named,
a `defaulted` one has a value already,
and an `optional` one is off until it is named.

- Most kinds take a `query`, the same jq program the listing takes,
  re-run whenever the store changes, so a view stays live.
- `group_by` sections the rows by a field, the ones with no value last,
  the groups in the value's own order on every view —
  an enum's schema order, a relation's issues by rank —
  never the order the query met them in.
  On a board `"empty_groups":true` adds the lanes no card falls in:
  every value of an enum, both of a bool,
  and for a relation the lane issues' siblings under the same parent.
- `expand` on a list and a gantt nests the issues one relation reaches under each row,
  as `"children"` or as a layer that says what that level draws.
- `shows` says how each issue type is shown, on every kind:
  `{"story":{"fields":["status","links"],"expand":["children","links"]}}`
  maps a type key to a spec, which is what `show` takes less the id.
  `show` itself takes only `id` and `shows`;
  `Enter` on a list, a gantt, a board or a matrix opens show with the same `shows`,
  and so does every page opened from there.
  A spec's `fields` are the rows of show's fields table,
  and its `expand` a relation or a layer, or a list of them,
  each drawn as a flat table of the issues it reaches beside the fields;
  there `"links"` is a table per link relation the issue has entries in,
  and a list's or a gantt's `expand` refuses it.
  A type the call leaves out is shown by its `show` in the schema,
  else by `{"expand":["links"]}`, the whole spec and never merged,
  drawn dim on the first line as a defaulted argument is.
  `esc e` on show toggles what its type's spec expands,
  among the type's relations and `links`,
  for that page and the pages it opens: nothing is written,
  and the changed spec is part of the call on the first line.
- A relation field marked `link: true` in the schema
  (`blocks`, `clones`, what a Jira link type becomes) is a link, and so is its inverse:
  show folds every link into one `links` row,
  the count and then each relation's (`3 · blocks 1 · is-blocked-by 2`), or `—`;
  `Enter` on it goes to the link tables, and `Space` picks a relation, stored or inverse,
  then an issue to add or remove — an inverse is written on the other issue.
  `esc l` is that picker from anywhere on the page,
  and `esc u` picks one of the issue's links to remove.
  A spec's `fields` naming a link field draws it as a row, and naming `links` places the row.
- On a list and a gantt a row the query makes may carry a top-level `key`,
  its identity on the screen, so one issue can be drawn twice;
  its `id` stays the issue it acts on, and may be left out beside a key,
  where `fields.type` must name a type;
  and a top-level `children`, the ids or rows nested under it
  in place of what the layer's relation reads.
  A row is keyed when its key is not its id or it has no id;
  the rows of one level under one parent are all keyed or none,
  and a keyed row's drag is kept by the view, never written.
- A nested view opens folded; `"open":true` opens every parent,
  a number that many levels from the roots.
  A row that could have children reads `▸ 0` and opens to an empty table;
  `esc z` folds or opens every parent, `esc Z` opens every row, leaves too.
- `git work view new '{"draft":{…}}'` opens the creator on an `issue new` document,
  every value still editable, and prints the id it creates;
  in a list, a gantt or a board the dim `+ (new)` row at the foot of every group,
  and of every opened parent's child table, opens it prefilled with where it stands:
  the group's values, the type the rows share, and the parent —
  where the parent's own field holds the relation, the parent is written after Create,
  and a set single relation reads `(new, replacing abc1234)`.
  A list, a gantt or a board takes `draft` too, the same document,
  which every one of those rows starts from:
  what the query decides and the row cannot see, such as the sprint.
- `git work view home '{"flows":["overview","report"]}'` draws those flows,
  each with its docstring's first line, a name the store lacks refused;
  `Enter` runs one with its defaults, its view opening over home,
  and a flow that prints shows its output on a page.
  A `home` flow calling it is what bare `git work` opens.

In a view `Enter` opens, `Space` edits or grabs a row to move it,
typing (or `/`) filters a list, a board or a gantt, and `C-q` quits.
`Esc` opens the palette: where you are, the whole call,
and every command under a letter that is the same on every page
(`w` copy cell, `c` copy id, `s` search, `n` new, `h` home, `r` run a flow, `q` quit, `g` close;
`esc x` is `alt+x`);
`esc j` edits the page's query and draws it again, and `esc J` puts back the one it was called with;
a second `Esc` goes back, and quits from the first page.
`esc s` finds any issue as `git work issue search` does, and `Enter` opens it.
A picker is typed into at once: typing narrows, `Enter` chooses,
the first `Esc` clears the text and the second cancels.
`?` lists every key; emacs's `C-` and `M-` keys work, vim's letters do not.

## Sync and Jira

- `git work pull` and `git work push` carry the tracker's namespaces over the git remote
  (`work-issues`, `work-flows`, `work-schema`, `work-users`),
  and `git work sync` does both in one run.
  Pushing publishes the tracker, so do it only when asked to.
- None of them carries git-bug's frozen store (`refs/issues/*`, `refs/identities/*`).
  To import a git-bug repository,
  `git work migrate --from REMOTE` fetches that store from the remote
  and then migrates it, once, into the tracker;
  `--dry-run` fetches it too and prints the plan.
- The tracker's refs are outside `refs/heads`,
  so any `git fetch --prune` drops their remote-tracking copies
  and the next pull fetches everything again.
  Add a fetch refspec once, one per namespace
  (`work-issues`, `work-flows`, `work-schema`, `work-users`):
  `git config --add remote.origin.fetch '+refs/work-issues/*:refs/remotes/origin/work-issues/*'`.
  Plain `git fetch` then carries the tracker, and a prune leaves it alone.
- `git work jira sync` (or `git work sync --jira`) syncs one bound clone
  with one Jira Cloud project, both ways, field by field;
  Jira wins a field edited on both sides and says so in a comment.
  A clone is bound by `git config git-work.jira.url`, `git-work.jira.project`
  and `git-work.jira.email`, with the token in `JIRA_API_TOKEN` or git's credential store.
  `git work jira schema` prints the schema the project maps to, to review and import first.
  The sync never pushes, prints one JSON object per line,
  and exits 1 when an issue failed; run it from cron, it has no daemon.
  Bind one clone per project.
- Two local copies of one Jira issue are consolidated into one,
  the other archived with `metadata["jira-consolidated-into"]` naming the survivor.

## Practicalities

- `git work user me` is the identity you write as.
  It is settled from git's `user.name` and `user.email` on the first write,
  so there is nothing to set up.
- `git` must be on the `PATH`: config and remotes go through it.
- Readers take no lock. A writer holds a short lock across one commit;
  `timed out after 5s waiting for the write lock (held by pid N)`
  means another writer really is running.

## Basic types

A field's kind says what its value has to be:

- `text` — a string.
- `enum`, `ordinal-enum` — one value id out of the field's list.
- `multi-enum` — an array of those ids.
- `bool` — `true` or `false`.
- `number` — a number.
- `date` — an RFC 3339 string, `"2026-10-02"` or a full timestamp.
- `identity`, `multi-identity` — the id of an identity, as `git work user list` lists them.
- `relation`, `multi-relation` — the id of another issue,
  of a type the field's target types allow;
  `link: true` marks one a link, which changes how a view draws it and nothing else.
  `links` is reserved: no field or inverse takes it.

`title` (text), `type` (enum), `archived` (bool) and `rank`
are built in fields on every type and cannot be removed.
`rank` is internal: a fractional index for manual order,
of a kind no other field takes, null until a view's drag writes one;
every view orders by it and no view argument names it.

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
git work issue list 'map(select(category("status") | done | not)) | group_by(.fields.type)'
```

One story's tasks, one line each for a human (`issue list` for the JSON):

```sh
git work issue 'map(select(.fields.parent == "<story id>"))'
```
