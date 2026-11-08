# The issue

The issue entity:
what an issue stores, the operations that write it,
the four built-in fields as the entity holds them,
how an issue and a comment are removed,
and what a local type change does.
What a field *means*, its kind, its values and the check a write goes through,
is the [schema](schema.md).
The dag, the namespaces and the write lock every write takes are the [store](store.md);
the verbs and their arguments are the [command line](cli.md);
the Jira side of every decision here is the [Jira sync](jira.md);
how a git-bug store becomes this one is the [migration](migration.md).

## The model

### The issue entity is ours

The issue is `entities/issue`, package `issue`,
under `refs/work-issues/*`, at `formatVersion` 5.
It is git-work's model, not git-bug's:
no upstream fix is expected to cherry-pick into it,
and git-bug's `entities/bug` survives only as the migration's decoder.

The version gate in `entity/dag` refuses a pack of another version outright,
so a binary that predates the model refuses the store instead of misreading it.

Not compatible evolution,
which keeps every old operation readable forever
and reinterprets git-bug's `SetStatusOp` and labels as fields:
that carries git-bug's model, title, status and labels as special members,
for as long as the store exists.
Instead a store is migrated once,
which one team with no external clones can afford.

### A structural core and a fields map

The snapshot has a **structural core**,
what the operation log is made of:
id, author, create time, comments, timeline, actors and participants,
and the **fields map** itself.
Everything an issue *has* is a field,
title and status included,
and so is every relation to another issue.

A field value is the JSON the operation carried, kept verbatim.
The entity does not know the schema,
so it cannot decode a value into its kind;
the [schema](schema.md) reads it.
A JSON `null` is the absence of a value:
a set to `null` clears the field.

### The entity checks shape only

An operation's `Validate` runs on every read,
and one that fails makes the whole issue unreadable,
so it checks only what must hold for every operation ever written:

- a field key matches `^[a-z][a-z0-9_-]*$`, at most 64 bytes;
- a value is valid JSON, at most 64 KiB, and never empty;
- an item of a list is not `null`;
- the title is a non-empty, safe, single line.

