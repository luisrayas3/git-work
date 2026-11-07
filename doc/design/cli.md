# The command line

What `git work` is on the command line:
the rules every command follows,
the commands there are,
and how an issue is named on the way in and on the way out.
`git work quickstart` and each command's `--help` are the reference for using them;
this document is why they are the way they are.
The Starlark spelling of the same commands, and what a flow is, is [flows](flows.md);
what a jq program sees, the TIME forms and the past, [query](query.md);
the view kinds and their arguments, [views](views.md);
what a view does once it is open, [tui](tui.md);
the operations behind the writers, [issue](issue.md);
the schema document, [schema](schema.md);
the Jira commands, [jira](jira.md).

## Rules

### Plumbing, agent-first

Every reader prints JSON by default,
and `--format text` for a human where there is a line to draw.
There is no sugar:
no `--fields`, no `-t`/`-m`, no per-field flags, no `KEY VALUE` positionals.
A jq program projects and a flow is the porcelain,
so a second spelling of what those already do is a second thing to learn and to keep in step.
The one porcelain default is the bare noun.

### The bare noun is the human form of list

`git work issue list`, `git work flow list` and `git work user list` are the plumbing:
JSON by default, the form a script or an agent calls.
`git work issue`, `git work flow` and `git work user` alone
are the same command with text as the default:
the same program, the same flags, the same rows, one line each.
A program whose values are not issues still prints as JSON,
because there is no line to draw.
The bare form keeps `--format json`,
because only the default differs,
and a flag that worked on one spelling and not the other would be a trap.

The reason is who types what:
the easy thing to type is the thing a human reads,
and plumbing is spelled out, the way an agent spells out every other verb.
Only the bare noun flips;
`get`, `log`, `user me` and every other verb keep JSON as their default.
`git work schema` is the same pattern already:
the YAML a human reads is the bare noun,
and `schema export --format json` the machine's form.

`list` is a sub-command, never a program:
cobra resolves a sub-command name before it reads a positional,
and `list` is no jq program.
The bare noun has no Starlark name (flows.md, "What has no Starlark name").

### The command line and Starlark are one API

`git work <module> <verb>` is `work.<module>.<verb>(...)`,
with the same arguments, returning what the command prints.
It is a mechanism rather than a discipline:
package `host` is one Go function per command,
over the repository cache and JSON-shaped values,
and both `commands/` and the Starlark modules of `flow/run` call it,
so there are not two implementations to disagree.
`commands/` parses argv and formats the result, and nothing else.
Argument resolution, an id prefix or an alias, lives in `host` too,
which is what makes `work.issue.get("PROJ-12")` mean what the shell means by it.
The Starlark side, and the commands that have no Starlark name, are [flows](flows.md).

### Arguments are one JSON object

Starlark has no positional-only parameters,
so a command that takes a function's arguments takes them as one JSON object of keyword arguments,
as the argument or on standard input as `-`:
`git work flow run board '{"iteration":"current"}'`,
`git work issue set ID '{"status":"done","estimate":3}'`,
`git work view board '{"columns":"status"}'`.
Defaults in the signature fill what the object omits;
an unknown key is an error naming the parameters.

### A document is read strictly

Every JSON document a command reads,
an issue for `new`, fields for `set`, a view's arguments,
is decoded by one rule (`host.DecodeStrict`):
an unknown key is an error, never a silent no-op,
and anything after the document is an error,
so that a truncated or doubled document is refused rather than half written.
One rule, because a document typed at the shell and the same document built in a script
must be refused for the same reasons.

### A commit is a list of operations

`set`, `add` and `remove` each take an object
and commit one operation per key, all in one commit:
SetField, AddValue, RemoveValue.
`null` in `set` clears a field;
`add` and `remove` take lists and have set semantics.

Not RFC 6902 patches:
a second grammar over the same three operations,
which would bake the read shape's paths into the write API.

### `get` pairs with `set`

`get ID` returns the whole issue,
its fields, its people and its comments;
`set ID FIELDS` writes part of it.
There is no per-field getter and no `show`:
a field is `get ID | jq .fields.status`.

### Writers print ids

A writer that creates prints the id of what it created, one per line, and nothing else:
`issue new` the issue's,
`issue comment new` the comment's,
`view new` the issue a form created,
`flow import`, `schema import` and `schema init` the entities they created.
A writer that creates nothing prints nothing.
Diagnostics go to standard error, and the exit status is the result.

