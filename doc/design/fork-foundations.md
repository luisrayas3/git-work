# Story: Fork foundations (`f6a60bd`)

**Outcome:** the one-time setup everything else assumes —
our own entity namespace, a Jira Cloud target to develop the bridge against,
and CI that keeps trunk building.

**Tasks:** `be69e67` namespace rename · `de1d8fb` Jira sandbox · `787c2e6` CI.

**Status:** design, awaiting approval.

## What changed since the tasks were written

Three findings from reading the tree; two of them move the design.

**1. The store is no longer empty, and it is already published.**
`be69e67` says "the store is empty, so this is free exactly once."
That stopped being true when we started dogfooding:
73 local `refs/bugs/*`, 2 `refs/identities/*`,
58 `refs/remotes/origin/bugs/*`,
and `git ls-remote origin 'refs/bugs/*'` confirms the tracker is on GitHub.
The rename is now a **migration**, not a fresh start.

**2. The migration is cheap anyway, because ids do not depend on the namespace.**
An entity's id is its create operation's id (`dag.Entity.Id`),
which is `entity.DeriveId(json.Marshal(op))` over the op alone
(`dag.IdOperation`); `dag.OpBase` serializes `type`, `timestamp`, `nonce`,
`metadata`, author — no namespace, no typename.
The ref name is not part of any object.
So migration is a pure ref rename:
no object rewriting, no id churn,
`be69e67` stays `be69e67` before and after.
Everything else the namespace names —
clocks, cache, index, selection — is derived local state that rebuilds itself.

**3. The CI comments are stale, in our favour.**
`.github/workflows/build-and-test.yml` claims the generated docs check needs
a webui build first. It doesn't: `webui/assets.go` is behind a `//go:build webui`
tag, and `go run doc/generate.go` runs clean with no Node toolchain —
verified, and a no-op against what's committed.
`go test ./...` also passes end to end with **no** bridge secrets set.
So a CLI-only CI needs no pnpm, no Playwright, and no secrets,
and can still keep the generated-artifact check.

## Decisions to confirm

### D1 — Migrate by ref rename, with a backup namespace, in one scripted pass

A `make migrate/issues-namespace` target, idempotent, in this order:

1. refuse to run if `.git/git-bug/lock` is held (termui/webui open);
2. copy every `refs/bugs/*` to `refs/backup/bugs-premigration/*` — the undo;
3. create `refs/issues/<id>` at the same hash for each, then delete `refs/bugs/<id>`;
4. same for `refs/remotes/origin/bugs/*` → `refs/remotes/origin/issues/*`;
5. drop derived local state: `.git/git-bug/cache/bugs`,
   `.git/git-bug/clocks/bugs-{create,edit}`, `.git/git-bug/indexes/bugs`;
6. print what to do about origin, and stop.

The backup refs stay until the next story starts, then get deleted by hand.

**Alternative rejected:** a `git work migrate` command that detects legacy refs.
It is permanent code for a problem exactly one repository has, exactly once.
A Makefile target we delete later is the right weight.

### D2 — Origin is a separate, explicit step, run by you

Publishing `refs/issues/*` and deleting `refs/bugs/*` on origin is destructive
and remote, and AGENTS.md says not to push the tracker without explicit intent.
The target prints the two commands and does not run them:

```sh
git push origin 'refs/issues/*:refs/issues/*'
git ls-remote origin 'refs/bugs/*' | cut -f2 | xargs git push origin -d
```

Between those two commands the tracker exists twice on origin, which is
harmless — no other clone consumes it. If you have a second clone, the
migration there is: run the same target, or delete `refs/bugs/*` and re-fetch.

### D3 — Rename the namespace and typename, keep every Go identifier

Per `be69e67`: `entities/bug/bug.go` gets `Namespace = "issues"`,
`Typename = "issue"`. Package `bug`, type `BugCache`, directory `commands/bug`,
file names — all unchanged, so `cache/` and `bridge/` cherry-picks from
upstream still apply. A Go-level rename is a later mechanical change, if ever.

What the two constants actually move, all of it derived from
`dag.Definition` by pristine code we do not touch:

| Surface | Before | After |
| --- | --- | --- |
| entity refs | `refs/bugs/<id>` | `refs/issues/<id>` |
| remote-tracking refs | `refs/remotes/origin/bugs/<id>` | `…/issues/<id>` |
| lamport clocks | `.git/git-bug/clocks/bugs-{create,edit}` | `issues-{create,edit}` |
| cache excerpts | `.git/git-bug/cache/bugs` | `cache/issues` |
| bleve index | `.git/git-bug/indexes/bugs` | `indexes/issues` (deleted in `3500366`) |
| selection | `.git/git-bug/select/bugs` | `select/issues` |
| progress bars, errors | "bug" | "issue" |

Plus the CLI surface: `commands/bug/bug.go` `Use: "bug [QUERY]"` → `"issue [QUERY]"`,
its `Short`/`Long`/`Example` text, the `Makefile` clean targets that name
`refs/bugs/`, the AGENTS.md command table, and regenerated `doc/man`, `doc/md`,
`misc/completion`.

`repository/repo_testing.go` hardcodes `refs/bugs/ref1` — that is a pristine
package and those are arbitrary test ref names, not our namespace. Left alone.

### D4 — Keep `bug` as a hidden alias for one phase

`Aliases: []string{"bug"}` plus `Hidden` on nothing else: one line, keeps every
habit and every stale note working through the phase where the CLI is being
reshaped anyway. Revisit when `git work issue` becomes plumbing in `e8d6426`.

### D5 — Do the other once-free renames now, or not at all

