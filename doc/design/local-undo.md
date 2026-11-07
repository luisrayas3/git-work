# Local undo: `issue undo` and `issue redo`

**Outcome:** a writer who made a mistake on an issue — a wrong field, a comment
on the wrong issue, an issue that should not exist — takes it back,
as long as nobody else can have seen it,
and the log says which writes can still be taken back.

**Serves:** `01a559a`, undo my own unpublished writes.

**Status:** shelved 2026-11-07 on branch `shelf/local-undo` (U11).

## Problem

Every write is permanent.
An issue's ref only ever grows (`entity/dag/entity.go`, `Commit`),
and the only way to take a write back is another write over it:
set the field back, archive the issue.
That second write is history too,
replicated to `origin` on the next push,
so a slip made a minute ago is published there as a slip and its correction.
Jira is spared when the correction comes before the next sync:
the sync merges values against its base, not operations (`jira-sync.md` JS9),
so a field set back reads as unchanged, and an archived issue is never exported.
A comment is the exception: there is no local delete (JS12),
so a comment on the wrong issue reaches Jira on the next sync.

`git work issue rm` is the one exception, and a narrow one:
it drops the issue's local ref and its remote-tracking refs,
and if the issue was ever pushed the next pull brings it back.

## U1 — Undo drops the newest commits while they are unpublished

`git work issue undo ID OP` moves the issue's ref
to the parent of the commit that holds operation OP,
dropping that commit and every one after it.
Nothing is written: the ref moves back,
and the dropped commits stay in the object store, unreachable,
until git's garbage collection prunes them (by default after two weeks).

This only works because an unpublished commit is a commit only this clone has.
Once anything else holds it, a pull brings it back (the merge's
"remote ahead" case, `entity/dag/entity_actions.go`),
and a push before that pull is refused as non-fast-forward,
since pushes are never forced (`gitcli/remote.go`).
So undo refuses to drop a published commit (U3)
rather than make a state the next pull silently reverses.

## U2 — The unit is a commit, named by an operation in it

A commit is one write: one `issue set` with three keys is three operations
in one commit (`host`, `IssueCache.update`).
Undo takes the write back whole, never part of one,
since dropping an operation from inside a commit means rewriting the commit.

The argument is an operation id, not a commit hash,
because operations are what the log shows (U5) and what a user has in hand.
Undoing one operation undoes the operations committed beside it,
and the log groups them so this is visible.

## U3 — The floor: what any remote or Jira holds

A commit is published, and below the floor, when either:

- it is reachable from `refs/remotes/<remote>/work-issues/<id>` for any remote.
  Fetch and push are never forced, so these refs are an exact record
  of what each remote has, and nothing else records it;
- it is at or below the newest commit that carries Jira sync metadata:
  a `jira-sync` marker, a `jira-create` journal entry,
  or a `SetMetadata` pairing `jira-id` or `jira-comment-id` (`jira/types.go`).

Jira is a remote the refs do not record.
Its sync writes Jira first and then commits what it did (`jira-sync.md` JS13),
so the commit after an export is the record of it.
Dropping that record rolls the sync's base back:
a dropped field edit comes back as an import (JS9),
a dropped comment is imported again as a new one (JS12),
and with both the `jira-id` link and the `jira-create` journal gone
the issue can be POSTed a second time.
So the record is the floor, as a remote-tracking ref is.

Undo refuses an OP whose commit is at or below the floor,
and the refusal names the floor and which remote or Jira it belongs to.

## U4 — Undoing the create removes the issue; `issue rm` is retired

When OP is in the first commit, the one with the create operation,
undo has no parent to move to and removes the issue:
the local ref is deleted, through the same `SubCache.Remove` path `rm` takes.
The floor still applies, so an issue anyone else has cannot be removed this way.

That leaves `issue rm` with one use of its own,
removing an issue someone else has,
which lasts until the next pull.
It is retired, and `issue undo ID CREATE_OP` is the only local removal.
The replicated removal stays `issue archive`.

## U5 — The log is the plumbing

`issue log` already returns one entry per operation (`cmdjson.IssueOperation`).
Each entry gains two keys:

- `commit`, the hash of the commit that holds the operation;
- `undoable`, whether the commit is above the floor (U3).

The operations of one commit share `commit`, which is how a reader groups them.
No second history command is added:
the log is already the history, and what can be undone is a property of it.
`undoable` is computed when the log is read, never stored;
the floor moves on every push, fetch and Jira sync.