### `--dry-run` prints what would be committed

A writer with `--dry-run` prints the operations it would commit and writes nothing:
`issue set`, `add`, `remove` and `archive` the issue operations in their wire shape,
`schema import` and `init` and `flow import` the changes per entity, in the same shape.
The operations are checked against the schema when they are planned,
so a dry run reports what the commit would refuse;
with no type in the store there is nothing to check against,
and the dry run says so on standard error rather than reading as approval.

### `log` is one shape on every tree

`issue log`, `schema log` and `flow log` print one JSON object per operation, one per line,
and take `--format text` like every other reader.
A config operation renders by shape and key,
so `flow log` prints what `schema log` prints.
An `issue log` entry names the issue it belongs to,
because the log also takes a program over many issues (query.md).

### TIME is one grammar

`--at` on `issue get` and the list, and `--from`/`--to` on `issue log`,
take one grammar:
a date, an RFC 3339 time, or a duration back from now (`7d`, `2w`, `12h`).
One parser serves the whole tool, `jira sync --adopt` included,
so `7d` means the same wherever it is written.
What the times select is [query](query.md).

### `new` takes the issue as a document

```json
{"fields": {"title": "…", "type": "task"}, "body": "the first comment", "aliases": {"jira": "PROJ-12"}}
```

as the argument or on standard input.
A title is required and lives in `fields`, like every other property of an issue,
because a built-in is a field like the rest ([issue](issue.md)).

### Three removals, three words

`rm` is local, on every tree:
it deletes the local ref, and the entity returns on the next pull.
`archive` is replicated:
it is an operation and reaches every clone;
`issue archive ID` is `issue set ID '{"archived":true}'`, and a set back undoes it.
`delete` is replicated and final:
an operation that reaches every clone and Jira, with no undoing it,
though the history keeps what it removed;
`issue comment delete` is the one.
They are different verbs because they are different things,
and one word for two of them would make the local one look like it published.

### The list is a jq program

`issue list PROGRAM` runs a jq program over the array of issue excerpts,
the unarchived ones, the archived too with `--include-archive`.
The default program is that input, last edited first.
The program, its input and `issue search` are [query](query.md).

### The view command is the call

`git work view KIND KWARGS` is `work.view.KIND(**kwargs)` spelled for the shell.
Its whole input is that object, `query` included,
so nothing is read from standard input but the object itself,
and there is no `--format`: the input already is the serialization.
The view draws in the terminal and blocks until the user leaves it.
With no terminal it is an error, after the arguments are checked,
because the terminal is the only renderer
and an agent that wants the data runs `git work issue list PROGRAM`, where the data is.
`view new` is the one view that writes, and prints its id like every writer.
Which arguments a kind takes is a table in package `view`,
printed by `git work view KIND --help`, which is the authority ([views](views.md)).

## The map

The binary as it stands; each command's `--help` is the whole of its flags.

```
git work                                         # in a terminal, git work flow run home; else the help

git work issue [PROGRAM] [--at TIME] [--include-archive] [--format text|json]   # list's human form
git work issue list [PROGRAM] [--at TIME] [--include-archive] [--format json|text]
git work issue search TEXT [--include-archive] [--format json|text]
git work issue new DOC|-                         # prints the id
git work issue get ID [--at TIME] [--format json|text]
git work issue set ID FIELDS|- [--dry-run]       # {"key": value, ...}; null clears
git work issue add ID ITEMS|- [--dry-run]        # {"key": [item, ...], ...}
git work issue remove ID ITEMS|- [--dry-run]
git work issue archive ID [--dry-run]            # replicated
git work issue rm ID                             # local
git work issue log [ID|PROGRAM] [--from TIME] [--to TIME] [--include-archive] [--format json|text]
git work issue comment new ISSUE_ID BODY|-       # prints the comment id
git work issue comment edit COMMENT_ID BODY|-
git work issue comment delete COMMENT_ID         # replicated, final

git work schema [--format yaml|json]             # = schema export
git work schema export [--format yaml|json]
git work schema import FILE|- [--prune] [--dry-run]
git work schema init [PRESET] [--dry-run]
git work schema log [KEY | --id ID] [--format json|text]
git work schema archive KEY | --id ID
git work schema rm KEY | --id ID

git work flow [--format text|json]               # list's human form
git work flow list [--format json|text]          # names, descriptions, arguments
git work flow run NAME|- [KWARGS|-]
git work flow import FILE|DIR|-... [--prune] [--dry-run]
git work flow export NAME | --all DIR
git work flow log [NAME] [--format json|text]
git work flow archive NAME
git work flow rm NAME

git work view list|board|gantt|matrix|show|new|home [KWARGS|-]

git work user [--format text|json]               # list's human form
git work user list [--format json|text]
git work user me [--format json|text]            # the identity this repository writes as
git work user new [--name N --email E --avatar URL --non-interactive]
git work user adopt USER_ID

git work pull [REMOTE]
git work push [REMOTE]
git work sync [REMOTE] [--jira [--dry-run]] [--format json|text]
git work install [REMOTE]                        # the tracker's fetch refspecs on a remote
git work jira sync [ID...] [--dry-run] [--full [--accept-deletes]] [--adopt DURATION] [--format json|text]
git work jira schema [--format yaml|json] [-v]

git work migrate [--dry-run] [--from REMOTE]     # git-bug's store, once
git work quickstart                              # the guide and this repository's types, markdown
git work version
git work completion SHELL
```

