# Drawing an alias in place of the hash

**Outcome:** a person on a Jira-bound clone can say once that they want to
read `PROJ-12` where git-work draws `3f9a1c2`,
and from then on every screen and every text form names an issue by its
Jira key when it has one,
while every `id` a program compares, stores or is handed by a writer
stays the hash it always was.

**Serves:** story `fd6c808`.
It extends `cli-convention.md` ("Any `ID` position accepts an alias")
from the way in to the way out,
and follows the precedent `host.UserName` set for people:
store one thing, draw another (`terminal-renderer.md`, Show; `host/user.go`).

**Status:** approved 2026-10-08 (Luis), its open questions settled below.

## Problem

An issue's identity is the hash of its create operation,
and an alias is an immutable external id beside it,
`alias:jira = PROJ-12` on the create operation (`483dbe2`, `jira-sync.md` I3).
Every id position already accepts the alias,
so a Jira key pasted from a browser works anywhere.
The other direction does not exist:
every list, board, gantt, link, picker, status message and text line
draws the seven-character hash prefix,
so a team that talks in Jira keys has to translate every row it reads,
and the one person reading both Jira and the terminal sees two names for everything.

The hash cannot simply be replaced.
Agents and jq programs key on `.id`,
relation values store the full hash (AGENTS.md: cross-issue fields hold an `entity.Id`),
`(rank, id)` orders every view,
and a store holding a consolidated duplicate has two issues carrying one key
(`jira-sync.md` JS25).

## A1 — The opt-in is one git config key, `git-work.display.id`

```sh
git config git-work.display.id jira     # draw the jira alias where an issue has one
git config --unset git-work.display.id  # the hash, the default
```

The value is `hash` (the default, also what an absent key means)
or the name of an alias namespace — the `<name>` of `alias:<name>`.
It is read through the `git` CLI like every other `git-work.*` key
(package `gitcli`), so it may live in the repository's config,
in the user's global one or in an `[include]`d file, and git decides which wins.
That makes it per clone by default and per person at most,
which is the right reach for a way of reading:
it changes nothing anybody else sees.
It does not depend on the Jira binding (`git-work.jira.*`):
a clone that only pulls from the bound clone has the same aliases in its store
and may want to read them,
and this repository, which dogfoods the `jira` preset with no Jira behind it,
can set it and see every issue drawn by hash, because none has an alias.

**No per-call argument.**
A view's KWARGS is the spec of the view and is portable:
a saved view in a flow runs on every clone that imports it (`terminal-renderer.md`, "the command is the spec"),
and a way of reading is not part of what the view is.
A flag on every text command would be a flag on a dozen commands for one preference.
The per-call override already exists without either:
`git -c git-work.display.id=hash work issue --format text` sets the key for one command,
because git hands `-c` to its subcommands in `GIT_CONFIG_PARAMETERS`
and `gitcli` runs `git config` under that environment.
That the override reaches through is a test the implementation owes.

## A2 — The namespace, not Jira

The value names an alias namespace rather than switching Jira on,
because aliases are keyed by system already (`alias:<name>`, `cli-convention.md`)
and the schema's reserved `alias_` prefix was designed so that a Linear bridge
adds `alias_linear` with no change elsewhere (`jira-sync.md` JS2).
A later Linear binding sets `git-work.display.id linear` and nothing in this design changes.
One namespace, not an ordered list:
one clone is bound to one tracker (AGENTS.md, "one team, one repository, one Jira project"),
and an issue with aliases in two systems is a case nobody has;
a list was considered and declined on review: one value is the simplest thing to build and to explain (2026-10-08).

## A3 — What is drawn: an alias only when it names exactly that issue

An issue is drawn by its alias in the chosen namespace **if and only if that alias,
typed back into any id position, resolves to that issue**.
Otherwise it is drawn by its short hash, as today.
One rule covers every case where the two could disagree:

| case | drawn as | why |
| --- | --- | --- |
| linked issue, one copy | `PROJ-12` | the key resolves to it |
| local-only, not exported yet, or created by a ghost before the next sync | `3f9a1c2` | it has no alias |
| an issue excluded from the sync (`aliases: {jira: ""}`) | `3f9a1c2` | the empty alias is "never sync", not a name |
| a consolidated loser (`jira-consolidated-into` on its create op) | `3f9a1c2` | the key names the survivor (A6) |
| two unconsolidated copies of one key, before the sync has settled them | `3f9a1c2` each | the key is ambiguous until then |
| an alias that is also a hash prefix of another issue (an all-hex alias in some future namespace) | `3f9a1c2` | prefix resolution is tried first, so the alias would name the other issue |

The rule is what makes copy (A7) safe:
anything drawn as an id is something a person can paste back.
It costs one alias index per store load, a count of issues per alias,
which the excerpts already carry (`cache.IssueExcerpt.Aliases`).

