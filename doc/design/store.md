# The store

How git-work keeps its data in git:
the entity model it inherits from git-bug's `entity/dag`,
the ref namespaces,
the layers between a ref and a command,
and how many processes share one store without losing a write.
What an issue holds is [issue](issue.md);
what the schema and flows hold is [schema](schema.md) and [flows](flows.md);
what syncs with Jira is [jira](jira.md);
the one-time import of git-bug's own store is [migration](migration.md).

## The entity model

### An entity is a history of operations

An entity (an issue, a schema entry, a flow) stores no state:
it stores the operations that edited it,
and its state is compiled by applying them, in order, to an empty snapshot.
Many processes on many clones edit the same entity,
and a stored state could not say which of two concurrent changes wins;
an ordered list of operations can,
the way an operation-based CRDT does.

### A commit is one pack of operations

The operations one author appends in one write
form an operation pack,
stored as one JSON blob: `{"author": …, "ops": [...]}`.
Each operation carries its type code, a timestamp, a random nonce,
its metadata and its own data.
A commit's tree holds the pack under `ops`,
the format version as an empty entry `version-N`,
the lamport times as empty entries `edit-clock-N`
and, on the first commit only, `create-clock-N`,
and the files the operations attach in a subtree, `extra`.
The empty entries all point at the empty blob,
so they cost no transfer.
The commits chain from the entity's first one,
and the ref `refs/<namespace>/<id>` names the head;
pushing the ref carries everything it reaches, files included.
A commit is signed when the author's identity has a signing key.