## U6 — Redo, from the head undo dropped

`undo` prints the head it dropped, a commit hash,
the "a writer prints an id" case of `cli-convention.md`.
`git work issue redo ID COMMIT` moves the ref forward to COMMIT.
It refuses unless the current head is an ancestor of COMMIT,
so a redo only restores, and never discards a write made since the undo:
make one, and the undone commits are gone for good.

Redo after undoing the create recreates the ref at COMMIT.

Nothing on disk remembers a dropped head.
The command line has it from undo's output;
the terminal renderer keeps a stack of them for its session (U8),
lost when it exits.
A durable record (a local-only ref, the reflog) was considered and left out:
what is undone is small and was written a moment ago,
and writing it again by hand costs less than another namespace to keep.

## U7 — Both go through the cache, under the write lock

Moving a ref is a write, and the no-lost-operations guarantee rests on
every write taking the lock and re-reading inside it (`AGENTS.md`).
A ref moved outside it would erase a commit another process made meanwhile.

The floor's Jira half reads Jira's metadata keys, and `cache` cannot import `jira`,
so `host` computes the floor and the target from the head it read,
and hands the cache that head with the target.
So both are one `SubCache` method that, under `lockWrite`:

1. reads the ref, refuses if it is not the head `host` read,
   and refuses if the entity has staged operations;
2. checks the target: for undo, an ancestor of the head;
   for redo, a descendant of the head;
3. updates the ref (or removes it, U4), records the new hash (`noteRef`)
   and refreshes the excerpt.

The re-read in step 1 is the compare-and-swap:
a write between `host`'s read and the lock makes the undo refuse,
rather than drop a commit the floor was never checked against.
Other processes need nothing new:
the cache already re-reads an entity whose ref hash changed in either direction,
and drops one whose ref is gone (`refreshFromRefs`, `cache/subcache.go`),
and the watcher only triggers that.

Lamport clocks are left alone. They only move forward,
so a write after an undo still takes a time above its new parent's.

## U8 — The terminal renderer

The issue's `log` tab draws each undoable operation as such,
draws a rule at the floor,
and binds undo to the operation under the cursor,
and redo to the top of the session's stack.
The tab hides `noop` operations,
and the Jira markers are `noop`s,
so the rule comes from `undoable`, not from the rows shown.

## U9 — Both are one API

`work.issue.undo(id, op)` and `work.issue.redo(id, commit)`
are the Starlark side, through the same `host` functions.

## U10 — Out of scope

- Undoing a published write.
  That is a new write, replicated like any other:
  set the fields back to what `issue get --at` shows.
  A flow can do it today.
- Undo on the config entities (`work-schema`, `work-flows`).
  They use the same machinery and could follow,
  but `schema import` and `flow import` from the authoring copies
  are already their undo.

## U11 — Shelved, 2026-11-07

A review of this design against the code found it sound and not worth building yet.

- **The value is narrow.**
  Because Jira sync merges values (see Problem),
  a correction made before the next sync already keeps Jira clean
  for fields and for accidental creates.
  What undo adds is a cleaner git history,
  and a way to take back a comment, which today has no local delete.
- **The window is short.**
  Any push, fetch or Jira sync of the issue raises the floor past the write,
  and sync usually runs from cron.
- **The floor is the cost.**
  Its Jira half is required:
  without it, an undo that drops the `jira-create` journal and the `jira-id` link
  but keeps the issue leaves it unlinked,
  and when the Jira issue is older than the sync's search window,
  the next sync POSTs it a second time (`jira/candidates.go`, `jira/create.go`).
  It makes `host` know Jira's metadata keys,
  and every log read compute a floor per issue.
  Its git half only refuses an undo the next pull would reverse;
  nothing is lost without it.
- **Merges are unhandled.**
  After a pull with concurrent edits, the history has merge commits
  (`entity/dag/entity_actions.go`),
  and the log orders operations parents-first,
  so "this commit and every one after it" is not a suffix of the log.
  Undo would have to be defined over the first-parent chain
  and the log would have to show it.
- **The size.** About 700–1000 lines of code and as many of tests,
  roughly half of it the log keys, the terminal renderer and redo.
  Those are what make undo usable, so a head-only command without them
  was not taken as a cheaper substitute.

The comment case, the one place a correction leaks to Jira,
is taken up on its own, as a non-destructive comment delete.
