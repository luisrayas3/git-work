# Story: Bidirectional Jira sync, Jira canonical (`8ade811`)

**Outcome:** `git work jira sync` converges the tracker and one Jira Cloud
project in both directions, one issue at a time. A field edited on one side
reaches the other; a field edited on both takes Jira's value and says so on
the issue; a new issue on either side appears on the other; a run
interrupted anywhere is finished by the next one without duplicating
anything. The mapping between the two schemas lives in the tracker and is
reviewed once, then kept current by the sync itself.

**Tasks:** `3c6d07a` policy · `69b7be0` schema and mapping · `33148f2` import ·
`32d372e` export · `a3a8d16` the command · `0a4390d` Cloud v3 spike (the
first live run, when a site exists).

**Status:** implemented against the fake; awaiting live verification
(`0a4390d`). No Jira site exists (`de1d8fb`), so every Jira behaviour below
comes from the vetted API reference, and every behaviour the vetting could
not settle is a switch on the fake server (JS26), with the design choosing
what works either way. This document describes the code; godoc is the
authority for signatures.

## What decides the shape

**1. The op log orders by lamport time, and never forgets.** An operation
committed now is later than every operation already in the store, whatever
Jira's timestamp says. A 3-way decision must therefore be made *under the
write lock*, against the issue as it is at that instant, and whatever loses
is still in `git work issue log`.

**2. One commit is atomic and every operation carries hashed metadata.**
`dag.Entity.Commit` writes an entity's staged operations behind one ref
update; metadata is fixed before an operation's id is computed; `NoOp`
(code 7) exists "to store arbitrary metadata in the entity history";
`SetMetadata` (code 8) adds keys to an earlier operation, first writer wins.

**3. Jira has no compare-and-swap, no tombstones and a lagging index.**
`PUT /issue` is last-writer-wins; deleted issues and comments vanish;
`/search/jql` lags the database by seconds to minutes, while
`GET /issue/{id}` reads the database. The changelog is capped, unordered,
and omits comments.

**4. Jira ids are stable; names, keys and custom field ids are not.**
Issue type, status, priority, option and link type ids never change; names
are editable; `customfield_NNNNN` differs per site; a key changes on a move.

## Scope

**In v1:**

- `git work jira schema` (a reader) and
  `git work jira sync [ID...] [--dry-run] [--full] [--accept-deletes]`.
- Binding in git config (`git-work.jira.url`, `.project`, `.email`); the API
  token from `JIRA_API_TOKEN` or `git credential fill`.
- Discovery of one project (seven reads), `Derive` (additive plus renames),
  aliases on schema entities, `Compile` into a `Mapping`.
- Fields: `summary`↔`title`, `issuetype`↔`type` (import only),
  `description`↔comment #0, `status` (by transition), `priority`,
  `assignee`, `labels`, `duedate`↔`due`, `parent`, every link type as a
  `multi-relation`, story points↔`estimate`, and custom `number`, `date`,
  `datetime`, single-line text, `option`, `array(option)` and `user` fields
  on a type's create screen.
- Comments both ways, edits both ways, a Jira comment delete as a tombstone.
- Creates both ways, crash-safe; linking a local issue created with
  `aliases: {jira: KEY}` to that Jira issue.
- Identities by `jira-account-id`, created on demand.
- A Jira delete or a move out of the project: `Gone`, under `--full` only.
- Conflict notes on the issue; one JSON line per issue and a summary.
- `jira/jiratest`, one HTTP fake shared by every test layer.

**Deferred to v2**, each needing no format change in v1:
sprints (iteration issues, the sprint pass, closed-sprint selection);
rank (relative moves through `PUT /agile/1.0/issue/rank`);
`--watch` (cron is the loop, `a3a8d16`);
per-field attribution from the changelog;
`components` and `fixVersions`; textarea (ADF) custom fields;
`array(user)`, `array(string)`, cascading selects;
multi-hop transitions and workflow discovery;
degraded discovery (a forbidden read fails the run instead);
key-change aliases (`alias:jira-2`) — the link is the Jira id, and Jira
resolves old keys itself;
an export scope program (v1 exports every unarchived issue of a mapped type);
a `git work jira link ID KEY` for issues created without an alias;
Starlark bindings (`work.jira.*`, via `host.JiraSync`, one line each);
per-identity Jira credentials;
Data Center (REST v2, users by name).

## Invariants

The package doc states them and the tests check them as properties. Almost
every robustness question below is one of them applied.

**I1 — Fixpoint.** For every mapped key `k` of a linked issue, after a
successful per-issue commit, either `local[k] == base[k] == jira[k]`, or `k`
is reported pending and `base[k]` is unchanged. `merge(b, l, r)` with
`l == b == r` emits nothing. Everything that could loop violates this.

