# A Jira pull is not refused by schema policy

Status: **implemented, 2026-09-28.**
`schema.Checker.Shape()` is the shape check;
`cache.IssueCache.UpdateShape` and `RepoCacheIssue.NewRawShape` commit under it;
`admit` pends what the shape check refuses and reports what only the policy
check refuses under `off_schema` (`TestPullOffSchemaWritten`).
A key that is not a field of the issue's type, or a type the schema lacks,
is shape, not policy: without the field there is no kind to fit.

## Problem

`admit` (`jira/issue.go`) checks every pulled field change
with the full `Checker`,
and turns a refusal into a pending `Retry`.
On AUT, eight Tasks (e.g. AUT-173) have an Epic parent,
while AUT declares Task a sub-task type (hierarchyLevel -1);
`derive.go` therefore allows only level-0 parents.
Those parents are refused on every run and never converge:
the local copy silently disagrees with Jira.

The check buys nothing on a pull:

- Jira is the authority on what Jira holds;
  the derived schema only approximates it.
- Ops are schema-free and replay whatever was written
  (the `Checker` comment in `schema/check.go`).
- Nothing in `tui/` reads `TargetTypes`.
  Readers already tolerate off-schema values,
  since every schema change creates some.

## Proposal

Split `Checker` in two:

- **Shape**: the value fits its field's kind,
  and an identity or issue it names exists.
- **Policy**: allowed link targets, enum membership, required fields.

`admit` applies the shape check only.
A value the policy check would refuse is written
and reported as `off-schema`, not pending.
Local writes keep both checks,
so git-work still cannot create a Task with an Epic parent,
and `task.parent` stays as derived.
