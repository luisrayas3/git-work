# Story: Many processes share one store safely (`e68d62b`)

**Outcome:** a human keeps the TUI or GUI open while agents and other humans
mutate the store from short CLI calls; nothing blocks, nothing is lost, and the
open views update within a second. No daemon.

**Tasks:** `24e82d6` spike · `d35de2e` write lock · `2a51f66` re-read under lock ·
`d591cb3` staleness · `63c68d1` live invalidation · `f39878f` atomic cache
writes · `3500366` drop bleve.

**Status:** design, awaiting approval.

## What the code does today

Four facts, all verified in the tree, that together define the problem.

**1. `UpdateRef` is not a compare-and-swap.**
`dag.Entity.Commit` builds its operation pack with `e.lastCommit` as parent and
finishes with `repo.UpdateRef(ref, e.lastCommit)`
(`entity/dag/entity.go:510`). `GoGitRepo.UpdateRef` is a bare
`Storer.SetReference` (`repository/gogit.go:745`) — it does not check what the
ref pointed at. Two processes that both read issue E at commit C, each append
an operation and commit, each produce a commit whose parent is C, and the
second `SetReference` silently overwrites the first. The first writer's
operation stays in the object database, unreachable from the ref. That is the
loss `24e82d6` is asked to demonstrate.

The comment above that line says the remote will ensure the push is
fast-forward, which is true and is exactly why this is invisible today: the
*remote* rejects a non-fast-forward, the local ref accepts anything.

**2. The pid lock is held for the whole process lifetime.**
`RepoCache.lock` writes the pid at open and `Close` removes it
(`cache/repo_cache.go:175`, `:211`), and `repoIsAvailable` refuses to open when
a live pid holds it. So the lock does prevent concurrent writers — by
preventing concurrent *anything*. An open termui blocks every agent command for
as long as it is open, which is the thing this story exists to fix. Upstream
knew: there is a comment at `cache/repo_cache.go:270` saying a mutex "might be
nice to have".

**3. The cache never checks itself against the refs.**
`SubCache.Load` validates a format version and one heuristic — that the bleve
document count equals the excerpt count — and then trusts the file
(`cache/subcache.go:98`). There is a literal `TODO: find a way to check lamport
clocks`. Nothing maps refs to what the cache believes about them, so a cache
written by another process, or left behind by a `git fetch`, is indistinguishable
from a current one.

**4. Cache writes truncate in place.**
`SubCache.write` does `Create` then `Write` (`cache/subcache.go:178`), so a
crash or a kill mid-write leaves a truncated gob that fails to decode on next
open — a full rebuild at best.

## The shape of the fix

The pristine boundary decides most of this. `entity/dag` and `repository` are
untouchable, so we cannot add a compare-and-swap ref update, and we cannot
change how `Commit` writes. What we *can* do is guarantee, from the layer above
(`cache/`), that no two processes are ever inside the read-modify-commit window
at the same time — which makes CAS unnecessary rather than merely unavailable.

That is the whole design: **readers never lock, writers hold a short exclusive
lock across read-modify-commit, and everyone detects staleness from refs.**

### D1 — Writers take an OS file lock for the duration of one mutation

Replace the process-lifetime pid lock with an advisory `flock` on
`.git/git-work/write.lock`, acquired inside the mutation path and released when
the operation is committed. Held for a single read-modify-commit — milliseconds
— not for the life of the process.

`github.com/gofrs/flock` is the dependency: it is `flock(2)` on unix and
`LockFileEx` on Windows, which matters because we keep the windows build even
though CI no longer tests it. An `O_EXCL` pid file cannot do this correctly:
it cannot block-with-timeout, and it leaks on `SIGKILL`, which is what the
current stale-lock dance in `repoIsAvailable` exists to clean up. A kernel lock
is released by the kernel when the process dies, so the stale-lock problem
disappears rather than being handled.

Acquisition uses a timeout (5s, then an error naming the holder's pid, which we
keep writing into the file purely for that message). Everything that is not a
mutation — listing, showing, querying, the TUI's whole read path — takes no
lock at all.

The lamport clock increments come inside the lock, not outside it.
`repo.Increment` is a read-modify-write of a file in local storage
(`entity/dag/entity.go:471`), so two concurrent commits can hand out the same
edit time. Today the process-wide lock hides that; the short lock has to keep
covering it.

### D2 — Under the lock, re-read the entity before appending

`2a51f66`. A mutation takes the lock, resolves the entity's ref, and if the
hash differs from what the in-memory copy was built on, re-reads the entity
from git before appending the new operation. The parent is then always the
current tip, the `UpdateRef` that follows is a fast-forward in fact if not by
enforcement, and nothing is overwritten.

This is where the no-CAS constraint gets paid for: correctness rests on every
writer going through `cache/`. Anything writing `refs/issues/*` behind our back
— a stray `git update-ref`, a future daemon, a second implementation — breaks
it. Given the CLI, the TUI, the webui and the bridge all sit on `cache/`, that
is an acceptable invariant, and it is the same one upstream already relies on.

### D3 — Persist ref→hash beside the excerpts; diff on open

`d591cb3`. The cache file gains a `map[entity.Id]repository.Hash` recording,
for each entity, the ref hash the excerpt was built from, and the format
version is bumped so old caches rebuild once.