**I2 — The base is Jira-observed, and moves only with local.** `base[k]` is
written only in the commit that makes `local[k]` equal to it, and its value
is one Jira was seen to hold (`R` or the post-write `R′`), in local terms
(the body's as its digest). The export path never writes a base directly:
`B′[k] = written` is only the second merge's input, which records `R′`'s
value by row 1 or imports Jira's normal form; a written key `R′` does not
show yet is *unconfirmed* and keeps its prior base, so it is exported again
rather than the old value imported over the local edit
(`TestAdvUnconfirmedWriteNotBase`); unconfirmed twice, it has no base, and
Jira's value imports with a conflict note (JS13 step 5). A crash can only leave a base *older*
than both sides, which the first merge row (`l == r` ⇒ converged) absorbs.
One exception, `Gone` (JS19).

**I3 — Links are immutable facts on operations.** An issue's Jira id is
metadata on its create operation, a comment's Jira id metadata on its
add-comment operation, each set once — at creation or by one `SetMetadata`
— and never inferred from a marker. Merging clones' histories can never
un-pair or re-pair anything; two markers can disagree only on values.

**I4 — Destructive decisions come from the database, not the index.**
Merges, deletes and link repairs use `GET`: a link repair found by the
search's copy of the `git-work` property links only when `GET`'s
`properties=git-work` names the same entity. Search only answers "which ids
might have changed", and never names an id Jira may no longer show (JS20). A run starts with `GET /myself` and aborts on anything
but 200, because a missing `Authorization` header is 200-with-nothing.

**P1 — `fromIssue` is deterministic and idempotent over one round trip:**
`fromIssue(jira(toWrites(fromIssue(x)))) == fromIssue(x)`, where `jira()` is the
fake's model of Jira's normalisation (sorted labels, trimmed summary, float
rounding, ADF re-serialised with `localId`s). With I2 a non-idempotent
converter is not a ping-pong but import churn, one operation per run per
issue Jira touched. It is a property test per kind and for ADF.

## Decisions

### JS1 — State-based: three documents per issue, merged against `GET`

The inherited bridge replays Jira's changelog and aligns entries with
`jira-export-time` metadata to recognise its own writes. We do not port it.
Per issue the sync compares three documents in local terms — the **base**
(what both sides held after the last sync, JS8), the **local** snapshot, and
the **remote** issue from `GET /issue/{id}` — and decides per key.

Why: the changelog omits comments, their edits and deletes, and is capped
at 100 unordered entries with `fieldId` often absent, so a replay needs a
state diff as well; the state diff alone covers everything. An echo is not
detected but equal to the base (JS14). Comparing twice finds nothing the
second time, which every crash argument in JS13 rests on. And it is one pure
function over three documents, tested without Jira or a repository.

Search is used only to find candidates (`fields=updated`,
`properties=git-work`); nothing is merged against a search copy (I4), which
removes every index-lag rule. The costs are accepted: intermediate Jira
values between two runs arrive as one operation (Jira's history stays the
authority for what happened in Jira), and field imports are authored by the
runner at Jira's `updated` rather than by the Jira user who made the change.
Issue and comment authors stay exact, because they are free.

### JS2 — The mapping is aliases on the schema entities it maps

Every type, field and enum value that corresponds to something in Jira
carries that thing's Jira id as an **alias**, the word an issue already uses
for `alias:jira = PROJ-12`, stored as an attribute of the config entity:

| entity | attribute | value | example |
| --- | --- | --- | --- |
| type | `alias_jira` | issue type id | `task` → `"10001"` |
| field | `alias_jira` | field reference | `task/estimate` → `"customfield_10016"` |
| field | `alias_jira/<value-id>` | status, priority or option id | `task/status`: `alias_jira/in-progress` → `"3"` |

A field reference is a Jira field id (`status`, `priority`, `assignee`,
`labels`, `duedate`, `parent`, `customfield_…`) or `link:<link type id>`, the
outward side of a link type. In the document it is an `aliases` map at all
three levels (`aliases: {jira: "10001"}`), and `schema export` prints it.

Git config was rejected: values are stored by id (D2), so the mapping is what
a stored value *means*, and two clones mapping one Jira status to two value
ids would write contradicting data into one store. A `mapping` config shape
was rejected: it names a `<type>/<field>` it does not own, dangles when that
is archived, and needs its own validator and E7 handling. Attributes
replicate, merge per attribute (two clones mapping two new statuses keep
both), die with their entity, and are reviewed in the same file as the schema.

**An alias is written when a document states it and never removed by
reconcile**: `alias_*` is not in `typeAttributeNames`/`fieldAttributeNames`,
so this repository's alias-free `schema.yaml` can be imported at any time
without unmapping anything. **The empty string is a stated alias meaning
"never sync this"**; nothing else unmaps. `alias_` is reserved for every
later attribute in `schema/attr.go`, in the first commit, because attribute
names are forever. Alias values are opaque to `schema`; a Linear bridge adds
`alias_linear` with no change there.

### JS3 — Binding and credentials are per clone, through git

Which site and project a clone talks to is git config, read through
`gitcli` so `[include]` works: `git-work.jira.url` (`https://<site>.atlassian.net`,
or `https://api.atlassian.com/ex/jira/<cloudId>` for scoped tokens; anything
else is refused as Data Center, v2), `git-work.jira.project` (the key), and
`git-work.jira.email`. With no `git-work.jira.url` every `jira` verb fails
naming the three keys.

The token is `JIRA_API_TOKEN` when set (cron), else `git credential fill`
with `protocol`, `host` and `username = email`, run with
`GIT_TERMINAL_PROMPT=0`; an empty answer is an error naming
`git credential approve`. `bridge/core/auth` is bug-era and leaves with the
bridges; `AGENTS.md` already routes git's environment through `gitcli`, and a
credential helper is exactly that. There is no `configure` command and a
token never reaches argv. `JIRA_API_TOKEN` is read in `host`, not `gitcli`,
because `gitcli` is git's environment and the variable is not git's.

A clone bound to the wrong project is caught by the aliases (JS5), which is
why the binding need not replicate. **One clone is bound**: two bound clones
converge but pay two costs (JS25).

### JS4 — Discovery: seven reads, fail fast

`Discover` builds one normalised `Project`:

| # | call | gives |
| --- | --- | --- |
| 1 | `GET /myself` | the credential works (I4); `accountId`; `timeZone` for JQL (JS20) |
| 2 | `GET /project/{key}` | project id; issue types with `hierarchyLevel` |
| 3 | `GET /project/{key}/statuses` | statuses per issue type, workflow order, `statusCategory.key` |
| 4 | `GET /field` | every visible field with `schema{type,items,system,custom}` and `scope` |
| 5 | `GET /issue/createmeta/{key}/issuetypes/{id}`, per type, paged | the create screen: fields, `required`, `allowedValues` |
| 6 | `GET /priority/search?projectId=` , paged | the scheme's priorities, in Jira's order |
| 7 | `GET /issueLinkType` | link types `id, name, inward, outward` |

Any failure fails the run naming the permission, except 404 on step 7,
which means linking is disabled: no link fields. v1 must create issues, so a
forbidden createmeta is fatal rather than a degraded mode. `serverInfo`
(the cursor uses Jira's own `updated`, JS20), resolutions (JS13 fills one
from the transition screen), boards and sprints (v2) are not read.

Everything is sorted at read, because Jira promises no order: issue types
by `(level desc, id)`, fields by id, statuses in step 3's order, priorities
in Jira's. Custom fields are kept only when `scope` is absent or names this
project, which keeps two team-managed "Story point estimate" fields apart.

### JS5 — Derive, then the existing schema import; the first mapping is reviewed

There is no Jira-specific schema writer:

```
Discover(client, key)              -> *Project            (network)
Derive(schema.Export(current), p)  -> *schema.Document    (pure)
host.SchemaImport(repo, doc, prune=false, dryRun)         (the existing path)
Compile(repo.LoadSchema(), p)      -> *Mapping             (pure)
```

`Derive` returns **current ⊕ Jira**: the exported current schema with Jira's
types, fields and values adopted or added, Jira's names applied to what is
aliased, and nothing removed. `Reconcile` owns every `values/<id>` and
`target_types/<t>` of a field it is given, so a document that restates
everything current is what keeps local-only things alive. Its output is a
fixpoint, so a second run against an unchanged Jira writes nothing; and
review is `schema import --dry-run`, because it is the same call. `Derive`
never states `prune`, an archive, or a kind change, and never removes a
`target_types` entry Jira's hierarchy forbids: a forbidden local parent is a 400 on export, i.e.
pending, and "Derive never removes" is one invariant instead of a list of
exceptions. Schema writes are the runner's, at now, like any import.

**The binding check is in `Derive`, not only in `Compile`**, because
`Compile` runs after `SchemaImport`: a clone bound to the wrong project would
already have adopted that project's types into the tracker. `Derive` returns an error when no non-empty type alias names an
issue type of `p` (while any type alias exists): "the schema maps issue types
10001, 10002 that PROJX does not have; is this the project the tracker was
mapped against?". `Compile` checks it again, with the duplicate-alias check
(JS25).

**The first mapping is explicit.** `jira sync` refuses while no type has a
non-empty `alias_jira`, naming the review recipe:

```sh
git work jira schema > jira.yaml          # derived document; warnings on stderr, -v for info
$EDITOR jira.yaml                         # rename proposed keys, exclude with aliases: {jira: ""},
                                          # mark "Won't Do" canceled, map by hand
git work schema import jira.yaml --dry-run
git work schema import jira.yaml
```

After that the sync derives and imports at the start of every run.
Everything automatic is additive, a rename, or a Jira-enforced fact; nothing
needs a human's judgement, and `jira sync --dry-run` previews it.

### JS6 — Matching and keys: adopt by normalised name, slug once, never rename

**Matching**, per Jira entity, first rule that applies: an entity whose
alias equals the Jira id is matched; an entity aliased `""` is excluded; an
unaliased entity whose `norm(key)` or `norm(name)` equals `norm(jira name)`,
**with the same kind** for a field, is adopted and its alias stated;
otherwise a new entity. `norm` keeps lower-case letters and digits only, so
"Sub-task" and "Subtask" adopt `subtask`. When two Jira entities normalise to
one local one, the lower Jira id wins and the other is new, reported.

**`slug(name)`** makes a new key: NFKD with combining marks dropped; lower
case; apostrophes dropped; every run outside `[a-z0-9]` becomes `-`; `-`
trimmed; cut to 48 bytes at a `-` boundary (room for `alias_jira/` in 64);
a type or field key starting with a digit gets `t-` or `f-`; an empty result
is `jira-<id>`. Collisions within one namespace take `-2`, `-3`, … in a
deterministic order (the fixed table before custom fields, then Jira id); a
built-in (`title`, `type`, `archived`) always collides. "Won't Do" →
`wont-do`; "完了" → `jira-10002`.

**A key is made once.** Once aliased an entity is found by its alias; a Jira
rename changes `name` and never the key, because keys are what issues, flows
and views store. A person may rename a proposed key in the reviewed file
before the first import; the alias travels with it.

### JS7 — What maps, in which kind, with which canonical value

The fixed table (keys are the jira preset's, so an adopted preset stays itself):

| Jira | key | kind | on |
| --- | --- | --- | --- |
| `summary` | `title` | built in | all |
| `issuetype` | `type` | built in, via type aliases | all; import only (JS18) |
| `description` | comment #0 | text (JS11) | all |
| `status` | `status` | `enum`, values per type from step 3 | all |
| `priority` | `priority` | `ordinal-enum`, Jira's order | create screen has it |
| `assignee` | `assignee` | `identity` | create screen has it |
| `labels` | `labels` | `multi-enum`, freeform | create screen has it |
| `duedate` | `due` | `date` (`2006-01-02`) | create screen has it |
| `parent` | `parent` | `relation`, targets the mapped types one level up, `inverse: children` | every type with a level above it |
| link type | slug of `outward` (`blocks`) | `multi-relation`, `inverse` slug of `inward`; none when symmetric | all mapped types |
| story points | `estimate` | `number` | create screen has it |

Story points are the `jsw-story-points` field, else a `float` field whose
`untranslatedName` (else `name`) is exactly "Story Points" or "Story point
estimate"; boards are v2. Custom fields map only from a create screen, by
`schema.type` (`items`): `number`→`number`, `date`→`date`,
`datetime`→`date` (RFC 3339 UTC), `string` with `custom` ending
`:textfield`→`text`, `option`→`enum`, `array(option)`→`multi-enum`,
`user`→`identity`; values from `allowedValues`, aliased by option id.
Anything else is unmapped and reported with its `schema.custom`. Sprint,
Rank, `resolution`, `components` and `fixVersions` are not mapped in v1.

**Status categories** are Jira's up to the class: `new` and `undefined` allow
`backlog`/`unstarted` (new values `unstarted`), `indeterminate` allows
`started`, `done` allows `completed`/`canceled` (new values `completed`). An
aliased value inside its class keeps its category, so a person marks "Won't
Do" `canceled` once; one outside is reset to the class default and reported.
Category is read from `statusCategory.key`, never the name. Many-to-one value
aliasing is refused (JS25), so `toWrites(fromIssue(x)) == x`.

**Canonical values**, what the store would hold after writing them, so that
"did it change" is byte equality of compacted JSON: sets sorted by compacted
bytes (the order `AddValue` keeps); numbers in shortest form
(`strconv.FormatFloat(v, 'f', -1, 64)`); dates `2006-01-02`; datetimes RFC
3339 in UTC; relations and identities as full entity ids; labels verbatim.
Fields a type has that Jira lacks (`area`, `phase`, `rank`) and types Jira
lacks (`decision`) are local-only: never exported, never cleared, reported
once per run.

### JS8 — The base is a `NoOp` marker on the issue; every metadata key named

The base lives **in the issue, on a `NoOp` operation whose metadata
`jira-sync` holds the `Base` as JSON**, written in the same commit as the
imports it describes. It travels with `push`/`pull`, so any clone starts from
the same base; it is atomic with what it describes (fact 2); two clones'
markers are two operations and neither is lost; `git work issue log` shows
it. A local file was rejected (a new clone has no base, so every difference
looks like a double edit), and so were Jira properties (not atomic with the
local commit, invisible in the tracker) and a config entity (per-issue state
in the schema log). A new operation code was rejected: `NoOp` is decoded by
every binary and exists for this.

```go
type Base struct {
	V        int                    `json:"v"`                  // 1
	Id       string                 `json:"id"`                 // Jira issue id
	Key      string                 `json:"key"`                // as last seen
	Updated  time.Time              `json:"updated"`            // Jira updated of the state described, UTC
	Fields   map[string]issue.Value `json:"fields"`             // each mapped key's form: title, type, "body" (its digest)
	Comments map[string]string      `json:"comments,omitempty"` // Jira comment id -> digest; "" once deleted in Jira
	Retry    []string               `json:"retry,omitempty"`    // keys skipped with Retry (JS17)
	Gone     string                 `json:"gone,omitempty"`     // "deleted" | "moved" | "" (JS19)
}
```

Values are in local terms (schema keys, value ids, entity ids), so the base
compares with the snapshot as it is. Fields are whole, because a set merge
needs the base's items; texts are digests, because they only need equality.
The description is the key `body` of both documents (its text) and of the
base (its digest): `form(key, v)` is the one comparable form, so it merges
through the scalar table with no case of its own.
Every marker is the whole base, so reading it is reading one operation.
**The current base is the marker with the greatest `Updated`**, ties to the
later in compiled order: the base describing the newest Jira state best
predicts Jira. An undecodable marker is skipped and reported (D6). A marker
is written only when the plan's base differs from the current one in anything
but `Updated`, so a quiet run commits nothing: Jira bumps `updated` for what
the mapping does not cover and for both ends of a link, and a marker per bump
would be a commit per bump. The cost is a `GET` while the overlap re-returns
such an issue.

**Every key the sync writes:**

| where | key | value |
| --- | --- | --- |
| create op | `jira-id` | Jira issue id — the link (I3) |
| create op | `alias:jira` | Jira key at link time; resolves anywhere an id does |
| create op of an imported issue; `NoOp` | `jira-sync` | `Base` JSON |
| `NoOp` before a `POST /issue` | `jira-create` | the attempt's time on Jira's clock, RFC 3339 (JS15) |
| add-comment op | `jira-comment-id` | Jira comment id (I3) |
| add-comment op of a note | `jira-note` | `conflict` \| `deleted` |
| identity, immutable | `jira-account-id` | Jira accountId |
| Jira issue property | `git-work` | `{"id":"<entity id>"}` |
| Jira comment property | `git-work` | `{"op":"<add-comment op id>"}` |

`jira-id`, `alias:jira` and `jira-comment-id` are set at creation on an
import and by `SetMetadata` in the step-6 commit on an export, never
elsewhere. `alias:jira` is never refreshed in v1: after a move the old key
still resolves locally and in Jira. Local run state — the cursor, the failed
hits, the creates Jira refused, the per-issue edit lamport at last sync — is
in `.git/git-work/jira/state.json`, written through a temporary file and a
rename. Losing it costs a slower run, never a wrong one: the create journal
is in the store (JS15).

### JS9 — The merge rule for scalars

For every mapped key of the issue's type, with `b`, `l`, `r` compared as
canonical JSON, first row that applies:

| case | local op | Jira write | base becomes |
| --- | --- | --- | --- |
| `l == r` | — | — | `r` |
| `l == b`, `r != b` | `SetField r` | — | `r` |
| `r == b`, `l != b` | — | write `l` | `l` once written (JS13) |
| all three differ | `SetField r`, conflict (JS21) | — | `r` |
| `b` absent, `l` null | `SetField r` | — | `r` |
| `b` absent, `l` set, `l != r` | `SetField r`, conflict | — | `r` |

The first row comes first on purpose: two sides that agree are converged
whatever the base says, which makes a crash after a Jira write, a second
clone's concurrent import and a stale base all harmless. A missing base
(a newly mapped field, a link request) is a double edit, so Jira wins. A key
the remote side skipped (JS17) is left alone: no op, no write, base
unchanged. Keys the mapping does not cover on this type are never looked at.

### JS10 — Multi-values merge item-wise and cannot conflict

`multi-enum`, `multi-relation` and freeform labels merge as sets:

```
merged = (b − removed_locally − removed_remotely) ∪ added_locally ∪ added_remotely
```

An added item was not in `b` and a removed one was, so no item is both, and
a label added in Jira and another added locally both survive. Local ops are
`AddValue`/`RemoveValue` for `merged` against `l`; the Jira write is
`update.labels [{add},{remove}]`, `update.<custom> …`, or `POST`/`DELETE
/issueLink` for `merged` against `r`. A missing base is the empty set, so a
first sync is a union. Jira canonical holds per item: there is no double
edit of one item.

### JS11 — Texts merge by digest; a lossy Jira text is not overwritten

The description (comment #0, the key `body`) follows the scalar table on
digests; a text is never merged within itself. `digest(t)` is
`"v1:" + hex(sha256("v1\n" + norm(t)))`, where `norm` is `jiraapi.NormalizeText`, exactly the normal
form `TextToADF` preserves (CRLF to LF, trailing space and newlines, and
the rest `TextToADF` cannot tell apart), so two texts Jira holds alike never
differ; the version is in the input, so a later
normalisation change re-imports each text once instead of mis-comparing.

`jiraapi.ADFToText` renders ADF as Markdown over a fixed subset —
paragraphs, headings, bullet and ordered lists, code blocks and code,
blockquote, rule, hard break, strong, em, strike, links — and reports whether
the rendering is **lossless**. Anything else (mentions, tables, media,
panels, emoji, status, dates) is rendered as readable text and marks it
lossy. **A local edit over a lossy Jira text is not exported**: it is pending
("the Jira text holds content git-work cannot write back") and keeps its
edit locally, because overwriting would destroy what Jira holds. Importing a
lossy text is fine: the local copy is a rendering. `TextToADF` writes the
same subset; a string is never sent where ADF is expected.

### JS12 — Comments pair by id; a pairing is a fact on the operation

Local comments are `snap.Comments[1:]`; a comment's add-comment op is its
`TargetId()`, whose metadata gives `jira-comment-id` and `jira-note`. Remote
comments are `GET /issue/{id}/comment?expand=properties`, every page. Per
comment, with digests against `Base.Comments`:

| state | result |
| --- | --- |
| paired (op's `jira-comment-id`), both present | JS9 on digests: local edit → `PUT …/comment/{id}`; Jira edit → `EditComment` by the Jira `updateAuthor` at `updated`; both → Jira's, conflict |
| in Jira only, property names an unpaired local op | pair: `SetMetadata jira-comment-id` on that op, base = the Jira digest (we wrote it), no text op |
| in Jira only, otherwise | import: `AddComment` by the Jira author at `created`, metadata `jira-comment-id` |
| local only, not a note | export: `POST …/comment` with ADF and property `git-work={"op":…}`; `SetMetadata jira-comment-id` in the step-6 commit |
| paired, gone from Jira, base not deleted | `EditComment` to a one-line tombstone ("Deleted in Jira on 2026-09-28.") by the runner; base `""`; a local edit since the base adds a conflict |
| paired, base `""` | never exported again; a local edit is pending ("deleted in Jira") |
| a note (`jira-note`) | never exported |

The model has no local comment delete, so there is no local-delete row. The
property is the idempotence key: a crash between `POST` and the commit is
repaired by the pairing row next run, never by a second `POST`. The comments
a run posts pair by the ids `POST` returned, so only that crash needs the
property to come back. There is no digest-matching fallback: it would pair a
comment the runner typed in Jira with an identical unexported local one, and
lose the local one. If the live spike (`0a4390d`) shows `expand=properties`
is not honoured on the list, the repair is a `GET
/comment/{id}/properties/git-work` for each unpaired comment by the token's
account, not the fallback. An exported comment is authored in Jira by the
token's account; its property records the local op. Pending entries of
comments are keyed `comment:<Jira id>`, or `comment:<op>` before one exists.

### JS13 — One issue at a time: Jira writes first, then one commit decided under the lock

1. **Candidate** (JS20). Skip when the search's `updated` equals
   `base.Updated`, `local == base`, `base.Retry` is empty, the issue is not
   `Gone`, and the run is not `--full`.
2. **Read** `R = GET /issue/{id}?fields=<the mapping's>&properties=git-work`
   and its comments; ensure identities for the accounts they name (JS16),
   each its own commit. A 404 or a key outside the project is not a failure
   but JS19's business.
3. **Plan**, lock-free: `plan₁ = merge(B, L, fromIssue(R), export=true)`;
   `toWrites` turns `plan₁.Remote` into writes, one per local key.
4. **Write**, each independent, success recorded per key: one `PUT` with every
   edit; the transition; link adds and removes; comment creates and edits.
   A `PUT` refused with per-field `errors` is retried once without those
   fields, which become pending; a 400 naming no field sent fails the edit.
   The transition is the one whose `to.id` is the target; a required
   `resolution` is filled with the first of its `allowedValues` (pending
   would block "done" on most company-managed workflows); any other required
   field, or no such transition, is pending; a 409 re-reads the transitions
   and retries once.
5. **Re-read** `R′ = GET` if anything was written, else `R′ = R`. A key
   written this run that `R′` still shows at its `R` value is
   *unconfirmed* — a stale read, not a Jira edit: it keeps its prior base
   (I2) and is a `Skip{Retry}` for step 6, so `plan₂` never imports the
   pre-write value over local; the next run reads it again and, if Jira
   still holds the old value, exports again. Unconfirmed a second run in a
   row (the key is in the base's `Retry`), Jira holds its value — it
   normalised ours back to it, as a trimmed summary does, or someone set
   it — so the key loses its base and `plan₂` imports Jira's value with a
   conflict note: two runs, never a loop and never a silent revert
   (`TestAdvNormalisedBackToOld`).
6. **Commit**, `IssueCache.Update`: `B′ = B` with each written scalar and
   text set to the form written (a set's base stays `b`: against `r′ =
   merged`, the second merge reaches the merged set locally, where `B′ =
   merged` would remove Jira's additions); under the lock, on the fresh
   snapshot `L′`, `plan₂ = merge(B′, L′, fromIssue(R′), export=false)`; the
   ops are `plan₂`'s local changes, each pre-checked per key with the run's
   checker (`admit`: a refused key keeps its old base and moves to `Retry`,
   so `Update`'s own check never refuses the batch), the conflict note, the
   `SetMetadata`s for new pairings, and a marker when `plan₂`'s base differs
   from the current (JS8). Keys `plan₂` still wants to export are reported
   pending, not written.

Every local decision is made under the lock on the entity as it is then, so a
user editing during steps 3–5 is seen by `plan₂` — a double edit if Jira
changed that key, pending otherwise. The lock is never held across the
network. Setting `B′[k]` to what was written makes each confirmed success
collapse to row 1, each failure stay a local change, and each Jira
normalisation arrive as an ordinary import.

| stops after | state | next run |
| --- | --- | --- |
| 1–3 | nothing written | identical run |
| part of 4, or 5 | some Jira writes, nothing local | a written key is `l == r`: converged; an unwritten one is still local: written; comments pair by property, creates by their attempt and property (JS15) |
| the `POST /issue` answer lost | the issue may exist in Jira | the `jira-create` attempt, committed before the `POST`, is in doubt: found by property, linked; not found, pending until `Settle`, then created again (JS15) |
| 4, refused (400) | Jira refused a key | pending every run until fixed on either side |
| 4, no transition | status diverged | pending with the reason; a later Jira status change imports, with a conflict |
| 6, schema check | a value the schema lacks | that key `Retry`, the rest commits |
| 6, lock timeout | nothing local | as "part of 4" |

A local edit made after a crash on a key whose `PUT` landed becomes a double
edit that Jira wins, noted; accepted.

### JS14 — Echo suppression falls out of I2; nothing detects an echo

Our export, next run: step 6 recorded `b = l = r = v`, and the base's
`Updated` is `R′`'s, which the search returns, so step 1 skips it. Our
import, next run: `b = r = l`, so the key is not a local change. Jira
normalising what we wrote: `plan₂` sees `b′ = l`, `r′ ≠ b′` and imports the
normal form in the same commit; the next run is quiet. Overlapping windows
re-return synced issues, which step 1 skips with no `GET`.

### JS15 — Creates in both directions, and linking pre-existing aliases

**Local to Jira.** An unarchived local issue of a mapped type with no
`jira-id` is exported; `sync ID...` of one archived or of a local-only type
reports why it is not. Jira has no idempotency key and its search lags, so
the **create attempt** is what stands between a lost answer and a duplicate:

1. A `NoOp` with `jira-create: <Jira's now>` is committed on the issue,
   under the write lock, **before** `POST /issue`. Jira's now is the `Date`
   of its last response, so the attempt compares with Jira's `created` with
   no client skew. It is in the store, not the state file, so losing the
   state never duplicates (`TestAdvCrashAfterCreateStateLost`), and a clone
   that pulls the attempt sees it; the cost is one commit per `POST`.
2. `POST` carries project, `issuetype.id`, summary, description, the property
   `git-work={"id":…}`, and each field the create screen marks required with
   no default that local holds (a set stated whole) — nothing else, so a
   per-field refusal cannot fail a create that Jira would take. On `201` the
   issue is `GET` as `R` and continues from step 3 with a **create base**: a
   sent key's `B[k] = L[k]` (so Jira's normal form imports in `plan₂`);
   another key's `B[k] = R[k]` when local holds a value (so it is written now,
   in the one `PUT` and the transition every create pays anyway) and `L[k]`
   when local is null or empty (so a Jira default such as priority imports
   rather than being cleared forever); comments are local-only and exported.
   Step 6 adds `jira-id` and `alias:jira` by `SetMetadata` on the create op,
   which ends the doubt. An ordinary merge against the created issue as base
   would export `null` over every Jira default; the create base is why it
   differs.
3. A definitive answer — a 4xx other than 408 and 429 — says nothing was
   made: the attempt is recorded in the state's `Refused` with the issue's
   edit lamport, is not in doubt, and is neither `POST`ed nor committed again
   until the issue changes (the line is `failed` every run with Jira's
   reason). Any other error leaves the attempt in doubt.
4. The latest attempt of an unlinked issue, unless refused, is **in doubt**,
   resolved in `create` itself, the path `sync ID...` shares: one search per
   run, `project = P AND created >= "<oldest attempt in doubt − Overlap>"`
   with the property, and the lower Jira id whose `git-work.id` names the
   issue is linked (`linkCreated`, which checks the property by `GET` (I4)
   and resumes as a create). Not found, the issue is pending while the
   attempt is younger than `Settle` (15 minutes, an option: longer than any
   index lag seen), and created again after. A lost `Refused` makes a refused
   attempt look in doubt: one `Settle` of waiting, never a duplicate.

A crash between `POST` and step 6 is also repaired by the ordinary search:
every hit whose `git-work.id` names a local entity without `jira-id` is
linked, not imported; one whose entity this clone has not pulled is skipped
("pull first"), never imported, which would make a second entity. The search's
lower bound needs no attempt term: a created issue is always later than the
cursor, which only moves to hits seen before any `POST`. Two Jira issues
naming one entity: the lower id links, the other is skipped and reported as a
duplicate every run. A run holds a non-blocking `flock` on
`.git/git-work/jira/sync.lock` for its whole length, so two cron runs cannot
both `POST` one issue; the second exits with "a jira sync is already running",
having done nothing, and the kernel drops the lock when a process dies, like
the write lock's. `--dry-run` takes no lock.

**Jira to local.** A search hit with no link and no property is imported:
`merge(nil, empty, R)` decides it like any issue, the same pre-check as step 6
(`admit`) drops what the schema refuses into `Retry`, and the admitted fields
and body go to `Issues().NewRaw(reporter identity, created, summary,
description text, fields, {jira-id, alias:jira, jira-sync})` — the create op
is the first marker. An empty set or a null is not stored. Comments follow in
the ordinary step 6. A hit of an unmapped type is skipped silently, before any
`GET`: after `Derive`, every issue type is mapped or one the schema excludes
(`aliases: {jira: ""}`).

**Link requests.** A local issue with `alias:jira` and no `jira-id` is linked,
never created: `GET` by key, with the property; its `git-work` property
naming another entity is a skip; a 404 is a skip ("alias PROJ-9 names no Jira issue") — creating would
give a different key that the immutable alias could never follow. Otherwise
step 6 adds `jira-id` and the merge runs with no base: scalars take Jira's
value with a conflict note where they differed, sets union. This is how a
first sync with both sides populated avoids duplicates, `--dry-run` first.

### JS16 — Identities

A Jira user is an identity with immutable metadata `jira-account-id`,
found by `ResolveIdentityImmutableMetadata`; an unknown one is
`Identities().NewRaw(displayName, email if non-empty, login = accountId when
displayName is empty, "", nil, {jira-account-id})`, before step 6 and never
inside `Update` (the lock is not re-entrant). Email adoption is not
attempted, because Cloud hides most emails. At run start the `/myself`
account tags the current user identity by `SetMetadata` when no identity
carries it; one that does is left as it is. Two identities carrying one
account (two clones, JS25) resolve to the lower id, and both export as it.
Field imports, markers, notes and tombstones are authored by the current
user (the runner) — at Jira's `updated` for imports, at now for the rest.

### JS17 — Relations, and what cannot convert now

Relations resolve through the `Index`, built once per run from excerpts'
`jira-id` and identities' `jira-account-id`, and extended as the run imports
and creates. Links follow `api-vetting.md` C1: in `POST /issueLink`
`inwardIssue` is the source, so `{inwardIssue:A, outwardIssue:B, type:Blocks}`
means A blocks B; viewing A, `issuelinks` holds `{outwardIssue:B}`. The
relation is stored on the source only, so `fromIssue` reads only entries with
`outwardIssue`, and removal deletes the link id found there. `comment` is
never sent on a link. Parent is `fields.parent {id}`; its removal is
`update.parent [{"set":{"none":true}}]`.

A target outside the project (its key's prefix is not the bound key), or of
an issue type the schema does not map or excludes (the link's
`outwardIssue.fields.issuetype`, the parent's own), can never import and is
**dropped**: a scalar relation is excluded from the merge for that issue, a
multi item from both sides, so nothing is imported, exported or cleared. An
in-project target not yet in the `Index`, an account whose identity creation
failed, or a Jira value the schema lacks is a `Skip{Key, Reason, Retry:
true}` on the remote side: the key is left alone and recorded in the
marker's `Retry`, which makes the issue a candidate every run until it
converts. At the end of a run that imported or created an issue, the issues
that came back with `Retry` keys are re-run once, which resolves a child
imported before its parent. A local
value Jira cannot hold — a local-only status or target, a target not in Jira
yet, a dead alias, a label with a space, a summary over 255 runes, an identity
without an account — is a `Skip` from `toWrites`, never `Retry`: pending, and a
candidate anyway because `local ≠ base`. A link item whose target is not in
Jira is skipped alone; the key's other items are still written.

### JS18 — A Jira type change is imported; a local one is pending

`type` is import-only in v1. The merge takes `type` first, then maps the
remaining keys with the new type's mapping; base keys the two types share
carry over, because value ids are slugs of names. `Update` checks each
operation against the type the operation list sets, not the snapshot's old
one. A local type change on a linked issue is pending: "change the type in
Jira".

### JS19 — Deletes and moves: `Gone`, found only under `--full`

Polling by `updated` never sees an issue vanish. Under `--full` the search
returns every id of the project; a linked issue whose id is missing and not
`Gone` is read with `GET`. A 404 (deleted, or hidden: Jira will not say) is
`Gone: deleted`; a 200 in another project is `Gone: moved`. A sync that meets
either on its own `GET` — a locally edited issue, a lagging index still
returning a deleted one — is no failure: incrementally it is a skipped line
("…; --full marks it gone"), and under `--full` it goes to the Gone pass
whatever the search returned (`TestJiraDeleteSeenByGet`). The status field,
under its own key, is set to the first value in its canceled category, a note
with `jira-note = deleted` says why, and a `Gone` issue is never exported.
Never `rm`. **The one exception to I2**: the marker's base status is that
canceled value, so that when the issue answers again (permission regained),
Jira's status imports instead of the local canceled being pushed onto it.
Any later successful merge clears `Gone`, and exports the local edits made
while it was gone, like any local edit.

More than 10 missing issues in one scan marks none of them and fails the
run: a permission change looks exactly like a mass delete, and `/myself`
(I4) already rules out a missing credential. `--accept-deletes` marks them.

### JS20 — Candidates, the cursor, and time zones

A run's candidates, in this order: link requests (JS15); search hits of
`project = P AND updated >= "<lower bound>" ORDER BY updated ASC, id ASC`;
linked issues with `local ≠ base` or a non-empty `Retry`; unlinked local
issues to create. "Locally changed" is computed from snapshots, and an issue
whose excerpt's edit lamport equals the one the state file recorded after its
last converged sync (`Seen`, never recorded while it has `Retry` keys or
pending ones) is not even read. A `Gone` issue edited locally is reported
pending by every incremental run, without a `GET`. `ID...` takes exactly those issues (id prefixes or aliases, a Jira
key included), with no search, no cursor and no `Gone`. `--full` searches
`project = P` with no lower bound, ignores step 1's skip, and runs JS19.

The cursor is Jira's own `updated` (UTC in the state file), so client clock
skew is irrelevant. At the end of a run it becomes the greatest `updated`
reached; unreached hits are later than it. A hit that failed is kept in the
state's `Failed` and re-read by `GET` on the next run (at most 100 a run),
until it syncs, is dropped on a 404 or a move, so one issue failing forever
(a 403, a value the schema refuses on every run) costs one `GET`, not a
window that only grows. It is never named in the JQL: Jira refuses a whole
query naming an id it cannot see, and a failed hit is most often an issue
since deleted or hidden (`TestAdvFailedHitThenDeleted`). The lower bound is `cursor − Overlap` (5
minutes), converted
to `/myself.timeZone` and truncated to the minute, formatted
`"yyyy/MM/dd HH:mm"`: JQL literals are read in the **user's profile zone**
(C6), which can differ from the zone responses are rendered in. The overlap
covers minute truncation and index lag. A DST fold is an hour, which no
overlap covers: in a fold's first pass `jiraapi.JQLTime` names the instant
before the fold, so the bound errs an hour early, never late. Response
timestamps are parsed with their offset (`2006-01-02T15:04:05.000-0700`, `Z`
and colon forms accepted), never assumed `+0000` (C4). The binary embeds
`time/tzdata`, so any IANA zone loads; an empty zone fails the run. A missing
state file starts from `max(base.Updated) − Overlap` over the store's markers,
not a full search. The file records the site and project; a mismatch resets it.

### JS21 — Conflicts: a note on the issue and a line in the report

A double edit that Jira wins writes, in the same commit as the import, **one
note per issue per run**: a comment authored by the runner, metadata
`jira-note = conflict`, listing every overridden key:

```
Jira sync, 2026-09-28 14:03 UTC: Jira's values replaced local edits.
- status: local "in-review" -> Jira "done"
- body: the local description edit is kept in its history
- comment 10231: the local edit is kept in its history
```

A comment is visible where people look (`issue get`, the show view); a
"has conflicts" field would need defining on every type and clearing. The
note is never exported: Jira canonical means Jira's users are not told about
local values their edits replaced. There is no `jira-overrode` op metadata:
the overridden value is already an operation in the log, and the note is
what makes it not silent.

### JS22 — The commands and the report

```
git work jira schema [--format yaml|json] [-v]
git work jira sync [ID...] [--dry-run] [--full] [--accept-deletes] [--format json|text]
```

`jira schema` is a reader: `Derive` over the live schema, the document on
stdout, warnings and errors on stderr, and the info notes with `-v`.
`jira sync` is a writer in the remote group beside `push`/`pull`; it never
pushes. Its notes reach stderr only in a run that changed the schema, since
they say the same thing every run and cron mails output. Output is one JSON
object per line, like `log`: an optional schema line, one line per issue the
run touched, left pending, skipped or failed on, then a summary.

```json
{"schema":[{"action":"update","shape":"field","key":"task/status","id":"…","set":{"alias_jira/wont-do":"10005","values/wont-do":{…}}}]}
{"issue":"0a4390dd…","jira":"PROJ-12","action":"updated","imported":{"status":"done","body":"First line of the new descrip…"},"exported":{"priority":"high"},"comments":{"imported":1,"exported":0,"edited":0,"tombstoned":0},"conflicts":[{"key":"assignee","local":"a3a2829…","jira":"5b10ac8d…"}],"pending":[{"key":"status","reason":"no transition from In Progress to In Review"}]}
{"summary":{"imported":3,"created":1,"updated":7,"linked":0,"gone":0,"conflicts":1,"pending":1,"failed":0,"skipped":0,"unchanged":212,"cursor":"2026-09-28T21:02:00Z"}}
```

`action` is `imported` (new locally), `created` (new in Jira), `updated`,
`pending` (nothing moved; what waits is in `pending`), `linked`, `gone`,
`skipped` (refused or waiting, reported: a person may look) or `failed` (with
`"error"`). An issue with nothing to do is not a line but counted in
`unchanged`. The body appears in `imported`/`exported` as its first line, at
most 60 characters; a conflict on a text repeats no text; `retry` appears
only when true; empty members are omitted, the cursor too when there is
none. `--dry-run` reads both sides and writes neither — no schema import, no
issue or identity write, no Jira write, no state file — printing `plan₁` with
`"dry_run": true`; it cannot predict a transition that fails or a
normalisation Jira applies, and keys whose values need the schema changes or
the identities it did not write show as pending. `--format text` is one line
per issue. Exit status is 0 when every issue synced or is only pending, 1
when any failed, the run stopped early, deletes were held, or another run
holds the lock (JS15). A pending key of `*` is the whole issue.

### JS23 — Failure classes

A request fails **for the issue** (400, 403, 404, 409 after its retry, 413)
or **for the run** (401 "token invalid or expired", 429 or 5xx after its
retries, a network error). An issue failure is reported and the run goes on;
a run failure stops the run, saves the state file with the cursor rule of
JS20, and exits 1. What the engine does with a key it cannot sync is one
value, `Skip{Key, Reason, Retry}`: leave the key alone this pass, and retry
next run or not (JS17). `pending` in the report is every `Skip` plus every
local change `plan₂` did not export.

### JS24 — Rate limits and retries belong to the client

The client retries 429, and 503 on anything but a `POST` that is not a
search (a create is not idempotent, `TestPostNotRetriedOn503`); it waits
`Retry-After` (jittered up to +30 %, and returns the error when it asks for
more than 60 s), else backs off exponentially from 2 s, capped at 30 s,
jitter ×0.7–1.3, at most 4 retries; a request times out after 60 s, a
transport error like any other. The run-failure split (JS23) is the
engine's, not the client's. The engine
batches every field into one `PUT`, which with a transition, links and
comments stays under the per-issue limit (20 per 2 s) for any one issue; a
429 past it is waited out by `Retry-After`. It never sends `notifyUsers` (C5: `false`
without admin fails the whole edit), never calls `/search` (410, C7), always
sends `Content-Type: application/json`, pages `/search/jql` until `isLast`
or a missing, null or empty token, and never relies on a full page, and gives up an offset-paged read past 10000
items (a server ignoring `startAt`).

### JS25 — Two clones, duplicates, and E7

**One clone is bound.** Two bound clones that exchange before each sync lose
nothing and duplicate nothing: imports carry Jira's current value, the latest
lamport wins after merge, markers are chosen by `Updated`, pairings are facts
(I3), and a create is caught by the property. Their residual costs are two
clones exporting different values to one key (Jira last-writer-wins, no note)
and two clones deriving the same **new** Jira type or field (two config
entities with one key, E7, reported by every schema command; a new **value**
is an attribute and merges).

Two bound clones that sync **before** they exchange duplicate, and nothing
local can prevent it, because each has yet to see the other's work:

| race | result | what the sync does |
| --- | --- | --- |
| both import one new Jira issue | two local issues with one `jira-id` | the unarchived, then the lower entity id, is the `Index`'s and syncs; the other is reported every run and never syncs; archiving it silences it |
| both create one pulled local issue | two Jira issues with one property | after the exchange one `jira-id` wins on the create op (`SetMetadata`, first writer); the other Jira issue is reported as a second issue naming it (E24) |
| both create an identity for one account | two identities with one `jira-account-id` | the lower id is the account's; both export as it; the runner is never re-tagged |

A marker naming its clone was considered and rejected as a guard: it catches
only what an exchange already made safe, and it would forbid handing the
binding to another clone. `Compile` refuses two entities carrying one alias
for one system (types, fields of a type, values of a field), which two clones
can produce by merging; the check lives there rather than in `Reconcile`, so
`schema` stays alias-agnostic.

### JS26 — One fake, over HTTP, shared by every test

`jira/jiratest.Server` is the single double for the client tests, discovery
and the engine scenarios, instead of an in-memory client, a file-serving
getter and recorded fixtures. It imports nothing from `jiraapi`, so a wrong
struct tag fails a test instead of agreeing with itself. Every documented
behaviour is its default and every unverified one is a switch;
`TestAdvSwitches` runs one scenario covering E1–E5 under each switch, and the
crash matrix runs under both ADF modes. Always on: `PUT` all-or-nothing with
per-field errors; Jira's normalisation (sorted labels, trimmed summary,
float rounding, ADF with `localId`s); summaries over 255 and labels with
spaces refused; links stored `{source: inward, dest: outward}`; properties
inline on create and indexed with the search's lag; deleted issues dropped
from search, and a JQL `id`/`key` naming one refused; 410 on `/search`; the
token omitted on the last page; 429 with `Retry-After`; no `Authorization`
header is 200-with-nothing on search and 401 on `/myself`.

## Packages

```
jira/jiraapi/   Cloud v3 client, wire types, ADF codec, JQL literals, time parsing. Standard library only.
jira/jiratest/  the fake Jira over httptest (JS26). Imports nothing from jiraapi.
jira/           discovery, Derive, Compile and the Mapping, the merge, the engine, the state file.
host/jira.go    binding, credential, discover, derive + SchemaImport, compile, the sync lock, Sync.
commands/jira/  schema, sync.
```

New code does not live in `bridge/`, which is bug-coupled and leaves with
`entities/bug`; nothing here imports `bridge/...`. The engine uses the
concrete cache and client: tests run a real test repository against the real
client pointed at `jiratest`, which exercises what production runs. Godoc is
the authority for signatures; what follows is where each decision lives.

| file | holds |
| --- | --- |
| `types.go` | the seam: metadata keys, `Note`/`Level`, `Doc`, `Skip`, `Change`, `jiraWrite` |
| `project.go` | `Discover` → `Project` (JS4) |
| `derive.go`, `slug.go` | `Derive` (JS5–JS7), `slug`, `normName` (JS6) |
| `compile.go` | `Compile` → `Mapping` with the binding and duplicate-alias checks; `Mapped` (JS5, JS25) |
| `mapping.go`, `value.go` | `Mapping.Local`, `fromIssue`, `toWrites`, `createBody`; canonical values (JS7) |
| `index.go` | `Index`: Jira id ↔ entity id, accountId ↔ identity (JS17, JS25) |
| `merge.go`, `base.go` | `merge`, `form`, `decide`, `mergeSet`, `mergeComments`; `Base`, `CurrentBase`, `digest` (JS8–JS12) |
| `engine.go` | `Sync`, `Options`, the run/issue failure split `runFatal` (JS23), `report` |
| `candidates.go` | the scan, the search, failed hits, `changed`, the re-pass (JS17, JS20) |
| `issue.go`, `write.go` | JS13: `syncLinked`, `converge`, `unconfirmed`, `commit`, `admit`; the Jira writes and transitions |
| `create.go` | creates, attempts, `findCreated`, `linkCreated`, link requests, imports, Gone (JS15, JS19) |
| `report.go`, `state.go` | `Line`, `Summary`; `State` (JS8, JS20, JS22) |

`jiraapi` has one `Client` method per endpoint the sync calls and one
`*Error` with the status and per-field errors; `jiratest` models every
documented behaviour by default and every unverified one as a `With…` option.

**Outside `jira/`**, the whole footprint: `cache.IssueCache.Update(fn)`
(the write lock, a re-read, `fn` on the fresh snapshot, its operations
schema-checked as one change against the type they set and committed behind
one ref update; `fn` calls no cache writer and no network, and nothing else
stages on the entity meanwhile); `issue.NewNoOpOp`, the marker's
constructor; in `schema`, the reserved `alias_` prefix, `AliasName`,
`ValueAliasName` and `Aliases` on documents and entities (Reconcile writes
stated ones and never removes one); `gitcli.CredentialHelper`, reached as
`RepoCache.Credential`; and `host.JiraSchema`/`host.JiraSync`, the one path
for commands (and Starlark in v2). No pristine package is touched: identity
tagging uses `Identity.SetMetadata`, which already appends a version.

## Tests

The tests are the plan, named after what they check:

- **Merge**, pure tables: `TestMerge*` in `merge_test.go` — scalars, sets,
  missing bases, the body and its lossy guard, `Skip{Retry}`, `export=false`,
  every JS12 row, normalisation, type first, `CurrentBase`.
- **Schema**: goldens of `Discover` and `Derive` over
  `current ∈ {empty, jira preset, schema.yaml} × {company, team}`
  (`testdata/`), each validated and a fixpoint on re-derive; renames, dead
  aliases, exclusions, categories, mis-binding, duplicate aliases, the slug
  table (`derive_test.go`, `project_test.go`); the conversion round trip per
  kind (P1, `TestConversionRoundTrip`).
- **Client and fake**: `jiraapi` against `jiratest` (paging, retries,
  `Retry-After`, zones, ADF, no `notifyUsers`, no `/search`, 409), and
  `jiratest`'s self-tests of every behaviour it models.
- **Engine**, a real repository against the fake: the scenarios E1–E25 in
  `scenario_test.go` (echo, import, normalisation, crashes, creates, links,
  Gone, moves, rate limits, dry run, `ID...`, link requests, two clones, the
  lock), and `adversarial_test.go`: the crash matrix at every write of a run
  (before and after it lands, state saved or not, both ADF modes), crash
  after a create with the state file lost, refused creates, concurrent local
  commits, stale reads and unconfirmed writes, failed hits deleted, time
  zones, DST, clock skew, type changes, moves, links to deleted issues, two
  clones' races, lossy texts, unwritable values, and a seeded random-edit
  property run (`TestAdvPropertyRandomEdits`).

## Earlier tasks

Each difference was posted as a comment on its task, per the working
conventions: `33148f2` (state-based, no changelog in v1), `69b7be0` (aliases
on the schema entities; `jira schema` replaces `bridge new`'s introspection;
the ordinary `host.SchemaImport`, not a reconcile inside
`ConfigCache.Update`), `3c6d07a` (a `NoOp` marker; a note and the op log, no
`jira-overrode`; a type without a canceled value keeps its status; the cursor
is Jira's `updated`), `32d372e` (`jira-id` plus `alias:jira` by
`SetMetadata`; per-key pending entries), `a3a8d16` (`git work jira sync`;
`--watch` deferred, cron is the loop), `cli-convention.md` (`bridge` verbs
become `jira schema|sync`) and `config-entity.md` (the reserved `alias_`
family, stated aliases never removed).

## As implemented

- **E14**: `entity/dag` (pristine) fails to read a merged history whose two
  branches differ in length ("creation lamport time not set",
  `doc/design/dag-read-order.md`). It is independent of the sync and flagged
  rather than fixed; the two-clone scenario diverges by one commit per side.
- **Fake gaps**: `jiratest` cannot add a status mid-run, so E20 (a Jira
  status the schema lacks → `Retry`, then imported after the next schema
  step) is untested end to end; datetime custom fields are covered by
  conversion tests only (`TestDatetime`).
- **Awaiting the spike** (`0a4390d`): whether `GET /issue` is
  read-after-write consistent (the `unconfirmed` guard exists for
  `WithStaleReads`), whether `expand=properties` is honoured on the comment
  list (JS12), whether an issue property reaches the search index with the
  same lag as fields (I4), and how Jira answers JQL naming a deleted id
  (JS20; the fake answers 400, the worse case).
