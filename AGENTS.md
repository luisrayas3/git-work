# Agent guide for git-work

This repository is **git-work**,
a **hard fork of [git-bug](https://github.com/git-bug/git-bug)**,
grown into a project-management tool
with **Jira as a first-class sync backend**.

The project dogfoods itself:
its own work is tracked with `git work`,
in this repository's refs.

**Run `git work quickstart` before using the tool.**
It is the guide to using git-work,
both generally and for this repo.
Its static half is `host/quickstart.md`.

## Build and test

Requires **Go 1.26** (native, or `nix shell nixpkgs#go`)
and `git` on the `PATH`.

```sh
go build -o git-work .   # ~/.local/bin/git-work is a symlink to it; rebuild after a change
go test ./...
gofmt -l .               # must print nothing
go generate              # after a command or flag changes: doc/md, doc/man, misc/completion
```

## Layout

| Package | What it is |
| --- | --- |
| `entities/issue` | the issue entity: a structural core plus a fields map, and its operations |
| `entities/config` | the config entity: schema types and fields, and flows |
| `schema` | the compiled schema, and the validation every write goes through |
| `cache` | the only write path: the write lock, the excerpts, the ref watcher |
| `host` | the one code path behind both the command line and Starlark |
| `commands` | the cobra command tree, a thin layer over `host` |
| `flow`, `flow/run` | reading a flow's source, and the Starlark runtime |
| `view`, `tui` | the view kinds' argument table, and the Bubble Tea renderer, which runs the flows home names |
| `query/jq` | the query language (gojq) |
| `rank` | the fractional index behind manual order |
| `jira` | the Jira sync |
| `gitcli` | `git`-CLI overrides for what go-git gets wrong |
| `migrate` | the one-time git-bug store migration, kept to import other git-bug repositories |

**Legacy, frozen, being deleted** (`860d6e0`):
`entities/bug`, `commands/bug`, `termui`, `bridge`,
`query` (git-bug's query language), `label`,
and the cache's bug subcache.
They read git-bug's frozen `refs/issues/*`, not the tracker.
Do not extend them;
`entities/bug` stays as `migrate`'s decoder.

## Architectural rules

- **Every write goes through `cache/`.**
  The no-lost-operations guarantee rests on the write lock and the re-read inside it;
  `dag.Entity.Commit` ends in an unconditional `UpdateRef`,
  so anything else writing entity refs silently erases concurrent work.
  Reads take no lock.
- **One entity per commit.**
  There is no atomic multi-entity commit,
  so a relationship between issues is a field holding the other issue's full id:
  eventually consistent, like Jira.
  Do not add batch commits to the core.
- **The command line and Starlark are one API.**
  Both go through `host`,
  so every verb has the same name, arguments and output in both
  (`work.issue.get(id)` is `git work issue get ID`),
  and nothing exists on one side only.
  `doc/design/cli.md` is the map every command follows:
  plumbing is JSON in and out,
  writers print an id or nothing,
  no sugar flags.
- **No field roles.**
  Go never names a field such as `status` or `parent`;
  only `title`, `type`, `archived` and `rank` are built in.
  Policy lives in the schema and in flows.
- **Refs are the runtime truth.**
  `schema.yaml` and `flows/*.star` are authoring copies,
  applied by `git work schema import` and `git work flow import`;
  nothing reads the tree at run time.
- **Flows call views; views never call flows.**
  A view's input is one KWARGS object,
  the same one the Starlark call takes,
  and the argument table in package `view` is the authority for it.
  The `home` view only names a flow:
  the renderer runs it on its worker through `flow/run`,
  and the flow's own view call pushes onto the same stack (`tui/home.go`).
- **The terminal is the only interactive surface.**
  There is no GUI and no JS toolchain;
  everything is Go.
- **No automation inside git-work**:
  no daemon, scheduler or trigger.
  Anything periodic is an external cron calling the CLI.
- **Jira is canonical**,
  and the model is one team, one repository, one Jira project.
- **go-git's gaps go in `gitcli`**:
  where go-git reimplements git's environment (config, transport, credentials)
  and gets it wrong,
  add an exec-backed override there.

## This repository's tracker

- Types in use are `story`, `task` and `decision`;
  a task or decision names its story in `parent`,
  and `area` says which part of the code it touches.
  A title never repeats the type.
- The schema's authoring copy is `schema.yaml` and the flows' is `flows/`:
  edit them,
  then `git work schema import schema.yaml` or `git work flow import flows/`.
- On finishing a task,
  set its status to `done`
  and reference its id in the commit message.
  Close a decision once the decision and its reasoning are recorded on the issue.
- **Do not `git work push` or `git work sync` without explicit intent**:
  both publish the tracker to `origin`.
- Never write with `git work bug`:
  it writes the frozen copy, not the tracker.

## Working conventions

- **The design documents are organized by aspect, not by feature.**
  `doc/design/README.md` lists them,
  one per part of the system (the store, the schema, the views, the Jira sync …).
  Each says what holds *now* and why:
  the decisions in force, their reasoning,
  and the alternatives rejected where the reason still matters.
  It is not a history and not a plan:
  no "revised on", no "supersedes", no order of work, no deferred list
  (future work is an issue).
  What a decision used to be is in git and on the issues.
- **The set of aspects changes only on a human's explicit say-so.**
  New work goes into the aspect it belongs to,
  even when that makes a document longer.
  An agent may *recommend* adding, splitting or merging an aspect,
  conservatively and with its reason,
  but does not do it until a human has confirmed that change directly;
  approval of a story's design is not approval of a new aspect.
- **A story's design is written on the story**,
  in its description or a comment,
  and approved there before any of its code is written:
  which aspects change, the decisions, and why.
  A design that contradicts its tasks says so on the story
  *and* in a comment on the task,
  since whoever picks the task up may never read the story.
- **The change that builds a decision updates its aspect document**,
  so a document never describes what is not built
  and never keeps what no longer holds.
  A decision that changes is rewritten in place, where it stands,
  and every sentence elsewhere that it makes wrong is fixed in the same change.
- **A decision is a heading, and code cites it by document and heading**:
  `(doc/design/views.md, "The ghost")`.
  A heading names the decision, so keep it short and stable;
  renaming one means updating what cites it.
  `TestDesignCitations` fails on a citation, or a link between documents,
  that names a document or heading that does not exist.
- **Tests own their fixtures.**
  No test reads this repository's `schema.yaml`, `flows/` or tracker:
  those are this project's configuration,
  and change for their own reasons.
  A test that needs a schema defines one inline or in `testdata/`,
  or uses a preset from code when it means that preset.
- **Keep the guides accurate in the change that makes them wrong.**
  A change to how git-work is used updates `host/quickstart.md`
  (a test checks that every command it names exists);
  a change to a decision updates its aspect document in `doc/design`;
  a change to how it is built or structured updates this file.
