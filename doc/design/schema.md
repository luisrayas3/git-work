# The schema

The schema says which types an issue may be,
which fields each type has, what kind of value each holds,
and so what a write may contain.
It is stored as config entities, one per type and one per field,
compiled on demand into the schema every issue write is checked against,
and edited as a YAML document that `schema import` reconciles into the entities.
The issue's own model, its operations and its built-in fields as stored,
is the [issue](issue.md);
the dag, the namespaces and the write lock are the [store](store.md);
flows are config entities too, and are [flows](flows.md);
the command forms are the [command line](cli.md);
a program's `$schema` is [query](query.md);
how show draws a type and its links is [views](views.md);
how the Jira sync derives a schema and maps it is the [Jira sync](jira.md).

## Config entities

### Configuration is entities, not a file

Types and fields are CRDT entities under `refs/work-schema/*`,
so the schema travels with the tracker rather than with a branch,
merges when two people edit different things,
and `schema log` shows who changed what.
The refs are the runtime truth:
`schema.yaml` in a working tree is an authoring copy
that reaches them only through `git work schema import`,
and nothing reads the tree at run time,
so a checkout can never disagree with the store.

Review happens on a file:
`schema export`, edit, `schema import`.

Not one blob holding the whole document:
that is last writer wins over everything,
so two people adding two statuses means one silently loses.
Not a worktree file read at run time:
it differs per branch, and the tracker does not.
A sync that writes only what differs emits one operation per real change,
with its author, which is the audit trail worth having, not noise.

### One entity per type and per field

An issue type is one entity, keyed by the type;
a field is one entity, keyed `<type>/<field>`.
The entity is the coarse merge unit:
two people adding two fields are two entities and never meet,
exactly like two people opening two issues.
Inside one entity, per-attribute operations are the fine unit.

There is no singleton:
the schema is the set of type and field entities, read by listing the namespace,
so nothing has to be found as "the one entity of kind X".

Not one entity holding a flat map of paths,
the first draft's design:
it rebuilt inside one entity the per-key merge the dag already gives per entity,
and needed a singleton whose duplicate no operation could repair.

### A field belongs to one type

`task/status` and `epic/status` are two entities with the same field key,
and may have different values;
an issue stores `status`, and the schema resolves the entity by the issue's type.
This is Jira's model, field configurations and workflows per issue type,
so a sync instantiates per type instead of inferring what is shared.
Sharing is an authoring concern:
YAML anchors in the document, reconciled into one entity per type.

### Four generic operations

```
Create      {shape, key}       immutable; the id derives from this operation
Set         {name, value}      one attribute, last in dag order wins
Remove      {name}
SetArchived {archived}         the replicated removal
```

plus dag's `NoOp` and `SetMetadata`.
An entity's state is its shape, its key,
a map of attributes with last writer wins per attribute,
and the archived flag: a plain document.
The discriminator is the **shape**, `type`, `field` or `flow`,
because `kind` is a field's data type and nothing else uses the word.

The operations are generic across shapes
so that a shape added later is not a new operation type:
an older binary reads a newer shape's entity in full, lists it and syncs it,
and only its typed projection ignores it.
Nobody outside `entities/config` and `schema` writes an attribute name by hand.

### Collections are one attribute per member

An enum's values are attributes named `values/<id>`,
a relation's allowed targets `target_types/<type>`.
Adding a member is a `Set` and removing one a `Remove`,
so two people adding two members concurrently both keep theirs,
which one attribute holding a list could not give.
Two people editing the *same* member resolve last writer wins on that member.
Export folds them back into lists.

### What a type and a field carry

```
shape  key             attributes
type   <type>          name, description, ordinal, show, retype_targets, color, alias_<system>
field  <type>/<field>  kind, name, description, ordinal,
                       freeform                                      enum kinds
                       values/<id> -> {name, ordinal, category, description, color}
                       alias_<system>/<id>                           a value's alias
                       inverse, link, target_types/<type> -> {}      relation kinds
                       alias_<system>
```

A type's `show` is how show draws its issues ([views](views.md));
`alias_*` is what an entity is in another system ([Jira sync](jira.md)),
and the `alias_` prefix is reserved for that.
Both change no storage, no write and no query.

Attribute names are forever:
old operations keep theirs,
so renaming one is a new name plus a projection that reads both.

### Keys and names

A type key, and each half of a field key, is a slug, `^[a-z][a-z0-9_-]*$`,
the issue entity's own field-key rule,
so every field key is one an issue can store.
The create operation checks only the looser, frozen shape,
`[a-z0-9][a-z0-9_.-]*(/[a-z0-9][a-z0-9_.-]*)?`, at most 64 bytes;
the per-shape rule is checked on write.
Display names are free text in attributes,
which is what lets a rename in Jira change one attribute and no history.

