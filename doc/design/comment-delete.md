# Comment delete: `issue comment delete`

**Outcome:** a comment written on the wrong issue, or one that should not
have been written, is taken down everywhere it was published:
every clone on its next pull, and Jira on its next sync.
The text is not destroyed in git: the issue's history still holds it.

**Serves:** `9f8aed0`, delete a comment everywhere it was published.

**Status:** approved 2026-11-07.

## Problem

A comment cannot be taken back.
`issue comment edit` can blank it to a placeholder,
but the comment is still there, in every view, in the count, and in Jira.
Every other slip has a replicated correction that keeps Jira clean
when it comes before the next sync:
the sync merges values (`jira-sync.md` JS9),
so a field set back reads as unchanged,
and an archived issue is never exported.
A comment has none.
The model has no local comment delete,
so JS12 has no local-delete row,
and a comment on the wrong issue reaches Jira on the next sync,
where only an edit can follow it.

Local undo (`01a559a`, `local-undo.md`, shelved on `shelf/local-undo`)
would have covered the case before a push,
and was shelved as too costly for its value (U11 there).
This story covers it before and after a push.

## CD1 — A new operation, DeleteComment, replicated

`DeleteComment{target}` names the add-comment operation of the comment,
as `EditComment` does (`entities/issue/op_edit_comment.go`).
It takes type code 12, the next unused one (`entities/issue/operation.go`),
and the name `delete-comment` in the log.

It is an operation, not a ref move,
so it travels like any other write:
pushed, pulled, merged without conflict,
and it works on a comment that is already published,
which is the case undo could not reach.

## CD2 — Deleted is hidden from views, never removed from history

Applying it removes the comment from `snapshot.Comments`,
so `issue get`, the comments tab and the excerpt's `comments` count leave it out.
The operations are untouched,
so the text stays readable:

- `issue log` shows the add-comment, its edits and the `delete-comment`;
- `issue get --at TIME` from before the delete shows the comment.

That is the non-destructive half: in git, delete is hiding,
and the history is the archive.

The snapshot's timeline is left alone:
nothing outside `entities/issue` reads it.

## CD3 — Delete is final, and wins over a concurrent edit

There is no undelete.
Jira cannot restore a deleted comment (CD6),
so an undelete would be a local-only resurrection Jira never sees,
or a second comment in Jira under the old one's identity.
To bring the text back, a writer copies it from the log into a new comment.

Both follow from removing the comment from `snapshot.Comments`,
with no rule of their own:

- `EditComment.Apply` updates the comment it finds in `snapshot.Comments`,
  and finds none once it is deleted,
  so an edit applied after the delete changes nothing,
  and one applied before it is removed with the comment.
  A concurrent edit and delete end deleted on every clone,
  and the edit stays in the history.
- `ResolveComment` (`cache/issue_subcache.go`) searches `snapshot.Comments`,
  so a deleted comment's id no longer resolves,
  and a second delete or an edit is refused as "comment doesn't exist".

## CD4 — The description cannot be deleted

The first comment is the issue's description (`snap.Comments[0]`),
carried by the create operation.
Deleting it would leave an issue whose description is a hole
that every reader of `Comments[0]` must allow for.
`host` refuses it, naming `issue comment edit` as the way to clear it,
and `Apply` ignores a `DeleteComment` that targets the create operation,
for one written by something other than `host`.

## CD5 — The verb is `delete`, not `rm` or `archive`

`cli-convention.md` pairs the two existing removals:
`rm` is local and the entity comes back on the next pull,
and `archive` is replicated and reversible.
This is neither:
it is replicated, and final (CD3),
so it takes a third word, and the convention gains a line saying so.

```sh
git work issue comment delete COMMENT_ID   # prints nothing
```

`work.issue.comment.delete(id)` is the Starlark side,
through the same `host.IssueCommentDelete`.
The comment id is the one `issue comment new` printed
and `issue get` shows, resolved by `ResolveComment` as edit does.

## CD6 — Jira: the paired comment is deleted

JS12 gains rows.
A local comment is deleted when its add-comment operation is the target
of a `DeleteComment`:

| state | result |
| --- | --- |
| deleted locally, never paired | never exported |
| deleted locally, paired, present in Jira, base not `""` | `DELETE …/comment/{id}`; base `""` |
| deleted locally, paired, gone from Jira | base `""`, no request and no tombstone |
| deleted locally, paired, base `""` | nothing to do |

Deleting in Jira is destructive there, and that is the choice made:
the alternatives, leaving Jira's copy or redacting it to a placeholder,
leave the comment visible in the canonical system,
which is what a delete is for.

**A Jira edit since the base is imported first.**
When the Jira comment changed since the base and the local side deleted it,
the run also writes Jira's text as an `EditComment` by the Jira editor,
the change JS12 already makes for a Jira edit,
so the text Jira held is not lost to git.
It changes nothing on the snapshot (CD3), and the delete goes ahead;
no conflict is reported, since the writer asked for the comment gone.

**A lossy Jira text does not block the delete.**
JS11 holds back a local *edit* over a lossy text,
because the edit would overwrite what git-work cannot render.
A delete removes the comment whatever it holds,
which is what the writer asked for,
and the import above keeps the rendering in git.

**The request is idempotent.**
A `DELETE` that returns 404 means already gone,
and is the "gone from Jira" row.
A crash between the `DELETE` and the commit
leaves a paired, locally deleted comment that Jira no longer has,
which the next run settles by the same row.
Any other failure, a 403 for a token without delete permission among them,
is a failed request like every other, recorded and retried on the next run.

`jiraapi.Client.DeleteComment` already exists (`jira/jiraapi/meta.go`).
The report's comment counts gain `deleted`.

## CD7 — A comment deleted in Jira keeps its tombstone

JS12 edits a comment deleted in Jira to a one-line tombstone,
"Deleted in Jira on 2026-09-28.", because the model had no delete.
It could now import a `DeleteComment` instead,
and that is left for later:
the tombstone row works and is tested,
and changing it means deciding what becomes of comments already tombstoned,
for no gain to the case this story is for.

## CD8 — Older binaries read it as unknown

An older build decodes type 12 as an unknown operation
(`dag.NewUnknownOp`, `operation.go`),
which applies as a no-op,
so it shows the deleted comment and keeps working.
If an older build runs the Jira sync after a newer one deleted the Jira comment,
it sees a paired comment gone from Jira and writes a tombstone edit,
which a newer build applies to a comment it no longer has, changing nothing.
Nothing breaks; the cost is a stale view on the older build until it upgrades.

## CD9 — The terminal renderer

The comments tab binds delete to the comment under the cursor,
as a chord in the `esc` style the palette uses (`esc g`, `esc j`),
so it is never one stray key.
It does not ask first: the renderer has no confirmation yet
(`tui/tui.go`, `Render`, lists it as deferred),
and a chord costs nothing new.
The log tab draws a `delete-comment` line, "deleted comment",
beside "edited comment" (`tui/show.go`, `opSummary`).

## CD10 — Out of scope

- Undelete (CD3).
- Importing a Jira delete as a `DeleteComment` (CD7).
- A confirmation in the renderer (CD9).
- Purging the text from git.
  The history is replicated by design;
  a comment that must not exist anywhere, such as a leaked secret,
  needs history rewriting on every clone, which is not a tracker operation.
- Who may delete. Any writer may, as any writer may edit (`op_edit_comment.go`):
  there are no per-author permissions without signed operations.
