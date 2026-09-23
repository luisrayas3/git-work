# The command line

The target `git work` command line,
decided 2026-09-23 as the map to build toward,
and the rules every command follows.
The tables in `AGENTS.md` describe what the current binary verifies;
this document is where it is going.
Decisions are recorded on `e8d6426` (plumbing), `b511c63` (flows),
`52a2797` (views), `84dfbde` and `8b06191` (renderers) and `3556569` (schema).

## Rules

- **Plumbing, agent-first.**
  JSON out by default, `--format text` for a human.
  There is no sugar: no `--fields`, no `-t`/`-m`, no per-field flags.
  A jq program projects, a flow is the porcelain.
- **The Starlark host API mirrors the command line one to one.**
  `git work <module> <verb>` is `<module>.<verb>(...)`,
  same arguments, returning what the command prints.
  Nothing is reachable from a script that the shell cannot reach, and the reverse.
  `me()` is the only script-only name.
- **Arguments are keyword arguments.**
  Starlark has no positional-only parameters,
  so a command that takes a function's arguments takes them as one JSON object,
  `git work flow run board '{"iteration":"current"}'`.
  Commands with fixed positionals (`set ID KEY VALUE`) are that object spelled out.
  Defaults in the signature fill what the object omits;
  an unknown key is an error naming the parameters.
- **Writers print ids only.**
  `new` prints the issue id, `comment new` the comment id, one per line,
  nothing else on stdout.
  Mutators that create nothing print nothing;
  diagnostics go to stderr and the exit status is the result.
- **`--dry-run`** on every writer that has one prints the operations it would commit.
- **Any `ID` position accepts an alias**, a Jira key for instance (`483dbe2`).
- **`rm` is local, `archive` is replicated**, on every tree.
  `rm` deletes the local ref and the entity returns on the next pull;
  `archive` is an operation and reaches every clone.
- **The list is a jq program** (`3c9c24d`) over the array of excerpts,
  the same JSON `--format json` prints.
  The default program is mine and unarchived; `.` is everything.
- **Views are a module, not a surface.**
  A `view.*` function builds a spec from items and field bindings;
  renderers consume specs.
  In a TTY a view opens interactively, otherwise it prints the spec,
  and `--gui` sends it to the browser.
  A renderer that lacks a view type fails at render time naming itself,
  so no command exists on one surface and not the other.

## Map

```
git work issue [PROGRAM] [--format json|text]
git work issue new DOC|-                        # prints the id
git work issue show ID
git work issue set ID KEY VALUE                 # null clears
git work issue set ID KEY --add ITEM... --remove ITEM...
git work issue patch ID PATCH|- [--dry-run]     # RFC 6902; one patch op ↔ one CRDT op
git work issue comment ID
git work issue comment new ID BODY|-            # prints the comment id
git work issue comment edit COMMENT_ID BODY|-
git work issue log ID
git work issue archive ID                       # = set ID archived true
git work issue rm ID

git work schema [--format yaml|json]
git work schema init [PRESET]
git work schema import FILE|- [--prune] [--dry-run]
git work schema export [--format yaml|json]
git work schema log [KEY]
git work schema archive KEY
git work schema rm KEY

git work flow                                   # names, descriptions, arguments
git work flow run NAME [KWARGS] [--gui] [--format json|text]
git work flow show NAME                         # the script
git work flow import FILE|DIR|-... [--prune] [--dry-run]   # one def per file; its name is the flow's
git work flow export NAME > FILE
git work flow export --all DIR
git work flow log [NAME]
git work flow archive NAME
git work flow rm NAME

git work view list  [KWARGS] [--gui] < ITEMS
git work view board [KWARGS] [--gui] < ITEMS    # {"columns": KEY, "card_title": KEY, ...}
git work view gantt [KWARGS] [--gui] < ITEMS    # {"start": KEY, "end": KEY, "group_by": KEY, ...}

git work push
git work pull
git work migrate                                # one shot; removed after bf6f392

git work bridge configure|pull|push|rm          # Jira, phase 3
git work user
git work user new | adopt ID
git work gui [--port N] [--no-browser]          # every flow as a page, every view as a renderer
git work version
git work completion SHELL
```

## A flow

```python
def board(iteration="current"):
    """Kanban of one iteration, a column per status."""
    items = issue.list('map(select(.fields.iteration == "%s"))' % iteration)
    return view.board(items, columns="status", card_title="title")
```

The same kanban with no flow at all:

```sh
git work issue 'map(select(.fields.status != "done"))' | git work view board '{"columns":"status"}'
```

## Starlark

`issue.list(program)`, `issue.new(doc)`, `issue.show(id)`,
`issue.set(id, key, value)`, `issue.add(id, key, item)`, `issue.remove(id, key, item)`,
`issue.patch(id, ops)`, `issue.comment.new(id, body)`, `issue.comment.edit(id, body)`,
`issue.log(id)`, `issue.archive(id)`;
`schema.export()`, `schema.log(key)`;
`flow.run(name, **kwargs)`;
`view.list(items, ...)`, `view.board(items, ...)`, `view.gantt(items, ...)`;
`me()`.
Every function returns what the command would print, as a Starlark value.

## Gone

`git work bug` leaves with the migration (`bf6f392`).
`tui` as a command: the terminal renderer sits behind `view` and `flow run`.
`--fields`, `-t`, `-m`, `--set`, `--ARG VALUE`: sugar.
`flows/` as a directory the tool knows: import takes any files.