`Load` then lists `refs/issues/*` and diffs:

- hash unchanged → keep the excerpt, read nothing;
- hash changed → re-read that one entity, rebuild its excerpt;
- ref present, not in the map → new entity, read it;
- in the map, ref gone → removed, drop it.

A pull that touched 3 issues out of 2000 costs 3 reads, not 2000. This is also
the mechanism the watcher in D5 reuses, which is why it comes first.

### D4 — Atomic cache writes

`f39878f`. Write to `cache/<namespace>.new`, then `Rename` over the real file.
`LocalStorage` is a `billy.Filesystem` (`repository/repo.go:81`), so `Rename`
is available without touching the pristine interface. On unix that is atomic;
on Windows rename-over-existing fails, so that path is remove-then-rename,
which is racy in principle and not in practice because cache writes happen
under the D1 write lock.

### D5 — Watch refs, debounce, re-read only what changed

`63c68d1`. A watcher in `cache/` using `fsnotify` on `.git/refs/issues/`,
`.git/refs/identities/` and `.git/packed-refs`, debounced ~100ms, feeding the
D3 diff and emitting the existing `cache.Event` types so the TUI and webui get
the same events they already handle for local edits.

Two things the naive version gets wrong. `git gc` or `git pack-refs` moves
loose refs into `packed-refs`, which looks like a mass deletion if you only
watch the directory — hence watching both, and treating the diff as
authoritative rather than the event. And fsnotify can drop events under load or
fail entirely on odd filesystems, so a slow poll (10s, `ListRefs` plus a hash
compare) runs alongside as a backstop. The poll alone would also work and is
one less dependency; it costs a directory walk per tick and a worst-case
latency of a full interval, which is why it is the fallback and not the
mechanism.

### D6 — Drop bleve, and accept that text search becomes title-scoped

`3500366`. Bleve costs ~20 modules, an on-disk index that must stay coherent
with the excerpts, and the doc-count heuristic that is `Load`'s only real
consistency check. AGENTS.md already settles that search is a non-goal.

The honest part: bleve currently indexes **comment bodies as well as titles**
(`cache/bug_subcache.go:28` feeds every `comment.Message` into the indexer),
and `BugExcerpt` holds no comment text. So filtering over in-memory excerpts —
what the task asks for — means `git work issue "text"` searches titles and
labels only, and stops finding issues by something said in a comment.

**Recommendation: take that loss and document it.** None of the five target
workflows searches comment bodies, and the query language work in `3c9c24d`
is about fields and relations, not text.

The alternative, if you'd rather not lose it: add a lowercased text blob to
`BugExcerpt`, capped per comment. It keeps comment search, costs memory
proportional to all comment text in every process that opens the cache, and
makes "excerpt" a misnomer. A third option — scan entities lazily when a text
term appears in the query — keeps excerpts small and search exact, at the cost
of reading every entity from git on a text query. I'd rather ship the cheap one
and revisit if it ever bites.

## Order of work

1. **`24e82d6` first, as a failing test**, not a throwaway spike: two
   goroutines sharing a repo, each appending a comment to the same issue,
   asserting both survive. It fails today — that is the point — and becomes the
   regression test for D1 and D2.
2. **`3500366`** next, because it deletes the index-coherency problem that D3
   would otherwise have to preserve.
3. **`d35de2e` + `2a51f66`** together: the lock is meaningless without the
   re-read and the re-read is unsafe without the lock. One commit, one test.
4. **`f39878f`**, small and independent.
5. **`d591cb3`**, the ref→hash map and the `Load` diff.
6. **`63c68d1`**, the watcher on top of the diff.

## Done when

- Two processes committing to the same issue concurrently both keep their
  operations, proven by the `24e82d6` test;
- `git work issue` runs, and returns, while `git work termui` is open — and
  vice versa;
- an edit made in one process appears in an open TUI within a second, without
  a daemon and without a full cache rebuild;
- a pull touching 3 of N issues re-reads 3;
- `kill -9` during a cache write leaves a loadable cache;
- no index directory is created, and nothing in `cache/` touches bleve.

  Note the correction: bleve cannot leave `go.mod`. `repository/index_bleve.go`
  implements the pristine `RepoIndex` interface with it, so the dependency
  survives as dead weight until someone decides that deleting it is worth a
  pristine-package edit. What this story actually removes is the *use*: the
  on-disk index, the writes during build, and the doc-count heuristic that was
  `Load`'s only consistency check.

## Risks

- **A short lock is only as good as its coverage.** If any mutation path
  forgets to take it, the failure is silent data loss, not an error. Mitigation:
  the lock is acquired in one place in `cache/`, on the path every mutation
  already goes through, and the `24e82d6` test exercises it.
- **fsnotify across platforms.** macOS kqueue and Linux inotify differ on
  rename semantics; the poll backstop means a watcher bug degrades to 10s
  latency rather than to a view that never updates.
- **Format version bump rebuilds every cache once.** Cheap at our size, worth
  mentioning because it will look like a hang on a large store.
- **Lock ordering with the bridge.** A long import holds the write lock per
  mutation, not for the import — worth checking when `a3a8d16` lands that it
  does not accidentally wrap the whole sync in one acquisition.