**A fallen-back hash is drawn dim** (2026-10-08, on review), wherever there is styling to draw it with:
when the setting names a namespace and an issue is drawn by its hash,
the terminal renderer draws that id faint,
so the issues that have not reached Jira stand out at no width cost —
cheap, and it says exactly what is true: this issue has no name in the namespace asked for.
The shapes differ as well (lowercase hex with no dash, against a project key, a dash and a number),
which is all plain text has: `--format text` and the report's markdown carry no dimming.
The renderer needs no second rule to know which is which:
with a namespace set, a `human_id` that is a prefix of the row's `id` is a fallback,
since A3 never draws an alias that a prefix could be confused with.
With the setting off nothing is dim, because nothing fell back.

**A key is never cut.**
A hash prefix may be shortened because every prefix still names the issue;
`PROJ-12` cut from `PROJ-123` names a different issue.
So the id column stops being the constant seven cells (`tui/list_view.go`, `idWidth`)
and becomes the widest id drawn in that table, at least seven,
measured over every row of the layer, hidden or drawn,
the rule nested tables already use for their columns (`terminal-renderer.md`, Nesting),
so folding and filtering move nothing.
Ids are left-aligned, the hashes padded to the keys. Today:

```
 id       type   status       title
 3f9a1c2  story  in-progress  Board in the terminal
 b07e4d1  task   to-do        Column widths follow the widest key
 a51c0e9  task   to-do        Created this morning, not exported yet
```

With `git-work.display.id = jira`:

```
 id        type   status       title
 PROJ-9    story  in-progress  Board in the terminal
 PROJ-112  task   to-do        Column widths follow the widest key
 a51c0e9   task   to-do        Created this morning, not exported yet
```

The gantt's label column and a board card's id line take the same measure;
a card already has 32 cells, which any key fits.
When a sync links an issue the ref watcher redraws it and the hash becomes the key,
and the column may widen by a cell or two; that is the redraw doing its job.

## A4 — The carrier is `human_id`; `id` never changes

The JSON every reader prints keeps `id`, the full hash, unconditionally.
It is the identity, what a program compares and stores,
what `(rank, id)` sorts on,
what a relation value holds,
and what a writer prints when it creates an issue —
the contract an agent depends on,
and it must not depend on a person's preference on the clone the agent happens to run in.
The alias is already in the same document, `metadata["alias:jira"]`,
for a program that wants it.

`human_id` follows the setting.
On an issue excerpt, in `issue get`'s document and in a snapshot replayed `--at` a time,
it is the drawn id of A3: `PROJ-12` where that rule allows, the seven-character prefix otherwise.
This is a change to the JSON, made deliberately:

- `human_id` has only ever been a rendering —
  a truncation, which is why a growing store could make it ambiguous —
  and every reader that draws an issue already draws it from there:
  `--format text` (`humanIdOf`, `commands/issue/issue.go`),
  the list, the board and the gantt (`item["human_id"]` in `tui/`).
  Moving the choice into that one field moves every one of them with no second rule.
- It keeps the property it always had: it is accepted back as an id.
- A query that makes its own rows (`query-rows.md`) carries `.human_id` across
  and its rows are drawn the same way, with nothing for its author to learn.
- The Starlark mirror is free, below (A9).

The alternative, a new key such as `display_id` beside an unchanged `human_id`,
varies per clone exactly as much,
and adds a second name every reader must switch to.
A program that must not vary keys on `id`, which this document makes the rule for agents
(`host/quickstart.md` says so in the change that builds this).