`bug`, `label`, `termui`, `bridge` and `wipe` are git-bug's,
reading or writing its frozen `refs/issues/*` and not the tracker;
they are being deleted and are not part of the command line above.

## Aliases

### Any ID position accepts an alias

An issue's id is the hash of its create operation;
an alias is an external id beside it, a Jira key for instance.
It is stored as `alias:<name>` metadata on the create operation,
the one place in the entity that can never change,
and an issue carries one per external system.
Every `ID` position resolves an id prefix first and an alias after,
so a key pasted from a browser works anywhere.
A relation value takes an id prefix or an alias like any `ID` position,
and is stored as the full id it resolves to, on every write, the Jira pull's included,
so a key never reaches the store as a value.

### `git-work.display.id` draws an alias in place of the hash

```sh
git config git-work.display.id jira     # draw the jira alias where an issue has one
git config --unset git-work.display.id  # the hash, the default
```

The value is `hash`, which an absent key also means,
or the name of an alias namespace, the `<name>` of `alias:<name>`.
It is read through the `git` CLI (package `gitcli`),
so it may live in the repository's config, the global one or an `[include]`,
and git decides which wins:
per clone by default and per person at most,
which is the right reach for a way of reading,
since it changes nothing anybody else sees.
It does not depend on the Jira binding:
a clone that only pulls from a bound clone has the same aliases and may want to read them.

There is no per-call argument.
A view's arguments are the view and travel in a flow to every clone,
and a way of reading is not part of what the view is;
a flag on every text command would be a dozen flags for one preference.
The per-call override exists without either:
`git -c git-work.display.id=hash work issue`,
because git hands `-c` on in `GIT_CONFIG_PARAMETERS`
and `gitcli` runs `git config` under that environment.

### The namespace, not Jira

The value names an alias namespace rather than switching Jira on,
because aliases are keyed by system already,
so another tracker's binding needs nothing here.
One namespace, not an ordered list:
one clone is bound to one tracker,
an issue with aliases in two systems is a case nobody has,
and one value is the simplest thing to build and to explain.

### An alias is drawn only where it names that issue

An issue is drawn by its alias in the chosen namespace
if and only if that alias, typed back into any id position, resolves to that issue;
otherwise by its short hash.
One rule covers every case where the two could disagree:

| case | drawn as | why |
| --- | --- | --- |
| linked issue, one copy | `PROJ-12` | the key resolves to it |
| not exported yet | `3f9a1c2` | it has no alias |
| excluded from the sync (`aliases: {jira: ""}`) | `3f9a1c2` | the empty alias is "never sync", not a name |
| a consolidated duplicate | `3f9a1c2` | the key names the survivor |
| two unconsolidated copies of one key | `3f9a1c2` each | the key is ambiguous until a sync settles them |
| an all-hex alias that is another issue's prefix | `3f9a1c2` | a prefix is resolved before an alias |

The rule is what makes copying what is drawn safe:
anything drawn as an id can be pasted back.
It costs one index of aliases, rebuilt when the excerpts change,
from the create metadata the excerpts already carry.

### A consolidated duplicate is drawn by its hash

