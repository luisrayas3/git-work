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

**Status:** design, awaiting approval. Written 2026-09-28 against trunk
`dcf08a29`. No Jira site exists (`de1d8fb`, `0a4390d`), so every Jira
behaviour below comes from the vetted API reference, and every behaviour the
vetting could not settle is a switch on the fake server (JS26), with the
design choosing what works either way.

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
is reported pending and `base[k]` is unchanged. `Merge(b, l, r)` with
`l == b == r` emits nothing. Everything that could loop violates this.

**I2 — The base is Jira-observed, and moves only with local.** `base[k]` is
written only in the commit that makes `local[k]` equal to it, and its value
is one Jira was seen to hold (`R` or the post-write `R′`), converted by
`FromJira`. The export path never writes a base directly: it sets
`B′[k] = written` and lets the second merge import Jira's normal form. A
crash can only leave a base *older* than both sides, which the first merge
row (`l == r` ⇒ converged) absorbs. One exception, `Gone` (JS19).

**I3 — Links are immutable facts on operations.** An issue's Jira id is
metadata on its create operation, a comment's Jira id metadata on its
add-comment operation, each set once — at creation or by one `SetMetadata`
— and never inferred from a marker. Merging clones' histories can never
un-pair or re-pair anything; two markers can disagree only on values.

**I4 — Destructive decisions come from the database, not the index.**
Merges, deletes and link repairs use `GET`; search only answers "which ids
might have changed". A run starts with `GET /myself` and aborts on anything
but 200, because a missing `Authorization` header is 200-with-nothing.

**P1 — `FromJira` is deterministic and idempotent over one round trip:**
`FromJira(jira(ToJira(FromJira(x)))) == FromJira(x)`, where `jira()` is the
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
token never reaches argv. The review placed the environment override inside
`gitcli`; it belongs in `host`, because `gitcli` is git's environment and
`JIRA_API_TOKEN` is not git's.

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
never states `prune`, an archive, or a kind change, and — against the scratch
design, as the review argued — never removes a `target_types` entry Jira's
hierarchy forbids: a forbidden local parent is a 400 on export, i.e.
pending, and "Derive never removes" is one invariant instead of a list of
exceptions. Schema writes are the runner's, at now, like any import.

**The binding check is in `Derive`, not only in `Compile`.** The review moved
it to `Compile`, but `Compile` runs after `SchemaImport`, so a clone bound to
the wrong project would already have adopted that project's types into the
tracker. `Derive` returns an error when no non-empty type alias names an
issue type of `p` (while any type alias exists): "the schema maps issue types
10001, 10002 that PROJX does not have; is this the project the tracker was
mapped against?". `Compile` checks it again, with the duplicate-alias check
(JS25).

**The first mapping is explicit.** `jira sync` refuses while no type has a
non-empty `alias_jira`, naming the review recipe:

```sh
git work jira schema > jira.yaml          # derived document; report on stderr
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
aliasing is refused (JS25), so `ToJira(FromJira(x)) == x`.

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
	Fields   map[string]issue.Value `json:"fields"`             // mapped keys, local terms, title and type included
	Body     string                 `json:"body"`               // Digest of comment #0
	Comments map[string]string      `json:"comments,omitempty"` // Jira comment id -> Digest; "" once deleted in Jira
	Retry    []string               `json:"retry,omitempty"`    // keys skipped with Retry (JS17)
	Gone     string                 `json:"gone,omitempty"`     // "deleted" | "moved" | "" (JS19)
}
```

Values are in local terms (schema keys, value ids, entity ids), so the base
compares with the snapshot as it is. Fields are whole, because a set merge
needs the base's items; texts are digests, because they only need equality.
Every marker is the whole base, so reading it is reading one operation.
**The current base is the marker with the greatest `Updated`**, ties to the
later in compiled order: the base describing the newest Jira state best
predicts Jira. An undecodable marker is skipped and reported (D6). A marker
is written only when `Plan.Base` differs from the current base, so a quiet
run commits nothing.

**Every key the sync writes:**

| where | key | value |
| --- | --- | --- |
| create op | `jira-id` | Jira issue id — the link (I3) |
| create op | `alias:jira` | Jira key at link time; resolves anywhere an id does |
| create op of an imported issue; `NoOp` | `jira-sync` | `Base` JSON |
| add-comment op | `jira-comment-id` | Jira comment id (I3) |
| add-comment op of a note | `jira-note` | `conflict` \| `deleted` |
| identity, immutable | `jira-account-id` | Jira accountId |
| Jira issue property | `git-work` | `{"id":"<entity id>"}` |
| Jira comment property | `git-work` | `{"op":"<add-comment op id>"}` |

`jira-id`, `alias:jira` and `jira-comment-id` are set at creation on an
import and by `SetMetadata` in the step-6 commit on an export, never
elsewhere. `alias:jira` is never refreshed in v1: after a move the old key
still resolves locally and in Jira. Local, disposable run state — the cursor,
the create journal, the per-issue edit lamport at last sync — is in
`.git/git-work/jira/state.json`; losing it costs a slower run, never a
wrong one.

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

