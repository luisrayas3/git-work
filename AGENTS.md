# Agent guide for git-work

This repository is a **fork of [git-bug](https://github.com/git-bug/git-bug)**
(forked at `e1c21a42`),
evolving into a project-management tool
with **Jira as a first-class sync backend**.

We **dogfood**:
this project's own tasks live in its own git-bug store (`refs/issues/*`),
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
task commands live under `git work issue`
(`bug` remains as an alias while the CLI is reshaped; `be69e67`).
Forms below are verified against 0.10.x.

| Action | Command |
| --- | --- |
| List all | `git work issue` |
| Filter | `git work issue --label phase:2-bridge` · `--status open` |
| Query | `git work issue status:open sort:edit-desc` · `git work issue "text"` |
| Create | `git work issue new -t "Title" -m "Body"` → `<id> created` |
| Show | `git work issue show <id>` |
| Add label(s) | `git work issue label new <id> <label> [<label>…]` |
| Remove label | `git work issue label rm <id> <label>` |
| Close / reopen | `git work issue status close <id>` · `status open <id>` |
| Comment | `git work issue comment new <id> -m "…"` |
| Sync | `git work push` · `git work pull` (writes/reads `refs/issues/*`) |
| Interactive | `git work termui` (TTY) · `git work webui` (webui build) |

Gotchas, hardened from use:

- `ls` is not a command; the list is the bare `git work issue`.
- No `user new` needed:
  the first mutating command sets your identity from git's `user.name`/`user.email`,
  adopting an existing identity with that email or creating one
  (`cache.RepoCache.EnsureUserIdentity`, our `828c228`).
  `user new`/`user adopt` remain as overrides.
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
- Only one process may hold the store at a time
  (pid lock at `.git/git-work/lock`).
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
(4) doing work — my tasks, pick one, link PRs, agents own subtasks (1 subtask ↔ 1 PR);
(5) sprint planning — cross-project allocation, re-prioritizing and grooming for the next iteration.

Settled calls (details live in the referenced issues):

- `git work issue *` is **plumbing, agent-first**: JSON out by default,
  RFC 6902 JSON Patch in (`e8d6426`).
  `git work flow *` is porcelain, one verb per workflow (`b511c63`).
- Schema is *just configurable enough* to represent both Jira's and
  Linear's native models: fixed field kinds, configurable values;
  parent is a cardinality-1 relation (`bb9e89e`, `c090f9b`, `59fed1c`).
  Iterations are a first-class entity, not a text field (`aba17f4`, `87a48c1`).
  Manual rank is a LexoRank-style fractional index ordered by `(rank, id)`,
  so concurrent drags both survive (`441dcbb`).
- Schema, flows, saved views and automation rules all live in **one CRDT config
  entity** under `refs/work/*`, with per-key ops so concurrent edits to
  different keys both survive (`7c90fbd`, `3df330f`).
  Automation has no daemon either: rules fire opportunistically and from live
  views, with a scheduled backstop, so actions are idempotent (`221b629`).
- Concurrency: no daemon. Lock-free readers, a short write lock,
  ref→hash staleness diff, a ref watcher for live views (`d35de2e`, `d591cb3`, `63c68d1`).
  Bleve is dropped; search is a non-goal (`3500366`).
- Entity namespace becomes `refs/issues` (`be69e67`); Go package names stay `bug`
  so upstream fixes to `cache/` and `bridge/` still cherry-pick.
- Jira sync is bidirectional and **Jira is canonical**: 3-way per field,
  Jira wins on double-edit (`3c6d07a`).
  This repo dogfoods the `jira` preset with no Jira instance behind it, because
  an unverified preset exercised daily beats one exercised never (`59fed1c`).
- TUI: Bubble Tea rewrite later (`84dfbde`). GUI: extend the inherited webui;
  a framework rethink is parked (`867db1a`).

## Label taxonomy

git-bug is flat (no epics, priority, or dates),
so structure is simulated with labels until the schema work lands
(`bb9e89e` schema engine, `c090f9b` relations).
This whole section is scheduled for deletion: `bf6f392` migrates these labels
onto real fields and rewrites what you are reading.

| Prefix | Values |
| --- | --- |
| `type:` | `story` `task` `decision` |
| `story:` | 7-char id of the parent story (on tasks and decisions) |
| `phase:` | `0-bootstrap` `1-concurrency` `2-issue-model` `3-jira-sync` `4-flows` `5-surfaces` |
| `area:` | `core` `issue-model` `bridge` `cli` `tui` `gui` `mcp` `infra` |
| `prio:` | `high` `med` `low` |

Two levels for now: **stories** (outcomes, roughly one per workflow) contain
**tasks** and **decisions**. Titles carry the level for easy scanning:
`Story: …`, `Task: …`, `Decision: …`.
A story's body lists its tasks; `git work issue --label story:<id>` lists them live.
Stories carry `type:story` and an `area:`, not a phase.

Phases are ordered by dependency, not calendar:
concurrency precedes the model because agent + TUI coexistence
and the mutate path both rest on it.

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
  unconditional `UpdateRef`, so anything writing `refs/issues/*` from outside
  — a stray `git update-ref`, a second implementation — silently erases
  concurrent work. Reads are unrestricted and take no lock.
- On finishing a task, close its issue (`git work issue status close <id>`)
  and reference the id in the commit message.
  Decisions get closed too, once the decision and its reasoning are recorded
  on the issue.
- Keep this guide accurate:
  if a CLI form or convention changes, update it in the same change.