### Order is an ordinal

Every type, every field and every enum value carries an integer `ordinal`,
and sorts by `(ordinal, key)`.
A fresh numbering is spaced by tens,
so inserting one entry between two is one operation, not a renumber.
Renumbering a field's values is many `Set`s in one commit, so it is atomic;
field order across entities is not, and need not be:
a half-applied reorder is still an order.

Not a fractional index as issue rank uses:
inside one entity the dag already makes a renumber atomic.

### Structural rules are frozen

An operation's `Validate` runs on every read,
and one that fails makes the entity unreadable,
so it checks structure only, and those rules can only be loosened:
the key's frozen shape, an attribute name `^[a-z][a-z0-9_]*(/[a-z0-9][a-z0-9_.-]*)?$`,
a value that is valid, printable JSON under 64 KiB.
It does not check that a shape or an attribute is one this binary knows,
so a newer binary's entities read on an older one.

Everything that can change is checked when a document is imported
(`Document.Validate`), before anything is written:
kinds from the fixed list, categories from the fixed set,
values, inverses and target types only on the kinds that take them,
an inverse unique within its type and colliding with none of its fields,
target types that exist in the document or the store,
no alias claimed twice in one system,
and a built-in's kind unchanged.
Each problem names the thing and its valid alternatives.

Compiling never fails on what is stored:
an attribute it cannot read, a field naming no type,
a target type that is gone,
are `Problems` on the compiled schema, printed as warnings, never an error.

### Duplicate keys

Two clones that create one key before either pushes
hold two entities of one shape and key.
`Current` picks the one with the lowest creation lamport time, then the lowest id,
from the excerpts alone,
and ignores the other, the same way on every clone.
Every schema command warns on stderr, naming the remedy:

```
field task/status is defined twice; db9cdb7 is ignored:
git work schema archive --id db9cdb7
```

Archiving the loser is an operation, so it reaches every clone.
Nothing is folded from the loser into the winner:
`schema log --id` shows the loser's values,
and someone who wants one on the winner edits the document and imports it.

### Key or id, and the caller says which

`schema archive`, `schema rm` and `schema log` name an entity
either by key, `KEY`, or by id, `--id ID`, never both.
Nothing is inferred from the spelling:
a key may be hexadecimal (`bad`, `face/due`) and an id prefix is,
so any rule that guesses is wrong for some store.
`--id` takes a full id or a unique prefix, and is never read as a key;
a positional argument is never read as an id.
In Starlark the two are the keywords `key=` and `id=`,
with no positional form, so a script cannot pass a value whose kind is unsaid.

### A write refuses a duplicated key

`archive KEY` and `rm KEY` refuse a key two unarchived entities hold,
naming them, the winner marked current, and asking for one by id.
Not resolving to the winner and writing to it:
the intent of a write on a duplicated key is to end the duplication,
which is a write to the loser,
and landing on the winner instead is the edit lost without a word.

`schema log KEY` reads every entity that ever held the key,
archived included, the winner first then by creation,
so a reader sees the whole history and the ids to choose between.

### By id is exact

`--id` resolves the prefix and nothing else:
archived or not, winner or loser, type or field.
`archive --id` on an archived entity sets the flag again;
`log --id` prints that entity alone.

### Import sees only winners

`schema import` reconciles against the compiled view, which holds winners only.
A document never names an id,
so a duplicated key is settled with `--id` before or after an import, never through one,
and `--prune` never archives a loser whose key the document keeps.

## Fields

### Kinds are a fixed list

| Kind | Used by | Value |
| --- | --- | --- |
| `text` | title, free text | string |
| `enum` | status, type, any closed list | value id |
| `ordinal-enum` | priority | value id |
| `bool` | archived | bool |
| `number` | estimate, story points, capacity | number |
| `date` | start, due | `2026-09-23` or RFC 3339 |
| `identity` | assignee | an identity's id |
| `multi-enum` | labels, components | list of value ids |
| `multi-identity` | reviewers, watchers | list of identity ids |
| `rank` | the built-in `rank` alone | a fractional-index string |
| `color` | the built-in `color` alone | one of twelve color ids |
| `relation` | parent, iteration | an issue's full id |
| `multi-relation` | blocks, relates-to | list of issue ids |

A missing value is config; a missing kind is a change to the schema language.
The fixed list is the guard against the engine outgrowing "just configurable enough",
and it is closed.
The `multi-*` kinds are the ones `add` and `remove` apply to.