The description (comment #0) follows the scalar table on digests; a text is
never merged within itself. `Digest(t)` is `"v1:" + hex(sha256("v1\n" +
norm(t)))`, where `norm` turns CRLF into LF, trims trailing space on every
line and trailing newlines; the version is in the input, so a later
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
repaired by the pairing row next run, never by a second `POST`. The review
dropped the scratch design's digest-matching fallback; it stays dropped until
a live run shows comment properties are not returned. An exported comment is
authored in Jira by the token's account; its property records the local op.

### JS13 — One issue at a time: Jira writes first, then one commit decided under the lock

1. **Candidate** (JS20). Skip when the search's `updated` equals
   `base.Updated`, `local == base`, and `base.Retry` is empty.
2. **Read** `R = GET /issue/{id}?fields=m.Request()&properties=git-work` and
   its comments; ensure identities for `m.Users` (JS16), each its own commit.
3. **Plan**, lock-free: `plan₁ = Merge(B, L, FromJira(R), export=true)`;
   `ToJira` turns `plan₁.Remote` into writes, one per local key.
4. **Write**, each independent, success recorded per key: one `PUT` with every
   `Edit`; the transition; link adds and removes; comment creates and edits.
   A `PUT` refused with per-field `errors` is retried once without those
   fields, which become pending. `TransitionTo` takes the transition whose
   `to.id` is the target; a required `resolution` is filled with the first
   of that transition's `allowedValues` (the review made it pending, which
   would block "done" on most company-managed workflows, and needs no
   discovery); any other required field, or no such transition, is pending.
5. **Re-read** `R′ = GET` if anything was written, else `R′ = R`.
6. **Commit**, `IssueCache.Update`: `B′ = B` with each written key set to the
   value written; under the lock, on the fresh snapshot `L′`,
   `plan₂ = Merge(B′, L′, FromJira(R′), export=false)`; the ops are
   `plan₂`'s local changes, each pre-checked per key with the run's checker
   (a refused key moves to `Retry`), the conflict note, the `SetMetadata`s
   for new pairings, and a marker when `plan₂.Base` differs from the current.
   Keys `plan₂` still wants to export are reported pending, not written.

Every local decision is made under the lock on the entity as it is then, so a
user editing during steps 3–5 is seen by `plan₂` — a double edit if Jira
changed that key, pending otherwise. The lock is never held across the
network. Setting `B′[k]` to what was written makes each success collapse to
row 1, each failure stay a local change, and each Jira normalisation arrive
as an ordinary import. The review's call to drop the scratch design's
"unconfirmed write" guard is accepted: it looped whenever Jira normalised our
write back to the value it already held.

