# Importing any git-bug repository: a mapping read from the schema

**Outcome:** `git work migrate` converts a git-bug repository into git-work
against whatever schema the repository has,
with no label taxonomy written into the command.

**Serves:** story `01c6231` (`git work migrate` imports any git-bug repository).

**Status:** proposed 2026-10-09.

## Why the command has to change

`migrate` was written for one store, this one, on 2026-09-25 (`store-migration.md`).
Its label mapping is that store's taxonomy, in Go:
`type:`, `prio:`, `phase:`, `area:` and `story:` labels,
fixed value tables (`med` is `medium`, `spike` is a task),
and a `Needs()` list that refuses to run unless `story`, `task` and `decision`
each carry `status`, `priority`, `phase`, `area`, `parent` and `labels`.
Since this repository's schema was trimmed (`5eb9228e`)
the command refuses even here,
and another team's git-bug repository will not share our labels.
The mapping also names fields from Go, which the no-field-roles rule forbids.

What stays is everything that is not the mapping:
the replay with entity and comment ids preserved,
lamport times reproduced, identities copied to `refs/work-users`,
the old refs kept,
and the refusal when `refs/work-issues/*` already holds anything.

## M1 — the schema is the whole mapping

The command reads the repository's schema and takes the rules below from it,
so preparing an import is writing a schema,
which the user does anyway with `git work schema init` or `schema import`.
There is no `Needs()` list:
the only refusal is a schema with no type at all.

## M2 — a `<name>:<value>` label is a field

A label with a colon in it is read as a field and its value,
split at the first colon.
`name` is matched against the issue's type,
first a field key exactly, then a field's display name, ignoring case;
`type` is the built-in and matches a type key or a type's display name.
The value is then written by the field's kind:

- **`enum`, `ordinal-enum`:** a value id, else a value name ignoring case.
- **`multi-enum`:** the same, added as one item;
  a freeform field takes any value as it is.
- **`relation`, `multi-relation`:** an id prefix of an issue in the same import,
  resolved to its full id (git-bug's `story:3f9a1c2`).
- **`bool`, `number`, `date`, `text`:** the value parsed as the kind takes it.
- **`identity`:** not mapped; git-bug labels name no identity.

A label added sets the field (or adds the item);
a label removed with nothing added in its place clears it (or removes the item);
a removed `type:` alone changes nothing, a type never being unset.
These are the replay rules `store-migration.md` already had,
kept for whichever field a label lands in.

`--field NAME=KEY` maps a label name to a field explicitly,
for a taxonomy whose words differ from the schema's:
this repository's own migration would be
`--field prio=priority --field story=parent`.
An explicit mapping wins over the match.

## M3 — what does not fit is a plain label

A label with no colon,
one whose name matches no field of the issue's type,
or one whose value the field refuses (an enum value it does not list,
a prefix that names no imported issue)
is a plain label.
Plain labels go to the type's freeform `multi-enum` field
when the type has exactly one;
otherwise they are not written.

The schema decides, not the command:
an enum value git-bug used and the schema lacks is refused, never added,
because the schema is the user's to author.
`--dry-run` lists every label that did not land, with its count and why,
so the user extends the schema and runs it again.

## M4 — type and status

**Type:** the first `type:` label an issue was ever given, matched as in M2;
else `--type KEY`;
else the first type in schema order.
It is set in the create pack, as before,
because an issue needs a type from its first operation.

**Status:** git-bug's open and closed are the one status every issue has.
They map onto the type's workflow field,
the one `enum` whose values carry status categories,
when the type has exactly one:
open to its first value in the `unstarted` category,
closed to its first in `completed`.
A type with no such field gets no status.
This reads categories, never the name `status`,
the way the `overview` flow does.

## M5 — what this repository loses

`spike` as a task plus a label,
and `med` as `medium`, were this store's value tables.
They do not come back:
the migration of 2026-09-25 ran and stays in history,
and re-running it is not a goal.
`TestIdsMatchBug` and the replay tests stay,
their fixtures their own (`AGENTS.md`, tests own their fixtures).

## Open questions

- **Q1. How loosely a name matches a field.**
  Proposed: exact key, then display name ignoring case, and nothing looser.
  A prefix (`prio` for `priority`) is convenient and would guess wrong on `st` or `p`;
  `--field` covers it explicitly.
- **Q2. Plain labels with no freeform field to hold them.**
  Proposed: not written, and listed by `--dry-run` and in the run's summary.
  The alternative is to refuse the import until there is a home for them,
  which is stricter than losing a label deserves.
- **Q3. `--type` versus the first type in schema order.**
  Proposed: both, the flag winning.
  A required `--type` would be safer and is one more thing to know.

## Out of scope

- Importing into a repository whose `refs/work-issues/*` already holds issues.
  Two id spaces meeting is a merge, not an import.
- git-bug's bridges' metadata (GitHub, GitLab ids).
  They are kept on the create operation as they are,
  and nothing reads them.
