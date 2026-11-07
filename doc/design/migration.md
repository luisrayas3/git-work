# Migration

How git-work leaves git-bug behind:
`git work migrate`, which replays a git-bug store as issues of the owned model,
and which names are git-work's and which keep git-bug's.
The store the migration writes into is [store](store.md);
the issue it produces is [issue](issue.md);
the schema it checks against is [schema](schema.md).

## The command

`git work migrate` reads git-bug's issues under `refs/issues/*`
and writes them as issues under `refs/work-issues/*`,
and copies the identities they are signed by
from `refs/identities/*` to `refs/work-users/*`.
It is, with `entities/bug` as its decoder,
the way to import another git-bug repository.

### A copy, not a move

`refs/issues/*` and `refs/identities/*` stay where they are,
locally and on origin,
and `git work bug` keeps reading them.
The old store is frozen once it is copied:
anything written to it afterwards reaches `refs/issues/*` alone,
so `git work bug` is never used to write.

### It runs once

`migrate` refuses when `refs/work-issues/*` holds anything,
so a second run, on this clone or another, does nothing.
Importing into a repository that already has issues
would be two id spaces meeting, a merge rather than an import.

### The schema comes first

The mapping writes into the fields `status`, `priority`, `phase`, `area`, `parent` and `labels`
of the types `story`, `task` and `decision`,
and `migrate` refuses before reading anything,
naming every key it needs and does not find (`migrate.Needs`).
The schema is applied by `git work schema import` beforehand.

### Ids are preserved and checked

An operation's id hashes its JSON, type code included,
and a `Create`, `AddComment` or `EditComment` built by `entities/issue`
from the same inputs serializes to the same bytes as `entities/bug`'s
(`TestIdsMatchBug`).
The replay copies the old operation's base (nonce, timestamp, metadata)
onto the new one and compares the bytes,
so every issue and every comment keeps its id,
and a mismatch aborts the run before anything is written.
After writing, every issue is read back
and its comment ids and lamport times compared with the original's.

### Lamport times are reproduced exactly

`dag.Entity.Commit` takes both times from the repository,
so the replay commits through a `repository.ClockedRepo`
whose `Increment` hands out the original pack's time
and witnesses the real clock with it (`migrate/clock.go`).
The new packs carry the very create and edit times of the old ones,
and the real clocks end where a replay of the same history would leave them.
Not witnessing the clock to one below the wanted time and letting the increment land on it:
two clones can legally produce the same time,
and the drift compounds from the first repeat.

### One commit per original commit

Each old commit becomes one new commit,
replayed in global edit order,
so ordering by edit time is unchanged
and `git work issue log` reads as the history did.
A commit whose operations mean nothing in the new model
gets a no-op operation,
so it still exists and keeps its times.
The replay reads linear histories only:
a merge commit in the old store is refused, not guessed at.

### Every old operation has one meaning

| Old operation | New operations |
| --- | --- |
| `Create` | `Create`, same bytes, plus `SetField type` and `SetField status` in the same pack |
| `AddComment`, `EditComment` | the same, same bytes |
| `SetTitle` | `SetField title`, its metadata kept |
| `SetStatus open` · `closed` | `SetField status "to-do"` · `"done"`, its metadata kept |
| `LabelChange` | one operation per label (below) |

Any other operation, and a label change carrying metadata, aborts the run.

### Labels become fields

| Label | Field | Value |
| --- | --- | --- |
| `type:story` · `type:task` · `type:decision` | `type` | the same |
| `type:spike` | `type` | `task`, plus the label `spike` |
| `prio:high` · `prio:med` · `prio:low` | `priority` | `high` · `medium` · `low` |
| `phase:X` | `phase` | `X` |
| `area:X` | `area` | `X`, one item |
| `story:PREFIX` | `parent` | the full id of the one old issue the prefix names |
| anything else | `labels` | the label, verbatim |

A label added sets a single-valued field or adds an item to a multi-valued one.
When the label behind a single-valued field is removed,
the field takes another label of the same prefix the issue still has,
and is cleared when none is left;
a removed item is removed.
A `type:` label is never cleared, a type never being unset.
A `type:` or `prio:` value outside the table,
two labels of one single-valued prefix added at once,
or a `story:` prefix that names no issue or several,
aborts the run.

### Type and status are set in the create pack

The owned model requires a type from the first operation,
and a board needs every issue in a column;
git-bug has no type until a label arrives, and an implicit open.
So the create pack gets `SetField type`,
the first type the issue was ever given, `task` if never,
and `SetField status "to-do"`,
with the create operation's author and time.
The create operation keeps its bytes, so the id is untouched:
a pack's blob is not part of an entity id.
A later change of either replays where its label or status change was.

### Identities are copied first

The old issues resolve their authors through `refs/work-users/*`,
so `migrate` copies every identity there before it reads an issue,
skipping those already copied.
Identity clocks are not named after a namespace, so nothing else moves.
Other clones need no copy:
`git work pull` fetches `refs/work-users/*`.

### The schema check reports, it does not abort

After the replay is planned,
every issue's final fields are checked against the schema
the way `git work issue new` checks them,
and every refusal is printed under its issue.
A refusal is a fact about the old data, not a reason to stop.

### A failed run writes nothing

The plan is computed and checked before anything is written,
and written under the write lock, since it writes what the cache owns ([store](store.md)).
If a write or the read-back fails,
every ref written so far is removed,
and the store is as it was.
`--dry-run` prints the plan,
one line per issue with its type, its title and the schema's refusals,
and writes only the identity copy,
which every command needs anyway.

### The old store is fetched only by migrate

`git work pull`, `push` and `sync` carry the `work-*` namespaces only.
`git work migrate --from REMOTE` fetches `refs/issues/*` and `refs/identities/*` from a configured remote,
merges them into the local refs under the write lock,
and then migrates as without it.
The merge only creates or fast-forwards:
a frozen store never needs a merge commit,
and a local history that diverged from the remote's is an error.
`--dry-run --from` fetches too:
fetching is not migrating,
and the plan is only worth printing over the store it will migrate.
Not carried in every sync: it would fetch, merge and push a frozen store each time,
and every `git remote prune` would drop its remote-tracking copies for the next sync to bring back.
Not a `--legacy` flag on pull, push and sync:
it would keep extending a store being left behind, for the one reader that needs it.

## The fork's names

### The binary is git-work

`execenv.RootCommandName` is `git-work`,
so usage lines, errors and generated man pages name the binary the user has,
invoked as `git work`.
Local state is under `.git/git-work` ([store](store.md)),
and git-work's own git config keys are `git-work.*`
(`git-work.remote`, `git-work.display.id`, `git-work.jira.*`).

### What keeps git-bug's names

The Go module path, `github.com/git-bug/git-bug`,
and the identifiers inherited with the code keep git-bug's names,
so a cherry-pick from upstream still applies.
So do the user-identity config key `git-bug.identity`,
the legacy bridges' `git-bug.bridge.*` keys,
and the keyring's service name and directory (`git-bug`).