| stops after | state | next run |
| --- | --- | --- |
| 1–3 | nothing written | identical run |
| part of 4, or 5 | some Jira writes, nothing local | a written key is `l == r`: converged; an unwritten one is still local: written; comments pair by property, creates by journal (JS15) |
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
`jira-id` is exported: journal `(entity id, now)` in the state file;
`POST /issue` with project, `issuetype.id`, summary, description and every
mapped field on the create screen, plus the property `git-work`; on `201`,
`GET` it as `R` and continue from step 3 with a **create base**: for a key the
body carried, `B[k] = L[k]` (so Jira's normal form imports in `plan₂`); for a
key it could not carry, `B[k] = R[k]` when local holds a value (so status,
links and the rest are written now) and `L[k]` when local is null (so a Jira
default such as priority imports rather than being cleared forever); comments
are local-only and exported. Step 6 adds `jira-id` and `alias:jira` by
`SetMetadata` on the create op and drops the journal entry. The review's
"ordinary merge against the created issue as base" would export `null` over
every Jira default; this rule is why it differs.

A crash between `POST` and step 6 is repaired by the property: every search
hit whose `git-work.id` names a local entity without `jira-id` is linked, not
imported. While a journal entry is younger than `Overlap` it forbids a
second `POST`; the search's lower bound is `min(cursor, oldest journal
entry) − Overlap`, so the created issue is inside the next search window —
the review dropped the scratch design's extra `created >=` query on the
assumption that the ordinary search covers it, which holds only with that
bound. Two Jira issues naming one entity: the lower id links, the other is
skipped and reported as a duplicate every run.

**Jira to local.** A search hit with no link and no property is imported with
`Issues().NewRaw(reporter identity, created, summary, description text,
fields, {jira-id, alias:jira, jira-sync})` — the create op is the first
marker, its base holding the fields and body; fields that fail the pre-check
are omitted and `Retry`. Comments follow in the ordinary step 6. An issue of
an unmapped type is skipped, reported once per run.

**Link requests.** A local issue with `alias:jira` and no `jira-id` is linked,
never created: `GET` by key; its `git-work` property naming another entity is
a skip; a 404 is a skip ("alias PROJ-9 names no Jira issue") — creating would
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
carries it; another identity already carrying it is reported, not merged.
Field imports, markers, notes and tombstones are authored by the current
user (the runner) — at Jira's `updated` for imports, at now for the rest.

### JS17 — Relations, and what cannot convert now

Relations resolve through the `Index`, built once per run from excerpts'
`jira-id` and identities' `jira-account-id`, and extended as the run imports
and creates. Links follow `api-vetting.md` C1: in `POST /issueLink`
`inwardIssue` is the source, so `{inwardIssue:A, outwardIssue:B, type:Blocks}`
means A blocks B; viewing A, `issuelinks` holds `{outwardIssue:B}`. The
relation is stored on the source only, so `FromJira` reads only entries with
`outwardIssue`, and removal deletes the link id found there. `comment` is
never sent on a link. Parent is `fields.parent {id}`; its removal is
`update.parent [{"set":{"none":true}}]`.

A target outside the project (its key's prefix is not the bound key) is
**dropped**: a scalar relation is excluded from the merge for that issue, a
multi item from both sides, so nothing is imported, exported or cleared. An
in-project target not yet in the `Index`, an account whose identity creation
failed, or a Jira value the schema lacks is a `Skip{Key, Reason, Retry:
true}` on the remote side: the key is left alone and recorded in the
marker's `Retry`, which makes the issue a candidate every run until it
converts. At the end of a run, issues that came back with `Retry` keys are
re-run once, which resolves a child imported before its parent. A local
value Jira cannot hold — a local-only status or target, a dead alias, a label
with a space, a summary over 255 runes, an identity without an account — is a
`Skip{Retry: false}` from `ToJira`: pending, and a candidate anyway because
`local ≠ base`.

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
`Gone: deleted`; a 200 in another project is `Gone: moved`. The status is
set to the first value in the canceled category of its type's status field
(`Mapping.Canceled`), a note with `jira-note = deleted` says why, and a
`Gone` issue is never exported. Never `rm`. **The one exception to I2**: the
marker's base status is that canceled value, so that when the issue answers
again (permission regained), Jira's status imports instead of the local
canceled being pushed onto it; a later search hit clears `Gone` by the
ordinary merge.

More than 10 missing issues in one scan marks none of them and fails the
run: a permission change looks exactly like a mass delete, and `/myself`
(I4) already rules out a missing credential. `--accept-deletes` marks them.

### JS20 — Candidates, the cursor, and time zones

A run's candidates, in this order: link requests (JS15); search hits of
`project = P AND updated >= "<lower bound>" ORDER BY updated ASC, id ASC`;
linked issues with `local ≠ base` or a non-empty `Retry`; unlinked local
issues to create. "Locally changed" is computed from snapshots, skipping any
issue whose edit lamport equals the one the state file recorded after its
last sync. `ID...` takes exactly those issues (id prefixes or aliases, a Jira
key included), with no search, no cursor and no `Gone`. `--full` searches
`project = P` with no lower bound, ignores step 1's skip, and runs JS19.

The cursor is Jira's own `updated` (UTC in the state file), so client clock
skew is irrelevant. At the end of a run it becomes the `updated` of the first
search hit that failed or was not reached, else the greatest seen. The lower
bound is `min(cursor, oldest journal entry) − Overlap` (5 minutes), converted
to `/myself.timeZone` and truncated to the minute, formatted
`"yyyy/MM/dd HH:mm"`: JQL literals are read in the **user's profile zone**
(C6), which can differ from the zone responses are rendered in. The overlap
covers minute truncation, index lag and a DST fold at once. Response
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
local values their edits replaced. The scratch design's `jira-overrode` op
metadata is cut, as the review argued: the overridden value is already an
operation in the log, and the note is what makes it not silent.

### JS22 — The commands and the report

```
git work jira schema [--format yaml|json]
git work jira sync [ID...] [--dry-run] [--full] [--accept-deletes] [--format json|text]
```

`jira schema` is a reader: `Derive` over the live schema, the document on
stdout, notes on stderr. `jira sync` is a writer in the remote group beside
`push`/`pull`; it never pushes. Output is one JSON object per line, like
`log`: an optional schema line, one line per issue the run touched or failed
on, then a summary.

```json
{"schema":[{"action":"update","shape":"field","key":"task/status","id":"…","set":{"alias_jira/wont-do":"10005","values/wont-do":{…}}}]}
{"issue":"0a4390dd…","jira":"PROJ-12","action":"updated","imported":{"status":"done"},"exported":{"priority":"high"},"comments":{"imported":1,"exported":0,"edited":0,"tombstoned":0},"conflicts":[{"key":"assignee","local":"a3a2829…","jira":"5b10ac8d…"}],"pending":[{"key":"status","reason":"no transition from In Progress to In Review","retry":false}]}
{"summary":{"imported":3,"created":1,"updated":7,"linked":0,"gone":0,"conflicts":1,"pending":1,"failed":0,"skipped":212,"cursor":"2026-09-28T21:02:00Z"}}
```

`action` is `imported` (new locally), `created` (new in Jira), `updated`,
`linked`, `gone`, `skipped` (reported, not synced) or `failed` (with
`"error"`). Text keys appear in `imported`/`exported` with their digest as
the value; empty members are omitted. `--dry-run` reads both sides and
writes neither — no schema import, no issue write, no Jira write, no state
file — printing `plan₁` with `"dry_run": true`; it cannot predict a
transition that fails or a normalisation Jira applies, and keys whose values
need the schema changes it did not apply show as pending. `--format text` is
one line per issue. Exit status is 0 when every issue synced or is only
pending, 1 when any failed, the run stopped early, or deletes were held.

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

The client honours `Retry-After` on 429 and 503; otherwise it backs off
exponentially from 2 s, capped at 30 s, jitter ×0.7–1.3, at most 4 retries.
It spaces writes to one issue under the per-issue limit (20 per 2 s) and
batches every field into one `PUT`. On 409 from a transition it re-reads the
transitions and retries once. It never sends `notifyUsers` (C5: `false`
without admin fails the whole edit), never calls `/search` (410, C7), always
sends `Content-Type: application/json`, pages `/search/jql` until `isLast`
or a missing, null or empty token, and never relies on a full page.

### JS25 — Two clones, duplicates, and E7

No value is lost when two bound clones sync: imports carry Jira's current
value, the latest lamport wins after merge, markers are chosen by `Updated`,
pairings are facts (I3), and duplicate creates are caught by the property.
Two residual costs are why one clone is bound: two clones exporting
different values to one key is Jira last-writer-wins with no note; two clones
deriving the same **new** Jira type or field create two config entities with
one key (E7, reported by every schema command). A new **value** is an
attribute and merges. `Compile` refuses two entities carrying one alias for
one system (types, fields of a type, values of a field), which two clones can
produce by merging; the check lives there rather than in `Reconcile`, so
`schema` stays alias-agnostic.

### JS26 — One fake, over HTTP, shared by every test

`jira/jiratest.Server` is the single double for the client tests, discovery
and the engine scenarios, instead of an in-memory client, a file-serving
getter and recorded fixtures. It imports nothing from `jiraapi`, so a wrong
struct tag fails a test instead of agreeing with itself. Every documented
behaviour is its default and every unverified one is a switch, and the
engine scenarios E1–E5 and E12 run in both positions of each.

## Packages and interfaces

```
jira/jiraapi/   Cloud v3 client, wire types, ADF codec, time parsing. Standard library only.
jira/jiratest/  the fake Jira over httptest (JS26). Imports nothing from jiraapi.
jira/           Project + Discover, Derive, slug, Compile + Mapping, Index, Merge + Base,
                the engine (Sync), the state file. Imports schema, entities/issue, entity,
                cache, jiraapi; the pure files (derive, mapping, merge, base) import no cache.
