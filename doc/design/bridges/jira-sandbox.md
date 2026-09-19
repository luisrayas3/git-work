# The Jira sandbox (`de1d8fb`)

This repository dogfoods its own tracker and has no Jira behind it, so the
bridge work in phase 3 needs a target somewhere else. This document says what
that target has to look like, what to extract from it, and how the bridge is
built while it does not exist yet.

Creating the site needs an Atlassian account, so **this one is yours**, not an
agent's. Everything below is the checklist.

## 1. A free Jira Cloud site

One site, two projects. The first is the realistic one; the second exists to
keep the schema honest.

### Project A — shaped like the company's Jira

As close to the real configuration as you can get without exporting anything
sensitive. What the bridge work actually depends on:

- [ ] **Issue types** covering the real hierarchy: Epic, Story, Task, Sub-task,
      Bug. `33148f2` imports the hierarchy through these, and `c090f9b` models
      parent as a cardinality-1 relation, so the Epic→Story→Sub-task chain
      needs to exist to be tested.
- [ ] **A custom workflow**, not the default one, with at least one status per
      category (to-do / in-progress / done) and — this is the part that matters
      — **at least one status whose name does not match its category**, such as
      "In Review" sitting in the in-progress category, or "Won't Do" in done.
      Tooling keys off categories, never names (`bb9e89e`); a workflow where
      name and category coincide would let a name-keyed bug pass every test.
- [ ] **Transitions that are not universal**: at least one status reachable
      only from a specific other status. `32d372e` exports status changes as
      transitions, and the interesting failure is a transition that is not
      available from where the issue currently sits.
- [ ] **Priorities**, ideally the real set rather than the default five.
- [ ] **A board with sprints enabled**, and at least one closed sprint, one
      active, one future. `aba17f4` decides how iterations are modeled and
      `33148f2` imports the sprint field; closed sprints are where carry-over
      behaviour shows up.
- [ ] **A custom field or two** of different kinds (a number for story points,
      a date, a single-select), to prove `69b7be0`'s field-mapping config is
      mapping rather than hardcoding.
- [ ] **A handful of issues** exercising all of it: an epic with children, a
      sub-task, a link between two issues (blocks / is blocked by), assignees,
      a few comments, something closed.

### Project B — shaped like Linear

The point of `59fed1c` is that the schema represents both native models, so
one project is configured the way Linear works rather than the way Jira does:

- [ ] Initiative → Project → Issue as the hierarchy, no sub-tasks.
- [ ] Cycles rather than sprints — fixed cadence, auto-rolling.
- [ ] Linear's status set (Backlog, Todo, In Progress, In Review, Done,
      Canceled) mapped onto categories.

It does not need to be a faithful Linear clone. It needs to be different enough
from project A that a preset which works for both cannot be accidental.

## 2. Credentials

An Atlassian API token, added through `git work bridge auth add-token`, which
stores it in the keyring (`repository/keyring.go`) — never in the repository,
never in git config, never in an environment variable that ends up in a shell
history file.

Two notes:

- The keyring's service name is still `git-bug` rather than `git-work`. That
  rename lives below the pristine-library boundary, so it was deliberately not
  done (see //doc/design:fork-foundations.md, D5). It costs one re-add of the
  token whenever we decide to do it.
- `0a4390d` checks the inherited `bridge/jira/client.go` against Jira Cloud
  REST v3 and current auth before any of phase 3 leans on it. Upstream's client
  predates the v2→v3 move, so assume nothing until that spike runs.

## 3. The fixtures — the part that is actually on the critical path

The site unblocks integration testing. The **schema dump** unblocks design, and
it is needed earlier:

- [ ] `GET /rest/api/3/field` — every field, custom ones included.
- [ ] `GET /rest/api/3/issuetype`.
- [ ] `GET /rest/api/3/project/<KEY>/statuses` — statuses per issue type, with
      their categories.
- [ ] `GET /rest/api/3/priority`.
- [ ] The board's sprints, from the agile API.

Committed under `bridge/jira/testdata/sandbox/`, one JSON file per endpoint,
for both projects. Sanitize before committing: drop `emailAddress`,
`displayName`, `avatarUrls` and account ids from every user object, and rename
anything that identifies the company. The shape is what matters, not the
contents.

These fixtures are what `59fed1c` builds the `jira` preset against, and what
lets the bridge's unit tests run in CI with no network and no secrets.

## 4. Until it exists

Phase 3 is not blocked on the site, only on the fixtures:

- Field mapping (`69b7be0`), import (`33148f2`) and export (`32d372e`) are
  built and unit-tested against the fixtures.
- Anything needing a live site — the auth spike, the first real round trip,
  `a3a8d16`'s sync loop under a real rate limiter — gets a test gated on an
  env var naming the sandbox, and skipped when it is unset. CI never sets it,
  which is why the trimmed workflows carry no secrets (`787c2e6`).

So the order that helps most: create the site, dump the fixtures, and the rest
can proceed while the site sits idle.
