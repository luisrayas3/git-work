# Flows

A flow is a team's own command:
one Starlark function, stored in the tracker,
run by `git work flow run NAME`,
reaching the store through the same API the command line is.
This document covers what a flow is and how it is stored and imported,
the runtime that runs it,
the `work` module it sees,
how a flow and a view meet,
`home`, the flow bare `git work` runs,
and the report flow.
The command line the `work` module mirrors is [cli](cli.md);
the config entities a flow is stored as, their operations and their duplicate keys, are [schema](schema.md);
the view kinds a flow calls, [views](views.md);
the pages and keys a flow's view is drawn in, [tui](tui.md);
the jq programs and the past the report reads, [query](query.md).

## A flow

### A flow is a config entity

A flow is a config entity of shape `flow` under `refs/work-flows/<id>`,
its key the flow's name,
with two attributes:
`script`, the Starlark source,
and `description`, its docstring.
It merges, travels with `git work push` and `pull`,
and records who changed it, as every config entity does ([schema](schema.md)).
The description is mirrored into an attribute so that a listing has it without a parse;
the arguments are not, and `flow list` parses each script for them,
tolerating one that will not parse with a warning rather than failing the listing.
The whole script is last-writer-wins,
because the only writer is `flow import`
and the text merge already happened in git.

There is no `view` shape:
a saved view is a flow whose function calls `work.view.*`.
There is no rule shape and no trigger:
a flow runs when it is invoked,
and nothing in git-work fires on its own.

### A flow is one function

A flow's script is exactly one `def` and nothing else:
its name is the flow's name,
its docstring is the description,
and its parameters are the flow's arguments, defaults included.
`flow.Parse` reads all three from the syntax tree without executing anything,
and refuses a file with a second top-level statement, a `load`, or no `def`,
naming the line.
Helpers are nested functions,
and `work` is the only global.

The name is a Starlark identifier and a config key at once,
`^[a-z][a-z0-9_]*$`, at most 64 bytes.
A default is a literal,
a string, a number, a bool, `None`, or a list or dict of those,
because a default is read and never evaluated.
A parameter with no default is required.
`*args` and `**kwargs` are refused:
the arguments arrive as one object of keyword arguments,
and a flow whose arguments cannot be listed has no caller.

### Arguments are keyword arguments

`git work flow run board '{"iteration":"current"}'`
is `work.flow.run("board", iteration="current")`.
Defaults in the signature fill what the object omits,
applied by Starlark itself so that what a flow does with no argument is what its source says;
an unknown key is an error naming the arguments,
and so is a required argument nobody named.
An argument is any JSON value; its type is not declared.

### Refs are the runtime truth; import is the way in

