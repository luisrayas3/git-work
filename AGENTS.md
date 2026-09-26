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
and `entities/bug`, `commands/bug`, `termui` and what serves only them
are deleted in the round after, once the new surface has proven itself
on the tracker (`f4bac00`, 2026-09-22; deferred 2026-09-25 as `860d6e0`).
Cherry-picking upstream fixes into `cache/` and `bridge/` is not a goal.
If you believe you must edit a pristine package, **stop and flag it** —
it breaks upstream tracking and is a real architectural decision.
One such decision is on record and done:
the migration (`bf6f392`) changed three ref-name constants in `entities/identity`
so identities live at `refs/work-users` like every other namespace (`483dbe2`; named `users` on 2026-09-25, because every word a user meets says user);
nothing else in the seven is touched.

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

| Action | Command |
| --- | --- |
| Open work | `git work issue 'map(select(.fields.status != "done"))'` · `--format text` |
| One type | `git work issue 'map(select(.fields.type == "decision"))'` · by area: `select(.fields.area // [] \| index("cli"))` |
| Live list | `git work view list '{"fields":["type","status","priority","title"],"group_by":"status"}'` (TTY) |
| Create | `git work issue new '{"fields":{"title":"…","type":"task","status":"to-do","priority":"medium","area":["cli"],"parent":"<story id>"},"body":"…"}'` → prints the id |
| Show | `git work issue get <id>` · `--format text` |
| Close / reopen | `git work issue set <id> '{"status":"done"}'` · `'{"status":"to-do"}'` |
| Comment | `git work issue comment new <id> -` with the body on standard input |
| Tasks of a story | `git work issue 'map(select(.fields.parent == "<full story id>"))'` |
| Sync | `git work push` · `git work pull` (every namespace) |

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
| List | `git work issue [PROGRAM]` · `--format text`; PROGRAM is a jq program over the array of excerpts, the default being unarchived, last edited first |
| Create | `git work issue new DOC\|-` → prints the new id |
| Show | `git work issue get <id>` · `--format text` |
| Set fields | `git work issue set <id> '{"status":"done","estimate":3}'` (`null` clears; one commit whatever the number of keys) |
| Add / remove items | `git work issue add <id> '{"labels":["area:core"]}'` · `git work issue remove <id> …` (set semantics; relations of many cardinality too) |
| Comment | `git work issue comment new <id> BODY\|-` → prints the comment id · `comment edit <comment-id> BODY\|-` |
| History | `git work issue log <id>` · `--format text` |
| Archive / remove | `git work issue archive <id>` (an operation, replicated) · `git work issue rm <id>` (the local ref only) |

Everything in is JSON, everything out is JSON unless `--format text` is asked for,
and a document argument is read from standard input when it is `-`.
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

The schema itself, types and fields under `refs/work-schema`
(`3556569`, `bb9e89e`, design in `doc/design/config-entity.md`):

| Action | Command |
| --- | --- |
| Show | `git work schema` · `--format json` (alias of `export`) |
| Bootstrap | `git work schema init [jira\|linear]` → prints the created ids; refuses if any field exists |
| Round trip | `git work schema export > schema.yaml` · `git work schema import schema.yaml` (writes only what differs; a no-op when nothing did) |
| Import a partial file | `git work schema import FILE\|- [--prune] [--dry-run]`; an upsert unless `--prune`, which archives what the file omits |
| History | `git work schema log [KEY]` · `--format text`; one JSON object per line |
| Archive / remove | `git work schema archive <key>` (an operation, replicated) · `git work schema rm <key>` (the local ref only) |

A field's KEY is `<type>/<field>`: every field belongs to exactly one type, so
`task/status` and `epic/status` are two entities (`e7e58f2`). `title`, `type`
and `archived` are built in on every type and appear in the file only when an
entity overrides a name or a description. List position is the order — the file
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
| Run | `git work flow run <name>\|- [KWARGS\|-]` (`-` as the name runs the script on standard input without importing it) · `--gui` (errors until the gui process exists) |
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
`work.issue.*`, `work.schema.*`, `work.flow.*`, `work.view.*` and
`work.user.me()` —
the same verbs, the same arguments, the same output as the commands,
because both go through package `host` —
and writes through the cache, schema check included, like any command does.
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
`list` and `show` render in the terminal (`84dfbde`);
`board` and `gantt` error naming the renderer until it is built,
and `--gui` errors until the gui process exists (`8b06191`):

| Action | Command |
| --- | --- |
| List | `git work view list [KWARGS\|-] [--gui]` (nothing required) |
| Show | `git work view show [KWARGS\|-] [--gui]` (`id` required) |
| Board | `git work view board [KWARGS\|-] [--gui]` (`columns` required) |
| Gantt | `git work view gantt [KWARGS\|-] [--gui]` (`start` and `stop` required) |

Which other arguments a kind takes is `git work view <kind> --help`,
generated from the table in package `view`, which is the authority
(designed in `doc/design/terminal-renderer.md`).

Every kind but `show` takes `query`, a jq program over the same array `git work issue` prints,
which the view runs itself and re-runs on a ref-watcher change and after its own writes,
so a kanban with no flow at all is one command:
`git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'`.
KWARGS is read from standard input when it is `-`, like every document argument.
Arrows, vim and emacs keys all navigate;
`Space` grabs an item to move it (only when `rank` is bound),
`e` edits the field under the cursor,
where a value list ends with `(none)` and an emptied box clears the field
(`title` excepted, it cannot be cleared),
`Enter` opens show, `y` yanks the id, `?` lists the keys, `q` quits.

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
- Do not `git work push` without explicit intent;
  it publishes the tracker to `origin`.
- `termui` and `webui` need a real TTY; a human runs them, not the agent.
  Both read the frozen `refs/issues/*` copy, not the tracker.

## Direction (decided 2026-09-17)

Four target workflows drive every design call:
(1) roadmapping — initiatives/epics on a quarter-scale Gantt with resourcing, AI-assisted;
(2) weekly status report generated from the op log;
(3) in-person sync on a live, edit-heavy kanban;
(4) doing work — my tasks, pick one, link PRs, agents own subtasks (1 subtask ↔ 1 PR);
(5) sprint planning — allocating, re-prioritizing and grooming the store's backlog for the next iteration.

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
  participants) plus a fields map; three fields are built in and
  unremovable — `title`, `type`, `archived` — and everything else, status
  and labels included, is preset config. There are **no field roles**: a
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
  so concurrent drags both survive (`441dcbb`).
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
  Bleve is dropped; search is a non-goal (`3500366`).
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
a `decision` type, and `area` on `story`, `task` and `decision`
(the labels the tracker used to simulate a schema with,
migrated onto fields by `bf6f392`, mapping in `doc/design/store-migration.md`;
`phase` came the same way and was archived the same day,
because the parent story orders the work).
Edit the file and `git work schema import schema.yaml`;
the import writes only what differs.

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