host/jira.go    binding, credential, discover, derive + SchemaImport, compile, sync.
commands/jira/  schema, sync.
```

New code does not live in `bridge/`, which is bug-coupled and leaves with
`entities/bug`; nothing here imports `bridge/...`, and `git work bridge`
stays untouched until then. Two packages named `jira` never meet in one file.
The engine uses the concrete cache and client: tests run a real test
repository against the real client pointed at `jiratest`, which exercises
what production runs.

### `jira/jiraapi`

```go
type Client struct{ /* unexported */ }
type Option func(*Client)

func New(baseURL, email, token string, opts ...Option) *Client
func WithHTTPClient(h *http.Client) Option
func WithSleep(sleep func(ctx context.Context, d time.Duration) error) Option // tests wait for nothing

func (c *Client) Myself(ctx context.Context) (*User, error)
func (c *Client) Project(ctx context.Context, key string) (*Project, error)
func (c *Client) ProjectStatuses(ctx context.Context, key string) ([]IssueTypeStatuses, error)
func (c *Client) Fields(ctx context.Context) ([]Field, error)
func (c *Client) CreateMeta(ctx context.Context, projectKey, issueTypeId string) ([]FieldMeta, error) // all pages
func (c *Client) Priorities(ctx context.Context, projectId string) ([]Priority, error)                // all pages
func (c *Client) LinkTypes(ctx context.Context) ([]LinkType, error)                                   // 404: *Error
func (c *Client) Search(ctx context.Context, q Search, page func([]Issue) error) error               // POST /search/jql
func (c *Client) Issue(ctx context.Context, idOrKey string, fields, properties []string) (*Issue, error)
func (c *Client) Comments(ctx context.Context, issueId string) ([]Comment, error) // expand=properties, all pages
func (c *Client) CreateIssue(ctx context.Context, body CreateIssue) (*Created, error)
func (c *Client) EditIssue(ctx context.Context, issueId string, e Edit) error // PUT; never notifyUsers
func (c *Client) Transitions(ctx context.Context, issueId string) ([]Transition, error) // expand=transitions.fields
func (c *Client) TransitionTo(ctx context.Context, issueId, statusId string) error      // PickTransition; 409: re-read, once
func (c *Client) AddComment(ctx context.Context, issueId string, body json.RawMessage, props []Property) (*Comment, error)
func (c *Client) EditComment(ctx context.Context, issueId, commentId string, body json.RawMessage) (*Comment, error)
func (c *Client) CreateLink(ctx context.Context, l NewLink) error // {inwardIssue: Source, outwardIssue: Dest} (C1); no comment
func (c *Client) DeleteLink(ctx context.Context, linkId string) error

type Search struct{ JQL string; Fields, Properties []string; MaxResults int } // Properties ≤ 5; MaxResults advisory
type User struct{ AccountId, AccountType, DisplayName, EmailAddress, TimeZone string; Active bool }
type Project struct{ Id, Key, Name, Style string; IssueTypes []IssueType }
type IssueType struct{ Id, Name, Description string; Subtask bool; HierarchyLevel int }
type IssueTypeStatuses struct{ Id, Name string; Statuses []Status } // Id, Name: the issue type
type Status struct{ Id, Name, Description, Category string }        // Category: statusCategory.key
type FieldSchema struct{ Type, Items, System, Custom string }
type Field struct{ Id, Name, UntranslatedName string; Custom bool; Schema FieldSchema; ScopeProject string } // "" = global
type FieldMeta struct{ FieldId, Name string; Required bool; Schema FieldSchema; AllowedValues []Option }
type Option struct{ Id, Name, Value string } // Name for priorities and resolutions, Value for options
type Priority struct{ Id, Name string }
type LinkType struct{ Id, Name, Inward, Outward string }
type Issue struct{ Id, Key string; Fields, Properties map[string]json.RawMessage }
type Comment struct {
	Id                   string
	Author, UpdateAuthor User
	Body                 json.RawMessage // ADF
	Created, Updated     time.Time
	Properties           map[string]json.RawMessage
}
type Property struct{ Key string; Value json.RawMessage }
type CreateIssue struct{ Fields map[string]json.RawMessage; Properties []Property }
type Created struct{ Id, Key string }
type Edit struct{ Fields, Update map[string]json.RawMessage } // a field in one of the two only
type Transition struct{ Id, Name, To string; Fields map[string]TransitionField } // To: target status id
type TransitionField struct{ Required bool; AllowedValues []Option }
type NewLink struct{ TypeId, Source, Dest string } // Jira issue ids; Source blocks Dest

// PickTransition is JS13 step 4: the transition to statusId, and the fields it
// must send (a required resolution filled from its allowedValues).
func PickTransition(ts []Transition, statusId string) (Transition, map[string]json.RawMessage, error)

var ErrNoTransition = errors.New("no transition to the target status")

