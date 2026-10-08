# The command line

The target `git work` command line,
decided 2026-09-23 as the map to build toward,
and the rules every command follows.
The tables in `AGENTS.md` describe what the current binary verifies;
this document is where it is going.
Decisions are recorded on `e8d6426` (plumbing), `b511c63` (flows),
`52a2797` (views), `84dfbde` and `8b06191` (renderers), `3556569` (schema)
and `0aeb7d2` (the bare noun).
What a view does once it is open —
the keys, editing, grab, rank and nesting —
is `terminal-renderer.md`; this document is the command line it is reached by.

## Rules

- **Plumbing, agent-first.**
  JSON out by default, `--format text` for a human.
  There is no sugar: no `--fields`, no `-t`/`-m`, no per-field flags.
  A jq program projects, a flow is the porcelain.
  The one porcelain default is the bare noun, below.
- **The bare noun is the human form of `list`** (decided 2026-10-08, `0aeb7d2`).
  `git work issue list`, `git work flow list` and `git work user list`
  are the plumbing: JSON by default, the form a script or an agent calls.
  `git work issue`, `git work flow` and `git work user` alone
  are the same command with text as the default:
  the same program, the same flags, the same rows,
  one line each, as `list --format text` prints them;
  a program whose values are not issues, an empty array among them,
  still prints as JSON, because there is no line to draw.
  The bare form keeps `--format json`, because only the default differs
  and a flag that worked on one spelling and not the other would be a trap.
  The reason is who types what:
  the easy thing to type is the thing a human reads,
  and plumbing is spelled out, the way an agent spells out every other verb.
  Only the bare noun flips; `user me`, `get`, `log` and every other verb
  keep JSON as their default.
  `git work schema` already worked this way, the YAML a human reads
  being the bare noun and `schema export --format json` the machine's form.
  `list` is a sub-command, never a program:
  cobra resolves a sub-command name before it reads a positional,
  as it always has for `get` or `log`, and `list` is no jq program.
- **The Starlark host API mirrors the command line one to one.**
  The SDK is one module, named after the binary:
  `git work <module> <verb>` is `work.<module>.<verb>(...)`,
  same arguments, returning what the command prints.
  Nothing is reachable from a script that the shell cannot reach, and the reverse;
  there is no script-only name.
  The bare noun is the human form of `list`, a porcelain default with no
  Starlark name: `work.issue.list()`, `work.flow.list()` and `work.user.list()`
  mirror `issue list`, `flow list` and `user list`, and return the raw value,
  because a script reads data and never wants the text.
  One predeclared name rather than five
  leaves `issue`, `flow`, `schema`, `view` and `user` free for a script's own locals.
- **Arguments are keyword arguments.**
  Starlark has no positional-only parameters,
  so a command that takes a function's arguments takes them as one JSON object,
  `git work flow run board '{"iteration":"current"}'`,
  `git work issue set ID '{"status":"done","estimate":3}'`.
  Defaults in the signature fill what the object omits;
  an unknown key is an error naming the parameters.
- **A commit is a list of operations**, and that is the write unit.
  `set`, `add` and `remove` each take an object and commit one operation per key,
  of one type: SetField, AddValue, RemoveValue.
  A mixed commit has no consumer today;
  if one appears, `patch ID OPS` with the operations in their wire shape
  is the superset and the three verbs stay as its special cases.
  RFC 6902 is not used: it is a second grammar over the same three operations
  and bakes the read shape's paths into the write API (revises `e8d6426`).
- **`get` pairs with `set`** at the document level:
  `get ID` returns the whole issue, `set ID FIELDS` writes part of it.
  There is no per-field getter and no `show`;
  a field is `get ID | jq .fields.status`.
- **Writers print ids only.**
  `new` prints the issue id, `comment new` the comment id, one per line,
  nothing else on stdout.
  Mutators that create nothing print nothing;
  diagnostics go to stderr and the exit status is the result.
- **`log` is one shape on all three trees.**
  `issue log`, `schema log` and `flow log` print one JSON object per operation, one per line,
  and take `--format text` like every other reader;
  a config operation renders by shape and key,
  so `flow log` prints what `schema log` prints.
- **`--dry-run`** on every writer that has one prints the operations it would commit.
- **TIME is one grammar** wherever it appears —
  `--at` on `issue get` and the list, `--from`/`--to` on `issue log`:
  a date, an RFC 3339 time, or a duration back from now (`7d`, `2w`, `12h`).
  A window is half-open, `[from, to)`,
  and the cut is each operation's own wall clock, never its lamport time
  (`report.md`).
