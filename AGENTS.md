# Agent guide for git-work

This repository is a **fork of [git-bug](https://github.com/git-bug/git-bug)**
(forked at `e1c21a42`),
evolving into a project-management tool
with **Jira as a first-class sync backend**.

We **dogfood**:
this project's own tasks live in its own git-bug store (`refs/bugs/*`),
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
If you believe you must edit a pristine package, **stop and flag it** —
it breaks upstream tracking and is a real architectural decision.

Design consequence:
there is **no atomic multi-entity commit**
(`dag.Entity.Commit` writes one ref).
Model cross-issue relationships (parent, dependencies) as ops
that store the other issue's `entity.Id` and resolve via `entity.Resolvers` —
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

# Full build including the web UI (needs pnpm): make build
```

## Operating the tracker

The CLI is namespaced;
task commands live under `git work bug`
(this becomes `git work issue`/`work` as the entity is reshaped).
Forms below are verified against 0.10.x.

| Action | Command |
| --- | --- |
| List all | `git work bug` |
| Filter | `git work bug --label phase:2-bridge` · `--status open` |
| Query | `git work bug status:open sort:edit-desc` · `git work bug "text"` |
| Create | `git work bug new -t "Title" -m "Body"` → `<id> created` |
| Show | `git work bug show <id>` |
| Add label(s) | `git work bug label new <id> <label> [<label>…]` |
| Remove label | `git work bug label rm <id> <label>` |
| Close / reopen | `git work bug status close <id>` · `status open <id>` |
| Comment | `git work bug comment new <id> -m "…"` |
| Sync | `git work push` · `git work pull` (writes/reads `refs/bugs/*`) |
| Interactive | `git work termui` (TTY) · `git work webui` (webui build) |

Gotchas, hardened from use:

- `ls` is not a command; the list is the bare `git work bug`.
- Config reads go through the `git` CLI
  (package `gitconfig`, wired in `execenv.LoadRepo`)
  because go-git ignores `[include]`/`[includeIf]`
  (upstream #1475, our `68abc13`).
  `git` must be on `PATH`;
  without it, reads fall back to go-git
  and included `user.*` is invisible.
- Only one process may hold the store at a time
  (pid lock at `.git/git-bug/lock`).
  `termui` and `webui` hold it while open, so quit them first.
  `already locked by … pid N` with a dead pid N is a stale lock, safe to remove.
- Do not `git work push` without explicit intent;
  it publishes the tracker to `origin`.
- `termui` and `webui` need a real TTY; a human runs them, not the agent.

## Direction (decided 2026-09-17)

Four target workflows drive every design call:
(1) roadmapping — initiatives/epics on a quarter-scale Gantt with resourcing, AI-assisted;
(2) weekly status report generated from the op log;
(3) in-person sync on a live, edit-heavy kanban;
(4) doing work — my tasks, pick one, link PRs, agents own subtasks (1 subtask ↔ 1 PR).

Settled calls (details live in the referenced issues):

- `git work issue *` is **plumbing, agent-first**: JSON out by default,
  RFC 6902 JSON Patch in (`e8d6426`).
  `git work flow *` is porcelain, one verb per workflow (`b511c63`).
- Schema is *just configurable enough* to represent both Jira's and
  Linear's native models: fixed field kinds, configurable values;
  parent is a cardinality-1 relation (`bb9e89e`, `c090f9b`, `59fed1c`).
- Concurrency: no daemon. Lock-free readers, a short write lock,
  ref→hash staleness diff, a ref watcher for live views (`d35de2e`, `d591cb3`, `63c68d1`).
  Bleve is dropped; search is a non-goal (`3500366`).
- Entity namespace becomes `refs/issues` (`be69e67`); Go package names stay `bug`
  so upstream fixes to `cache/` and `bridge/` still cherry-pick.
- Jira sync is bidirectional and **Jira is canonical**: 3-way per field,
  Jira wins on double-edit (`3c6d07a`). This repo dogfoods without Jira.
- TUI: Bubble Tea rewrite later (`84dfbde`). GUI: extend the inherited webui;
  a framework rethink is parked (`867db1a`).

## Label taxonomy

git-bug is flat (no epics, priority, or dates),
so structure is simulated with labels until the schema work lands
(`bb9e89e` schema engine, `c090f9b` relations).

| Prefix | Values |
| --- | --- |
| `phase:` | `0-bootstrap` `1-concurrency` `2-issue-model` `3-jira-sync` `4-flows` `5-surfaces` |
| `area:` | `core` `issue-model` `bridge` `cli` `tui` `gui` `mcp` `infra` |
| `type:` | `spike` `decision` (omit for ordinary tasks) |
| `prio:` | `high` `med` `low` |

Phases are ordered by dependency, not calendar:
concurrency precedes the model because agent + TUI coexistence
and the mutate path both rest on it.

## Working conventions

- Read the upstream package you build on before changing app-layer callers;
  never touch the pristine seven.
- On finishing a task, close its issue (`git work bug status close <id>`)
  and reference the id in the commit message.
- Keep this guide accurate:
  if a CLI form or convention changes, update it in the same change.