type Error struct {
	Method, Path string
	Status       int
	Messages     []string          // errorMessages
	Fields       map[string]string // errors
	RetryAfter   time.Duration
}
func (e *Error) Error() string
func StatusOf(err error) int  // 0 when err is not an *Error
func RunFatal(err error) bool // 401, retries exhausted, transport: stop the run (JS23)

func ADFToText(doc json.RawMessage) (text string, lossless bool, err error)
func TextToADF(text string) json.RawMessage
func ParseTime(s string) (time.Time, error)        // -0700, Z and colon offsets; UTC out
func JQLTime(t time.Time, loc *time.Location) string // "2006/01/02 15:04", truncated to the minute
```

### `jira/jiratest`

```go
type Server struct {
	URL, Email, Token string
	// unexported: the database, the lagging index, the request log
}
type Option func(*config)
type Fixture struct{ /* project, types, workflows, fields, create screens, priorities, link types, users */ }

func New(t testing.TB, fx Fixture, opts ...Option) *Server // closed on t.Cleanup
func Company() Fixture // company-managed: Epic, Story, Task, Bug, Sub-task; custom workflows with
                       // "In Review" indeterminate, "Won't Do" done, "Ready" new; Story Points (float);
                       // a "Team" option field; a date picker; an unmapped textarea and cascading select;
                       // Blocks, Duplicate, Relates, Cloners; a same-name field scoped to another project
func Team() Fixture    // team-managed: project-scoped Epic, Story, Task, Subtask; jsw-story-points

// Switches: the default is the documented, or else the harder, behaviour.
func SearchLag(reads int) Option          // search answers from a copy n reads old; GET is consistent. Default 1
func CommentAddBumpsUpdated(b bool) Option  // default true
func CommentEditBumpsUpdated(b bool) Option // default false
func CommentProperties(b bool) Option      // comment properties returned with expand=properties; default true
func Zones(site, user string) Option       // default "America/Los_Angeles", "Europe/Berlin"
func PageSize(n int) Option                // default 37: never a full page
func Admin(b bool) Option                  // notifyUsers=false accepted; default false (400, C5)
func Clock(now func() time.Time) Option

// Jira-side edits, as a named user; each bumps updated like Jira does.
func (s *Server) AddUser(accountId, displayName, email string)
func (s *Server) Create(as, issueType string, fields map[string]any) (id, key string)
func (s *Server) Set(as, key, field string, value any)
func (s *Server) Transition(as, key, status string)
func (s *Server) Link(as, linkType, source, dest string)
func (s *Server) Comment(as, key, text string) (commentId string)
func (s *Server) EditComment(as, key, commentId, text string)
func (s *Server) DeleteComment(key, commentId string)
func (s *Server) Delete(key string)
func (s *Server) Move(key, project string) (newKey string)
func (s *Server) Hide(key string)                        // 404 for the token's user
func (s *Server) AddStatus(issueType, name, category string) (id string)
func (s *Server) RenameStatus(id, name string)
func (s *Server) Advance(d time.Duration)

// Inspection and faults.
func (s *Server) Get(key string) map[string]any // the database copy, as GET renders it
func (s *Server) Requests() []Request
type Request struct {
	Method, Path, Query string
	Body                []byte
	Status              int
}
func (s *Server) Fail(method, pattern string, times, status int, body string)
func (s *Server) RateLimit(method, pattern string, times int, retryAfter time.Duration)
func (s *Server) RefuseField(field, message string) // 400 errors.<field> on PUT and POST
func (s *Server) RemoveTransition(issueType, from, to string)
```

Always on: `PUT` all-or-nothing with per-field errors; sorted labels,
trimmed summary, float rounding, ADF re-serialised with `localId`s; a string
where ADF belongs is 400; summary over 255 and labels with spaces are 400;
`issuetype` accepted by `id` only; links stored `{source: inward, dest:
outward}`; issue and comment properties, inline on create; deleted issues
dropped from search; 410 on `/rest/api/3/search`; the token omitted on the
last page with `isLast`; a differing `reconcileIssues` between pages is 400;
429 with `Retry-After` and `RateLimit-Reason`; no `Authorization` header is
200 with nothing on search and 401 on `/myself`; the `X-AAccountId` header.

### `jira`

```go
// ---- discovery (JS4) and schema (JS5–JS7): Derive and Compile are pure ----

type Project struct {
	Id, Key    string
	Me         jiraapi.User       // TimeZone formats JQL literals (JS20)
	IssueTypes []IssueType        // (Level desc, Id)
	Fields     []jiraapi.Field    // visible, scope-filtered, by Id
	Priorities []jiraapi.Priority // Jira's order
	LinkTypes  []jiraapi.LinkType // nil when linking is disabled
}
type IssueType struct {
	Id, Name, Description string
	Level                 int
	Statuses              []jiraapi.Status    // workflow order
	Screen                []jiraapi.FieldMeta // the create screen
}
func Discover(ctx context.Context, c *jiraapi.Client, projectKey string) (*Project, error)

type Level string // "info" | "warn" | "error"
type Note struct{ Level Level; Key, Message string } // json: level, key ("task/status:in-review"), message
func Derive(current *schema.Document, p *Project) (*schema.Document, []Note, error) // error: mis-binding
func Compile(s *schema.Schema, p *Project) (*Mapping, []Note, error)                 // error: no type aliased, mis-binding, duplicate alias
func Slug(name string) string
func Norm(name string) string

// ---- the seam between mapping and engine: pure ----

