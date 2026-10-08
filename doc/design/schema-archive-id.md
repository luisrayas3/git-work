# A schema entity is named by key or by id, never by guessing (E7)

**Outcome:** the loser of a duplicated schema key can be archived,
removed or read on its own,
so that the remedy E7 promises is one command
and a store where every schema command warns can be made quiet.

**Serves:** `6555e36` (the story); amends E7 in `config-entity.md`
and the schema rows of `cli-convention.md`.

**Status:** decided 2026-10-07 (Luis); not built.

## Problem

Two clones that derive one new type or field before exchanging
hold two entities with one key (E7).
`Current` picks the winner, lowest creation lamport time then lowest id,
and every schema command warns:

```
field task/status is defined twice; db9cdb7 is ignored, archive or import it
```

Neither remedy is reachable:

- `schema archive KEY`, `schema rm KEY` and `schema log KEY`
  resolve their argument with `ResolveSchemaKey`,
  which tries each shape's `ResolveKey`,
  and `ResolveKey` fails with a multiple-match error
  as soon as two unarchived entities hold the key.
  The loser cannot be named, and neither can the winner:
  a duplicated key is unusable by every command that takes one.
- `ResolveSchemaKey` reports the last shape's error,
  so the person reads `no type or field task/status: schema doesn't exist`
  for a key the warning just printed twice.
- `schema import` reconciles a document against `SchemaEntries`,
  which is the compiled view and so holds only winners;
  exporting and importing the live schema is a no-op
  and touches no loser.

The commands take a key because a key is what a person edits,
and an entity id appears nowhere in a schema document.
The one place an id is shown is the warning,
and the warning names the only entity the commands cannot reach.

## A1 — Two argument kinds, told apart by the caller

A schema command names an entity either **by key** or **by id**,
and the caller says which.
Nothing is inferred from the argument's spelling:
a key may be hexadecimal (`bad`, `face/due` are valid keys)
and an id prefix is hexadecimal,
so any rule that guesses is wrong for some store.

| Form | Spelling | Resolves to |
| --- | --- | --- |
| by key | `git work schema archive KEY` | the entity `Current` picks for the key |
| by id | `git work schema archive --id ID` | the entity whose id has `ID` as a prefix |

`KEY` is a type key or `<type>/<field>`, as today.
`ID` is a full entity id or a unique prefix of one,
resolved by `SubCache.ResolvePrefix`,
which fails when the prefix matches more than one entity;
an `ID` is never read as a key,
and a positional argument is never read as an id.
`--id` takes exactly one value, and with it the command takes
no positional argument; giving both is a usage error.
The same two forms, with the same meanings, are on
`schema archive`, `schema rm` and `schema log`.

## A2 — By key on a duplicated key

A command **that writes** by key,
`archive` and `rm`,
refuses a key that two or more unarchived entities hold,
and the refusal names them:

```
Error: task/status is defined twice: 3e11a08 (current) and db9cdb7;
name one with --id
```

Resolving the key to the winner and writing to it silently
was rejected:
the person's intent on a duplicated key is to end the duplication,
which is a write to the loser,
and a write that lands on the winner by default
is the edit lost without a word that E7 warns of.

`schema log KEY`, which reads,
prints the operations of every entity holding the key,
winner first then by creation,
each operation already naming its `entity`,
so a reader sees the whole history of the key
and the two ids to choose between.

The misleading message is fixed on the way:
`ResolveSchemaKey` returns the multiple-match error when any shape
produced one, and not-found only when every shape did.

## A3 — By id is exact

`--id ID` resolves the prefix and nothing else:
archived or not, winner or loser, type or field.
`archive --id` on an entity already archived sets the flag again
and leaves it archived, as `issue archive` does.
`rm --id` deletes the local ref, as `rm KEY` does,
and the entity returns on the next pull (E10).
`log --id` prints that entity's operations alone.

## A4 — The warning names its remedy

```
field task/status is defined twice; db9cdb7 is ignored:
git work schema archive --id db9cdb7
```

The warning is printed by every schema command (`SchemaDuplicates`),
so the command it names is pasted, not composed.
The loser's values are not folded by the command:
E7's "import of the loser's exported values" needs an export of the loser,
which `log --id` now gives,
and a person who wants a loser's attribute on the winner
edits the document and imports it, as for any other edit.

## A5 — Starlark

`work.schema.archive(key=None, id=None)`,
`work.schema.rm(key=None, id=None)` and
`work.schema.log(key=None, id=None)`
take exactly one of the two keywords, both as keywords;
neither has a positional form,
so a script cannot pass a value whose kind is unsaid.
Passing both, or neither for `archive` and `rm`, is an error.
`work.schema.log()` with neither keeps its meaning, every entity.

## A6 — `import` is unchanged

`schema import` keeps reconciling against the compiled view.
A document never names an id,
and a duplicated key in the store is resolved by `--id`
before or after an import, never through one.
`--prune` archives what the document omits
and continues to see one entity per key, the winner;
a loser is not pruned by a document that keeps its key,
because the key is present.

## What this is not

- Not a change to who wins. `Current`, `candidates` and the lamport rule
  stand, and so does the deterministic ignore.
- Not an id on issues. `issue` commands already take an id prefix
  or an alias in every `ID` position (`cli-convention.md`);
  schema commands gain `--id` and no alias.
- Not a merge of two entities. The loser is archived, its history kept,
  and nothing is copied into the winner by this command.

## Tests

In `commands/schema/schema_test.go`:

- two entities with one key: `archive KEY` and `rm KEY` refuse,
  the message names both ids and `--id`;
  `log KEY` prints both entities' operations, winner first.
- `archive --id <loser>` archives the loser; the warnings stop;
  `Current` still picks the same winner.
- `archive --id <winner>` archives the winner;
  the former loser is now `Current` and no warning prints.
- `archive --id <ambiguous prefix>` fails with the multiple-match error;
  `archive --id <key>` fails with not-found, never resolving a key.
- `archive KEY --id ID` is a usage error.
- `log --id` of an archived entity prints it.
- `SchemaDuplicates` prints the `--id` command.
- Starlark: `work.schema.archive(id=…)` archives;
  `archive(key=…, id=…)` and `archive()` raise.

## Tasks

- `cache.RepoCacheConfig.ResolveSchemaKey` keeps the multiple-match error
  and names the ids; a `ResolveSchemaId(prefix)` over `ResolvePrefix`.
- `host.SchemaArchive`, `SchemaRm`, `SchemaLog` take a
  `host.SchemaRef{Key, Id string}` with exactly one set;
  `SchemaLog` by key returns every holder.
- `commands/schema`: the `--id` flag on `archive`, `rm` and `log`;
  `cobra.MaximumNArgs(1)` with the usage check;
  `KeyCompletion` unchanged.
- `host.SchemaDuplicates` prints the command.
- `flow/run/modules.go`: the two keywords on the three verbs.
- `doc/design/cli-convention.md` schema rows and the Starlark line;
  `doc/design/config-entity.md` E7 names this document;
  `AGENTS.md`'s schema table gains the `--id` forms.