- **`new` takes the issue as a document**,
  `{"fields": {"title": "…", "type": "task"}, "body": "the first comment", "aliases": {"jira": "PROJ-12"}}`,
  as the argument or on standard input.
  A title is required and lives in `fields`, like every other property of an issue;
  an unknown key at the top level is an error, not a silent no-op.
- **Any `ID` position accepts an alias**, a Jira key for instance (`483dbe2`).
  An alias is stored as `alias:<name>` metadata on the create operation,
  the one place in the entity that can never change,
  and an issue carries as many as it has external systems.
  A relation value takes an id prefix or an alias like any `ID` position,
  and is stored as the full id it resolves to (2026-10-08, `2086c12`).
- **`rm` is local, `archive` is replicated**, on every tree.
  `rm` deletes the local ref and the entity returns on the next pull;
  `archive` is an operation and reaches every clone.
- **The list is a jq program** (`3c9c24d`) over the array of excerpts,
  the same JSON `git work issue list` prints.
  Its input is the unarchived issues, the archived too with `--include-archive`,
  and the default program is that input, last edited first;
  `.` is the whole input (`df6ff51`, `include-archive.md`).
- **Views are a module, not a surface** (revised 2026-09-24).
  A view is an atomic capability, and **flows call views; views never call flows**.
  `work.view.board(columns="status")` renders:
  it draws the board, handles its own interaction,
  writes its own edits through the host —
  a card moved between columns sets the field `columns` names —
  and **blocks on the script's thread**,
  returning only when the user quits,
  so the language never sees concurrency.
  The return value is the user's answer, `None` for every kind today.
  Two panes at once would be a composite view, `work.view.split(...)`, not two views at once.
  A backend that lacks a view kind fails naming itself,
  so no capability exists on one surface and not the other.
- **The command is the spec.**
  A view's whole input is one KWARGS object,
  the same object `work.view.KIND(**kwargs)` takes,
  so `git work view board '{"columns":"status"}'` is that call spelled for the shell.
  Nothing is read from standard input and nothing is printed:
  the input already is the serialization,
  so `git work view` has no `--format`
  and `{"view", "bindings", "items"}` is gone.
  No TTY is an error, the terminal being the only renderer (no GUI, 2026-10-08);
  an agent that wants the data runs `git work issue list PROGRAM`, where the data is.
  Every kind but `show` takes `query`, a jq program the view runs, re-runs on a
  ref-watcher change and after its own writes, which is what keeps a view live.
  Which other arguments a kind takes, and which of them it cannot do without,
  is a table in package `view`, read by the view functions, the help and every backend,
  and printed by `git work view <kind> --help`, which is the authority.
  Only what a kind cannot do without is worth repeating here:
  `list` requires nothing, `board` `columns`, `gantt` `start` and `stop`, `show` `id`.
  Every argument that names a field is one field key, because there are no field roles:
  a script names the fields it means when it calls the view.
  `terminal-renderer.md` is what each one does.

## Map

```
git work issue list [PROGRAM] [--at TIME] [--include-archive] [--format json|text]
git work issue [PROGRAM] [--at TIME] [--include-archive] [--format text|json]   # list's human form, one line each
git work issue new DOC|-                        # prints the id
git work issue get ID [--at TIME]               # the issue as it stood then, replayed
git work issue set ID FIELDS|- [--dry-run]      # {"key": value, ...}; null clears; one SetField per key, one commit
git work issue add ID ITEMS|- [--dry-run]       # {"key": [item, ...], ...}; set semantics
git work issue remove ID ITEMS|- [--dry-run]
git work issue comment new ISSUE_ID BODY|-      # prints the comment id
git work issue comment edit COMMENT_ID BODY|-
git work issue log [ID|PROGRAM] [--from TIME] [--to TIME] [--include-archive]  # half-open [from, to); each entry names its issue
git work issue archive ID                       # first class on every tree; = set ID '{"archived":true}'
git work issue rm ID

git work schema [--format yaml|json]
git work schema init [PRESET]
git work schema import FILE|- [--prune] [--dry-run]
git work schema export [--format yaml|json]
git work schema log [KEY | --id ID]             # a key is every entity that held it, archived too
git work schema archive KEY | --id ID           # a key two entities hold is refused, naming both ids
git work schema rm KEY | --id ID                # --id is an id or unique prefix, never read as a key

git work flow list [--format json|text]         # names, descriptions, arguments
git work flow [--format text|json]              # list's human form: name and description
git work flow run NAME|- [KWARGS|-]              # - runs the script on stdin without importing it
git work flow import FILE|DIR|-... [--prune] [--dry-run]   # one def per file; its name is the flow's
git work flow export NAME > FILE
git work flow export --all DIR
git work flow log [NAME]
git work flow archive NAME
git work flow rm NAME

git work view list   [KWARGS|-]                 # requires nothing
git work view board  [KWARGS|-]                 # requires columns
git work view gantt  [KWARGS|-]                 # requires start and stop
git work view matrix [KWARGS|-]                 # requires rows and columns
git work view show   [KWARGS|-]                 # requires id
git work view new    [KWARGS|-]                 # requires nothing; prints the created id
                                                # git work view KIND --help is the whole argument list

git work push
git work pull
git work migrate [--dry-run]                    # ran once here on 2026-09-25 (bf6f392); stays to import git-bug repos (01c6231)

git work bridge configure|pull|push|rm          # Jira, phase 3
git work user list [--format json|text]
git work user [--format text|json]              # list's human form: id and name
git work user me                                # the identity this repository writes as
git work user new | adopt ID
git work quickstart                             # the model and this repository's types, markdown, for an agent
git work version
git work completion SHELL
```