The `.star` files in a tree are authoring copies,
reviewed and merged by git,
and reach `refs/work-flows` only through `git work flow import`;
nothing reads the tree at run time ([store](store.md#refs-are-the-runtime-truth)).
The function is the whole declaration,
so the file's name and location do not matter:
`flow import` takes files, directories of `*.star` files, or standard input,
a scratch file in `/tmp` as well as one under the repository,
and a directory such as this repository's `flows/` is a convention for review,
not something the tool knows.

Import is an upsert keyed on the def's name:
a flow that is not there is created and its id printed,
one whose script or description differs is updated,
and one that is unchanged emits nothing.
`--prune` archives every flow the inputs do not mention,
explicitly, so that a partial import from anywhere can never archive anyone's work.
Every input is parsed before anything is written,
so one file that does not parse aborts the whole import;
the writes are one entity at a time,
and the ids created before a failure are printed, since they are in the store.
`--dry-run` prints the changes per flow and writes nothing.

`flow export NAME` prints the script verbatim,
so a redirection writes the file an import takes back unchanged;
`--all DIR` writes one `<name>.star` per flow.

### Two flows with one name

Two clones importing the same name before either pushes
create two entities with one key.
One is chosen deterministically, as for every config key ([schema](schema.md)),
and every flow command warns on standard error, naming the other,
because a team that loses an edit this way is never told otherwise;
`flow archive` repairs it.

## Running a flow

### `flow run`

`git work flow run NAME [KWARGS|-]` runs a stored flow.
`git work flow run - [KWARGS]` runs the script on standard input without importing it,
which is how a flow is tried before it is worth a name in the store;
the script and the arguments cannot both come from standard input.

### `print` is the flow's standard output

`print()` writes to standard output, as every language's print does,
so a flow that renders text, a report, prints its page and returns `None`.
A flow that returns a value has it printed as JSON after whatever it printed;
one that returns `None` prints nothing more.
The runtime's own diagnostics go to standard error.

Not `print` to standard error, to keep standard output JSON for `jq`:
that protected a composition nobody used,
and left a text-rendering flow with no way out.

### `work.stderr`

`work.stderr(*values)` writes one line to the runtime's standard error,
the values joined by a space the way `print` joins them, and returns `None`.
Starlark's `print` takes no file argument and `work` is the only global,
so a diagnostic needs a verb.
It is named for the stream and not for an action,
because `work.log` beside `work.issue.log` and `work.flow.log`
would read as a history.
It is the one name in the module that is no command:
its shell mirror is `>&2`, a redirection.

### The dialect and the caps

A flow is written with `set` on,
`while`, recursion and top-level reassignment off:
reassigning a global is how a script silently stops meaning what it reads as,
and with no `while` and no recursion only the step cap can run long.
One execution is capped at fifty million steps,
which trips in about a second on a loop that never ends
and is orders of magnitude more than a real flow needs;
the context cancels a host call that hangs, or an interrupt.
None of this is a sandbox:
a flow comes from the team's own refs and writes to the store by design,
and the caps are against a mistake, not an attacker.

### Flows compose

`work.flow.run(name, **kwargs)` runs another flow,
with the same output streams and the same renderer,
and returns what it returned.
Nesting deeper than eight is refused as a cycle,
because a cycle between two flows is a mistake that would otherwise fill memory.

## The `work` module

### `work` is the only predeclared name

The SDK is one module named after the binary:
`git work <module> <verb>` is `work.<module>.<verb>(...)`,
with the same arguments, returning what the command prints, as a Starlark value.
A flow is therefore exactly a shell script that runs in-process.
One name rather than five
leaves `issue`, `flow`, `schema`, `view` and `user` free for a script's own locals.
A root command is a verb on the module itself:
`work.quickstart()` is `git work quickstart`, the markdown as a string.
Every verb is a call into package `host`,
which is also what the command calls ([cli](cli.md#the-command-line-and-starlark-are-one-api)),
and a failure names the call as a script writes it, `work.issue.comment.new`.

```
work.issue.list(program="", at=None, include_archive=False)   # "" is the default program
work.issue.search(text, include_archive=False)
work.issue.new(doc)
work.issue.get(id, at=None)
work.issue.set(id, **fields)
work.issue.add(id, **items)
work.issue.remove(id, **items)
work.issue.archive(id)
work.issue.rm(id)
work.issue.log(id, from_=None, to=None, include_archive=False)   # id is an id or a program
work.issue.comment.new(id, body)
work.issue.comment.edit(id, body, base=None)   # the comment's id; base a message_hash
work.issue.comment.delete(id)            # the comment's id

work.schema.export()
work.schema.import_(doc, prune=False, dry_run=False)
work.schema.init(preset="jira", dry_run=False)
work.schema.log(key=None, id=None)       # at most one of the two, by keyword
work.schema.archive(key=None, id=None)
work.schema.rm(key=None, id=None)

work.flow.list()
work.flow.run(name, **kwargs)
work.flow.import_(scripts, prune=False, dry_run=False)
work.flow.export(name)
work.flow.log(name="")
work.flow.archive(name)
work.flow.rm(name)

work.view.list(**kwargs)    # and board, gantt, matrix, show, new, home: keywords only
work.user.list()
work.user.me()
work.quickstart()
work.stderr(*values)
```

The view verbs are built from the kinds table in package `view`,
so a kind added there is callable with no other edit,
and they take keywords only, `work.view.show(id=…)`.
A command that reads a file takes its contents:
`work.flow.import_` takes scripts and `work.schema.import_` a document,
because finding the bytes is the shell's job,
and one to one is about the verb and its arguments.

### A reserved word takes a trailing underscore

`import` and `from` are reserved words in Starlark,
so `schema import`, `flow import` and `issue log --from`
are `import_` and `from_`, and nothing else is renamed.
`None` and the empty string both mean an absent TIME,
so a flow can pass its own argument straight through whether its caller filled it in or not.

### The schema keeps its order

A schema document is the one value whose key order carries meaning,
so `work.schema.export()` and `work.schema.import_()` convert it keeping each mapping's order,
rather than by the general conversion, which sorts keys so a script reads the same twice.
Without that, `work.schema.import_(work.schema.export())` would renumber the whole schema.

### What has no Starlark name

- **The bare nouns**, `git work issue`, `flow` and `user`:
  they are the human form of `list`,
  and a script reads data and never wants the text,
  so `work.issue.list()`, `work.flow.list()` and `work.user.list()` mirror the `list` sub-commands.
  `--format` has no Starlark form for the same reason.
- **What a person does once to a checkout**:
  `user new` and `user adopt`, interactive identity setup,
  and `install`, which adds the tracker's fetch refspecs to a remote's config.
- **The remote and the one-off commands**:
  `pull`, `push`, `sync`, `jira sync`, `jira schema`, `migrate`, `version` and `completion`.
- **`flow export --all DIR`**, which writes files.
- **`--dry-run` on the issue writers**:
  `work.issue.set(id, **fields)` takes every keyword as a field,
  so there is no keyword left for it.

## Flows and views

### Flows call views; views never call flows

A view is a capability a flow calls, not a surface:
`work.view.board(columns="status")` draws the board,
handles its own interaction,
writes its own edits through the host,
and returns when the person leaves it ([views](views.md#a-view-call-blocks)).
A flow that wants a board calls one;
nothing returns a view for something else to draw.
A view never runs a flow:
where a page offers flows, as home does, it names one,
and the host runs it.

Not non-blocking views:
a script that ends while its view is still open would have nowhere to live,
and two views started from one script would compete for one terminal.

### A view needs a terminal; a flow does not

A flow runs with a renderer only where there is a terminal.
A flow that never calls a view does not notice,
so a report runs the same in a pipe;
one that does fails at that call, from `host.View`,
which is where `git work view` fails too.

## Home

### `git work` is `git work flow run home`

Bare `git work` runs the flow named `home`.
With no `home` flow, no repository or no terminal it prints the help,
and the identity is settled only once the flow is known to run,
so `git work` in a repository with no home flow writes nothing.

This repository's `home` is:

```python
def home():
    work.view.home(flows=["overview", "board", "report"])
```

### The home view names flows

`work.view.home(flows=[…])` is `git work view home '{"flows":[…]}'`,
a view kind with an argument table like any other.
It draws the flows it names, each with its docstring's first line,
and its rows are a picker's: typing narrows by name or first line.
`flows` is required and may not be empty,
and a name the store lacks is refused at the call, naming the flows there are,
as an unknown field is refused:
a home whose entry fails only when chosen is a home that rots unseen.
A flow archived after the call is drawn *(no such flow)* and rings on enter.

### How a page runs a flow

Enter on a flow answers a message naming it,
as enter on a row names an issue.
The renderer runs it on its worker,
the goroutine `Render` was called on,
through `flow/run`'s `Flow`, the function `git work flow run` calls,
with the renderer as the flow's renderer.
The flow's `work.view.*` call reaches `host.View` and then `Render`,
which, finding its own program drawing,
pushes the page onto the same stack instead of starting a second program,
and blocks the flow until that page leaves the stack, with the page's answer.
So the page a flow draws from home is a page over home, and back is back home.
The worker keeps serving while a flow is blocked,
so a flow run from a page a flow drew works too;
quitting ends a blocked flow.
`tui` imports `flow/run`, which is no cycle,
since `flow/run` knows only the `view.Renderer` interface.

A flow run from a page runs with its defaults:
one with a required argument fails at enter, saying which,
because a page has no form for arguments.
A flow that prints has its output, and its returned value as JSON,
on a page of its own, `flow run NAME`;
one that neither prints nor draws says so on the status line,
and one that fails says why there,
or on the output page when the reason is a backtrace.
The palette's `r` runs any stored flow and `h` goes back home ([tui](tui.md)).

## The report flow

### What changed since

The report is this repository's flow, `flows/report.star`,
and the one flow that only prints:

```python
def report(from_="7d", to=None, iteration=None, query=None)
```

The window is `[from_, to)` in the TIME forms;
an `iteration` id reads that issue's `start` and `end` instead,
warning on `work.stderr` when they are missing,
because which fields hold an iteration's dates is policy, and policy lives in a flow.
The issues are `query` over every issue, the archived included,
selected at both ends of the window,
so an issue archived or moved out of the selection during the window is still reported.
For each, the flow reads the snapshot at the start, the snapshot at the end
and the operations in between ([query](query.md)), compares, and prints markdown.

What it says:

- **Created** is absent at the start and present at the end.
  **Closed** is a status whose schema category became `completed` or `canceled`.
  **Archived** is `archived` becoming true.
  Nothing else is named.
- **Every changed field** of an issue that already existed is reported, `before → after`,
  because the flow does not know which fields matter,
  and a missing line is worse than an extra one.
- **A created issue is described rather than diffed**:
  `with status done, priority high, …`, the fields it ends the window with,
  in the schema's order for its type, the empty ones left out,
  since `(none) → value` on every field is the whole issue written the long way.
- **The status gets its whole path**, `to-do → done (via in-progress)`,
  read from the operations,
  because the states a task passed through are often the interesting part of a week.
- **Comments in the window** are listed with author, date and first paragraph,
  cut at 200 characters:
  a status report is a table of contents, not a transcript.
- **Grouped by `parent`**, under the parent's title, `(none)` last,
  because the tracker's shape is stories with tasks under them.
- **A summary line** at the top: the window, and the counts created, closed, changed and commented.

`status` and `parent` are named by the flow, in the open,
because there are no field roles and a flow is where policy lives.
An issue is named by its `human_id`, and a relation value by the `human_id` of the issue it names,
so with `git-work.display.id` set the report reads in Jira keys,
which is where it is most likely to be pasted ([cli](cli.md)).
Markdown, because its two readers are a human scanning it
and a model summarizing it,
and markdown suits both.

It is not a narration:
the flow emits the deterministic half, and prose over it is someone else's.
It is not a burndown: it counts what changed, not what remains.
It is not incremental:
every run replays from scratch, with no stored cursor to invalidate,
so a re-run of last week's window gives the same answer once everyone has pushed.
