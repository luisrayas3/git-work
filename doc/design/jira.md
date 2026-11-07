# The Jira sync

How `git work jira sync` keeps one clone's store
and one Jira Cloud project converged in both directions, one issue at a time:
the mapping between the two schemas,
the three-way merge per issue,
the order of writes that makes an interrupted run harmless,
and the report.
Jira is canonical: a field edited on both sides takes Jira's value,
and says so on the issue.
The write path, the lock and the operations the sync writes through
are the [store](store.md)'s;
aliases as schema attributes, the shape and policy checks,
the `link` mark and `retype_targets` are the [schema](schema.md)'s;
what a local type change or a comment delete does to the issue
is the [issue](issue.md)'s;
how link fields are drawn is the [views](views.md)'.
The Jira behaviour everything here relies on is in
[the API reference](bridges/jira-api.md) and [its vetting](bridges/jira-api-vetting.md);
[the sandbox](bridges/jira-sandbox.md) says what a test site needs.

## What the design rests on

### The facts it starts from

- **The op log orders by lamport time and never forgets.**
  An operation committed now is later than every operation in the store,
  whatever Jira's timestamp says,
  so a three-way decision is made under the write lock,
  against the issue as it is at that instant,
  and whatever loses is still in `git work issue log`.
- **One commit is atomic, and every operation carries hashed metadata.**
  `dag.Entity.Commit` writes an entity's staged operations behind one ref update;
  metadata is fixed before an operation's id is computed;
  `NoOp` exists to store metadata in an entity's history;
  `SetMetadata` adds keys to an earlier operation, first writer wins.
- **Jira has no compare-and-swap, no tombstones and a lagging index.**
  `PUT /issue` is last-writer-wins;
  deleted issues and comments vanish;
  `/search/jql` lags the database by seconds to minutes,
  while `GET /issue/{id}` reads the database.
  The changelog is capped, unordered, and omits comments.
- **Jira ids are stable; names, keys and custom field ids are not.**
  Issue type, status, priority, option and link type ids never change;
  names are editable;
  `customfield_NNNNN` differs per site;
  a key changes on a move.

### Fixpoint

For every mapped key `k` of a linked issue,
after a successful per-issue commit,
either `local[k] == base[k] == jira[k]`,
or `k` is reported pending and `base[k]` is unchanged.
`merge(b, l, r)` with `l == b == r` emits nothing.
Everything that could loop violates this,
and the tests check it as a property.

### The base is what Jira was seen to hold

