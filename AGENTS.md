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
- Only one process may hold the store at a time
  (pid lock at `.git/git-bug/lock`).
  `termui` and `webui` hold it while open, so quit them first.
  `already locked by … pid N` with a dead pid N is a stale lock, safe to remove.
- Do not `git work push` without explicit intent;
  it publishes the tracker to `origin`.
- `termui` and `webui` need a real TTY; a human runs them, not the agent.

## Label taxonomy

git-bug is flat (no epics, priority, or dates),
so structure is simulated with labels.
Replacing these with first-class schema is the product itself
(issues `1adfc5c` schema, `72d751d` hierarchy and dependencies).

| Prefix | Values |
| --- | --- |
| `phase:` | `0-bootstrap` `1-issue-model` `2-bridge` `3-app-reshape` `4-harness` `5-roadmap` |
| `area:` | `core` `issue-model` `bridge` `cli` `tui` `mcp` `infra` |
| `type:` | `spike` `decision` (omit for ordinary tasks) |
| `prio:` | `high` `med` `low` |

## Working conventions

- Read the upstream package you build on before changing app-layer callers;
  never touch the pristine seven.
- On finishing a task, close its issue (`git work bug status close <id>`)
  and reference the id in the commit message.
- Keep this guide accurate:
  if a CLI form or convention changes, update it in the same change.