type Mapping struct{ /* unexported tables */ }
func (m *Mapping) Request() []string                            // fields= for GET
func (m *Mapping) LocalType(issueTypeId string) (string, bool)  // unmapped: skipped, reported
func (m *Mapping) IssueType(typeKey string) (string, bool)      // local-only: never exported
func (m *Mapping) Multi(typeKey, key string) bool
func (m *Mapping) Canceled(typeKey string) (issue.Value, bool)  // JS19
func (m *Mapping) Users(ri *jiraapi.Issue, cs []jiraapi.Comment) []jiraapi.User // to ensure first (JS16)
// Local is the snapshot in local terms for typeKey's mapped keys; the engine
// passes the type the merge settles on (JS18). Notes carry Note.
func (m *Mapping) Local(snap *issue.Snapshot, typeKey string) Doc
// FromJira converts one issue and its comments: canonical values (JS7), texts
// through ADFToText, out-of-project relations dropped, the rest in Skip (JS17).
func (m *Mapping) FromJira(ri *jiraapi.Issue, cs []jiraapi.Comment, ix *Index) Doc
// ToJira turns merged changes into writes, one per local key; remote gives link ids.
func (m *Mapping) ToJira(typeKey string, ch []Change, remote *jiraapi.Issue, ix *Index) ([]Write, []Skip)
// Create is the POST body and the keys it carries (JS15's create base).
func (m *Mapping) Create(local Doc, id entity.Id, ix *Index) (jiraapi.CreateIssue, []string, []Skip)

type Index struct{ /* Jira id <-> entity id, accountId <-> identity id, exportable unlinked ids */ }
func NewIndex(repo *cache.RepoCache, m *Mapping) (*Index, error) // one pass over excerpts and identities
func IndexOf(issues, users map[string]entity.Id, exportable []entity.Id) *Index // tests
func (ix *Index) Issue(jiraId string) (entity.Id, bool)
func (ix *Index) JiraIssue(id entity.Id) (string, bool)
func (ix *Index) User(accountId string) (entity.Id, bool)
func (ix *Index) Account(id entity.Id) (string, bool)
func (ix *Index) WillExport(id entity.Id) bool // unlinked, mapped type, unarchived: a target to Retry on
func (ix *Index) AddIssue(jiraId string, id entity.Id)
func (ix *Index) AddUser(accountId string, id entity.Id)

type Doc struct {
	Id, Key  string                 // remote only
	Updated  time.Time              // remote only
	Type     string
	Fields   map[string]issue.Value // mapped keys, title and type included
	Body     Text                   // comment #0
	Comments []Comment              // #1 on
	Skip     []Skip                 // remote only
}
type Text struct{ Text string; Lossless bool } // local texts are always lossless
type Comment struct {
	JiraId         string    // local: jira-comment-id; remote: the Jira id
	Op             entity.Id // local: the add-comment op; remote: from the git-work property
	Author, Editor string    // accountIds (remote) or identity ids (local)
	At, Edited     time.Time
	Text           Text
	Note           bool // jira-note: never exported
}
type Change struct{ Key string; Set issue.Value; Add, Remove []issue.Value } // Set scalar, Add/Remove multi
type Skip struct{ Key, Reason string; Retry bool }                           // json: key, reason, retry
type WriteKind int // WriteEdit (batched into one PUT) | WriteTransition | WriteLink
type Write struct {
	Key    string          // the local key: success is reported per Write
	Kind   WriteKind
	Field  string          // Edit: Jira field id
	Set    json.RawMessage // Edit: fields.<Field>; "null" clears
	Update json.RawMessage // Edit: update.<Field>, when Set cannot say it
	Status string          // Transition: target status id
	Add    []jiraapi.NewLink
	Remove []string // Link: issueLink ids
}

// ---- the merge: pure (JS9–JS12) ----

type Base struct{ /* JS8 */ }
func CurrentBase(snap *issue.Snapshot) (*Base, []string) // nil when unlinked; problems for undecodable markers
func Digest(text string) string
func Merge(b *Base, local, remote Doc, multi func(key string) bool, export bool) Plan

// LocalKind: LocalSet | LocalAdd | LocalRemove | LocalEditBody | LocalAddComment |
// LocalEditComment | LocalPairComment (a SetMetadata of JiraId on Op, no text).
type LocalKind int
type LocalChange struct {
	Kind   LocalKind
	Key    string
	Value  issue.Value
	Text   string
	Op     entity.Id
	JiraId string
	Author string    // accountId; "" is the runner
	At     time.Time // Jira's updated for field imports
}
type CommentWrite struct{ Op entity.Id; JiraId, Text string } // JiraId "" creates
type Conflict struct{ Key string; Local, Jira issue.Value; Comment string } // Key: field, "body" or "comment"
type Plan struct {
	Local     []LocalChange
	Remote    []Change
	Comments  []CommentWrite
	Conflicts []Conflict
	Pending   []Skip
	Base      Base
}

// ---- the engine (JS13–JS24) ----

type Options struct {
	Ids                         []entity.Id
	DryRun, Full, AcceptDeletes bool
	Overlap                     time.Duration // default 5m
	MaxDeletes                  int           // default 10
}
type State struct {
	Site, Project string
	Cursor        time.Time
	Creating      map[entity.Id]time.Time    // the create journal
	Seen          map[entity.Id]lamport.Time // edit lamport after the last sync
}
func LoadState(fs repository.LocalStorage) (*State, error) // jira/state.json
func (s *State) Save(fs repository.LocalStorage) error     // temp file and rename

func Sync(ctx context.Context, repo *cache.RepoCache, c *jiraapi.Client, p *Project, m *Mapping,
	st *State, opts Options, emit func(Line)) (Summary, error)