A reader refuses a format version it does not know (`entity/dag`'s version gate),
so a binary older than the model refuses the store instead of misreading it.

### Ids are content hashes

An operation's id is the SHA-256 of its JSON,
which is why every operation carries a nonce.
An entity's id is its first operation's id,
and a pack's is the hash of the pack's JSON.
Nothing in an id names the namespace or the ref,
so a ref can be renamed or copied without changing an id
([migration](migration.md) relies on it).
A human sees an id truncated to seven characters,
and any unambiguous prefix resolves.

### Lamport clocks order the history

Wall-clock timestamps are for display only:
another clone's clock may be wrong, or gamed.
Order comes from two lamport clocks per namespace,
`<namespace>-create` and `<namespace>-edit`,
kept under `.git/git-work/clocks/`.
Every commit takes the next edit time,
the first commit also the next create time,
and every read witnesses the times it finds,
so a clock is always ahead of everything this clone has seen.

### Concurrent edits merge into a DAG

A pull that finds the remote ahead fast-forwards the ref.
A pull that finds both sides moved writes a merge commit,
parents local then remote, carrying no operations,
so an entity's history is a DAG with one root.

Reading orders the operations deterministically:

1. the DAG is checked: one root,
   every commit's edit time above each of its parents',
   no merge commit with operations,
   and no non-merge commit jumping more than 1,000,000 past its parent
   (a clock pushed toward overflow is an attack);
   a commit that fails is refused;
2. the packs are sorted by edit time,
   and equal times, which are concurrent edits on different clones,
   by pack id, which means nothing and is hard to bias;
3. the operations are taken pack by pack in that order.

The DAG constrains the clocks,
so the order inherits the causality the DAG records,
and the clocks refine it.
Identities are not DAG entities:
an identity merges only by fast-forward.

### The read sorts parents first

`dag.read` walks the commits breadth-first from the head
and then validates them in an order where every commit comes after all its parents
(`parentsFirst`, Kahn's algorithm),
the root first.
Not a reversed breadth-first walk: it is not such an order.
When a merge joins two branches whose lengths differ by more than one,
the root would be reached through the short branch before the long one is exhausted,
and the read would fail (`creation lamport time not set`, or a panic).
The merge moves the ref before any read,
so one pull after unequal divergence would make the entity unreadable on every clone that took it.
The stored DAG is valid either way;
the order is the reader's alone and is no part of the format.
`TestMergeUnevenBranches` (`entity/dag`) and `TestIssueUnequalDivergence` (`cache`) cover it.

Not a workaround above `entity/dag`:
`read` is the only reader and the merge commit's shape is fixed by `dag.merge`,
so padding the short branch only works for a single fork point,
and rebasing in the cache would be a second implementation of the merge.

## Namespaces

### Every namespace carries the work- prefix

| Ref namespace | Holds |
| --- | --- |
| `refs/work-issues/*` | issues ([issue](issue.md)) |
| `refs/work-schema/*` | schema types and fields ([schema](schema.md)) |
| `refs/work-flows/*` | flows ([flows](flows.md)) |
| `refs/work-users/*` | identities |

The prefix makes them read as git-work's next to anything else under `refs/`.
The namespace names follow what a user meets, not the Go types:
`work-users`, though the type is `identity`.
Identities' namespace is set by three constants in `entities/identity`.

git-bug's frozen store, `refs/issues/*` and `refs/identities/*`,
is read only by the legacy commands and [migration](migration.md),
and is not carried by `git work pull`, `push` or `sync`.

### Flat, not nested

A namespace is `work-issues`, not `work/issues`.
A namespace also names its clock files and its cache file,
so a slash in it would make directories of them,
and the identity refs are flat already.

### Two formats never share a namespace

The issue entity is format 5,
git-bug's bug format 4,
and each reader refuses the other's packs,
so the owned issue lives in its own namespace
rather than beside git-bug's bugs.

### Local state is under .git/git-work

Everything that is not a ref is derived, local, and rebuilt when missing:
the lamport clocks (`clocks/`),
one excerpt cache file per namespace (`cache/<namespace>`),
and the write lock (`write.lock`).
Nothing under it is pushed.

## Layers

### The layering

| Layer | Packages |
| --- | --- |
| git | `repository` (go-git), with `gitcli` overriding what go-git gets wrong |
| entity model | `entity`, `entity/dag` |
| entities | `entities/issue`, `entities/config`, `entities/identity` |
| validation | `schema`, the compiled schema every write is checked against |
| store | `cache`: excerpts, the write lock, the ref watcher |
| the API | `host`, the one code path behind the command line and Starlark |
| surfaces | `commands`, `flow/run`, `tui`, with `view`'s argument table |

Each layer reaches the data through the one below it:
a command or a flow asks `host`,
`host` asks `cache`,
and nothing above `cache` reads or writes an entity ref itself.
`tui` and `flow/run` call `host` as `commands` does,
so the terminal and Starlark see the same verbs ([cli](cli.md), [flows](flows.md)).
`view` depends on `cache` and `schema` only;
views never call flows.

`entities/bug` and its bug subcache are a peer of the issue subcache,
reading git-bug's frozen `refs/issues/*` for the legacy commands and for [migration](migration.md);
nothing new is built on them.

### Every write goes through the cache

The no-lost-operations guarantee rests on the write lock and the re-read inside it,
both in `cache`;
`dag.Entity.Commit` ends in an unconditional `UpdateRef`,
so anything else writing entity refs (a stray `git update-ref`, a second implementation)
silently erases concurrent work.
The one writer outside `cache` that touches its namespaces, `migrate`,
takes the same lock (`cache.LockWrite`).

### One entity per commit

A write commits one entity.
There is no atomic multi-entity commit,
so a relationship between issues is a field holding the other issue's full id,
eventually consistent, as in Jira,
and the core has no batch commit.

### Refs are the runtime truth

What git-work runs on is what the refs hold.
`schema.yaml` and `flows/*.star` in a tree are authoring copies,
applied by `git work schema import` and `git work flow import`;
nothing reads the tree at run time.

## Many processes, one store

A human keeps the terminal renderer open
while agents and other humans write from short command-line calls:
nothing blocks a reader, nothing is lost,
and open views update within a second, without a daemon.

### Readers take no lock

Listing, showing, querying and the renderer's whole read path lock nothing.

### Readers use the excerpts

Anything built over the issues reads the cache's excerpts and loads no entity:
a listing, a view's rows, a search, the drawn ids and alias resolution,
and any index derived from many issues.
An excerpt is rebuilt on every write and every ref change
([below](#the-cache-records-the-hash-behind-each-excerpt)),
so it is as current as the entity,
and a reader that reads the entity instead pays the disk on every row for nothing:
a ref lookup and a replay of the issue's whole history.
So what such a reader needs goes on the excerpt.
An entity is read whole for one issue's history,
which an excerpt of the present does not hold
(`issue get`, `issue show`, `log`, and a replay `--at` a past time,
[query](query.md#--at-is-a-replay)),
and by a write, which reads it again under the lock
([below](#re-read-under-the-lock)).

### The excerpt carries the reach

Each issue excerpt carries,
for every alias namespace with an ordering hook,
the copy's reach: the hook's answer for its history
([cli](cli.md#the-copy-that-reached-the-system-first-is-canonical)).
So alias resolution, the drawn ids and the Jira sync's consolidation
order the copies of one issue from the excerpts.
A hook is registered from an `init` function (`cache.RegisterReach`),
before any store opens,
because the excerpts are built while it opens;
it carries a version,
and the issue cache file records the hooks and versions its excerpts were built with,
so a file built with others is rebuilt, as one of another format version is.
A hook reads only the copy's immutable history,
so the excerpt, rebuilt whenever the copy changes, carries its answer current.

Not the raw metadata a hook reads, kept on the excerpt for the hook to run over:
Jira's reads every sync marker, each a whole base,
which would put every base of every issue in the cache file.
Not a hook given to an open store:
its excerpts are already built, and could not carry the hook's answer
without reading every entity again.

### A writer locks one read-modify-commit

A writer takes an advisory `flock` on `.git/git-work/write.lock`
(`github.com/gofrs/flock`, `LockFileEx` on Windows)
for one read-modify-commit, milliseconds,
not for the life of the process.
It waits up to 5 seconds,
then fails naming the holder's pid,
which the holder writes into the file for that message only.
The kernel releases the lock when the process dies, crash included,
so there is no stale lock to clean up.
The lamport increments happen inside the lock:
`repo.Increment` is a read-modify-write of a clock file,
and two concurrent commits would otherwise hand out the same time.

Not git-bug's pid file taken at cache open:
it serialized every process, readers included,
so an open renderer blocked every command.
Not an `O_EXCL` file:
it cannot wait with a timeout, and it leaks on `SIGKILL`.

### Re-read under the lock

Inside the lock, a commit reads the entity from git again
and replays its staged operations onto the fresh copy,
so the new commit's parent is the current tip
and the `UpdateRef` that follows is a fast-forward in fact.
`IssueCache.Update` goes further:
it hands the caller the snapshot read under the lock,
so a decision that depends on the issue's current state
(a three-way merge, say)
is made on it and not on what was read before.
`TestConcurrentWritersLoseOperations` shows the loss without it;
`TestConcurrentCacheWritersKeepBothOperations`, that the lock and the re-read prevent it.

Not a compare-and-swap ref update:
`repository`'s `UpdateRef` is a bare set, and `entity/dag`'s commit path is git-bug's;
a lock around the whole window makes a CAS unnecessary
rather than merely unavailable.

### A pull holds the lock for its merge

A merge writes many refs inside `entity/dag`'s `MergeAll`,
with no seam to lock each one,
so a subcache's merge holds the lock for all of it.
It is the one writer that holds it for longer than one entity's commit;
a pull is a bulk write, and treating it as one is simpler than interleaving it.
An entity the merge updated is read again before its excerpt is rebuilt,
because the copy the merge returns was read before the merge commit was written.

### The cache records the hash behind each excerpt

Each namespace's cache file holds, beside the excerpts,
the ref hash each excerpt was built from.
On open, and on every refresh, the cache lists the namespace's refs in one pass and diffs:
an unchanged hash keeps its excerpt,
a changed or new one is read again,
a ref gone drops its excerpt.
A cache written by another process, or left behind by a `git fetch`,
is then corrected entity by entity:
a pull that touched 3 issues of 2000 costs 3 reads.
A cache file without hashes upgrades itself through the same diff;
a format version that does not match rebuilds it.

### Cache writes are atomic

The cache file is written to `<name>.new` and renamed over the old one,
so a crash or a kill mid-write leaves the previous cache, not a truncated one.
Windows refuses a rename over an existing file,
so there the old file is removed first;
two writers racing that window are excluded by the write lock.

### The watcher only says when to look

`RepoCache.Watch`, which the renderer starts,
watches the git directory (for `packed-refs`), `refs/`,
and each namespace's ref directory with `fsnotify`,
debounces events by 100 ms,
and runs the same ref-hash diff,
emitting the events a local edit emits.
A 10-second poll runs beside it.
The event decides only when to look, never what changed:
`git gc` and `git pack-refs` move loose refs into `packed-refs`,
which looks like a mass deletion to a directory watch,
and fsnotify drops events under load or fails on some filesystems,
so a missed event costs latency and a spurious one costs nothing.
Not the poll alone: it would work, at a full interval's latency.

### No search index

The cache keeps no full-text index.
Not a bleve index over titles and comment bodies, as git-bug keeps:
it would be a second on-disk structure to keep coherent with the excerpts,
and its document count would be the cache's only consistency check.
Searching what was said in a comment is the loss, accepted:
none of the workflows does it.
The `bleve` dependency stays in `go.mod`, used only by `repository`'s index.
How queries search is [query](query.md).