After a sync consolidates two local copies of one Jira issue,
both create operations still carry the alias, because create metadata is immutable;
the loser is archived and stamped `jira-consolidated-into`.
An alias resolves to the one issue carrying it that is not so stamped,
and is ambiguous only when more than one unstamped issue carries it,
the window before a sync consolidates them.
So the survivor is drawn by the key,
and the loser, which the key no longer names,
by its hash wherever `include_archive` brings it back.

### A hash is drawn faint

In the terminal an id column is drawn faint,
and an alias drawn under the setting at full strength,
so the issues with no name in the namespace asked for stand out at no width cost.
The renderer needs no second rule to tell them apart:
with a namespace set, a `human_id` that is a prefix of the row's `id` is a hash.
Plain text has no styling;
there the shapes differ, lowercase hex against a key.

### A key is never cut

A hash prefix may be shortened, because every prefix still names the issue;
`PROJ-12` cut from `PROJ-123` names a different issue.
So the id column is the widest id drawn in the table, at least seven cells,
measured over every row of the layer, hidden or drawn,
so folding and filtering move nothing;
ids are left-aligned, the hashes padded to the keys.
A gantt's label column and a board card's id line take the same measure.
When a sync links an issue the ref watcher redraws it,
and the column may widen by a cell or two.

### `human_id` carries the drawn id; `id` never changes

The JSON every reader prints keeps `id`, the full hash, unconditionally.
It is the identity:
what a program compares and stores,
what `(rank, id)` sorts on,
what a relation value holds
and what a writer prints,
and it must not depend on a preference of the clone an agent happens to run in.
`human_id` follows the setting:
on an excerpt, in `issue get`'s document and in a snapshot replayed `--at` a time,
it is the drawn id.
It has only ever been a rendering, a truncation accepted back as an id,
and every reader that draws an issue draws it from there,
so moving the choice into that one field moved all of them with no second rule;
a query that makes its own rows carries `.human_id` across and is drawn the same way.
A flow sees the `human_id` its clone's setting gives, as the command beside it does,
with no name of its own in the API.

Not a new key such as `display_id` beside an unchanged `human_id`:
it would vary per clone exactly as much,
and add a second name every reader must switch to.

Only the issue's own `human_id` follows.
A comment's, an operation's and a config entity's stay hashes:
nobody reads Jira's comment ids,
and the schema and flows are named by key already.
The alias is the present one even under `--at`,
because an id names the same issue at every time.

### What is drawn follows; what is a value does not

Every place a person reads an issue named follows the setting:
the bare `git work issue`, `issue log --format text`'s issue column,
a list's id column, a board card, a gantt label,
a relation cell, a matrix's relation axis, show's children and lead,
a relation's picker, the status line,
and the report flow's markdown.
`issue get --format text` draws both, `PROJ-9 (3f9a1c2)`,
because it is the one page about a single issue,
and where a person maps a key to the hash an agent printed.
The `/` filter matches the drawn text, as it does every other cell.

Out of reach of the setting, because each is a value or a diagnostic and not a name for a reader:
a jq program's `.id` and relation values,
so a query compares hashes;
the arguments of a call, copied as it was called;
a writer's output, and a new issue has no alias anyway;
and errors, warnings and the Jira sync's lines,
which are mostly about two copies sharing a key,
where the key is exactly what does not distinguish them.

### Copy copies what is drawn

Every copy of an issue's id copies what is shown:
with an alias drawn, the alias,
because the point of drawing `PROJ-12` is that it is the name a person carries elsewhere,
into Jira, a chat or a commit message,
and it is accepted back at every id position;
where the hash is drawn, the full hash.

### Order stays on the hash

`(rank, id)` orders every view, and the default program orders by edit time;
neither reads `human_id`.
An order that changed with a person's setting would put rows in a different place on two clones,
and a rank written between two neighbours is computed from the neighbours the writer saw,
so a drop on one clone would land somewhere else on another.
Sorting by key is a jq program's business: `sort_by(.human_id)`.

## Not done

- **Following a Jira move.**
  An alias is the key at link time and is never refreshed;
  after a move the old key is drawn, still resolves here, and redirects in Jira.
  That is the sync's question.
- **Jira keys as entity ids.**
  The id stays the hash; an alias sits beside it.
- **A mixed commit.**
  No caller needs set, add and remove in one commit,
  so there is no verb for one.