// Line is one JSON line (JS22): exactly one of Schema, Issue, Summary is set.
type Line struct {
	Schema    []schema.Change            `json:"schema,omitempty"`
	Issue     entity.Id                  `json:"issue,omitempty"`
	Jira      string                     `json:"jira,omitempty"`
	Action    string                     `json:"action,omitempty"`
	Imported  map[string]json.RawMessage `json:"imported,omitempty"`
	Exported  map[string]json.RawMessage `json:"exported,omitempty"`
	Comments  *CommentCounts             `json:"comments,omitempty"`
	Conflicts []Conflict                 `json:"conflicts,omitempty"`
	Pending   []Skip                     `json:"pending,omitempty"`
	Error     string                     `json:"error,omitempty"`
	DryRun    bool                       `json:"dry_run,omitempty"`
	Summary   *Summary                   `json:"summary,omitempty"`
}
type CommentCounts struct{ Imported, Exported, Edited, Tombstoned int } // json: snake_case
type Summary struct {
	Imported, Created, Updated, Linked, Gone, Conflicts, Pending, Failed, Skipped int
	Cursor time.Time
}
```

### Outside `jira/`

```go
// cache/issue_cache.go — the only cache change.
//
// Update takes the write lock, re-reads the issue, and hands fn the fresh
// snapshot. The operations fn returns are schema-checked as one change —
// against the type they set, if they set one — then appended and committed
// behind one ref update. No operations: nothing is written. fn runs under the
// entity's mutex: it may read other excerpts and a *schema.Checker obtained
// beforehand, but must not call a cache writer, c.Snapshot(), or the network.
// Implementation: split CachedEntityBase.Commit into the lock and a
// commitLocked that Update calls after reload and fn, so there is one path.
func (c *IssueCache) Update(fn func(snap *issue.Snapshot) ([]issue.Operation, error)) error

// entities/issue (ours): the marker's constructor.
func NewNoOpOp(author identity.Interface, unixTime int64, metadata map[string]string) *dag.NoOpOperation[*Snapshot]

// schema/: aliases (JS2).
const AliasPrefix = "alias_"                       // reserved in attr.go, in the first commit
func AliasName(system string) string               // "alias_jira"
func ValueAliasName(system, valueId string) string // "alias_jira/<value-id>", on the field entity
// Aliases map[string]string `yaml:"aliases,omitempty"` on TypeDoc, FieldDoc, ValueDoc,
// and Aliases map[string]string on Type, Field, Value.
// Load reads them; Export writes them (export | import stays zero operations);
// Reconcile writes stated ones and, owning no alias_ name, never removes one;
// Validate checks the system name [a-z][a-z0-9_]* and duplicates within the document.
func NewDocument() *Document
func (d *Document) SetType(key string, t TypeDoc)
func (t *TypeDoc) SetField(key string, f FieldDoc) // allocates Fields lazily
func (f *Field) ValuesInCategory(c Category) []Value

// gitcli/: git's credential helpers, like its config and transport.
type CredentialHelper interface {
	// CredentialFill runs `git credential fill` with GIT_TERMINAL_PROMPT=0.
	CredentialFill(url, username string) (secret string, err error)
}
// cache/: reaches it through the wrapped repository; an error when git is absent.
func (c *RepoCache) Credential(url, username string) (string, error)

