# git-work

`git-work` is a project tracker that lives in your git repository,
with Jira as a first-class sync backend.

- **Stored in git.** Issues, the schema, saved views and identities are
  ordinary refs (`refs/work-issues/*`, `refs/work-schema/*`,
  `refs/work-flows/*`, `refs/work-users/*`). No server, no database,
  nothing added to your working tree.
- **Distributed and offline.** Every issue is a log of operations that
  merges without conflicts, so two clones editing the same issue both
  keep their edits. `git work push` and `git work pull` go over your
  normal git remote.
- **Jira as a peer.** `git work jira sync` keeps a clone and a Jira Cloud
  project in step in both directions, field by field, with Jira
  canonical on a conflict.
- **Agent-first plumbing.** `git work issue` reads and writes JSON, and
  every listing is a [jq](https://jqlang.org) program over the issues.
- **A real terminal UI.** Lists, boards, gantt charts, allocation
  matrices and an issue page, all live and editable from the keyboard.
- **Scriptable.** Saved views and workflows are *flows*: Starlark
  functions stored in the repository that call the same API the command
  line does.
- **A schema you choose.** Types, fields and their values are
  configuration, with presets that mirror Jira's and Linear's models.

`git-work` is a hard fork of [git-bug](https://github.com/git-bug/git-bug).
It keeps git-bug's distributed entity engine, unmodified, and replaces
everything built on it: the issue model, the schema, the command line,
the views and the Jira bridge.

## Install

`git-work` is a single Go binary. It needs `git` on your `PATH` and Go
(see `go.mod`) to build:

```sh
git clone https://github.com/luisrayas3/git-work
cd git-work
go build -o git-work .
```

Put `git-work` anywhere on your `PATH`; git then dispatches
`git work <command>` to it. `make install` builds and installs into
`$GOPATH/bin` instead. Shell completions are in
[`misc/completion`](misc/completion), man pages in [`doc/man`](doc/man).
[`INSTALLATION.md`](INSTALLATION.md) has the details.

## Getting started

Pick a schema, either a preset or your own file:

```sh
git work schema init jira          # or: linear
git work schema                    # print it as YAML
git work schema export > schema.yaml   # edit, then: git work schema import schema.yaml
```

Create, change and read issues. Writers print the id they created and
nothing else; an id prefix or a Jira key works wherever an id does:

```sh
id=$(git work issue new '{"fields":{"title":"Rebuild the index on pull","type":"task","status":"to-do"},"body":"Why this is worth doing."}')
git work issue set $id '{"status":"in-progress","priority":"high"}'
echo "Started on it." | git work issue comment new $id -
git work issue get $id --format text
```

List with jq. The bare `git work issue` prints one line per issue for a
human; `git work issue list` prints JSON for a program:

```sh
git work issue 'map(select(.fields.status != "done"))'
git work issue list 'map(select(.fields.type == "task")) | group_by(.fields.status)'
```

The past is readable too: `--at 7d` shows the issues as they stood a week
ago, and `git work issue log --from 2026-09-21` lists what changed since.

Your identity comes from git's `user.name` and `user.email` on your first
write, so there is nothing to set up. Share the tracker with
`git work push` and `git work pull`, or `git work sync` for both.

For an AI agent, `git work quickstart` prints the whole model and this
repository's types as one markdown page.

## Views

Views draw in the terminal and write their edits straight back. Each is
one command taking one JSON object of arguments:

```sh
# a kanban, a column per status
git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'

# a list grouped by status, stories with their tasks folded under them
git work view list '{"fields":["status","priority","title"],"group_by":"status","expand":"children"}'

# dated work on a week chart
git work view gantt '{"start":"due","stop":"due","query":"map(select(.fields.due != null))"}'

# one issue, with its children in a side table
git work view show '{"id":"<id>","expand":{"relation":"children","fields":["status"]}}'

# create an issue in a form
git work view new
```

`Enter` opens, `Space` edits, a drag reorders or moves a card between
columns, `/` filters and `?` lists every key, in standard, vim and emacs
bindings. `git work view <kind> --help` lists each kind's arguments.

## Flows

A flow is a Starlark function stored under `refs/work-flows`, so it
travels with the repository. Its name is the command, its docstring the
help, its parameters the arguments. A script reaches everything through
one name, `work`, which mirrors the command line one to one:
`git work issue get ID` is `work.issue.get(id)`, and
`git work view board ...` is `work.view.board(...)`.

```sh
git work flow import flows/                 # upsert every .star in a directory
git work flow                               # what is installed
git work flow run report '{"from_":"7d"}'   # what changed this week, as markdown
```

This repository's own flows are in [`flows/`](flows): `overview`,
`board` and `report`.

## Jira

Bind one clone to one Jira Cloud project, review the mapping once, then
sync, from cron if you like:

```sh
git config git-work.jira.url https://<site>.atlassian.net
git config git-work.jira.project KEY
git config git-work.jira.email you@example.com
export JIRA_API_TOKEN=...            # or store it with git credential

git work jira schema > jira.yaml     # review the derived schema
git work schema import jira.yaml
git work jira sync
```

`git config git-work.display.id jira` then draws every issue by its Jira
key. The design, including conflicts, deletes and duplicate copies, is
[`doc/design/jira-sync.md`](doc/design/jira-sync.md).

## Documentation

- [`AGENTS.md`](AGENTS.md): the full command reference and the
  project's conventions. It is written for coding agents and is the most
  complete guide there is.
- [`doc/md`](doc/md/git-work.md): every command and flag, generated from
  the binary.
- [`doc/design`](doc/design): one design document per feature, recording
  each decision and its reasoning.
- [`doc/design/data-model.md`](doc/design/data-model.md): the
  distributed entity engine inherited from git-bug.

## Status

`git-work` is young and changes quickly. It tracks its own work in its
own store: clone this repository, run `git work pull`, then
`git work flow run overview`.

The git-bug commands it inherited, `git work bug`, `bridge`, `termui`
and `label`, still read git-bug's frozen `refs/issues/*` and are being
removed. Nothing written through them reaches the tracker. `git work
migrate` stays, to import an existing git-bug repository.

## License

GPLv3 or later, like git-bug, from which this project is forked
(© Michael Muré and the git-bug contributors). See [`LICENSE`](LICENSE).