These rules can only ever be loosened:
tightening one would make existing history unreadable.
Whether a value fits its field is checked above the entity,
on the write path in `cache`, and never here
([schema](schema.md#validate-on-write-lenient-on-read)).

### The built-in fields

Four fields exist on every issue whatever its type,
because tooling cannot work without them:

- **`title`**, a string:
  required at creation and never cleared, since nothing can show an issue without one.
  The create operation carries it in its own member, not in its fields,
  so that an issue created from the same inputs has the same bytes and the same id
  as the git-bug issue it was migrated from.
- **`type`**, a type key:
  what the rest of the schema is found by.
- **`archived`**, a bool:
  what the default listing hides ([Archive is replicated](#archive-is-replicated)).
- **`rank`**, a string, the manual order a drag writes
  ([Rank starts null](#rank-starts-null)).

A fifth built-in, **`color`**, a string, is on the issues of a type that switches it on
([schema](schema.md#color-is-a-built-in-a-type-switches-on)).

Status is not one of them.
It is a field of kind `enum` that a type may have,
and whatever keys off "is this done" reads its value's category
where the type has one, and treats the issue as never done where it does not
([schema](schema.md)).

The entity knows `title` and `archived` by name and nothing else about the built-ins;
that they are built in, and their kinds, is the [schema](schema.md)'s.

## Operations

### The operation set

| Code | Name in the log | Operation |
| --- | --- | --- |
| 1 | `create` | `Create{title, message, files, fields}` |
| 3 | `add-comment` | `AddComment{message, files}` |
| 6 | `edit-comment` | `EditComment{target, message, files}` |
| 7 | `noop` | dag's `NoOp` |
| 8 | `set-metadata` | dag's `SetMetadata` |
| 9 | `set-field` | `SetField{key, value}` |
| 10 | `add-value` | `AddValue{key, item}` |
| 11 | `remove-value` | `RemoveValue{key, item}` |
| 12 | `delete-comment` | `DeleteComment{target}` |

The codes shared with git-bug, 1, 3, 6, 7 and 8, keep their numbers,
because an operation's id hashes its JSON, code included,
so a migrated entity and its comments keep their ids.
The retired codes 2, 4 and 5 (git-bug's title, status and label changes)
are never reused,
so an old pack can not be misread as a valid new one
even if the version gate were bypassed.

The name is what `git work issue log` prints and a script matches on;
the code is what the store holds.
A code this binary does not know reads as an unknown operation,
which applies as a no-op and re-marshals verbatim.

### Set replaces, add and remove are per item

`SetField` replaces a field whole;
concurrent sets of one field resolve last writer wins
in the dag's order, lamport time then pack id.

`AddValue` and `RemoveValue` act on one item of a list-valued field,
with set semantics:
two people adding two labels, two reviewers or two blocking issues concurrently
must both win, which a list-valued `SetField` cannot give.
Adding an item already present, or removing one absent, changes nothing;
a field holding something that is not a list reads as empty to `AddValue`.
Items are kept sorted by their canonical JSON,
so a set reads the same whatever order its items arrived in.

A concurrent `SetField` and `AddValue` on one field resolve in the dag's order,
so a replace that lands last means replace,
as `replace` against `add` on an array does in JSON Patch.

Which fields are list-valued is the schema's:
the `multi-*` kinds take `add` and `remove`, and only they.

### One write is one commit

`issue set` writes one `SetField` per key it is given, in key order,
`issue add` and `issue remove` one operation per item,
and each call lands as one commit on the one issue.
The planned operations are checked together,
every problem reported at once,
and what `--dry-run` prints is exactly what the same call commits.

Every key of one call is checked against the type the call leaves the issue with:
a `type` set in the call is the type its other keys belong to.

### Relations are fields

A relation to another issue is a field of a relation kind:
`parent` a `relation` set with `set`,
`blocks` a `multi-relation` edited with `add` and `remove`.
The value is the other issue's **full** id,
whatever prefix or alias the writer gave
([schema](schema.md#a-relation-is-stored-as-the-full-id)).

Only the issue that makes the statement stores it.
There is no multi-entity commit,
so nothing is written to the other side;
the inverse, "children of X", is derived by the reader from the schema.

Not relations with operations of their own and a slice on the snapshot.
The property that argued for them, concurrent adds both surviving,
is the property every multi-valued field needs,
so it belongs to `AddValue`;
and validating a target, or deriving an inverse,
is a lookup by kind like any other field's.

## Removal

### Archive is replicated

`git work issue archive ID` is `set ID '{"archived": true}'`:
an operation, so it reaches every clone,
and reversible, by setting `archived` back to `false`.
An archived issue is left out of every listing's input
unless the caller asks for it ([query](query.md)).

`archived` says whether an issue is still worth looking at;
status says how its work ended.
They are separate because they answer separate questions.

### rm is local

`git work issue rm ID` deletes the local ref.
Refs cannot be deleted across clones,
so the issue returns on the next pull.

### Delete is a third removal

A comment is taken down with `git work issue comment delete COMMENT_ID`,
which prints nothing.
It is neither `rm`, which is local,
nor `archive`, which is reversible:
it is replicated and final,
so it takes a third word ([command line](cli.md)).

## Comments

### The description is the first comment

The create operation carries the issue's first comment, its description,
and every reader finds it at `Comments[0]`.
It is edited like any other comment,
with `issue comment edit` on its id.

A comment is named by its own id,
the one `issue comment new` prints and `issue get` shows,
resolved by prefix.

### An edit can name its base

`issue get` prints each comment's `message_hash`,
the lowercase hex SHA-256 of its message as stored,
computed on output.
`issue comment edit --base HASH` (`base=` in Starlark) names the text the edit was made on,
and is refused, writing nothing,
when the comment's current hash is not HASH, in full:
the comment changed since it was read,
and the writer gets it again and redoes the edit on the new text.
Without a base, the edit replaces whatever is there.

The check runs under the write lock, on the issue read again inside it
([store](store.md#re-read-under-the-lock)),
so nothing committed between the check and the write is decided against blind.
It is a content hash, not an operation id:
an edit that writes back the text that was read is no conflict,
and the text is what the writer's edit depends on.
It is checked against this clone only:
an edit made on another clone and not yet pulled is not seen,
and the two merge by operation order, as any concurrent edit does.

Not a patch verb, taking old and new replacements:
it would be a second way to do one thing,
since a writer that gets the message into a file
makes targeted edits with its own editor,
and the base gives the conflict check.

### Delete hides, the history keeps

`DeleteComment{target}` names the add-comment operation of the comment,
as `EditComment` does.
Applying it removes the comment from the snapshot's comments,
so `issue get`, the comments tab and the excerpt's comment count leave it out.
Nothing else changes:
the timeline is left alone
and the operations are untouched,
so `issue log` still shows the comment, its edits and the delete,
and `issue get --at` from before the delete shows the comment.
In git, delete is hiding, and the history is the archive.

It is an operation, not a ref move,
so it travels like any other write:
pushed, pulled and merged without conflict,
and it works on a comment already published,
which a local undo could never reach.
Jira deletes its copy of the comment on the next sync ([Jira sync](jira.md)).

### Delete is final

There is no undelete.
Jira cannot restore a deleted comment,
so an undelete would be a local resurrection Jira never sees,
or a second comment in Jira under the old one's identity.
To bring the text back, a writer copies it from the log into a new comment.

A delete wins over a concurrent edit with no rule of its own.
`EditComment` changes the comment it finds in the snapshot,
and finds none once it is deleted,
so an edit applied after the delete changes nothing
and one applied before it is removed with the comment:
both clones end deleted, and the edit stays in the history.
A deleted comment's id no longer resolves,
so a second delete or an edit is refused as a comment that does not exist.

### The description cannot be deleted

Deleting the first comment would leave a hole
every reader of `Comments[0]` must allow for.
`host` refuses it, naming `issue comment edit` as the way to clear it,
and `Apply` ignores a `DeleteComment` that targets it,
for one written by something other than `host`.

### Older binaries ignore a delete

A binary that predates `DeleteComment` reads code 12 as an unknown operation,
which applies as a no-op:
it shows the deleted comment and keeps working.
If it runs the Jira sync after a newer one deleted the Jira comment,
it sees a paired comment gone from Jira and writes a tombstone edit,
which a newer binary applies to a comment it no longer has, changing nothing.
The cost is a stale view on the older binary until it upgrades.

### Any writer may delete

As any writer may edit:
there are no per-author permissions without signed operations.

## Type

### A type change is a set

`git work issue set ID '{"type": "task"}'` changes the type,
and the call's other keys are checked against the new type.
The fields the old type had and the new one lacks stay on the issue,
as any off-schema value does:
readers already tolerate them.

Which types an issue may become is its current type's `retype_targets`
([schema](schema.md#a-type-says-what-it-may-become)).
A local write to a type the list does not name is refused,
naming the types it may become:

```
type: type change from epic to initiative is not allowed; allowed: (none)
```

The change is checked once per call,
from the type the issue has to the one the call leaves it with,
so `issue set`, `work.issue.set` and `--dry-run` refuse alike.
`issue new` is not a type change and is never checked against it,
nor is an issue with no type yet, or one whose type the schema lacks.

It is policy, not shape:
a pull runs the shape check only,
so a type changed by a Move in Jira is imported whatever the list says,
since Jira is the authority on what Jira holds.
A local type change that is allowed is exported to Jira,
alone and before any other field;
if Jira refuses it, the sync takes Jira's type back
and reports a conflict on `type` with Jira's reason
([Jira sync](jira.md#a-local-type-change-is-exported-alone-and-first)).

### The policy is per type, not per issue

Which types an issue may become is decided by its type alone:
an epic created locally and never synced cannot become an initiative either,
when epic's list does not allow it.
That keeps the checker free of Jira state,
since whether an issue is linked lives in the sync's base, not the schema;
and a repository that syncs to Jira has its types' lists derived from Jira
([Jira sync](jira.md#retype-targets-follow-jiras-levels-and-workflows)),
so the rule is the one the sync expects Jira to apply.

## Rank

### Rank starts null

`issue new` writes no rank;
the first drop on a view writes one.
Everything a rank orders sorts by **(rank, id)**:
two people dragging into the same slot derive the same key,
the tie breaks by id, and both drags survive.
A drag computes a key strictly between its neighbours,
so it writes one issue.

A null rank sorts after every set one.
Two issues with no rank are equal to that rule, not ordered by id,
so they keep the query's own order,
which on a store nobody has dragged in yet is the whole order.
A drop that writes the first rank in its scope
also ranks the unranked rows drawn above it, so the screen reads as it was drawn
([terminal](tui.md)).

No view takes a rank argument:
every view orders by the built-in `rank`, and every drag writes it
([schema](schema.md#rank-is-built-in)).

## Deliberately absent

- **Undelete.** Jira cannot restore a deleted comment,
  so an undelete would be a local resurrection Jira never sees
  ([Delete is final](#delete-is-final)).
- **Purging a comment's text from git.**
  The history is replicated by design;
  a comment that must not exist anywhere, a leaked secret,
  needs history rewritten on every clone, which is not a tracker operation.