// host/jira.go: the one path for commands (and Starlark in v2).
func JiraSchema(ctx context.Context, repo *cache.RepoCache) (*schema.Document, []jira.Note, error)
func JiraSync(ctx context.Context, repo *cache.RepoCache, opts jira.Options, emit func(jira.Line)) (jira.Summary, []jira.Note, error)
```

That is the whole footprint outside `jira/`, `host/jira.go` and
`commands/jira/`. No pristine package is touched: identity tagging uses
`Identity.SetMetadata`, which already appends a version.

## Test plan

**Merge**, table tests, pure:

- M1 Jira-only edit → one `LocalSet` by the runner at Jira's `updated`; base = remote.
- M2 Local-only edit → one `Remote` change, no local op.
- M3 Double edit → Jira's value imported, one conflict; base = remote.
- M4 The same edit on both sides → no op, no conflict, base recorded.
- M5 Sets: an add and a remove on each side → all four survive, no conflict.
- M6 Missing base: local null → import; local set and different → import and conflict; set → union; `l == r` → converged.
- M7 Lossy remote description edited locally → pending, not exported.
- M8 Remote `Skip{Retry}` → key untouched, base unchanged, key in `Base.Retry`.
- M9 `export=false` → every local change pending.
- M10 Comments: new in Jira; new locally; edited on each side; on both; deleted in Jira; deleted after a local edit; a property naming an unpaired local op → `LocalPairComment`, no text op; a note → never exported.
- M11 Jira normalises our write back to the value it held → converges in one run, no second export.
- M12 Type changed in Jira → type first, other keys under the new type; local type change → pending.
- M13 `CurrentBase`: two markers → greatest `Updated`; an undecodable one skipped and reported.

**Schema**, goldens over `current ∈ {empty, jira preset, schema.yaml} × {Company, Team}`,
every output passing `Document.Validate`:

- D1 `Discover` → `Project` golden per fixture.
- D2 `Derive` golden and its `Reconcile` change list per cell.
- D3 Fixpoint: import, derive again → zero changes.
- D4 A Jira status rename → one `Set` of one `values/<id>`; an added status → its value and alias; a removed one → no change, a dead-alias warning.
- D5 Importing the alias-free `schema.yaml` over a mapped store removes no alias; `export | import` is zero operations.
- D6 `aliases: {jira: ""}` is never adopted, across runs.
- D7 A `canceled` value aliased to a `done` status stays `canceled`; moved to `indeterminate`, it resets and warns.
- D8 Company aliases against Team → `Derive` and `Compile` refuse; `sync` refuses with no type aliased.
- D9 Two entities with one alias → `Compile` refuses.
- D10 `Slug`/`Norm` table (apostrophes, non-Latin, leading digit, 48-byte cut, collisions, built-ins).
- D11 Conversion table, one row per kind × direction, `ToJira(FromJira(x)) == x`; P1 per kind and for ADF against the fake's normalisation; link direction per C1.

**Client** against `jiratest`:

- C1 Paging stops on `isLast` or a missing, null or empty token; short pages are not the end.
- C2 429 with `Retry-After` is waited and retried; exhausted → `RunFatal`.
- C3 Timestamps in two non-UTC zones parse to the right instants; `JQLTime` uses the user's zone.
- C4 `notifyUsers` is never sent; `/search` is never called; bodies are ADF where ADF belongs.
- C5 409 on a transition → re-read, retried once; a required resolution is filled; another required field → `ErrNoTransition`.
- C6 401 on `/myself` → `RunFatal` with "token invalid or expired".

**Engine**, a real test repository against `jiratest`:

- E1 Echo: export a local edit, run again → nothing written on either side.
- E2 Import echo: import a Jira edit, run again → nothing exported.
- E3 Normalisation: sorted labels and a trimmed description → imported in the same commit; the next run is quiet.
- E4 Crash after the Jira writes, before step 6 → next run converges; no duplicate comment, one marker.
- E5 Crash after `POST /issue`, index lagging, next run inside and after `Overlap` → linked by property, never created twice.
- E6 No transition to the target → status pending with the reason, other keys synced; a later Jira status change imports with a conflict.
- E7 `PUT` refused for one field → retried without it; the rest land; that key pending.
- E8 A concurrent local commit between steps 3 and 6 → a conflict if Jira changed that key, pending otherwise; nothing lost.
- E9 New local issue → created with type, parent and create-screen fields; status and links written in the same run; `jira-id` and `alias:jira` on the create op; resolvable by key; a Jira default priority imported, not cleared.
- E10 New Jira issue → `NewRaw` by the reporter at `created`, first marker on the create op, comments by their authors; a child imported before its parent resolves in the end-of-run re-pass.
- E11 Parent or link outside the project → dropped; the local value untouched; nothing reported as `Retry`.
- E12 Jira delete under `--full` → `Gone`, canceled status, note, never exported; more than 10 → nothing marked, exit 1; `--accept-deletes` marks them; the issue answering again → Jira's status imported.
- E13 Move to another project → `Gone: moved`.
- E14 Two clones on one fake, exchanging through a bare remote → no value lost, markers read as one base, a local edit on B before pulling A's import exported or noted; one new Jira field derived on both → E7 reported; one new status → one value.
- E15 The overlap re-returns synced issues → no `GET`, no commit.
- E16 429 exhausted mid-run → the run stops, exit 1, the cursor not past the failing issue.
- E17 Cursor: a failed issue mid-page → the next run starts at or before it; a lost state file → a search from the markers, every hit skipped.
- E18 `--dry-run` → plan printed; refs, state file and the fake's write log unchanged.
- E19 `ID...` → only those issues read and synced; no search; cursor untouched.
- E20 A Jira status the schema lacks mid-run → `Retry`; the next run's schema step adds it and the key imports.
- E21 First sync with `alias:jira` link requests → linked, not duplicated; an alias naming no Jira issue → skipped, never created.
- E22 `/myself` 401 → abort before any search or write.
- E23 A Jira comment edit that does not bump `updated` → missed incrementally, synced by `--full`.
- E24 Two Jira issues whose property names one entity → the lower id linked, the other reported every run.
- E25 A `Gone` issue edited locally → pending, never exported.

E1–E5 and E12 run in both positions of every `jiratest` switch.

## Contradicts earlier tasks

Each is also a comment on its task, per the working conventions.

- **`33148f2`** asked for "changelog where available, state diff otherwise".
  Revised: state-based always, merged against `GET`, **no changelog in v1**,
  not even for attribution (JS1). Sprints and rank are v2.
- **`69b7be0`**: the open question is closed — the mapping is aliases on the
  schema entities (JS2), not git config and not a `mapping` shape; `bridge
  new`'s introspection is `git work jira schema`, reviewed as a file (JS5).
  Its comment asked for the reconcile inside `ConfigCache.Update`; the sync
  uses `host.SchemaImport` as it stands, because derivation is idempotent and
  the next run corrects a lost race.
- **`3c6d07a`**: the policy stands. The base is a `NoOp` marker (JS8); the
  loser is recorded by a note and the op log, with no `jira-overrode`
  metadata (JS21); "delete → canceled" needs a canceled value on the type,
  and a type without one keeps its status with the note (JS19). The bridge
  core's `since`-timestamp loop does not stay: the cursor is Jira's own
  `updated` with an overlap (JS20).
- **`32d372e`**: "store the Jira key as op metadata and register it as an
  alias" is `jira-id` plus `alias:jira` by `SetMetadata` on the create op
  (JS15); validation errors are per-key pending entries, not `ExportResult`
  warnings.
- **`a3a8d16`**: the command is `git work jira sync`, not `git work sync`
  (the module is named after what it talks to); **`--watch` is deferred**:
  cron is the loop (every minute, `--full` nightly), per "automation is out
  of scope".
- **`cli-convention.md`**: `git work bridge configure|pull|push|rm` becomes
  `git work jira schema|sync`, unbound in Starlark in v1.
- **`config-entity.md`**: E3 gains the reserved `alias_` family; E9 gains
  "reconcile writes stated aliases and never removes them".
- **`AGENTS.md`**, when the command lands: the recipe, one bound clone, `sync`
  never pushes, and the cron lines.
