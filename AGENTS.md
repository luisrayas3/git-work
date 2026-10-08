# Agent guide for git-work

This repository is a **fork of [git-bug](https://github.com/git-bug/git-bug)**
(forked at `e1c21a42`),
evolving into a project-management tool
with **Jira as a first-class sync backend**.

We **dogfood**:
this project's own tasks live in its own store
(`refs/work-issues/*`, migrated from git-bug's `refs/issues/*` on 2026-09-25, `bf6f392`),
managed with the locally-built `git work`.

## The pristine-library boundary (read first)

git-bug's core value is its op-based CRDT engine.
We consume it **unmodified** to keep pulling upstream fixes.
**Never edit these seven packages:**

```
entity/dag   entity   repository   entities/identity
util/lamport   util/text   util/timestamp
```

All new work goes **above** this line:
the generalized `issue` entity, the PM schema, bridge changes,
and the new harness
(`entities/`, `cache/`, `query/`, `commands/`, `bridge/`, a new TUI).
`entities/bug` is **ours**, not pristine.
Its successor `entities/issue` carries our own model and operation set;
the store was migrated once, on 2026-09-25
(`bf6f392`, `doc/design/store-migration.md`),
and `commands/bug`, `termui` and what serves only them
are deleted in the round after, once the new surface has proven itself
on the tracker (`f4bac00`, 2026-09-22; deferred 2026-09-25 as `860d6e0`).
`entities/bug` and `git work migrate` stay, as the decoder and the command
that will import other git-bug repositories (`01c6231`, decided 2026-09-28).
Cherry-picking upstream fixes into `cache/` and `bridge/` is not a goal.
If you believe you must edit a pristine package, **stop and flag it** —
it breaks upstream tracking and is a real architectural decision.
One such decision is on record and done:
the migration (`bf6f392`) changed three ref-name constants in `entities/identity`
so identities live at `refs/work-users` like every other namespace (`483dbe2`; named `users` on 2026-09-25, because every word a user meets says user).
A second, a bug fix, on 2026-09-28:
`entity/dag`'s reader ordered commits by reversing a BFS,
which is not topological once a merge joins branches of different lengths,
so such a pull left the entity unreadable, with `creation lamport time not set` or `panic: DFS failed` (upstream #845, misread there as old data, and still in upstream's trunk);
`dag.read` now sorts parents first (Kahn), tested by `TestMergeUnevenBranches`,
and is worth offering upstream (`7cb8b39`, `doc/design/dag-read-order.md`).
Nothing else in the seven is touched.

Design consequence:
there is **no atomic multi-entity commit**
(`dag.Entity.Commit` writes one ref).
Model cross-issue relationships (parent, dependencies) as fields
whose value is the other issue's `entity.Id`, resolved via `entity.Resolvers` —
eventually-consistent, not transactional.
This matches Jira's behavior and is sufficient;
do not add core batch commits.

## Build and run

Requires **Go 1.26** (native, or `nix shell nixpkgs#go`).

```sh
# CLI-only build; skips the webui (no pnpm), which sits behind a build tag.
go build -o git-work .

# Already symlinked onto PATH so `git work <cmd>` dispatches:
#   ~/.local/bin/git-work -> ~/ws/git-work/git-work
# After code changes, re-run the build above; the symlink tracks it.

# The React webui and its pnpm toolchain are being removed (938434e); until then
# `make build` still needs pnpm. The Go-only GUI replaces it (867db1a).
```

## Operating the tracker

The tracker is `git work issue` over `refs/work-issues/*`,
since the migration of 2026-09-25 (`bf6f392`).
`git work bug` still reads git-bug's copy under `refs/issues/*`,
kept as a frozen fallback:
**nothing written through `git work bug` reaches the tracker**,
so never write with it.
The schema is `schema.yaml` at the root of this repository,
applied with `git work schema import schema.yaml`;
the recipes below use its keys.

`git work quickstart` is the short version of all this, for an agent:
the model, the issue commands, and this repository's types read from the store.
Its static half is `host/quickstart.md`,
documentation like this file and kept accurate the same way —
in the change that makes it wrong.

| Action | Command |
| --- | --- |
| Quickstart | `git work quickstart`: the model and this repo's types, for an agent |
| Open work | `git work issue 'map(select(.fields.status != "done"))'` · `--format text` |
| One type | `git work issue 'map(select(.fields.type == "decision"))'` · by area: `select(.fields.area // [] \| index("cli"))` |
| Live list | `git work view list '{"fields":["type","status","priority","title"],"group_by":"status"}'` (TTY) |
| Overview | `git work flow run overview` (TTY): open stories, open decisions and open tasks, each story's tasks folded under it, grouped by type; `'{"group_by":"status"}'` regroups |
| Board | `git work flow run board` (TTY): the same issues as a kanban, a column per open status, a swimlane per type; `'{"group_by":"area"}'` relanes |
| Due dates | `git work view gantt '{"start":"due","stop":"due","query":"map(select(.fields.due != null))"}'` (TTY): the dated work as milestones on a week chart; `"expand":"children"` nests tasks, folded, under their stories |
| Allocations | `git work view matrix '{"rows":"work","columns":"iteration","value":"points","query":"map(select(.fields.type == \"allocation\"))"}'` (TTY): the points allocated, work down, iterations across, totals both ways |
| What changed | `git work flow run report '{"from_":"7d"}'` · `'{"iteration":"<id>"}'`: created, closed, changed, commented, grouped by parent, as markdown |
| Create | `git work issue new '{"fields":{"title":"…","type":"task","status":"to-do","priority":"medium","area":["cli"],"parent":"<story id>"},"body":"…"}'` → prints the id |
| Show | `git work issue get <id>` · `--format text` |
| Close / reopen | `git work issue set <id> '{"status":"done"}'` · `'{"status":"to-do"}'` |
| Comment | `git work issue comment new <id> -` with the body on standard input |
| Tasks of a story | `git work issue 'map(select(.fields.parent == "<full story id>"))'` |
| Sync | `git work sync [--jira]` (pull, then push; every namespace) · `git work pull` · `git work push` |

Types in use are `story`, `task` and `decision`;
`status` is the jira workflow (`to-do`, `in-progress`, `in-review`, `done`, …),
`priority` is `highest` … `lowest`,
`area` a multi-enum (`core`, `issue-model`, `bridge`, `cli`, `tui`, `gui`, `mcp`, `infra`),
and `parent` the story a task or decision belongs to.
A title never repeats the type: the list shows both.

The whole tree, plumbing with explicit ids, no editor and no sugar flag,
built to the map in `doc/design/cli-convention.md` (`e8d6426`):

| Action | Command |
| --- | --- |
| List | `git work issue [PROGRAM] [--at TIME] [--include-archive]` · `--format text`; PROGRAM is a jq program over the array of unarchived excerpts (the archived too with `--include-archive`), the default being all of them, last edited first |
| Create | `git work issue new DOC\|-` → prints the new id |
| Show | `git work issue get <id> [--at TIME]` · `--format text` |
| Set fields | `git work issue set <id> '{"status":"done","estimate":3}'` (`null` clears; one commit whatever the number of keys) |
| Add / remove items | `git work issue add <id> '{"labels":["area:core"]}'` · `git work issue remove <id> …` (set semantics; relations of many cardinality too) |
| Comment | `git work issue comment new <id> BODY\|-` → prints the comment id · `comment edit <comment-id> BODY\|-` |
| History | `git work issue log [<id>\|PROGRAM] [--from TIME] [--to TIME] [--include-archive]` · `--format text`; each entry names its issue |
| Archive / remove | `git work issue archive <id>` (an operation, replicated) · `git work issue rm <id>` (the local ref only) |

Everything in is JSON, everything out is JSON unless `--format text` is asked for,
and a document argument is read from standard input when it is `-`.
**TIME is one grammar** everywhere it appears: a date (`2026-09-21`),
an RFC 3339 time, or a duration back from now (`7d`, `2w`, `12h`).
`--at` replays the issue's operations into the snapshot that stood then —
an issue created later is absent, `archived` is the value it had then —
and `--from`/`--to` is half-open, `[from, to)`.
The cut is each operation's own wall clock, never its lamport time,
so an operation pulled late still lands in the window it was written in
(`doc/design/report.md`).
The log takes the list's PROGRAM as well as one id;
an id prefix or alias is tried first, and what does not resolve is a program.
**A program's input is the unarchived issues** (`df6ff51`, `doc/design/include-archive.md`):
the list, the log's PROGRAM and every view's `query` run over them,
so no query carries an archived filter of its own,
and `--include-archive` (`include_archive` in a view's KWARGS and in Starlark) brings the archived back;
with `--at` the input reads the `archived` that stood then,
and an issue named by id is that issue, archived or not.
In Starlark these are `work.issue.get(id, at=…)`,
`work.issue.list(program, at=…, include_archive=False)` and
`work.issue.log(id_or_program, from_=…, to=…, include_archive=False)` —
`from_` with a trailing underscore, like `import_`, because `from` is a reserved word.
`--dry-run` on `set`, `add`, `remove` and `archive`
prints the operations they would commit and writes nothing.
Every id position takes an id prefix or an alias —
`git work issue new '{"fields":{"title":"…"},"aliases":{"jira":"PROJ-12"}}'` —
and a writer prints an id or nothing at all.

**Issue writes are validated against the schema** as soon as one type exists
(`bb9e89e`): `new` needs a known `type`, a key that is not a field of that type
is refused naming the ones that are, and a value is checked by kind — an enum
value against the field's ids, a relation against its `target_types`. Every
problem is reported at once, and the check happens at planning time, so
`--dry-run` refuses what a commit would. With no type defined nothing is
checked, and `--dry-run` says so on stderr.
The one writer that skips the policy half — enum membership, `target_types` —
is the Jira pull, which checks shape only (a value fits its kind, what it names
exists), because Jira is the authority on what Jira holds
(`doc/design/pull-schema-check.md`).

The schema itself, types and fields under `refs/work-schema`
(`3556569`, `bb9e89e`, design in `doc/design/config-entity.md`):

| Action | Command |
| --- | --- |
| Show | `git work schema` · `--format json` (alias of `export`) |
| Bootstrap | `git work schema init [jira\|linear]` → prints the created ids; refuses if any field exists |
| Round trip | `git work schema export > schema.yaml` · `git work schema import schema.yaml` (writes only what differs; a no-op when nothing did) |
| Import a partial file | `git work schema import FILE\|- [--prune] [--dry-run]`; an upsert unless `--prune`, which archives what the file omits |
| History | `git work schema log [KEY \| --id ID]` · `--format text`; one JSON object per line; a key is every entity that ever held it, archived included, the current first |
| Archive / remove | `git work schema archive <key>` (an operation, replicated) · `git work schema rm <key>` (the local ref only); `--id ID` in place of the key names one entity by id or prefix, archived or not, and is how the loser of a key two clones defined is reached, since by key that is refused (`schema-archive-id.md`) |

A field's KEY is `<type>/<field>`: every field belongs to exactly one type, so
`task/status` and `epic/status` are two entities (`e7e58f2`). `title`, `type`,
`archived` and `rank` are built in on every type and appear in the file only
when an entity overrides a name or a description; a file that still lists one
of them as a field is read as that override, never as a second field. List position is the order — the file
carries no ordinals — and `shared:` is YAML anchors the parser expands, never
written back. Keys defined twice by two clones (`E7`) are reported on stderr by
every schema command.

Flows, the config entities of `refs/work-flows` (`b511c63`, `3556569`).
A flow is one Starlark function:
its name is the flow's name, its docstring the description,
its parameters the arguments.
It runs in-process over the same host API a command reaches (`52a2797`):

| Action | Command |
| --- | --- |
| List | `git work flow` · `--format text` (name, description, arguments) |
| Run | `git work flow run <name>\|- [KWARGS\|-]` (`-` as the name runs the script on standard input without importing it; `print()` is stdout, a returned value is printed as JSON after it) · `--gui` (errors until the gui process exists) |
| Import | `git work flow import FILE\|DIR\|-…` `[--prune] [--dry-run]` → prints the id of each flow it creates |
| Show / export | `git work flow export <name>` prints the script, verbatim (`> FILE`) · `git work flow export --all DIR` |
| History | `git work flow log [<name>]` · `--format text` (one JSON object per operation) |
| Archive / remove | `git work flow archive <name>` (an operation, replicated) · `git work flow rm <name>` (the local ref only) |

Import is an upsert keyed on the function's name,
so the file's name and location never matter
and `export | import` writes nothing;
`--prune` archives the flows the inputs do not mention, and is the only removal.
Every input is parsed before anything is written,
so one file that is not exactly one `def` aborts the whole import.

KWARGS is one JSON object of the flow's arguments,
defaults from the signature filling what it omits;
an unknown key is an error naming the parameters.
A flow's script reaches one predeclared name, `work`, and through it
`work.issue.*`, `work.schema.*`, `work.flow.*`, `work.view.*`,
`work.user.list()` and `work.user.me()` —
the same verbs, the same arguments, the same output as the commands,
because both go through package `host` —
and writes through the cache, schema check included, like any command does.
`work.stderr(*values)` is the one name that is no command:
one line on standard error, the values joined by a space as `print` joins them,
because `print()` is the flow's standard output
and the shell's mirror of `work.stderr` is `>&2` (`6bbfc3b`).
Nothing else is predeclared, so `issue`, `flow`, `schema`, `view` and `user`
are a script's to use as locals.
`import` is a reserved word in Starlark,
so `git work schema import` is
`work.schema.import_(doc, prune=False, dry_run=False)`;
every other verb keeps its name.

A view call renders and blocks (`work.view.board(...)`, decided 2026-09-24),
and the command is the whole input:
one KWARGS object, the same one the Starlark call takes,
nothing on standard input and nothing printed
(design in `doc/design/terminal-renderer.md`).
Every kind renders in the terminal (`84dfbde`; the gantt and nesting `565d57a`; the matrix `3289ec1`),
and `--gui` errors until the gui process exists (`8b06191`):

| Action | Command |
| --- | --- |
| List | `git work view list [KWARGS\|-] [--gui]` (nothing required) |
| Show | `git work view show [KWARGS\|-] [--gui]` (`id` required) |
| Board | `git work view board [KWARGS\|-] [--gui]` (`columns` required) |
| Gantt | `git work view gantt [KWARGS\|-] [--gui]` (`start` and `stop` required) |
| Matrix | `git work view matrix [KWARGS\|-] [--gui]` (`rows` and `columns` required) |

Which other arguments a kind takes is `git work view <kind> --help`,
generated from the table in package `view`, which is the authority
(designed in `doc/design/terminal-renderer.md`).

Every kind but `show` takes `query`, a jq program over the same array `git work issue` prints,
which the view runs itself and re-runs on a ref-watcher change and after its own writes,
so a kanban with no flow at all is one command:
`git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'`.
KWARGS is read from standard input when it is `-`, like every document argument.
Standard, vim and emacs keys are all read at once, and `?` shows them as three tabs;
the bottom line is two places (2026-10-02):
the keys that act under the cursor on the left —
`enter` and `space` first, in the standard spelling, then `? keys`,
and never a key that rings the bell there —
and the last action's message right-aligned, the count beside it,
staying until the next action replaces it
(the hints win a window too narrow for both, the message cut from its left).
The cursor starts on the id column and the row under it is washed,
and `Enter` opens while `Space` edits (2026-10-02):
`Enter` on the id or any plain cell opens the row's issue as show,
and on a relation, drawn as the issue it names, opens that issue at once
(a `multi-relation` cell on a list, the first);
`Space` on a cell edits it, a column that is not a field of the row's type ringing the bell,
and on a list's id grabs the row to move it
(`rank` is built in on every type and is the argument's default, so a drag
always has somewhere to go, and `(rank, id)` orders every view, the issues
with no rank keeping the query's order at the end);
a drop that writes a rank first gives one to every unranked row drawn above
it in the same scope — the same group, parent or board stack — one commit
each, so that the drop reads as it was drawn (`rank set · 2 ranked`).
A grabbed row carried past the edge of its group enters the next one —
on a list, on a gantt, and across a board's swimlanes — and the drop writes
the `group_by` field to that group's value with the rank, one commit
(`(none)` writes null; `type` and a set-valued `group_by` ring the bell,
and a nested child stays among its siblings).
A relation's edit is its picker: the issues the field's `target_types` allow,
the cursor on the current one, marked, `/` narrowing them and `(none)` last
(changing a `multi-relation`'s set rings the bell for now: `issue add`/`remove`);
a person, an `identity` field such as `assignee`, is drawn by name
and its list shows names and writes the id
(`issue get --format text` names it too, and JSON keeps the id);
a value list ends with `(none)` and an emptied box clears the field
(`title` excepted, it cannot be cleared), `Enter` in the editor writes and `Esc` cancels,
and a bool flips at once.
A board's card has no cells: `Enter` opens it, copy copies its id,
and its only edits are moves — `Space` grabs,
`←`/`→` carry the card into the next column (a drop sets the `columns`
field, `null` in `(none)`), `↑`/`↓` reorder it,
and one drop is one commit.
Columns are `values` or the field's schema order off the types on the board,
then the values the data has that are not listed, then `(none)`;
they keep `column_width`, 32 cells, and scroll sideways to follow the cursor
(2026-09-28, `doc/design/terminal-renderer.md`, Board).
A gantt's cursor is a cell, a row and a period of `scale`
(`day`, `week`, `month`, `quarter`; the chart is `from` to `to`, else the data's extent and today, opening with today's period left-most and filling the window):
`Enter` opens the row, `←`/`→` move a period, `↑`/`↓` a row keeping it, the period washed down the chart, group headers included, as the row is across it,
`Space` grabs the bar and `←`/`→` shift it a period —
on its first cell only `start` moves, on its last only `stop`, between them both,
and a one-cell bar grows — `↑`/`↓` reorder it,
one drop is one commit,
a row with one date is a milestone fading away from its date (a start to the right, a stop to the left) and running on as a dull band,
and **a band begins at today** (2026-10-02): a row that has not started could start at any point from now on, so a stop with no start bands from today to that stop (a stop already past keeps its marker and bands back to today in red, overdue) and a row with no dates at all bands from today to the chart's end, while a start with no stop still runs to the chart's edge;
`group_by` gives each group a color and heads it as a cell draws the value,
and `progress` fills the bar.
A matrix is a row per value of `rows` and a column per value of `columns`,
each cell the sum of the number `value` names or a count of its issues when none is named,
the axes ordered as a board's columns are (`row_values`/`column_values`, else an enum's schema order, then what the data has, then `(none)`)
and a relation axis drawn as the issue it names, in title order;
the cursor is a cell, `Enter` opens the issues summed into it as a list whose query selects them
(on a totals cell, the whole row, column or matrix),
`Space` rings because a sum is not a value,
copy takes the number and `M-c`/`Y` the cell's `git work view list …` command,
`/` narrows the axes rather than the issues,
the dim totals row and column are of what is drawn,
`group_by` is blocks of rows under one column header,
and the row labels stay put while the columns scroll sideways
(2026-10-02, `doc/design/allocations.md`).
`expand` nests the list and the gantt along a relation,
either side of it a name it takes — the derived `children` is read through the stored `parent`.
It is a **layer spec** (2026-10-02, `f4426ff`; `depth` is gone):
`{"relation":"children","query":…,"include_archive":…,"fields":…,"details":…,"group_by":…,"rank":…,"expand":…}`,
where a layer's `query` runs over that row's own unarchived children (every one of them without it,
the archived too with `include_archive`, which a layer inherits from the call or the layer above
unless it names its own, because archive visibility is one choice for the whole view),
the keys it leaves out are the layer above's,
and its `expand` is the level below — none means leaves, a layer is the next level,
and a number is this same layer again for that many more levels, `0` for every level down.
`"expand":"children"` is the shorthand for one layer with every default.
**A nested layer is its own table** (2026-10-07): the rows under an opened parent are drawn
indented as one unit, two cells a level, with their own header line above the first of them
and their own columns in the layer's widths, id first and flush within the table;
widths are measured over every row of a layer, hidden or drawn, so folding moves nothing,
the top header is the roots' and never changes,
and a child table whose header has scrolled off keeps it on the first body line as a group header is.
Within a table the id column is first and the **fold arrow is the cell after it**:
`▾`/`▸` and, folded, the count of the rows under it (`▸ 3`), the cell as wide as the largest count;
`→` reaches it, `Space` there folds (on the gantt it is `←` from the first period), `Enter` opens the row,
and `Z` folds or unfolds every parent (`z` is gone).
**A nested view opens folded**, so the roots and their counts read as a summary;
`Tab` goes into the first child and `S-Tab` up to the parent,
a list's `↑`/`↓` stay on the level, a rank moves a row among its siblings with its subtree,
and a parent with no dates draws its children's envelope on the gantt
(2026-09-28, 2026-10-02, `doc/design/terminal-renderer.md`, Gantt and Nesting).
With `group_by` bound, every kind keeps the current group's header on the first body line,
because a header scrolled off the top cannot be reached
and the rows under it lose their label (2026-10-02).
`C-Enter` and `F2` are gone, because `C-Enter` is `Enter` on most terminals (2026-09-28).
The terminal's own copy and paste keys stay the terminal's,
`C-c`/`y`/`M-w` copy the cell under the cursor and `M-c`/`Y` the id
(over OSC 52 and, where the machine has `wl-copy`/`xclip`/`xsel`/`pbcopy`, through that too),
a paste opens the editor with the text in it, `/` or `C-s` filters.
Show is a header — type, title, `[ ] archived`, each a cell `Space` edits or flips —
then the fields table, the comment box, and description/comments/log tabs switched with ←/→;
it opens on the comment box, not typing: `Enter` there does nothing,
`Space` puts the cursor in the text, where `Enter` sends, `M-Enter` (or `S-Enter` where the terminal reports it) is a newline
and `Esc` leaves the text keeping the draft (`Tab` skips the box whole).
`Space` on the description tab edits the description — the issue's first comment —
in that same editor, `Enter` writing it, an emptied one refused (2026-10-02).
Show's `children` lists the issues pointing at it, a section of table rows per entry,
each child a link `Enter` follows and `Space` rings on:
`git work view show '{"id":"<story>","children":[{"type":"task","relation":"parent","fields":["status"]}]}'`,
where `relation` is the field on the child holding this issue's id, or its inverse name
(`{"relation":"children"}` alone is every type's `parent`), `type` and `fields` optional;
names are checked against the schema before anything draws
(2026-09-29, `doc/design/terminal-renderer.md`, Show, Children).
`Esc` (or vim's `q`, emacs's `C-g`) is always back;
from the first view it parks on the call line, the query formatted under it
(`query/jq.Format`, a pipe per line; `C-c` copies the whole `git work view …` command), and from there it quits;
`C-q` quits at once; `C-c` does not.
Every view's first line is the call that drew it.

The Jira sync, one bound clone against one Jira Cloud project
(design in `doc/design/jira-sync.md`, `8ade811`):

| Action | Command |
| --- | --- |
| Bind | `git config git-work.jira.url https://<site>.atlassian.net` · `.project KEY` · `.email ME`; the token is `JIRA_API_TOKEN`, else `git credential approve` |
| Review the first mapping | `git work jira schema > jira.yaml` (warnings on stderr; `-v` adds the info notes) · edit · `git work schema import jira.yaml [--dry-run]` |
| Sync | `git work jira sync [ID...] [--dry-run] [--full] [--accept-deletes] [--adopt DURATION]` · `--format text` |
| Cron | `git work jira sync` every minute, `git work jira sync --full --adopt 7d` nightly |

`sync` refuses until one type carries a Jira alias, derives and imports the
schema itself after that, prints one JSON object per line (schema changes,
one line per issue touched, pending, skipped or failed, a summary whose
`unchanged` counts the rest; a value Jira holds that the schema's policy
refuses, such as a parent of a type `target_types` omits, is written and listed
under `off_schema`, never left pending) and exits 1 when an issue failed or
deletes were held; the mapping's notes reach stderr only in a run that changed
the schema, so cron stays quiet. Jira wins a field edited on both sides, with a
`jira-note: conflict` comment on the issue, and so does a value Jira shows
other than the one written, unless it is the normal form Jira answered the
write with; a write Jira's `GET` does not show yet is pending, never written
again, until Jira's `updated` reaches it or 15 minutes of Jira's clock pass. It **never pushes**; bind one
clone only: two bound clones syncing before they exchange duplicate issues,
which the sync consolidates once they have (JS25). One run at a time: a
second exits 1 with `a jira sync is already running`, having done nothing.
Run state is `.git/git-work/jira/state.json` (cursor, failed hits, refused
creates); it is disposable: deleting it costs a slower run, never a wrong one,
because a create's attempt is a `jira-create` marker committed on the issue
before its `POST`.
A Jira issue created by an export names its local issue in a property;
one whose issue this clone has not pulled is skipped, remembered,
and named as the cause on every issue waiting on it,
until `--adopt DURATION` says an issue that old is lost for good
and imports it (`7d`; `0` takes every one).
Two local copies of one Jira issue, an adoption whose original later arrives
or two clones importing one issue, are consolidated into the copy that
reached Jira first, whatever either has archived:
the other is synced once more, archived, its local-only values carried over,
and every relation naming it is pointed at the survivor, every run.
There is no `work.jira.*` in Starlark yet (v2), a known gap in the 1:1 rule.

Gotchas, hardened from use:

- `ls` is not a command; the list is the bare `git work issue`.
- `git work migrate` ran once on 2026-09-25 and refuses to run again;
  a fresh clone pulls the migrated refs and needs nothing.
  The migration copies `refs/identities/*` to `refs/work-users/*`
  before it reads anything; a command run before that copy
  fails with `identity doesn't exist`.
- No `user new` needed:
  the first mutating command sets your identity from git's `user.name`/`user.email`,
  adopting an existing identity with that email or creating one
  (`cache.RepoCache.EnsureUserIdentity`, our `828c228`).
  `user new`/`user adopt` remain as overrides,
  and `git work user me` prints the identity it settled on —
  the same document `work.user.me()` returns.
  `git work user` and `git work user me` print JSON like every other reader,
  `--format text` for a human.
- Config reads and remote transport go through the `git` CLI
  (package `gitcli`, wired in `execenv.LoadRepo`),
  because go-git reimplements git's environment incompletely:
  it ignores `[include]`/`[includeIf]`
  (upstream #1475, our `68abc13`)
  and authenticates to remotes with ssh-agent alone,
  ignoring `~/.ssh/config` and the default identity files
  (our `0ce996c`).
  Local object access stays on go-git.
  `git` must be on `PATH`;
  without it the repo is unwrapped and go-git's behaviour returns.
- Nothing holds the store open: readers take no lock at all,
  and a writer takes a short flock on `.git/git-work/write.lock`
  across one read-modify-commit and releases it at the commit (`d35de2e`).
  `termui`, `webui` and the view renderer hold nothing while they are open,
  so any number of commands run alongside them.
  The kernel drops the lock when the process dies,
  so there is no such thing as a stale one to remove;
  `timed out after 5s waiting for the write lock (held by pid N)`
  means a real concurrent writer.
- `flow archive` and `flow import --prune` did not commit until 2026-09-25:
  the archive reached the local cache file and nothing else,
  so it came back on the next cache rebuild.
  A flow you archived before that day may be back; archive it again.
- Do not `git work push`, or `git work sync`, without explicit intent;
  both publish the tracker to `origin`
  (`sync` pulls first, runs the Jira sync with `--jira`, then pushes).
- A GitHub remote may cap the refs one push can update
  (`GH013 … Pushes can not update more than N branches or tags`,
  enforced on these namespaces too, and on a private repository
  with no ruleset in sight, 2026-10-02).
  `PushRefs` then pushes the refs the rejection lists N at a time,
  silently, so a first push of a large store takes minutes, not forever.
- `termui` and `webui` need a real TTY; a human runs them, not the agent.
  Both read the frozen `refs/issues/*` copy, not the tracker.

## Direction (decided 2026-09-17, narrowed 2026-09-28)

This repository tracks two stories,
a board (`2298f37`) and a gantt (`00a63d9`),
each in the terminal first and then in the GUI.

Settled calls (details live in the referenced issues):

- **One team, one repository, one Jira project.** There is no project
  dimension anywhere; "cross-project" in older text meant across epics
  (`cd41e40`, 2026-09-21).
- `git work issue *` is **plumbing, agent-first**: JSON out by default,
  `new` takes a JSON document only, `get` returns one, `set`/`add`/`remove`
  take an object and commit one operation per key (no RFC 6902), writers
  print the id they created and nothing else, no sugar flags (`e8d6426`).
  `git work flow *` is porcelain, one verb per workflow (`b511c63`).
  The **Starlark host API mirrors the CLI one to one**: it is one module
  named after the binary, so `git work issue get ID` is
  `work.issue.get(id)`, a command's arguments are one JSON object of
  keyword arguments, and no name is script-only. `work` is the only
  predeclared name, which leaves `issue`, `flow`, `schema`, `view` and
  `user` for a script's own locals (2026-09-24). The target map is
  `doc/design/cli-convention.md` (2026-09-23).
- The query language is **jq**, via gojq, over the same JSON `--format json`
  prints; a saved view is a flow whose action renders (`483dbe2`, `3c9c24d`, `d56e6f1`).
  External ids such as Jira keys are immutable **aliases** kept in create-op
  metadata and accepted wherever an id is; the entity id stays the hash (`483dbe2`).
- Schema is *just configurable enough* to represent both Jira's and
  Linear's native models: fixed field kinds, configurable values;
  parent is a cardinality-1 relation (`bb9e89e`, `c090f9b`, `59fed1c`).
  An issue is a structural core (id, author, comments, timeline,
  participants) plus a fields map; four fields are built in and
  unremovable — `title`, `type`, `archived`, `rank` — and everything else,
  status and labels included, is preset config. There are **no field roles**: a
  flow's script names the fields it needs when it calls the host API (`f4bac00`,
  `d56e6f1`).
  Iterations are issues of type `iteration`; capacity is a field, not first
  class (`aba17f4`, `87a48c1`, revised 2026-09-23).
  **Every field and relation belongs to exactly one type**, Jira's model:
  `status` on `task` and on `epic` are two config entities with the same key,
  and sharing is YAML anchors in the schema file, not the store (`e7e58f2`).
  The entity is named `issue` for good, because Jira, Linear and GitHub call
  it that and Jira's types already include Initiative and Epic (`e7e58f2`).
  Manual rank is a LexoRank-style fractional index ordered by `(rank, id)`,
  so concurrent drags both survive (`441dcbb`); `rank` is the fourth built-in
  since 2026-10-02 (`e524644`), because Jira's Rank and Linear's `sortOrder`
  put an order on every issue. It is null until a drag writes one, and a null
  rank sorts after every set rank, the unranked keeping the query's order.
- Schema and flows are **config entities** of three shapes, `type`, `field`
  and `flow`, under `refs/work-schema` (types and fields) and
  `refs/work-flows`, so the entity boundary is the merge unit. A config entity
  is a plain document: shape, key, attributes with last-writer-wins per
  attribute, archived; four operations. The word `kind` is reserved for a
  field's data type. Enum values are per-attribute, so two
  people adding two statuses both survive (`7c90fbd`, `3556569`, `483dbe2`,
  `d56e6f1`). Removal is an archive op. Relations are fields of kind
  `relation`/`multi-relation` with `inverse` and `target_types`.
  A flow is **one Starlark function**: its name is the key, its docstring
  the description, its parameters the arguments; import rejects anything
  else in the file. It runs with `git work flow run <name>`. Rendering is
  the `work.view.*` module and the `view` command (`work.view.list`,
  `work.view.show`, `work.view.board`, `work.view.gantt`): **flows call
  views; views never call flows**, a view call renders, blocks on the
  script's thread until the user quits, and writes its own edits through the
  host, so a saved view is a flow that *calls* a view rather than returning a
  spec. **The command is the spec**: a view's input is one KWARGS object, the
  same object the Starlark call takes, so nothing is read from standard input
  and nothing is printed — `--gui` posts that object to the gui, and no TTY
  and no `--gui` is an error, because an agent wanting data runs `git work
  issue PROGRAM`. Items are a jq `query` the view owns and re-runs on a
  watcher change and after its own writes; there is no provider function, no
  static list and no `pick`. Actions injected into views are the deferred
  direction, in place of the `on_change`/`on_select` sketch; the renderer is
  Bubble Tea v2, behind `view` and `flow run`, never a `tui` command
  (`b511c63`, `f37603c`, `3df330f`, `0740bf3`, `84dfbde`, `8b06191`,
  revised 2026-09-24, design in `doc/design/terminal-renderer.md`).
  `rm` deletes a local ref on every tree; `archive` is the replicated removal.
  **Refs are the runtime source of truth.** `schema.yaml` and `.star` files
  in the tree are authoring files, merged by git and applied only by
  `git work schema import` and `git work flow import`, which upsert unless
  `--prune`; nothing reads the tree at runtime (`0740bf3`).
  **Automation is out of scope**: no daemon, no scheduler, no trigger inside
  git-work. Anything periodic is an external cron calling the CLI
  (`47b8430` closed, `483dbe2`).
- Concurrency: no daemon. Lock-free readers, a short write lock,
  ref→hash staleness diff, a ref watcher for live views (`d35de2e`, `d591cb3`, `63c68d1`).
  Nothing uses bleve and search is a non-goal for now (`3500366`);
  the dependency stays, unused, in the pristine `repository/`,
  so full-text search can come back on it if a workflow needs it (`0578918`).
- Ref namespaces carry the `work-` prefix: `refs/work-issues`,
  `work-schema`, `work-flows`, and
  `work-users` after the migration (`483dbe2`, named 2026-09-25 to match `git work user` and `work.user.me()`). The store is **migrated
  once** to the owned format with entity and comment ids preserved, and
  `formatVersion` bumps so old binaries refuse it rather than misread it
  (`f4bac00`, `bf6f392`). Done on 2026-09-25 as a copy, not a move:
  `refs/issues` stays as git-bug's frozen fallback until the deletion round,
  and the version gate keeps the two formats out of one namespace
  (`doc/design/store-migration.md`).
- Jira sync is bidirectional and **Jira is canonical**: 3-way per field,
  Jira wins on double-edit (`3c6d07a`).
  This repo dogfoods the `jira` preset with no Jira instance behind it, because
  an unverified preset exercised daily beats one exercised never (`59fed1c`).
- Surfaces are **Go only**: no JS toolchain in the repo (`867db1a`, 2026-09-21).
  `git work gui` is server-rendered HTML plus htmx, live over Server-Sent
  Events from the ref watcher, reading the cache in-process; the React webui,
  pnpm and (recommended) GraphQL leave (938434e, 8b06191).
  The terminal renderer is Bubble Tea v2 behind `view` and `flow run`,
  not a `tui` command (`84dfbde`).

## The schema

`schema.yaml` is the `jira` preset plus what the tracker needs:
a `decision` type, an `allocation` type
(`iteration`, `work`, `assignee`, `points`; no status, because it is a plan
and not a piece of work — `doc/design/allocations.md`, which is also why the
presets ship no such type), and `area` on `story`, `task` and `decision`
(the labels the tracker used to simulate a schema with,
migrated onto fields by `bf6f392`, mapping in `doc/design/store-migration.md`;
`phase` came the same way and was archived the same day,
because the parent story orders the work).
Edit the file and `git work schema import schema.yaml`;
the import writes only what differs.

## The flows

`flows/` holds the tracker's own flows, one `.star` file per flow,
the authoring copy of `refs/work-flows` the way `schema.yaml` is of `refs/work-schema`.
The directory is a review convention, not something the tool knows
(`doc/design/config-entity.md`):
edit a file and `git work flow import flows/`,
an upsert keyed on the function's name.
The first is `overview` (`b322a8e`):
open stories, open decisions and open tasks,
a story's tasks nested under it along `children` and folded,
where open is the status category read from the schema at run time,
never a status name.
`board` is the same issues on a kanban,
a column per open status and a swimlane per type.
`report` is "what changed since" as markdown (`2c0c256`, `doc/design/report.md`):
it reads two snapshots and the log window over the plumbing's
`--at` and `--from`/`--to`, and prints what was created, closed, changed and
commented on, grouped by parent.
It names `status` and `parent` itself, in the open, because what to report is
policy and Go has no field roles.

## Working conventions

- Every story gets a design document at `doc/design/<slug>.md`,
  approved before any of its code is written,
  and it records the decisions and their reasoning — not a plan of steps.
  Design runs a story ahead of implementation.
  A design that contradicts its tasks says so in the document
  *and* in a comment on the task, because whoever picks the task up
  may never read the document.
- Read the upstream package you build on before changing app-layer callers;
  never touch the pristine seven.
- go-git's gaps belong in `gitcli`, never in `repository`:
  where go-git reimplements git's own environment
  (config, transport, credentials) and gets it wrong,
  add an exec-backed override to that decorator.
- Every write goes through `cache/`.
  The no-lost-operations guarantee rests on the write lock and the re-read
  inside it (`d35de2e`, `2a51f66`); `dag.Entity.Commit` ends in an
  unconditional `UpdateRef`, so anything writing the entity refs from outside
  — a stray `git update-ref`, a second implementation — silently erases
  concurrent work. Reads are unrestricted and take no lock.
- On finishing a task, close its issue (`git work issue set <id> '{"status":"done"}'`)
  and reference the id in the commit message.
  Decisions get closed too, once the decision and its reasoning are recorded
  on the issue.
- Keep this guide accurate:
  if a CLI form or convention changes, update it in the same change.