## A flow

```python
def board(iteration="current"):
    """Kanban of one iteration, a column per status."""
    work.view.board(query='map(select(.fields.iteration == "%s"))' % iteration,
                    columns="status")
```

The same kanban with no flow at all:

```sh
git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'
```

## Starlark

`work` is the only predeclared name, and the whole SDK hangs off it:

`work.issue.list(program, at=None, include_archive=False)`, `work.issue.new(doc)`, `work.issue.get(id, at=None)`,
`work.issue.set(id, **fields)`, `work.issue.add(id, **items)`, `work.issue.remove(id, **items)`,
`work.issue.comment.new(id, body)`, `work.issue.comment.edit(id, body)`,
`work.issue.log(id, from_=None, to=None, include_archive=False)`, `work.issue.archive(id)`, `work.issue.rm(id)`;
`from` is a reserved word in Starlark, so that keyword carries the same
trailing underscore `work.schema.import_` does;
`work.schema.export()`, `work.schema.import_(doc, prune=False, dry_run=False)`,
`work.schema.init(preset="jira", dry_run=False)`,
`work.schema.log(key=None, id=None)`, `work.schema.archive(key=None, id=None)`,
`work.schema.rm(key=None, id=None)`, each taking at most one of the two, by keyword only
(`schema-archive-id.md`);
`import` is a reserved word in Starlark,
so that one verb is spelled with a trailing underscore;
`work.flow.list()`, `work.flow.export(name)`, `work.flow.run(name, **kwargs)`,
`work.flow.import_(scripts, prune=False, dry_run=False)`,
`work.flow.log(name="")`, `work.flow.archive(name)`, `work.flow.rm(name)`;
`work.view.list(...)`, `work.view.board(...)`, `work.view.gantt(...)`, `work.view.show(id, ...)`;
`work.user.list()` and `work.user.me()`, which are `git work user list` and `git work user me`.
`work.issue.list`, `work.flow.list` and `work.user.list` are the `list` sub-commands;
the bare nouns, their human form, have no Starlark name.
A root command is a verb on the module itself:
`work.quickstart()` is `git work quickstart`, the same markdown as a string.
Every function returns what the command would print, as a Starlark value.

`work.stderr(*values)` sits beside it and is no command at all:
it writes one line to the runtime's standard error,
the values joined by a space the way `print` joins them,
and returns `None`.
`print()` is a flow's standard output (config-entity.md),
Starlark's `print` takes no file argument,
and `work` is the only predeclared name,
so a diagnostic needs a verb;
the shell's mirror of it is `>&2`, a redirection rather than a command,
which is the one place the one-to-one rule bends.
It is named for the stream and not for an action,
because `work.log` beside `work.issue.log` and `work.flow.log`
would read as a history.

The other exception to the one-to-one rule:
`git work user new` and `git work user adopt` are interactive identity setup,
a thing a human does once to a checkout, and are not bound in Starlark.

## Gone

`git work bug` reads git-bug's frozen copy since the migration (`bf6f392`, 2026-09-25) and leaves in the deletion round (`860d6e0`); `entities/bug` stays, for `git work migrate`.
`tui` as a command: the terminal renderer sits behind `view` and `flow run`.
`--fields`, `-t`, `-m`, `--set`, `--add`/`--remove`, `KEY VALUE`, `--ARG VALUE`: sugar.
`patch` (RFC 6902), `issue show`, `comment ID`, `comment show`: a second spelling of `set`/`add`/`remove` and `get`.
`flows/` as a directory the tool knows: import takes any files.
The view spec `{"view", "bindings", "items"}`, items on standard input,
and `--format` on `view`: the KWARGS object is the serialization (`84dfbde`).
`sort_by` and `card_title` as view bindings: jq orders, `rank` overrides, `card` is the list.
`pick=True` on a list: `flow pick ID` takes its id from the command line (`9a24c8e`).