Only the issue's own `human_id` follows.
A comment's, an operation's (`issue log`'s per-entry `human_id`)
and a config entity's stay hashes:
Jira's comment ids are not names anybody reads,
and the schema and flows are named by key already.
The alias is the present one even under `--at`:
an id names the same issue at every time,
and drawing the hash for the hours before an export would make one issue two names in one report.

## A5 — Everything drawn follows; what is a value does not

In scope, every place a person reads an issue named:

| surface | today | with `jira` |
| --- | --- | --- |
| `git work issue --format text` | `3f9a1c2  in-progress  Board…` | `PROJ-9  in-progress  Board…` |
| `issue get --format text` header | `3f9a1c2 [in-progress] Board…` | `PROJ-9 (3f9a1c2) [in-progress] Board…` |
| `issue log --format text`, issue column | `3f9a1c2  a71e0c4  SetFields…` | `PROJ-9  a71e0c4  SetFields…` (the operation's own id stays) |
| a list's id column, a board card, a gantt label | short hash | A3 |
| a relation cell, a matrix's relation axis, show's children | `3f9a1c2 Board…` | `PROJ-9 Board…` |
| a relation's picker | short hash and title | A3 and title |
| status line | `copied 3f9a1c2`, `created …` | `copied PROJ-9`; `created` names a new issue, which has no alias yet |
| show's call line, its lead | the short hash | A3 |
| the `report` flow's markdown | `short(id)` in the flow | the excerpt's `human_id` (A9) |

`issue get --format text` is the one place both are drawn (kept on review, 2026-10-08),
because it is the one page about a single issue
and the place a person goes to map a key to the hash an agent printed.

The `/` filter matches the drawn text, as it does for every other cell,
so `/PROJ-1` narrows to the keys and a hash prefix to the unexported.

Out of reach of the setting, because each is a value or a diagnostic, not a name for a reader:

- **jq programs.** `.id`, a relation value and `.fields.parent` are hashes,
  so a query compares hashes; the matrix's `M-c`, which copies a `view list` command whose query
  selects the cell's issues by id, keeps the full hashes there.
- **A call's arguments.** `C-c` on the call line copies the command as it was called.
  When show was opened from a list, that is the full hash; the lead drawn beside it is the key.
- **Writers.** `issue new` and `view new` print the created id, the hash:
  a writer's output is an id a program reads, and a new issue has no alias anyway.
- **Errors, warnings and the Jira sync's own lines.**
  A diagnostic is about the store,
  and the ones that name issues are mostly about two copies sharing a key or an issue with no key yet,
  where the key is exactly the thing that does not distinguish them.
  The sync's JSON lines already carry the key beside the id.

## A6 — A consolidated duplicate is drawn by its hash, and the key names the survivor

After JS25 consolidates two local copies of one Jira issue,
both create operations still carry `alias:jira = PROJ-12`, because create metadata is immutable;
the loser is archived and stamped `jira-consolidated-into`.
Today `ResolveAlias` matches both and fails with a multiple match,
so the key a person reads in Jira stops resolving on the very clone that settled it.
That is a defect of its own and this design depends on its fix:
**an alias resolves to the one issue carrying it that is not stamped `jira-consolidated-into`**,
and fails as ambiguous only when more than one unstamped issue carries it —
the window before a sync consolidates them.
The fix belongs in `cache.RepoCacheIssue.ResolveAlias`, above the pristine line.

With it, A3 needs no case of its own:
the survivor is drawn `PROJ-12`, and the loser, which `PROJ-12` no longer resolves to,
is drawn by its hash wherever `include_archive` brings it back —
which is the honest name, since it is the copy the key does not mean.
Until a sync settles two unstamped copies, both draw as hashes,
and the moment it does, the survivor becomes the key on every open view.

## A7 — Copy copies what is drawn

`M-c`/`Y` copies the issue's id, today the full hash (`copyId`, `tui/list.go`).
Both keys, and every other copy of an issue's id, copy what is shown (2026-10-08, on review):
with an alias drawn, the alias,
because the point of drawing `PROJ-12` is that `PROJ-12` is the name a person carries elsewhere —
into Jira, a chat, a commit message —
and A3 guarantees it is accepted back at every id position.
Where the hash is drawn the full hash is copied, as today.
`C-c` on the id column copies the drawn cell, which is now the same thing.

Every id position accepts it, field values included:
since `9527e14c` (2086c12) a relation value written as an id prefix or an alias
is stored as the full id it resolves to, on every write, the Jira pull's included,
so a pasted key never reaches the store as a key.

## A8 — Order stays on the hash

`(rank, id)` orders every view and the default program orders by edit time;
neither reads `human_id`.
An order that changed with a person's setting would put rows in a different place on two clones,
and a rank written between two neighbours is computed from the neighbours the writer saw,
so a drag on one clone would land somewhere else on another.
Rendering only: the id the tie-break reads stays the hash.
Sorting by key is a jq program's business — `sort_by(.human_id)` reads what is drawn.

## A9 — Starlark: no new name, the mirror comes with `human_id`

The one-to-one rule (`cli-convention.md`) is held without adding anything:
`work.issue.list()`, `work.issue.get()` and the views go through `host`,
so a flow sees the `human_id` its clone's setting gives,
exactly as the command beside it does.
There is no `work.display` and no argument,
for A1's reason: a preference is not part of a call.

The `report` flow changes to draw an issue by the snapshot's `human_id`
instead of cutting `id[:7]` itself,
and a relation value by the `human_id` of the issue it names in the `after` snapshot,
falling back to the cut hash for an issue it does not have.
Its markdown is the most likely thing to be pasted where people talk in keys.

## Out of scope

- **Renaming.** `alias:jira` is the key at link time and is never refreshed (`jira-sync.md` I3);
  after a Jira move the old key is drawn, and it still resolves here and redirects in Jira.
  Following moves is the sync's question, not this one.
- **Comments, operations, identities and config entities**, whose ids stay hashes (A4).
- **The frozen `git work bug` surfaces**, `termui` and `webui`, which are being deleted.
- **The GUI**, which does not exist yet and will read `human_id` like the terminal does.
- **Jira keys as entity ids.** The entity id stays the hash; that was settled with aliases (`483dbe2`).

## Settled on review (2026-10-08, Luis)

1. **`human_id` carries the setting** (A4): "`human_id` by definition is this".
2. **A fallen-back hash is dim** where styling exists (A3); plain text has none.
3. **One namespace**, not a list (A2): the simplest implementation.
4. **`issue get --format text` shows both**, `PROJ-9 (3f9a1c2)` (A5).
5. **Copy copies what is shown**, on `M-c`, `Y` and every other copy of an id (A7).