A category is a property of an enum *value*,
so there is no separate kind for an enum whose values have categories.
`ordinal-enum` is its own kind because ranked values are a property of the field.
`freeform` on an enum kind accepts values outside its list,
as Jira's labels do.

### Values are stored by id

A value is `{id: in-review, name: In Review, category: started}` in the schema,
and an operation stores `in-review`.
A rename then changes one attribute instead of history,
which, with content-addressed operations, cannot be rewritten.

### Categories are fixed

`backlog`, `unstarted`, `started`, `completed`, `canceled`.
Everything keyed on "is this done" reads the category, never the value's name;
`completed` and `canceled` are done.
A value may have none, as priority's do.

### type is checked against the types

`type`'s values are the type entities themselves:
the one field the engine resolves against something other than its own values.

### Relations

A field of kind `relation` holds one issue, `multi-relation` many;
cardinality is the kind,
so setting a second parent replaces the first.
The field entity is what says the string an issue holds is an issue id;
without it, a field holding an id is just a string.

- `target_types` restricts what the target may be:
  `[epic]` on `story/parent`, `[iteration]` on `story/iteration`.
  Empty is anything.
- `inverse` names how the other side reads the relation,
  `parent` as `children`, `blocks` as `blocked-by`.
  It is never a key an issue can write:
  the inverse is derived by the reader, never stored ([issue](issue.md#relations-are-fields)).
- A target that names an issue removed or not yet pulled is never an error on read.

### A type says what it may become

A type's `retype_targets` lists the types its issues may become
by a `set` of `type`:

```yaml
types:
  epic:
    retype_targets: []
  story:
    retype_targets: [task, bug]
```

- **Absent** is any type, so a schema that does not state it restricts nothing;
  **empty** is none, the type is fixed once the issue is created.
- A target that is neither in the document nor in the store,
  or one listed twice, is an import error;
  one whose type is later archived is a problem on the compiled schema.
- It is one-way, and need not be symmetric.
- A synced type may list only types synced to the same system
  ([below](#a-synced-types-targets-sync-to-the-same-system)).
- A local write is checked against it, a Jira pull is not
  ([issue](issue.md#a-type-change-is-a-set)).
  For a type mapped to Jira the sync derives it
  from Jira's hierarchy levels and workflows
  ([Jira sync](jira.md#retype-targets-follow-jiras-levels-and-workflows)).

It is one attribute holding a JSON list,
not one attribute per member as `target_types` is,
because absent and empty mean different things.
It is a property of the type, not of a field,
so it gives `type` no role:
`type` is built in already,
and this is the schema saying what the built-in accepts.

### A synced type's targets sync to the same system

A type with a non-empty alias for a system
may list as `retype_targets` only types with a non-empty alias for that same system;
anything else is an import error naming the system and the target.
A type with no non-empty alias, unaliased or aliased `""` only, is unconstrained.
The import checks the schema it would leave,
so a partial file meets the types already in the store.

Without the rule, an issue linked to a system could be retyped out of the sync,
and its type change would have nowhere to go.
The rule is about aliases, not Jira, so the schema stays backend-free.

### Hierarchy is target types

Hierarchy is `target_types` on each type's `parent` field,
`story/parent` targeting `[epic]`, `epic/parent` `[initiative]`.
That is the whole of it:
Jira's Initiative > Epic > Story > Sub-task
and Linear's Initiative > Project > Issue are both config.

### A relation is stored as the full id

Every id position takes an id prefix or an alias,
and a relation's value is one;
stored verbatim, a prefix is a value no exact comparison finds,
`select(.fields.parent == "<full id>")` among them,
and an alias names nothing once it is reused.
So the cache resolves a relation's value to the full id, on the field's kind,
before the check, on every write path, the Jira pull's included.
What names no issue, or more than one, is left as written for the check to refuse, named.

### A link is a marked relation

A relation field may carry `link: true`,
as the Jira sync marks every field it derives from a link type.

- Only `relation` and `multi-relation` take it;
  on another kind the import refuses it.
- The inverse of a link is a link:
  it has no entity of its own to mark.
- It changes no storage, no write and no query.
  Only the views read it, to draw every link of an issue as one row ([views](views.md)).
- It is not a field role: Go names no field by it, and any relation may be marked.

A bool on the field rather than a group entity listing field keys,
because a field is drawn either as a link or as itself,
and the mark then merges, archives and travels with the field it describes.
`links` is reserved: no field and no inverse may take it.

### No field roles

Go names no field but the five built-ins.
What a field is *for* is its consumer's business:
a flow passes the keys it needs to the host functions,
`work.view.gantt(start="start_date", stop="due_date")`.
A role on the field would be one consumer's choice stored on the schema.
There is no `on_open` or `on_close` either:
closing an issue is `git work issue set ID '{"status": "done"}'`,
or a flow that writes the value it wants.

Status is a field of kind `enum` a type may have,
not a built-in;
everything keyed on categories applies where a type has one
and reads "never done" where it does not.

### Built-ins are code

`title` (`text`), `type` (`enum`), `archived` (`bool`) and `rank` (`rank`)
exist on every type, defined in code in `schema.Builtins`:
`title` to show anything, `type` to find the rest of the schema,
`archived` to know what the default listing hides,
`rank` to hold the manual order.
The fifth, `color` (`color`), is defined there too,
and exists only on a type that switches it on
([Color is a built-in a type switches on](#color-is-a-built-in-a-type-switches-on)).
A field entity whose key is a built-in overrides its name and description;
its kind is code, and a document that changes it, or gives it values,
targets, an inverse, a link or aliases, is refused.
A built-in with no entity is never exported,
since importing it back would create an entity that changes nothing,
so a file that lists `rank` or `title` on a type is that override, not a duplicate.

A repository with no type entities runs on the built-ins alone,
and validates no write:
nothing can say which fields an issue has.

### Rank is built in

Both native models order every issue,
Jira by Rank on every issue type and Linear by `sortOrder`,
so a schema that cannot hold a manual order cannot hold either.
A field every type has in every preset is a field in code:
leaving it configurable only lets someone remove
what every board and backlog orders by.
How it is stored and sorted is the [issue](issue.md#rank-starts-null)'s.

### Rank's kind is the built-in's alone

The rank is an implementation detail:
every view orders by the built-in `rank` and every drag writes it,
and no view takes a rank argument.
So `rank` is not a kind a configured field takes:
`schema import` refuses `kind: rank` on any field but the built-in,
saying the order is internal and listing the kinds that remain,
and compiling an entity that holds one reports it.
A field of kind rank of a type's own would be an order no view can draw.
Jira's Rank, where mapped, maps onto the built-in.

Not `text` for the built-in:
an override would then say `kind: text` of a fractional-index string,
and a kind is what a bridge reads to know how to carry a value.

### Color is a built-in a type switches on

An issue may carry a color that every surface draws it in.
`color` is the fifth built-in, of a kind `color` that is its alone, as `rank`'s is:
`schema import` refuses `kind: color` on any other field.
Unlike the other four it exists only on a type whose `color` attribute is true,
`types: {story: {color: true}}` in the document,
written only when true and exported only when true.
On any other type it is no field,
and a write of it is refused as an unknown key is;
an override of it is refused on a type that does not switch it on.

Its values are a set fixed in code,
the twelve ANSI colors that read on any theme:
`red`, `green`, `yellow`, `blue`, `magenta`, `cyan`,
and `bright-` before each of the six.
Black, white and their bright forms are left out,
because each clashes with the background or the foreground on some theme.
A write of anything else is refused, naming the set;
being shape, not policy, that holds for a pull as for a local write.
`null` clears it, and the field holds no `values`,
so `ordinal()` and `category()` are null on it ([query](query.md)).
The surfaces draw it with the terminal's own palette,
indices 1–6 and 9–14,
never a 256-color or a truecolor one,
so that a color follows the user's theme ([views](views.md)).
It is stored like any field's value, and is never synced ([Jira sync](jira.md)).

Not a configured kind any field may take:
a view would then need an argument naming the field,
and the color belongs to the issue, so every surface finds it by one key.
Not on every type:
a color is wanted on the issues that head others, not on every issue,
and a field every type had would be one most types never use.

### Iterations are a type

A sprint, a cycle, a quarter is an issue of a type such as `iteration`,
in `refs/work-issues` like every issue,
with its dates and capacity as that type's fields
and its comments, timeline and participants because it is an issue.
Membership is a `relation` field with `target_types: [iteration]`.
The jira preset's sprint and the linear preset's cycle are both that.

Not a namespace of its own:
what one bought, lists needing no filter and a cheap "is this an iteration",
is what per-type fields give every type anyway.

Closing an iteration is not atomic:
marking it closed and moving N unfinished issues is N+1 commits.
Create the next iteration, move the issues, then close the old one,
so that a crash leaves some issues already in the next sprint,
which reads correctly and is idempotent to retry.

## Validation

### Validate on write, lenient on read

A schema says what may be written now, never what was written before.
Operations are immutable and content-addressed,
so a schema that rejected existing data would make history unreadable.

Every issue write is checked in `cache`, on the write path,
against the schema compiled at that moment,
and `--dry-run` reports what a commit would refuse.
Reads keep a value the schema no longer knows, and show it as written.
A field archived in the schema stops being settable;
it does not vanish from the issues that have it.
The entity package imports nothing of the schema.

A check is two checks:
the **shape**, a value fitting its field's kind on a field the type has,
and what it names existing;
and the **policy**, an enum value among the field's values,
a relation's target among its target types,
a type change among the current type's retype targets.
A local write gets both; a Jira pull gets the shape only ([Jira sync](jira.md)).

### Errors name the alternatives

A refusal names what is wrong and what would be right,
`field status: "shipped" is not in the schema; valid values: to-do, in-progress, done`,
and lists every problem of the write at once:
the main caller is an agent, which can act on that,
and three round trips to learn three mistakes is three commits.

A new issue needs a `type`, since nothing else can say which fields it has.
A key that is not a field of the type is refused, naming the type's fields.

### Compiled on demand

`RepoCache.LoadSchema` compiles the built-ins and every unarchived winning type and field
into one `*schema.Schema`,
from the excerpts alone, which carry each entity's whole document.
Nothing is memoised:
a schema is a few dozen small documents already in memory,
and a memo could go stale behind the ref watcher.
`RepoCache.Checker` is that schema plus the lookups a value check needs:
whether an identity exists, what type an issue is, which issue a prefix names.

## The document

### The document is a view over the entities

```yaml
preset: jira
shared:
  status: &status
    kind: enum
    name: Status
    values:
      - {id: to-do,       name: To Do,       category: unstarted}
      - {id: in-progress, name: In Progress, category: started}
      - {id: done,        name: Done,        category: completed}
types:
  story:
    name: Story
    fields:
      status: *status
      parent: {kind: relation, inverse: children, target_types: [epic]}
      blocks: {kind: multi-relation, inverse: blocked-by, link: true}
  epic:
    name: Epic
```

- `types` maps a type key to `{name, description, aliases, show, retype_targets, color, fields}`,
  and `fields` a field key to
  `{kind, name, description, aliases, freeform, inverse, target_types, link, values}`.
  A value is `{id, name, category, description, color, aliases}`.
- `shared` is authoring only, for anchors;
  the parser expands every alias, so an anchored field reads as a spelled-out one.
- `preset` is informational: nothing records it, and export never writes it.
- An unknown key anywhere is an error, never a silent no-op,
  as in every document argument ([command line](cli.md)).
- JSON is accepted, being YAML, and `--format json` exports it.

### Position is the order

The order of a mapping or a list in the file *is* the order,
of types, of fields and of values,
so `ordinal` appears nowhere in the file.
Export is deterministic,
so `export | import` emits no operation, which is the round-trip test.

### Import is an upsert

`git work schema import` validates the whole document first,
then `Reconcile` matches each entry to the unarchived entity of its shape and key:

1. an entry with no match is created;
2. a match gets a `Set` or a `Remove` for every attribute
   whose canonical JSON differs, and nothing else;
3. ordinals keep their numbers where the relative order is unchanged,
   a moved or new entry takes a number between its neighbours,
   and a list is renumbered only when no number fits.

An entity the document does not mention is archived only under `--prune`,
types after their fields,
so a partial file from anywhere can never archive anyone's work.
An import owns only the attributes it writes:
one a newer binary added is left alone,
and an alias is written when stated and never removed,
so an alias-free file unmaps nothing.

Writing only differences is what keeps every import, and every Jira sync,
which imports through the same function,
from filling the log with operations that change nothing.

### One commit per entity

An import writes one entity at a time, one commit each:
there is no atomic multi-entity commit,
and each entity is valid on its own,
so a failure part way leaves a schema that reads, and reports the ids already created.
Types come first, because a field's key names its type.
`--dry-run` prints the changes and writes nothing.

### Archive and rm

`schema archive` is the replicated removal, an operation that reaches every clone;
it names the fields an archived type leaves attached to no live type,
and archives nothing else.
`schema rm` deletes the local ref, and the entity returns on the next pull.

### Presets are embedded

`jira` and `linear` are embedded with `go:embed`
and instantiated by `git work schema init [PRESET]`, `jira` by default,
through the same import.
`init` refuses a store that already has a field entity:
a preset is a starting point, not a merge,
and a schema already there is exported, edited and imported.
The two presets are the test of "configurable enough for both native models":
each must load and round trip.
