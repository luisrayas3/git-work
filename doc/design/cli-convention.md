# The command line

The target `git work` command line,
decided 2026-09-23 as the map to build toward,
and the rules every command follows.
The tables in `AGENTS.md` describe what the current binary verifies;
this document is where it is going.
Decisions are recorded on `e8d6426` (plumbing), `b511c63` (flows),
`52a2797` (views), `84dfbde` and `8b06191` (renderers) and `3556569` (schema).
What a view does once it is open —
the keys, editing, grab, rank and nesting —
is `terminal-renderer.md`; this document is the command line it is reached by.

## Rules

- **Plumbing, agent-first.**
  JSON out by default, `--format text` for a human.
  There is no sugar: no `--fields`, no `-t`/`-m`, no per-field flags.
  A jq program projects, a flow is the porcelain.
- **The Starlark host API mirrors the command line one to one.**
  The SDK is one module, named after the binary:
  `git work <module> <verb>` is `work.<module>.<verb>(...)`,
  same arguments, returning what the command prints.
  Nothing is reachable from a script that the shell cannot reach, and the reverse;
  there is no script-only name.
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
- **`--dry-run`** on every writer that has one prints the operations it would commit.
- **`new` takes the issue as a document**,
  `{"fields": {"title": "…", "type": "task"}, "body": "the first comment", "aliases": {"jira": "PROJ-12"}}`,
  as the argument or on standard input.
  A title is required and lives in `fields`, like every other property of an issue;
  an unknown key at the top level is an error, not a silent no-op.
- **Any `ID` position accepts an alias**, a Jira key for instance (`483dbe2`).
  An alias is stored as `alias:<name>` metadata on the create operation,
  the one place in the entity that can never change,
  and an issue carries as many as it has external systems.
- **`rm` is local, `archive` is replicated**, on every tree.
  `rm` deletes the local ref and the entity returns on the next pull;
  `archive` is an operation and reaches every clone.
- **The list is a jq program** (`3c9c24d`) over the array of excerpts,
  the same JSON `--format json` prints.
  The default program is mine and unarchived; `.` is everything.
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
  which is what `--gui` posts to the `gui` process (`8b06191`),
  so `git work view` has no `--format`
  and `{"view", "bindings", "items"}` is gone.
  No TTY and no `--gui` is an error;
  an agent that wants the data runs `git work issue PROGRAM`, where the data is.
  Every kind takes `query`, a jq program the view runs, re-runs on a
  ref-watcher change and after its own writes, which is what keeps a view live.
  Which other arguments a kind takes, and which of them it cannot do without,
  is a table in package `view`, read by the view functions, the help and every backend:
  `list` takes `fields`, `details`, `group_by`, `expand`, `depth` and `rank`;
  `board` requires `columns` and takes `values`, `card`, `group_by` and `rank`;
  `gantt` requires `start` and `stop`
  and takes `label`, `scale`, `from`, `to`, `progress`, `group_by`, `expand`, `depth` and `rank`;
  `show` requires `id` and takes `fields`.
  Every argument that names a field is one field key, because there are no field roles:
  a script names the fields it means when it calls the view.
  `terminal-renderer.md` is what each one does.

## Map

```
git work issue [PROGRAM] [--format json|text]
git work issue new DOC|-                        # prints the id
git work issue get ID
git work issue set ID FIELDS|- [--dry-run]      # {"key": value, ...}; null clears; one SetField per key, one commit
git work issue add ID ITEMS|- [--dry-run]       # {"key": [item, ...], ...}; set semantics
git work issue remove ID ITEMS|- [--dry-run]
git work issue comment new ISSUE_ID BODY|-      # prints the comment id
git work issue comment edit COMMENT_ID BODY|-
git work issue log ID
git work issue archive ID                       # first class on every tree; = set ID '{"archived":true}'
git work issue rm ID

git work schema [--format yaml|json]
git work schema init [PRESET]
git work schema import FILE|- [--prune] [--dry-run]
git work schema export [--format yaml|json]
git work schema log [KEY]
git work schema archive KEY
git work schema rm KEY

git work flow                                   # names, descriptions, arguments
git work flow run NAME|- [KWARGS|-] [--gui]      # - runs the script on stdin without importing it
git work flow import FILE|DIR|-... [--prune] [--dry-run]   # one def per file; its name is the flow's
git work flow export NAME > FILE
git work flow export --all DIR
git work flow log [NAME]
git work flow archive NAME
git work flow rm NAME

git work view list  [KWARGS] [--gui]            # {"query": PROGRAM, "fields": [KEY, ...], ...}
git work view board [KWARGS] [--gui]            # {"columns": KEY, "values": [...], "card": [KEY, ...], ...}
git work view gantt [KWARGS] [--gui]            # {"start": KEY, "stop": KEY, "scale": "week", ...}
git work view show  [KWARGS] [--gui]            # {"id": ID, "fields": [KEY, ...]}

git work push
git work pull
git work migrate                                # one shot; removed after bf6f392

git work bridge configure|pull|push|rm          # Jira, phase 3
git work user
git work user me                                # the identity this repository writes as
git work user new | adopt ID
git work gui [--port N] [--no-browser]          # every flow as a page, every view as a renderer
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

`work.issue.list(program)`, `work.issue.new(doc)`, `work.issue.get(id)`,
`work.issue.set(id, **fields)`, `work.issue.add(id, **items)`, `work.issue.remove(id, **items)`,
`work.issue.comment.new(id, body)`, `work.issue.comment.edit(id, body)`,
`work.issue.log(id)`, `work.issue.archive(id)`, `work.issue.rm(id)`;
`work.schema.export()`, `work.schema.import_(doc, prune=False, dry_run=False)`,
`work.schema.init(preset="jira", dry_run=False)`,
`work.schema.log(key="")`, `work.schema.archive(key)`, `work.schema.rm(key)`;
`import` is a reserved word in Starlark,
so that one verb is spelled with a trailing underscore;
`work.flow.list()`, `work.flow.export(name)`, `work.flow.run(name, **kwargs)`;
`work.view.list(...)`, `work.view.board(...)`, `work.view.gantt(...)`, `work.view.show(id, ...)`;
`work.user.me()`, which is `git work user me`.
Every function returns what the command would print, as a Starlark value.

## Gone

`git work bug` leaves with the migration (`bf6f392`).
`tui` as a command: the terminal renderer sits behind `view` and `flow run`.
`--fields`, `-t`, `-m`, `--set`, `--add`/`--remove`, `KEY VALUE`, `--ARG VALUE`: sugar.
`patch` (RFC 6902), `issue show`, `comment ID`, `comment show`: a second spelling of `set`/`add`/`remove` and `get`.
`flows/` as a directory the tool knows: import takes any files.
The view spec `{"view", "bindings", "items"}`, items on standard input,
and `--format` on `view`: the KWARGS object is the serialization (`84dfbde`).
`sort_by` and `card_title` as view bindings: jq orders, `rank` overrides, `card` is the list.
`pick=True` on a list: `flow pick ID` takes its id from the command line (`9a24c8e`).