Three more strings still say `git-bug`, and each is free *today* and annoying
later:

- `execenv.RootCommandName = "git-bug"` — every usage line, every error
  message, and every generated man page currently names a binary the user does
  not have. `git work --help` today prints `Usage: git-bug bug [QUERY]`.
- the local storage directory `.git/git-bug` — holds only derived state
  (cache, clocks, indexes, selection, lock), so moving it costs a rebuild.
- the git-config namespace `git-bug.*` and the keyring service name `git-bug` —
  both empty until the Jira bridge stores its first token, i.e. until `de1d8fb`.

**Recommendation: yes, all three, in a separate commit after the namespace
rename.** The keyring one in particular stops being free the moment you add a
Jira token. Cobra takes `Use: "git work"` fine for help text; if the generated
completions come out malformed I fall back to `git-work` and say so.

**This is the one place I am extending the task's stated scope**, so it is the
decision I most want a yes or no on. A no costs nothing structural — it just
means `git-bug` stays in the help text and we pay for the keyring rename later.

### D6 — CI: one workflow, ubuntu + macos, Go 1.26 and 1.27, no secrets

`787c2e6` says trim rather than rewrite. Trimmed:

- **`build-and-test.yml`** keeps `with-go` and `lint`, drops the `with-node`
  job entirely (Playwright, pnpm, webui). `with-go` loses the pnpm/Node setup
  and the bridge secrets, drops `windows-latest`, and runs `go build ./...`,
  `go vet ./...`, `go test ./...` instead of `make` / `make test` — `make`
  requires pnpm. Matrix stays `[1.26.x, 1.27.x] × [ubuntu, macos]`: 1.26 is what
  AGENTS.md requires, 1.27 is `.tool-versions` and catches the next toolchain
  early. `lint` keeps `gofmt -l` and the generated-files check, minus its
  now-unnecessary `make build-webui` step.
- **`scan.yml`** keeps `govulncheck`, drops the CodeQL job — CodeQL's value on
  a two-person fork of an already-scanned upstream does not pay for the minutes.
- **`trunk.yml`** deleted: it pushes benchmark history to a gh-pages branch we
  do not have, for benchmarks that are upstream's concern.
- **`release.yml`** left exactly as is. It only fires on `v*` tags, which we
  will not cut for months, and it is the file most likely to take an upstream
  fix.

Webui CI comes back when `f32ea71` makes the webui ours again.

## Task `de1d8fb` — the Jira sandbox is yours, and here is the shopping list

This is the one task in the story an agent cannot do: it needs an Atlassian
account. What the bridge work in phase 3 needs from it, in priority order:

1. **A free Jira Cloud site** with a Scrum project whose configuration is as
   close to the company's real one as you can make it without exporting
   anything sensitive: the real issue-type set (Epic, Story, Task, Sub-task,
   Bug), a non-default workflow with at least one status per category
   (to-do / in-progress / done) and at least one status that is *not* named the
   same as its category — that asymmetry is exactly what `59fed1c` must
   represent — priorities, and a board with sprints enabled.
2. **A second project shaped like Linear**: Initiative → Project → Issue, no
   sub-tasks, cycles instead of sprints. It is how we prove the schema is
   configurable rather than Jira-flavoured.
3. **A sanitized schema dump** committed as fixtures, from
   `/rest/api/3/field`, `/rest/api/3/issuetype`, and the project's statuses and
   priorities. This is the real deliverable for the phase 2 schema work: it
   lets `59fed1c` build presets against a true Jira shape, and it lets
   bridge tests run offline in CI.
4. **An API token**, stored via `git work bridge auth add-token` — which is
   keyring-backed, never the repo, never git config. `0a4390d` checks the
   inherited client against REST v3 before we rely on any of it.

Until 1–2 exist, phase 3 is still buildable against the fixtures from 3;
anything that needs a live site gets a test gated on an env var and skipped in
CI. So this task is not on the critical path — but the fixtures are, and they
are the first thing to produce once the site is up.

## Order of work

1. **CI first** (`787c2e6`) — smallest, independent, and it is the safety net
   that tells us the rename did not break the build.
2. **Namespace rename** (`be69e67`) — code, then migration target, then run it,
   then regenerate docs/completions, then AGENTS.md in the same commit.
3. **Root-name and storage renames** (D5, if approved) — separate commit,
   because it is the one that is easy to revert.
4. **Sandbox** (`de1d8fb`) — docs and the fixture checklist land now; the task
   stays open until you have the site, and it does not block phase 1.

## Done when

- `git work issue` lists all 73 issues, and `git work bug` still works via the alias;
- `git for-each-ref refs/bugs` is empty locally and `git ls-remote origin 'refs/bugs/*'` is empty;
- ids are unchanged — `git work issue show be69e67` resolves to this story's rename task;
- a fresh clone of origin builds and shows the tracker with no local state;
- CI is green on a PR and on a trunk push, with no secrets configured;
- AGENTS.md's command table and this document agree with the binary.

## Risks

- **Losing the tracker mid-rename.** Mitigated by the backup namespace in D1
  and by origin still holding `refs/bugs/*` until you delete them — two
  independent copies during the risky window.
- **A held lock or an open termui during migration.** The target refuses to run
  rather than racing; a stale lock with a dead pid is safe to remove.
- **`Use: "git work"` confusing cobra's completion generation.** Contained: if
  the generated completions look wrong, fall back to `git-work` and note it.
- **Upstream cherry-pick friction.** Only `entities/bug/bug.go` (two constants),
  `commands/bug/bug.go` (help text), and the workflows diverge further than they
  already have. The pristine seven are untouched.