`base[k]` is written only in the commit that makes `local[k]` equal to it,
and its value is one Jira was seen to hold, in local terms
(the description as its digest).
**Jira's 2xx is its acceptance, and a `GET` its observation.**
What a run writes goes into the marker's `Sent`
(a comment's key is `comment:<Jira id>`, its value the digest),
with `Wrote`, Jira's clock just before the write;
the base does not move.
Before every merge, `confirm` settles `Sent` against the `GET` in hand,
by Jira's clock, never by counting runs:

| the `GET` shows | the key |
| --- | --- |
| the value written | confirmed: its base is that value |
| something else, `updated` older than `Wrote`, within `Settle` of it | possibly stale: pending with `Retry`, neither imported, exported nor re-based; `Sent` kept |
| something else, read right after this run's own write, `updated ≥ Wrote` | Jira's normal form of ours: the base is the value written, so it imports as an ordinary change |
| something else, any other case | Jira holds its own value: the base is `unknownBase`, which equals neither side, so Jira's value imports with a conflict note |

A set keeps its base in every row, [the set merge](#sets-merge-item-wise) deciding;
`Sent` only holds it back from a stale `GET`.
A comment written and then absent from a `GET` that is not stale
was deleted in Jira.

A `GET` older than our write cannot have seen it, and one at or past it has;
counting runs instead reverted local edits with false notes
when a slow `GET` came twice,
made a local clear a silent take once the base was dropped,
and posted a comment again every run.
The read that follows this run's own write and shows it landed
imports Jira's normal form silently,
because the only other explanation is a Jira edit in the milliseconds between,
and a note per trimmed summary would be a note per normalisation.
The window is `Settle`, the bound already trusted for an unanswered create,
because Jira normalising our value back to its old one leaves `updated` alone
and cannot be told from a stale read until then.
`Wrote` has second resolution,
so a Jira edit in the same second before our write passes as fresh:
it costs a note and one flap, never a silent loss.

A crash can only leave a base *older* than both sides,
which the first row of [the scalar merge](#scalars) absorbs.
The one exception is [Gone](#deletes-and-moves-are-gone).

### Links are facts on operations

An issue's Jira id is metadata on its create operation,
a comment's Jira id metadata on its add-comment operation,
each set once, at creation or by one `SetMetadata`,
and never inferred from a marker.
Merging two clones' histories can never un-pair or re-pair anything;
two markers can disagree only on values.

### The database decides; the index only finds

Merges, deletes and link repairs use `GET`.
A link repair found through the search's copy of the `git-work` property
links only when `GET`'s `properties=git-work` names the same entity.
Search answers only "which ids might have changed",
and never names an id Jira may no longer show.
A run starts with `GET /myself` and refuses an answer with no account,
because a request with no `Authorization` header is 200-with-nothing.

### Conversion is idempotent

`fromIssue(jira(toWrites(fromIssue(x)))) == fromIssue(x)`,
where `jira()` is the fake's model of Jira's normalisation
(sorted labels, trimmed summary, float rounding, ADF re-serialised with `localId`s).
With [the base rule](#the-base-is-what-jira-was-seen-to-hold)
a converter that is not idempotent is not a ping-pong but import churn,
one operation per run per issue Jira touched.
It is a property test per kind and for ADF.

## The merge

### Three documents per issue

Per issue the sync compares three documents in local terms —
the **base** (what both sides held after the last sync),
the **local** snapshot,
and the **remote** issue from `GET /issue/{id}` —
and decides per key.
It does not replay Jira's changelog.

The changelog omits comments, their edits and deletes,
and is capped at 100 unordered entries with `fieldId` often absent,
so a replay needs a state diff anyway, and the state diff alone covers everything.
An echo is not detected but equal to the base.
Comparing twice finds nothing the second time,
which every crash argument rests on.
And it is one pure function over three documents,
tested without Jira or a repository.

Search only finds candidates (`fields=updated`, `properties=git-work`);
nothing is merged against a search copy,
which removes every index-lag rule.
The costs are accepted:
the intermediate Jira values between two runs arrive as one operation
(Jira's history stays the authority for what happened in Jira),
and field imports are authored by the runner at Jira's `updated`
rather than by the Jira user who made the change.
Issue and comment authors stay exact, because they are free.

### The base is a marker on the issue

The base lives **in the issue**,
on a `NoOp` operation whose metadata `jira-sync` holds the `Base` as JSON,
written in the same commit as the imports it describes
(an imported issue's first marker is its create operation).
It travels with `push` and `pull`, so every clone starts from the same base;
it is atomic with what it describes;
two clones' markers are two operations and neither is lost;
`git work issue log` shows it.

```go
type Base struct {
	V        int                    // 1
	Id, Key  string                 // Jira id; key as last seen
	Updated  time.Time              // Jira's updated of the state described
	Fields   map[string]issue.Value // each mapped key's form; the body as its digest
	Comments map[string]string      // Jira comment id -> digest; "" once deleted
	Retry    []string               // keys skipped with Retry
	Gone     string                 // "deleted" | "moved" | ""
	Sent     map[string]issue.Value // written, not yet seen
	Wrote    time.Time              // Jira's clock before the last of those writes
	Fresh    bool                   // a create not merged yet
}
```

Values are in local terms (schema keys, value ids, entity ids),
so the base compares with the snapshot as it is.
Fields are whole, because a set merge needs the base's items;
texts are digests, because they only need equality.
Every marker is the whole base, so reading it is reading one operation.
**The current base is the marker with the greatest `Updated`**,
ties to the later in compiled order:
the base describing the newest Jira state best predicts Jira.
An undecodable marker is skipped and reported.
A marker is written only when the new base differs from the current one
in anything but `Updated`, so a quiet run commits nothing:
Jira bumps `updated` for what the mapping does not cover
and for both ends of a link, and a marker per bump would be a commit per bump.
The cost is a `GET` while the overlap re-returns such an issue.

Not a local file: a new clone would have no base,
and every difference would look like a double edit.
Not Jira properties: not atomic with the local commit, and invisible in the tracker.
Not a config entity: per-issue state does not belong in the schema's log.
Not a new operation code: `NoOp` is decoded by every binary and exists for this.

### Every key the sync writes

| where | key | value |
| --- | --- | --- |
| create op | `jira-id` | Jira issue id: the link |
| create op | `alias:jira` | Jira key at link time; resolves anywhere an id does |
| create op of an imported issue; `NoOp` | `jira-sync` | `Base` JSON |
| `NoOp` before a `POST /issue` | `jira-create` | the attempt's time on Jira's clock, RFC 3339 |
| add-comment op | `jira-comment-id` | Jira comment id |
| add-comment op of a note | `jira-note` | `conflict`, `deleted` or `consolidated` |
| consolidation note | `jira-consolidated-from` | the archived copy's id |
| create op of an archived copy | `jira-consolidated-into` | the surviving copy's id |
| identity, immutable | `jira-account-id` | Jira accountId |
| Jira issue property | `git-work` | `{"id":"<entity id>"}` |
| Jira comment property | `git-work` | `{"op":"<add-comment op id>"}` |

`jira-id`, `alias:jira` and `jira-comment-id` are set at creation on an import,
and by `SetMetadata` in the commit after the write on an export, never elsewhere.
`alias:jira` is never refreshed:
after a move the old key still resolves, locally and in Jira.

### The state file is disposable

Local run state —
the cursor, the failed hits, the creates Jira refused,
the orphans skipped, the edit lamport of each issue at its last converged sync —
is `.git/git-work/jira/state.json`,
written through a temporary file and a rename.
It records the site and project, and a mismatch resets it;
a damaged one is read as empty.
Losing it costs a slower run, never a wrong one:
the create attempts are in the store,
and a missing cursor starts from the newest marker's `Updated` less the overlap,
not a full search.

### Scalars

For every mapped key of the issue's type,
with `b`, `l`, `r` compared as canonical JSON, the first row that applies:

| case | local op | Jira write | base becomes |
| --- | --- | --- | --- |
| `l == r` | — | — | `r` |
| `l == b`, `r != b` | `SetField r` | — | `r` |
| `r == b`, `l != b` | — | write `l` | `l`, once a `GET` shows it |
| all three differ | `SetField r`, conflict | — | `r` |
| `b` absent, `l` null | `SetField r` | — | `r` |
| `b` absent, `l` set, `l != r` | `SetField r`, conflict | — | `r` |

The first row comes first on purpose:
two sides that agree are converged whatever the base says,
which makes a crash after a Jira write, a second clone's concurrent import
and a stale base all harmless.
A missing base (a newly mapped field, a link request) is a double edit, so Jira wins.
A key the remote side skipped is left alone: no op, no write, base unchanged.
Keys the mapping does not cover on this type are never looked at.

### Sets merge item-wise

`multi-enum`, `multi-relation` and freeform labels merge as sets:

```
merged = (b − removed_locally − removed_remotely) ∪ added_locally ∪ added_remotely
```

An added item was not in `b` and a removed one was, so no item is both,
and a label added in Jira and another added locally both survive.
Local ops are `AddValue`/`RemoveValue` for `merged` against `l`;
the Jira write is `update.labels [{add},{remove}]`, `update.<custom>`,
or `POST`/`DELETE /issueLink`, for `merged` against `r`.
A missing base is the empty set, so a first sync is a union.
A set cannot conflict: there is no double edit of one item.

### Texts merge by digest

The description (comment #0, the key `body`) follows the scalar table on digests;
a text is never merged within itself.
`digest(t)` is `"v1:" + hex(sha256("v1\n" + jiraapi.NormalizeText(t)))`,
where `NormalizeText` is exactly the normal form `TextToADF` preserves,
so two texts Jira holds alike never differ;
the version is in the input,
so a later normalisation change re-imports each text once instead of mis-comparing.

`jiraapi.ADFToText` renders ADF as Markdown over a fixed subset —
paragraphs, headings, bullet and ordered lists, code blocks and code,
blockquote, rule, hard break, strong, em, strike, links —
and reports whether the rendering is **lossless**.
Anything else (mentions, tables, media, panels, emoji, status, dates)
is rendered as readable text and marks it lossy.
**A local edit over a lossy Jira text is not exported**:
it is pending ("the Jira text holds content git-work cannot write back")
and keeps its edit locally,
because overwriting would destroy what Jira holds.
Importing a lossy text is fine: the local copy is a rendering.
`TextToADF` writes the same subset;
a string is never sent where ADF is expected.

### Comments pair by id

Local comments are those after the description;
a comment's add-comment op gives `jira-comment-id` and `jira-note`.
Remote comments are `GET /issue/{id}/comment?expand=properties`, every page.
Per comment, with digests against `Base.Comments`:

| state | result |
| --- | --- |
| paired, both present | the scalar table on digests: a local edit is `PUT …/comment/{id}`; a Jira edit is `EditComment` by the Jira editor at `updated`; both is Jira's, with a conflict |
| in Jira only, its property names an unpaired local op | pair: `SetMetadata jira-comment-id` on that op, base the Jira digest, no text op |
| in Jira only, otherwise | import: `AddComment` by the Jira author at `created`, with `jira-comment-id` |
| local only, not a note | export: `POST …/comment` with ADF and the property `git-work={"op":…}`, paired from the `201` |
| paired, gone from Jira, base not `""` | [a tombstone](#a-comment-deleted-in-jira-becomes-a-tombstone) |
| paired, base `""`, edited locally | never exported again; pending ("deleted in Jira") |
| a note (`jira-note`) | never exported |
| deleted locally | [deleted in Jira](#a-comment-deleted-locally-is-deleted-in-jira) |

The property is the idempotence key:
a crash between `POST` and the commit is repaired by the pairing row next run,
never by a second `POST`.
The comments a run posts pair by the ids `POST` returned,
whether or not the re-read lists them,
so only that crash needs the property to come back.
An exported comment is authored in Jira by the token's account;
its property records the local op.
Pending entries of comments are keyed `comment:<Jira id>`,
or `comment:<op>` before one exists.

Not a digest-matching fallback:
it would pair a comment the runner typed in Jira
with an identical unexported local one, and lose the local one.

### A comment deleted locally is deleted in Jira

A local comment is deleted when its add-comment op is the target of a `DeleteComment`
(what that does to the issue is [issue](issue.md)'s):

| state | result |
| --- | --- |
| never paired | never exported |
| paired, present in Jira, base not `""` | `DELETE …/comment/{id}`; base `""` |
| paired, gone from Jira | base `""`, no request and no tombstone |
| paired, base `""` | nothing to do |

Deleting in Jira is destructive there, and that is the choice:
leaving Jira's copy, or redacting it to a placeholder,
leaves the comment visible in the canonical system,
which is what a delete is for.

**A Jira edit since the base is imported first**,
as an `EditComment` by the Jira editor, committed once the delete lands,
so the text Jira held is not lost to git.
It changes nothing on the snapshot, since the comment is deleted there,
and no conflict is reported: the writer asked for the comment gone.
**A lossy Jira text does not block the delete**,
which removes the comment whatever it holds;
the import keeps the rendering in git.
**The request is idempotent**:
a `DELETE` answering 404 is the "gone from Jira" row,
so a crash between the `DELETE` and the commit is settled by the next run.
A deleted comment is based at once, since no `GET` can show it back.
Any other failure, a 403 for a token without delete permission among them,
is a failed request like every other.
The report counts it under `comments.deleted`.

### A comment deleted in Jira becomes a tombstone

A paired comment gone from Jira is edited, by the runner,
to a one-line tombstone, "Deleted in Jira on 2026-09-28.",
and its base becomes `""`;
a local edit since the base adds a conflict.
It is not imported as a `DeleteComment`:
the tombstone works,
and changing it means deciding what becomes of the comments already tombstoned,
for no gain.
An older build that has no `DeleteComment` and meets a comment
a newer build deleted in Jira writes a tombstone edit,
which a newer build applies to a comment it no longer has, changing nothing.

### A Jira type change is imported first

The merge takes `type` first,
then maps the remaining keys with the new type's mapping;
base keys the two types share carry over, because value ids are slugs of names.
The commit checks each operation against the type the operation list sets,
not the snapshot's old one.
A pull runs the shape check only,
so a type changed by a Move in Jira is imported
whatever the local `retype_targets` say:
Jira is the authority on what Jira holds.
A type changed on both sides to different values is the scalar table's:
Jira's, and a conflict.

### A local type change is exported alone and first

The sync settles `type` before any other key.
When the merge would give it (changed locally, not in Jira),
the sync maps the new type to its issue type id
and sends `PUT /issue/{id}` with `fields.issuetype` and nothing else,
**before** the edit carrying the other fields,
because Jira validates every other field against the issue's type as it stands.
A type change then ends one of three ways:
refused when written locally, landed in Jira, or taken back with a conflict.

- **Landed**: the type goes into `Sent` like any write,
  and is the run's first write, so `Wrote` is the clock before it.
  Every other key is merged under the new type's mapping,
  as an [import of a type change](#a-jira-type-change-is-imported-first) is:
  a field the new type does not map in Jira is no key of that merge,
  so nothing is pending for it,
  and its local value stays on the issue, unsynced, like any local-only field's.
- **Refused**, by Jira or for any other issue failure: the refusal is final.
  The local type becomes the merge's base,
  so the merge takes Jira's type back as it takes any key only Jira changed,
  and the sync reports a conflict on `type` with the reason
  (a `reason` on the report's conflict, and in the [conflict note](#conflict-notes)).
  The local attempt stays in the issue's history;
  the other keys merge under Jira's type, and the next run is quiet.
- **A type mapped to no Jira issue type** is refused the same way,
  with no request ("type X is not mapped to a Jira issue type").
  The schema import makes it unreachable by a local write
  ([schema](schema.md#a-type-says-what-it-may-become));
  only a store written around that check reaches it.
- **A run failure** (network, auth) is not a refusal:
  the run stops, as for any write, and the next run retries.

Not left pending:
a pending type change would be a discrepancy reported on every run
that nobody is likely to resolve.
`--dry-run` reports a change to a mapped type as exported,
with the rest under the new type,
and one to an unmapped type as its conflict;
it cannot predict Jira's own refusal.

Local writes are refused where Jira would refuse:
[Derive](#retype-targets-follow-jiras-levels-and-workflows) gives each mapped type
the `retype_targets` Jira's levels and workflows allow,
so a refusal is left for what only Jira knows,
such as a required field unset on this issue.
Not `editmeta` to predict it:
a request per type change to repeat a check Jira makes anyway on the write,
whose refusal the sync already settles.
Not Jira's Move API:
a change across levels or workflows is made in Jira and imported.

## The mapping

### The mapping is aliases on the schema

Every type, field and enum value that corresponds to something in Jira
carries that thing's Jira id as an **alias**,
the same word an issue uses for `alias:jira = PROJ-12`,
stored as an attribute of its config entity:

| entity | attribute | value | example |
| --- | --- | --- | --- |
| type | `alias_jira` | issue type id | `task` → `"10001"` |
| field | `alias_jira` | field reference | `task/estimate` → `"customfield_10016"` |
| field | `alias_jira/<value-id>` | status, priority or option id | `task/status`: `alias_jira/in-progress` → `"3"` |

A field reference is a Jira field id
(`status`, `priority`, `assignee`, `labels`, `duedate`, `parent`, `customfield_…`)
or `link:<link type id>`, the outward side of a link type.
In a schema document it is an `aliases` map at all three levels
(`aliases: {jira: "10001"}`), and `schema export` prints it.

Not git config: values are stored by id,
so the mapping is what a stored value *means*,
and two clones mapping one Jira status to two value ids
would write contradicting data into one store.
Not a separate mapping entity: it would name a `<type>/<field>` it does not own,
dangle when that is archived, and need its own validator.
Attributes replicate, merge per attribute
(two clones mapping two new statuses keep both),
die with their entity, and are reviewed in the same file as the schema.

**An alias is written when a document states it and never removed by reconcile**,
so an alias-free schema document can be imported at any time
without unmapping anything.
**The empty string is a stated alias meaning "never sync this"**;
nothing else unmaps.
The whole `alias_` family is reserved,
and alias values are opaque to `schema`:
another system adds `alias_<system>` with no change there.

### Binding and credentials

Which site and project a clone talks to is git config,
read through `gitcli` so `[include]` works:
`git-work.jira.url`
(`https://<site>.atlassian.net`, or `https://api.atlassian.com/ex/jira/<cloudId>` for scoped tokens;
anything else is refused as Data Center, which is REST v2 and users by name),
`git-work.jira.project` (the key),
and `git-work.jira.email`.
Without all three every `jira` verb fails naming them.

The token is `JIRA_API_TOKEN` when set (cron),
else `git credential fill` with `protocol`, `host` and `username = email`,
run with `GIT_TERMINAL_PROMPT=0`;
an empty answer is an error naming `git credential approve`.
A credential helper is git's environment, so it is `gitcli`'s;
`JIRA_API_TOKEN` is not git's, so `host` reads it.
There is no `configure` command, and a token never reaches argv.

A clone bound to the wrong project is caught by the aliases
([the binding check](#derive-then-the-schema-import)),
which is why the binding need not replicate.

### Discovery

`Discover` builds one normalised `Project` from seven kinds of read:

| # | call | gives |
| --- | --- | --- |
| 1 | `GET /myself` | the credential works; `accountId`; `timeZone` for JQL |
| 2 | `GET /project/{key}` | project id; issue types with `hierarchyLevel` |
| 3 | `GET /project/{key}/statuses` | statuses per issue type, workflow order, `statusCategory.key` |
| 4 | `GET /field` | every visible field with `schema{type,items,system,custom}` and `scope` |
| 5 | `GET /issue/createmeta/{key}/issuetypes/{id}`, per type, paged | the create screen: fields, `required`, `allowedValues` |
| 6 | `GET /priority/search?projectId=`, paged | the scheme's priorities, in Jira's order |
| 7 | `GET /issueLinkType` | link types `id, name, inward, outward` |

Any failure fails the run naming the permission,
and so does an empty create screen, which is how Jira answers a caller without Create issues;
the one exception is 404 on step 7, which means linking is disabled: no link fields.
The sync must create issues, so a forbidden create screen is fatal,
not a degraded mode.
`serverInfo` (the cursor is Jira's own `updated`),
resolutions (a transition fills one from its screen)
and boards are not read.

Everything is sorted at read, because Jira promises no order:
issue types by `(level desc, id)`, fields by id,
statuses in step 3's order, priorities in Jira's.
Custom fields are kept only when `scope` is absent or names this project,
which keeps two team-managed "Story point estimate" fields apart.

### Derive, then the schema import

There is no Jira-specific schema writer:

```
Discover(client, key)              -> *Project            (network)
Derive(schema.Export(current), p)  -> *schema.Document    (pure)
host.SchemaImport(repo, doc, prune=false, dryRun)         (the ordinary path)
Compile(repo.LoadSchema(), p)      -> *Mapping            (pure)
```

`Derive` returns **current ⊕ Jira**:
the exported current schema with Jira's types, fields and values adopted or added,
Jira's names applied to what is aliased,
and nothing removed, archived or re-kinded.
Reconcile owns every value and target type of a field it is given,
so a document that restates everything current
is what keeps local-only things alive.
Its output is a fixpoint, so a second run against an unchanged Jira writes nothing,
and review is `schema import --dry-run`, because it is the same call.
`Derive` never removes a `target_types` entry Jira's hierarchy forbids:
a forbidden local parent is a 400 on export, so pending.
What Derive rewrites rather than adds is Jira's to decide:
a type's name, a field's name, and a mapped type's `retype_targets`.
Schema writes are the runner's, at now, like any import.

**The binding check is in `Derive`, not only in `Compile`**,
because `Compile` runs after the import,
and a clone bound to the wrong project would already have adopted
that project's types into the tracker.
`Derive` refuses when type aliases exist and none names an issue type of the project:
"the schema maps issue types 10001, 10002 that PROJX does not have;
is this the project the tracker was mapped against?".
`Compile` checks it again,
and [refuses duplicate aliases](#duplicate-aliases-are-refused).

### The first mapping is reviewed

`jira sync` refuses while no type has a non-empty `alias_jira`,
naming the review recipe:

```sh
git work jira schema > jira.yaml          # derived document; warnings on stderr, -v for info
$EDITOR jira.yaml                         # rename proposed keys, exclude with aliases: {jira: ""},
                                          # mark "Won't Do" canceled, map by hand
git work schema import jira.yaml --dry-run
git work schema import jira.yaml
```

After that the sync derives and imports at the start of every run.
Everything automatic is additive, a rename, or a Jira-enforced fact;
nothing needs a person's judgement, and `jira sync --dry-run` previews it.

### Matching and keys

**Matching**, per Jira entity, first rule that applies:
an entity whose alias equals the Jira id is matched;
an entity aliased `""` is excluded;
an unaliased entity whose `norm(key)` or `norm(name)` equals `norm(jira name)`,
**with the same kind** for a field, is adopted and its alias stated;
otherwise a new entity.
`norm` keeps lower-case letters and digits only,
so "Sub-task" and "Subtask" adopt `subtask`.
When two Jira entities normalise to one local one,
the lower Jira id wins and the other is new.

**`slug(name)`** makes a new key:
NFKD with combining marks dropped; lower case; apostrophes dropped;
every run outside `[a-z0-9]` becomes `-`; `-` trimmed;
cut to 48 bytes at a `-` boundary (room for `alias_jira/` in 64);
a type or field key starting with a digit gets `t-` or `f-`;
an empty result is `jira-<id>`.
Collisions within one namespace take `-2`, `-3`, … in a deterministic order
(the fixed table before custom fields, then Jira id);
a built-in field key always collides.
"Won't Do" → `wont-do`; "完了" → `jira-10002`.

**A key is made once.**
Once aliased an entity is found by its alias;
a Jira rename changes `name` and never the key,
because keys are what issues, flows and views store.
A person may rename a proposed key in the reviewed file before the first import;
the alias travels with it.

### What maps

The fixed table (its keys are the `jira` preset's, so an adopted preset stays itself):

| Jira | key | kind | on |
| --- | --- | --- | --- |
| `summary` | `title` | built in | all |
| `issuetype` | `type` | built in, via type aliases | all |
| `description` | comment #0 | text | all |
| `status` | `status` | `enum`, values per type from discovery | every type with statuses |
| `priority` | `priority` | `ordinal-enum`, Jira's order | the create screen has it |
| `assignee` | `assignee` | `identity` | the create screen has it |
| `labels` | `labels` | `multi-enum`, freeform | the create screen has it |
| `duedate` | `due` | `date` (`2006-01-02`) | the create screen has it |
| `parent` | `parent` | `relation` to the mapped types one level up, `inverse: children` | every type with a level above it |
| link type | slug of `outward` (`blocks`) | `multi-relation`, `inverse` the slug of `inward`, none when symmetric | all mapped types |
| story points | `estimate` | `number` | the create screen has it |

Story points are the `jsw-story-points` field,
else a number field whose `untranslatedName` (else `name`)
is exactly "Story Points" or "Story point estimate".
Custom fields map only from a create screen, by `schema.type` (`items`):
`number` → `number`, `date` → `date`, `datetime` → `date` (RFC 3339 UTC),
`string` with `custom` ending `:textfield` → `text`,
`option` → `enum`, `array(option)` → `multi-enum`, `user` → `identity`;
values from `allowedValues`, aliased by option id.
Anything else is unmapped and reported with its `schema.custom`.
Sprint, Rank, `resolution`, `components` and `fixVersions` are not mapped.

Fields a type has that Jira lacks (`area`, `rank`)
and types Jira lacks (`decision`) are local-only:
never exported, never cleared, reported as info.

### Status categories

Jira's category bounds the local one:
`new` and `undefined` allow `backlog` and `unstarted` (a new value is `unstarted`),
`indeterminate` allows `started`,
`done` allows `completed` and `canceled` (a new value is `completed`).
An aliased value inside its class keeps its category,
so a person marks "Won't Do" `canceled` once;
one outside is reset to the class default, with a warning.
The category is read from `statusCategory.key`, never the name.

### Canonical values

Values are compared as what the store would hold after writing them,
so "did it change" is byte equality of compacted JSON:
sets sorted by compacted bytes (the order `AddValue` keeps);
numbers in shortest form (`strconv.FormatFloat(v, 'f', -1, 64)`);
dates `2006-01-02`; datetimes RFC 3339 in UTC;
relations and identities as full entity ids; labels verbatim.
Many-to-one value aliasing is refused, so `toWrites(fromIssue(x)) == x`.

### Link types are link fields

Every field derived from a link type (`aliases.jira: link:<id>`),
and nothing else, carries `link: true`,
which the views read to draw an issue's links as one row
(the mark itself is [schema](schema.md)'s).
Derive sets it on every run,
so a store derived before the mark gets it as an update of each such field,
and a local `link: false` on a link-type field is set back.
The `jira` preset marks its link fields the same way.

### Retype targets follow Jira's levels and workflows

Jira's edit changes an issue type only within one hierarchy level
and one workflow,
so Derive sets each mapped type's `retype_targets`
to the other mapped types of its level with the same set of status ids
(discovery's `GET /project/{key}/statuses`),
in the document's order.
Required fields are not considered:
whether one is set depends on the issue, not the type,
and a refusal is [settled on the write](#a-local-type-change-is-exported-alone-and-first).
Jira is canonical for what a Jira type can become, as for its name,
so a list written by hand is rewritten on the next run, with a note.
A local-only type, unaliased or aliased `""`, keeps its own list
and is never listed in a mapped type's:
a linked issue retyped to one would silently stop syncing,
and the schema import refuses it
([schema](schema.md#a-type-says-what-it-may-become)).

The policy is per type, not per issue:
an epic created locally and never synced cannot become an initiative either.
That keeps the checker free of Jira state,
since whether an issue is linked lives in the sync's base, not the schema;
and a repository that syncs to Jira has its types derived from Jira,
so the rule is Jira's own.

## One run

### One issue at a time

1. **Candidate.**
   Skip when the search's `updated` equals `base.Updated`, `local == base`,
   the base is settled (no `Retry`, no `Sent`, not `Fresh`),
   the issue is not `Gone`, and the run is not `--full`.
2. **Read** `R = GET /issue/{id}?fields=<the mapping's>&properties=git-work`
   and its comments;
   ensure identities for the accounts they name, each its own commit.
   A 404 or a key outside the project is not a failure
   but [Gone](#deletes-and-moves-are-gone)'s business.
3. **Plan**, lock-free, the type
   [settled first](#a-local-type-change-is-exported-alone-and-first):
   a local type change is written alone, or refused and taken back,
   and then `plan₁ = merge(confirm(B, R), L, fromIssue(R), export=true)`
   under the type that results,
   turned into writes, one per local key.
4. **Write**, each independent, success recorded per key:
   one `PUT` with every edit;
   the transition; link adds and removes; comment creates, edits and deletes.
   A `PUT` refused with per-field `errors` is retried once without those fields,
   which become pending; a 400 naming no field sent makes every key of it pending.
   The transition is the one whose `to.id` is the target;
   a required `resolution` is filled with the first of its `allowedValues`
   (pending would block "done" on most company-managed workflows);
   any other required field without a default, or no such transition, is pending;
   a 409 re-reads the transitions and retries once.
5. **Re-read** `R′ = GET` if anything was written, else `R′ = R`.
   Each write that landed is in `B′.Sent`, with `B′.Wrote`
   the `Date` of Jira's response before the first write.
   A failed re-read commits step 6 against `R`,
   where everything written is still pending, and then fails the issue:
   nothing Jira answered is lost.
6. **Commit**, `IssueCache.UpdateShape`:
   under the lock, on the fresh snapshot `L′`,
   with the comments step 4 created paired from their `201`s,
   `plan₂ = merge(confirm(B′, R′), L′, fromIssue(R′), export=false)`;
   the ops are `plan₂`'s local changes
   (each pre-checked: [a pull is checked for shape](#a-pull-is-checked-for-shape-not-policy)),
   the conflict note, the `SetMetadata`s for new pairings,
   and a marker when the base changed.
   Keys `plan₂` still wants to export are reported pending, not written.

Every local decision is made under the lock on the entity as it is then,
so a user editing during steps 3–5 is seen by `plan₂` —
a double edit if Jira changed that key, pending otherwise.
The lock is never held across the network.
Settling `Sent` before each merge makes each confirmed success collapse to row 1,
each failure stay a local change,
each Jira normalisation arrive as an ordinary import,
and each stale read a pending key.

### What an interruption leaves

| stops after | state | next run |
| --- | --- | --- |
| 1–3 | nothing written | the same run |
| part of 4 | some Jira writes, nothing local | a written key is `l == r`: converged; an unwritten one is still local: written; comments pair by property, creates by their attempt and property |
| 5, the re-read fails | Jira's answers in hand | step 6 commits them against `R`: pairings from the `201`s, `Sent` and `Wrote`; the issue fails; the next run settles `Sent` |
| 5, a stale `R′` | Jira does not show the writes | the written keys are pending with `Sent` kept, comments paired; left alone until a `GET` reaches `Wrote` or `Settle` passes |
| the `POST /issue` answer lost | the issue may exist in Jira | the attempt, committed before the `POST`, is in doubt: found by property, linked; not found, pending until `Settle`, then created again |
| `POST` `201`, the `GET` after it fails | the id and key in hand | committed at once: `jira-id`, `alias:jira` and a `Fresh` marker; a 404 within `Settle` is pending, not gone |
| 4, refused (400) | Jira refused a key | pending every run until fixed on either side; a refused type change is [taken back](#a-local-type-change-is-exported-alone-and-first) instead |
| 4, no transition | status diverged | pending with the reason; a later Jira status change imports, with a conflict |
| 6, shape check | a value that does not fit its field's kind, or names nothing | that key `Retry`, the rest commits |
| 6, policy check | an enum value or a relation target the schema does not allow | written, reported `off_schema`; converged |
| 6, lock timeout | nothing local | as "part of 4" |

A local edit made after a crash, on a key whose `PUT` landed,
becomes a double edit that Jira wins, noted; accepted.

### A pull is checked for shape, not policy

Every change the sync writes locally — a field import, a new issue, a re-point —
is committed under the shape check only
(`IssueCache.UpdateShape`, `NewRawShape`):
the value fits its field's kind on a field the type has,
and an identity or issue it names exists.
The pre-check before the commit (`admit`) drops a change the shape check refuses
into `Retry`, the key keeping its old base,
so the commit's own check never refuses the batch.
A change only the policy check refuses —
an enum value, a relation's target type, a type change the schema does not allow —
is written, and reported under `off_schema` with the reason the local check gives;
it converges, and the next run is quiet.

Jira is the authority on what Jira holds,
and the derived schema only approximates it:
a real site declares Task a sub-task type and still gives Tasks an Epic parent,
which the derived `task.parent` does not allow,
and refusing those parents left the local copy silently disagreeing with Jira on every run.
Operations replay whatever was written, and readers tolerate off-schema values,
since every schema change creates some.
Local writes keep both checks,
so git-work still cannot create a Task with an Epic parent.
A key that is not a field of the issue's type, or a type the schema lacks,
is shape, not policy: without the field there is no kind to fit.

### No echo detection

Nothing recognises the sync's own writes; the base makes them harmless.
Our export, next run: step 6 recorded `b = l = r`,
and the base's `Updated` is `R′`'s, which the search returns, so step 1 skips it.
Our import, next run: `b = r = l`, so the key is not a local change.
Jira normalising what we wrote: `plan₂` imports the normal form in the same commit,
and the next run is quiet.
Overlapping windows re-return synced issues, which step 1 skips with no `GET`.

### Creates to Jira

An unarchived local issue of a mapped type with no `jira-id` is exported;
`sync ID...` of one archived or of a local-only type reports why it is not.
Jira has no idempotency key and its search lags,
so the **create attempt** is what stands between a lost answer and a duplicate:

1. A `NoOp` with `jira-create: <Jira's now>` is committed on the issue,
   under the write lock, **before** `POST /issue`.
   Jira's now is the `Date` of its last response,
   so the attempt compares with Jira's `created` with no client skew.
   It is in the store, not the state file,
   so losing the state never duplicates, and a clone that pulls the attempt sees it;
   the cost is one commit per `POST`.
2. `POST` carries project, `issuetype.id`, summary, description, the property
   `git-work={"id":…}`, and each field the create screen requires with no default
   that local holds (a set stated whole) — nothing else,
   so a per-field refusal cannot fail a create Jira would take.
   On `201` the issue is read as `R` and continues from step 3
   with a **create base**:
   each sent key is in `Sent` with `Wrote` the attempt's time;
   the marker is `Fresh`,
   so another key's base is Jira's value when local holds one
   (so it is written now, in the one `PUT` and the transition every create pays anyway)
   and local's when local is empty
   (so a Jira default such as priority imports rather than being cleared forever);
   comments are local-only and exported.
   Step 6 adds `jira-id` and `alias:jira` to the create op, which ends the doubt.
   When the `GET` fails, the `201` is committed alone,
   so the next run syncs a linked issue rather than doubting a create whose answer it had;
   a 404 while `Settle` has not passed is the index not showing it yet, never `Gone`.
   An ordinary merge with the created issue as base
   would export `null` over every Jira default; the create base is why it differs.
3. A definitive answer, a 4xx other than 408 and 429, says nothing was made:
   the attempt is recorded in the state's `Refused` with the issue's edit lamport,
   is not in doubt, and is not `POST`ed again until the issue changes
   (the line is `failed` every run, with Jira's reason).
   Any other error leaves the attempt in doubt.
4. The latest attempt of an unlinked issue, unless refused, is **in doubt**:
   one search per run, `project = P AND created >= "<oldest attempt in doubt − Overlap>"`,
   with the property, and the lower Jira id whose `git-work.id` names the issue
   is linked, after `GET` confirms the property, and resumes as a create.
   Not found, the issue is pending while the attempt is younger than `Settle`
   (15 minutes, longer than any index lag seen) and created again after.
   A lost `Refused` makes a refused attempt look in doubt:
   one `Settle` of waiting, never a duplicate.

A crash between `POST` and step 6 is also repaired by the ordinary search:
every hit whose `git-work.id` names a local entity without `jira-id`
is linked, not imported.
The search's lower bound needs no attempt term:
a created issue is always later than the cursor,
which only moves to hits seen before any `POST`.
Two Jira issues naming one entity: the lower id links,
the other is skipped and reported every run.
`--dry-run` reports a create's follow-up writes too,
merging the create base against a Jira that holds what was `POST`ed and nothing else.

### One run at a time

A run holds a non-blocking `flock` on `.git/git-work/jira/sync.lock`
for its whole length,
so two cron runs cannot both `POST` one issue;
the second exits 1 with "a jira sync is already running", having done nothing,
and the kernel drops the lock when a process dies, like the write lock's.
`--dry-run` takes no lock.

### Imports from Jira

A search hit with no link and no property is imported:
`merge(nil, empty, R)` decides it like any issue,
`admit` drops what the shape check refuses into `Retry`,
and the admitted fields and description go to `Issues().NewRawShape`
by the reporter's identity at `created`,
with `jira-id`, `alias:jira` and `jira-sync` on the create op,
which is the first marker.
An empty set or a null is not stored.
Comments follow in the ordinary step 6.
A hit of an unmapped type is skipped silently, before any `GET`:
after Derive, every issue type is mapped or excluded by the schema.

### Link requests

A local issue created with `aliases: {jira: KEY}` and no `jira-id` is linked,
never created: `GET` by key, with the property.
A property naming another local entity is a skip,
and one naming an absent entity a skip unless [`--adopt`](#orphaned-exports) takes it;
a 404 is a skip ("alias PROJ-9 names no Jira issue"),
because creating would give a different key that the immutable alias could never follow.
Otherwise step 6 adds `jira-id` and the merge runs with no base:
scalars take Jira's value, with a conflict note where they differed, and sets union.
This is how a first sync with both sides populated avoids duplicates,
`--dry-run` first.

### Orphaned exports

A hit whose property names an entity this clone does not have
is skipped ("pull first"), not imported,
because an import would make a second local issue for one Jira issue
the moment the entity arrives.
Nothing observable separates an export in flight from one lost for good:
a push can come a week later or follow any fetch,
and the entity cannot be rebuilt from Jira, because its id is the hash of its history.
So the bound is the person's: `--adopt DURATION` imports such an issue
once it is that old on Jira's clock, measured from its `created`;
the line is `imported` with `"adopted"`, the absent entity's id,
and the summary counts `adopted`.
The property is left as it is; once the create op carries `jira-id`, no reader needs it.
`Settle` bounds index lag, which is bounded;
this bounds an unarrived entity, which only the person can,
since only they know how their clones exchange.
Adopting early is not a loss: the original's arrival is
[consolidated](#consolidation-keeps-the-copy-that-reached-jira-first).

The orphans a run skips are remembered in the state's `Orphans`,
Jira id → key, entity and `created`, until the `Index` knows the id,
so the `Retry` of every relation waiting on one names the cause:
"PROJ-12 is not imported yet: created in Jira on … from issue 1a2b3c4,
which this clone has not pulled; pull first, or --adopt takes it".
The summary carries `orphans`, one number per run rather than a line per orphan in cron's mail.

Not dropping the skip, which turns every export of a sibling clone
into a local duplicate within one cron minute.
Not a built-in age: a push is not bounded the way index lag is.

### Identities

A Jira user is an identity with immutable metadata `jira-account-id`;
an unknown one is created with Jira's display name, its email when Jira shows one,
and the accountId as login when the display name is empty,
before step 6 and never inside the write lock, which is not re-entrant.
Email adoption is not attempted, because Cloud hides most emails.
At run start the `/myself` account tags the current user's identity
when no identity carries it; one that does is left as it is.
Two identities carrying one account resolve to the lower id, and both export as it.
Field imports, markers, notes and tombstones are authored by the runner,
at Jira's `updated` for imports and at Jira's now for the rest.

### Relations and what cannot convert now

Relations resolve through the `Index`,
built once per run from the excerpts' `jira-id` and the identities' `jira-account-id`,
and extended as the run imports and creates.
Links follow [the vetting](bridges/jira-api-vetting.md) C1:
in `POST /issueLink`, `inwardIssue` is the source,
so `{inwardIssue: A, outwardIssue: B, type: Blocks}` means A blocks B,
and viewing A, `issuelinks` holds `{outwardIssue: B}`.
The relation is stored on the source only,
so `fromIssue` reads only entries with `outwardIssue`,
and a removal deletes the link id found there.
Parent is `fields.parent {id}`; its removal is `update.parent [{"set":{"none":true}}]`.

A target outside the project (its key's prefix is not the bound key),
or of an issue type the schema does not map or excludes,
can never import and is **dropped**:
a scalar relation is excluded from the merge for that issue,
a set's item from both sides,
so nothing is imported, exported or cleared.
An in-project target not yet in the `Index`, an account whose identity failed,
or a Jira value the schema lacks is a `Skip{Retry}` on the remote side:
the key is left alone and recorded in the marker's `Retry`,
which keeps the issue a candidate every run until it converts.
At the end of a run that imported or created an issue,
the issues left with `Retry` keys are run once more,
which resolves a child imported before its parent.
A local value Jira cannot hold —
a local-only status or target, a target not in Jira yet, a dead alias,
a label with a space, a summary over 255 runes, an identity without an account —
is a `Skip` without `Retry`: pending, and a candidate anyway because `local ≠ base`.
A link item whose target is not in Jira is skipped alone;
the key's other items are still written.

### Deletes and moves are Gone

Polling by `updated` never sees an issue vanish.
Under `--full` the search returns every id of the project,
and a linked issue whose id is missing, and not `Gone`, is read with `GET`:
a 404 (deleted, or hidden: Jira will not say) is `Gone: deleted`,
and a 200 in another project `Gone: moved`.
A run that meets either on its own `GET` —
a locally edited issue, a lagging index still returning a deleted one —
reports a skipped line ("…; --full marks it gone") incrementally,
and under `--full` hands it to the Gone pass whatever the search returned.
A `Fresh` create younger than `Settle` answering 404 is not gone at all.

The status field is set to the first value in its type's canceled category
(a type without one keeps its status),
a note with `jira-note = deleted` says why,
and a `Gone` issue is never exported. It is never `rm`'d.
**The one exception to [the base rule](#the-base-is-what-jira-was-seen-to-hold)**:
the marker's base status is that canceled value,
so that when the issue answers again (permission regained)
Jira's status imports instead of the local canceled being pushed onto it.
Any later successful merge clears `Gone`
and exports the local edits made while it was gone, like any local edit;
until then an incremental run reports such an edited issue without a `GET`.

More than 10 missing issues in one scan marks none of them and fails the run:
a permission change looks exactly like a mass delete,
and `/myself` already rules out a missing credential.
`--accept-deletes` marks them.

### Candidates and the cursor

A run's candidates, in this order:
link requests;
the [re-point sweep](#every-relation-names-the-surviving-copy) and the copies to consolidate;
search hits of `project = P AND updated >= "<lower bound>" ORDER BY updated ASC, id ASC`,
with the failed hits of earlier runs;
linked issues with `local ≠ base` or a base not settled;
unlinked local issues to create;
and under `--full`, the Gone pass.
An issue whose excerpt's edit lamport equals the one the state recorded
after its last converged sync (`Seen`, never recorded while anything is pending)
is not even read.
`ID...` takes exactly those issues (id prefixes or aliases, a Jira key included),
after the sweep, with no search, no cursor and no Gone.
`--full` searches `project = P` with no lower bound and ignores step 1's skip.

The cursor is Jira's own `updated` (UTC in the state file),
so client clock skew is irrelevant.
At the end of a run it becomes the greatest `updated` the search reached;
hits not reached are later than it.
A hit that failed is kept in the state's `Failed` and re-read by `GET` on the next run
(at most 100 a run, round robin, so each is read within `ceil(n/100)` runs),
until it syncs or is dropped on a 404 or a move,
so one issue failing forever costs one `GET`, not a window that only grows.
It is never named in the JQL:
Jira refuses a whole query naming an id it cannot see,
and a failed hit is most often an issue since deleted or hidden.

### Time zones

The lower bound is `cursor − Overlap` (5 minutes),
converted to `/myself.timeZone`, truncated to the minute,
and formatted `"yyyy/MM/dd HH:mm"`:
JQL literals are read in the **user's profile zone**,
which can differ from the zone responses are rendered in.
The overlap covers minute truncation and index lag.
A DST fold is an hour, which no overlap covers:
in a fold `jiraapi.JQLTime` names the instant before it,
so the bound errs an hour early, never late.
Response timestamps are parsed with their offset
(`2006-01-02T15:04:05.000-0700`, `Z` and colon forms accepted), never assumed `+0000`.
The binary embeds `time/tzdata`, so any IANA zone loads;
an empty or unloadable zone fails the run.

### Conflict notes

A double edit that Jira wins,
or a local type change Jira [refused](#a-local-type-change-is-exported-alone-and-first),
writes, in the same commit as the import,
**one note per issue per run**:
a comment by the runner, metadata `jira-note = conflict`,
listing every overridden key, with the reason when there is one:

```
Jira sync, 2026-09-28 14:03 UTC: Jira's values replaced local edits.
- status: local "in-review" -> Jira "done"
- type: local "bug" -> Jira "story" (Jira refused issuetype: The issue type selected is invalid.)
- body: the local description edit is kept in its history
- comment 10231: the local edit is kept in its history
```

A comment is visible where people look (`issue get`, the show view);
a "has conflicts" field would need defining on every type and clearing.
The note is never exported:
Jira canonical means Jira's users are not told about the local values their edits replaced.
There is no op metadata marking the overridden value:
it is already an operation in the log, and the note is what makes it not silent.

## Two clones

### One bound clone

Bind one clone.
Two bound clones that exchange before each sync lose nothing and duplicate nothing:
imports carry Jira's current value, the latest lamport wins after merge,
markers are chosen by `Updated`, pairings are facts, and a create is caught by its property.
Their residual costs are two clones exporting different values to one key
(Jira last-writer-wins, no note),
and two clones deriving the same **new** Jira type or field
(two config entities with one key, reported by every schema command;
a new **value** is an attribute, and merges).

Two bound clones that sync **before** they exchange duplicate,
and nothing local can prevent it, because each has yet to see the other's work:

| race | result | what the sync does |
| --- | --- | --- |
| both import one new Jira issue | two local issues with one `jira-id` | the copy that reached Jira first is the `Index`'s; the other is consolidated into it |
| both create one pulled local issue | two Jira issues with one property | after the exchange one `jira-id` wins on the create op (first writer); the other Jira issue is reported as a second issue naming it |
| both create an identity for one account | two identities with one `jira-account-id` | the lower id is the account's; both export as it; the runner is never re-tagged |

A marker naming its clone was rejected as a guard:
it catches only what an exchange already made safe,
and it would forbid handing the binding to another clone.

### Consolidation keeps the copy that reached Jira first

Two local issues for one Jira issue come from the import race
and from an adoption followed by the original's push.
The winner is the copy whose first contact with Jira is earliest,
on Jira's clock, to the second:
the earliest of its `jira-create` attempts and its markers' `Updated`;
at one second an export's attempt before an import's read,
since the `POST` is what the read saw;
then the lower entity id.
Every input is an immutable fact of the copy's own history,
identical in every clone that holds it,
so two clones holding both copies agree, with no window.
The archived flag is not an input:
it is replicated but mutable, and reading it opens a window in which two clones disagree.
The `Index` uses the same rule,
so the copy that syncs and the copy that survives are one.

A consolidation, in this order:

1. The loser is synced one last time, as the linked issue it is,
   so what it holds for Jira reaches Jira, Jira winning where they collide.
2. The winner takes the loser's local-only fields where it has none —
   a scalar it lacks, the union of a set —
   and a note (`jira-note = consolidated`, `jira-consolidated-from` the loser's id)
   says the loser is archived and its history kept, in one commit.
   This is gap filling, not a second merge:
   a merge between two local copies would be a three-way with no base.
3. The loser is archived,
   and its create op stamped with `jira-consolidated-into`, the winner's id,
   in one commit, so a loser is archived and stamped or neither.

The archive is last so that it is idempotent:
a failure before it leaves the loser unarchived, so the next scan retries it;
the fill is gap-only, and the note is matched by its metadata key,
not its text, so a retry writes neither twice.
Archived first, an interruption after it was final:
an archived loser is never a loser again, and its fill and note were lost.
The stamp is create-op metadata, which is the excerpt's `metadata`,
so a report over archived issues drops the losers with
`map(select(.metadata["jira-consolidated-into"] == null))`:
a loser is an archive the person did not decide.
A person's archive of the winner is neither undone nor a choice of copy.
An original that arrives unlinked
(an attempt and no `jira-id`, pushed after a crash)
finds the Jira issue its adopted copy is linked to by its property,
wins by the rule, and links as a create once the copy is consolidated.

### Every relation names the surviving copy

Every run holds, after its scan and before its losers and its search:

> No unarchived issue has a relation value naming an issue
> that is not the `Index`'s winner for its own Jira id.

The sweep enforces it over every issue of the store, `ID...` runs included,
because the invariant is the store's:
each value naming an issue with a `jira-id` that is not the winner is replaced by the winner,
a scalar by one `SetField`, an item of a set by `RemoveValue` then `AddValue`,
all of one issue in one commit.
A value naming an issue with no `jira-id`, or the winner,
or an archived issue with no duplicate, is left alone.
It runs **first**, so each loser's last sync and the winner's fill
already see relations naming winners,
and it re-points away from a loser whether or not the loser is archived yet.
It is the only re-point, so one place writes and reports it,
and it repairs a refusal, an interrupted run,
a loser archived by another clone, or a store consolidated before it existed,
on every clone's next run.
Its cost is one pass over in-memory excerpts.

A stale relation is not harmless:
exporting it asks the `Index` for the loser's Jira id, which only the winner has,
so it is "not in Jira" every run;
importing would repair it, but a run bounded by the cursor
never re-reads a child Jira did not touch;
and a nested view, drawing a row's unarchived children,
shows such a child under no parent at all.

The sweep writes under the shape check, as a pull does:
the value it writes is one the store already holds for the same field of the same issue,
only the target's identity changes, never its type,
and the old value was Jira's.
Values the policy refuses are reported under `off_schema`.
A write that still fails is a `failed` line for that issue, and the pass goes on,
in sorted id order, so two runs report the same lines in the same order.
Each re-pointed issue is one `repointed` line,
`imported` listing the relation fields that changed,
because a re-point is a local write the person did not make;
a dry run lists them and writes nothing.
It writes nothing to Jira:
the next export resolves the winner to the same Jira id and finds nothing to send.

### Duplicate aliases are refused

`Compile` refuses two entities carrying one alias for one system —
two types for one issue type, two fields of a type for one Jira field,
two values of a field for one Jira id —
which two clones can produce by merging.
The check lives there rather than in reconcile, so `schema` stays alias-agnostic.

## Commands and report

### The commands

```
git work jira schema [--format yaml|json] [-v]
git work jira sync [ID...] [--dry-run] [--full] [--accept-deletes] [--adopt DURATION] [--format json|text]
git work sync [REMOTE] [--jira] [--dry-run]
```

`jira schema` is a reader: Derive over the live schema,
the document on stdout, warnings and errors on stderr, the info notes with `-v`.
`jira sync` is a writer in the remote group beside `push` and `pull`;
it never pushes.
`git work sync --jira` does: a pull, then this sync, then a push.
A pull that fails stops it, since pushing over an unpulled tracker is rejected anyway;
a Jira step that fails does not, because what the pull brought in
belongs on the remote either way, and the command exits 1.
Its `--dry-run` is the Jira step's, so it needs `--jira`;
neither a pull nor a push is dry-runnable.
The mapping's notes reach stderr only in a run that changed the schema,
since they say the same thing every run, and cron mails output.

Not `--watch`: there is no automation inside git-work, and cron is the loop.

### The report

Output is one JSON object per line, like `log`:
an optional schema line,
one line per issue the run touched, left pending, skipped or failed on,
then a summary.

```json
{"schema":[{"action":"update","shape":"field","key":"task/status","id":"…","set":{"alias_jira/wont-do":"10005","values/wont-do":{…}}}]}
{"issue":"0a4390dd…","jira":"PROJ-12","action":"updated","imported":{"status":"done","body":"First line of the new descrip…"},"exported":{"priority":"high"},"comments":{"imported":1,"exported":0,"edited":0,"tombstoned":0,"deleted":0},"conflicts":[{"key":"assignee","local":"a3a2829…","jira":"5b10ac8d…"}],"pending":[{"key":"status","reason":"no transition from In Progress to In Review"}]}
{"summary":{"imported":3,"created":1,"updated":7,"linked":0,"gone":0,"adopted":0,"consolidated":0,"repointed":0,"orphans":0,"conflicts":1,"pending":1,"off_schema":0,"failed":0,"skipped":0,"unchanged":212,"cursor":"2026-09-28T21:02:00Z"}}
```

`action` is `imported` (new locally), `created` (new in Jira), `updated`,
`pending` (nothing moved; what waits is in `pending`), `linked`, `gone`,
`consolidated` (a second local copy, archived into the one that reached Jira first),
`repointed` (relations that named such a copy, pointed at the survivor),
`skipped` (refused or waiting, reported: a person may look),
or `failed` (with `"error"`).
An issue with nothing to do is not a line but counted in `unchanged`.
The body appears in `imported` and `exported` as its first line, at most 60 characters;
a conflict on a text repeats no text;
a conflict that is no double edit, a refused type change, carries its `reason`;
`retry` appears only when true;
empty members are omitted, the cursor too when there is none.
A pending key of `*` is the whole issue.
`off_schema` lists the keys written that the schema's policy would refuse
(`{"key","reason"}`).

`--dry-run` reads both sides and writes neither —
no schema import, no issue or identity write, no Jira write, no state file —
printing `plan₁` with `"dry_run": true`.
It cannot predict a transition that fails or a normalisation Jira applies,
and keys whose values need the schema changes or identities it did not write
show as pending.
`--format text` is one line per issue.
The exit status is 0 when every issue synced or is only pending,
and 1 when any failed, the run stopped early, deletes were held,
or another run holds the lock.

### Failure classes

A request fails **for the issue** (400, 403, 404, 409 after its retry, 413)
or **for the run** (401, 429 or 5xx after the client's retries, a network error).
An issue failure is reported and the run goes on;
a run failure stops the run, saves the state file with the cursor where it stopped,
and exits 1.
What the engine does with a key it cannot sync is one value,
`Skip{Key, Reason, Retry}`: leave the key alone this pass, and retry next run or not.
`pending` in the report is every `Skip`,
plus every local change `plan₂` did not export.

### Retries belong to the client

The client retries 429,
and 503 on anything but a `POST` that is not a search
(a create is not idempotent);
it waits `Retry-After` (jittered up to +30 %,
and returns the error when it asks for more than 60 s),
else backs off exponentially from 2 s, capped at 30 s, jitter ×0.7–1.3,
at most 4 retries;
a request times out after 60 s, a transport error like any other.
The split into run and issue failures is the engine's, not the client's.
The engine batches every field into one `PUT`,
which with a transition, links and comments stays under the per-issue limit (20 per 2 s);
a 429 past it is waited out.
It never sends `notifyUsers` (`false` without admin fails the whole edit),
never calls `/search` (410),
always sends `Content-Type: application/json`,
pages `/search/jql` until `isLast` or a missing, null or empty token,
never relies on a full page,
and gives up an offset-paged read past 10000 items (a server ignoring `startAt`).

## Code and tests

### One fake over HTTP

`jira/jiratest.Server` is the single double
for the client tests, discovery and the engine scenarios,
instead of an in-memory client, a file-serving getter and recorded fixtures.
It imports nothing from `jiraapi`, so a wrong struct tag fails a test
instead of agreeing with itself.
Every documented behaviour is its default and every unverified one is a `With…` switch,
and the design chooses what works either way:
the crash matrix runs under both ADF modes,
and one scenario runs under each switch.
Always on: `PUT` all-or-nothing with per-field errors;
Jira's normalisation (sorted labels, trimmed summary, float rounding, ADF with `localId`s);
summaries over 255 and labels with spaces refused;
links stored `{source: inward, dest: outward}`;
properties inline on create, and indexed with the search's lag;
deleted issues dropped from search, and a JQL `id` or `key` naming one refused;
410 on `/search`; the token omitted on the last page; 429 with `Retry-After`;
no `Authorization` header is 200-with-nothing on search and 401 on `/myself`.

The behaviours a live site has not yet confirmed, each a switch:
whether `GET /issue` is read-after-write consistent
(if it is, `Sent` and `Wrote` only cost the normalised-back case its `Settle` of pending);
whether `expand=properties` is honoured on the comment list
(if not, the repair is a `GET …/comment/{id}/properties/git-work`
for each unpaired comment by the token's account, never a digest match);
whether an issue property reaches the search index with the same lag as fields;
and how Jira answers JQL naming a deleted id (the fake answers 400, the worse case).

### Where it lives

```
jira/jiraapi/   Cloud v3 client, wire types, ADF codec, JQL literals, time parsing. Standard library only.
jira/jiratest/  the fake Jira over httptest. Imports nothing from jiraapi.
jira/           discovery, Derive, Compile and the Mapping, the merge, the engine, the state file.
host/jira.go    binding, credential, discover, derive + SchemaImport, compile, the sync lock, JiraSync.
commands/jira/  schema, sync.
```

Nothing here imports the legacy `bridge/`.
The engine uses the concrete cache and client:
tests run a real test repository against the real client pointed at `jiratest`,
which exercises what production runs.
Outside `jira/` the footprint is
`IssueCache.Update` and `UpdateShape` (the write lock, a re-read,
the closure on the fresh snapshot, its operations checked as one change
against the type they set, committed behind one ref update;
the closure calls no cache writer and no network),
`NewRawShape`, `issue.NewNoOpOp`,
the reserved `alias_` attributes in `schema`,
`gitcli.CredentialHelper`,
and `host.JiraSchema` and `host.JiraSync`, the one path for commands.
Identity tagging uses `Identity.SetMetadata`, which appends a version.

## Not done

- **Starlark.** There is no `work.jira.*`:
  the sync is reached from the command line only,
  a known gap in the rule that the command line and Starlark are one API.
- **Key changes.** `alias:jira` is the key at link time and is never refreshed:
  the link is the Jira id, and Jira resolves old keys itself.
- **Per-field attribution.** Field imports are the runner's
  (see [three documents per issue](#three-documents-per-issue)).
- **Data Center.** Refused at binding: REST v2 and users by name are another client.
