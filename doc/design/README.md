# Design

One document per aspect of git-work,
each saying what holds now and why:
the decisions in force, their reasoning,
and the alternatives rejected where the reason still matters.
What a decision used to be is in git's history and on the tracker's issues.

| Document | What it covers |
| --- | --- |
| [`store.md`](store.md) | entities as operation DAGs in git, merging, the cache and its write lock, the layering |
| [`issue.md`](issue.md) | the issue entity: its core, its fields, its operations, comments |
| [`schema.md`](schema.md) | types, fields and their kinds, the config entities, validation, the presets |
| [`query.md`](query.md) | jq programs, the archive, the schema in jq, rows, search, the past |
| [`cli.md`](cli.md) | the command line: the conventions every command follows, the map, aliases |
| [`flows.md`](flows.md) | flows: Starlark, the `work` API, home, the report flow |
| [`tui.md`](tui.md) | the terminal app: the keymap, the palette, the finder, navigation and editing, refresh |
| [`views.md`](views.md) | the view kinds and their arguments: list, board, gantt, matrix, show, new |
| [`jira.md`](jira.md) | the Jira sync |
| [`migration.md`](migration.md) | moving from git-bug: the store migration and importing a git-bug repository |

The Jira Cloud API the sync relies on is not a design aspect but a reference about Jira,
[`doc/reference/jira-api.md`](../reference/jira-api.md),
each fact marked confirmed or unconfirmed with its source.

## How these documents change

Only architectural decisions are recorded here:
which key does what, a label's wording or a colour is the code's to record
(the keymap in `tui/keys.go`, and the help drawn from it),
and a document states only the architecture such choices sit in.
Every sentence reads true to someone who never saw the old behaviour,
code comments included;
an example illustrating a reason is generic,
and the incident that prompted a decision belongs on the issue.
A rejected alternative is argued in the conditional,
and what a document deliberately does not do is listed
under "Deliberately absent", each entry stating its standing reason.
What Jira is inferred, not confirmed, to do is recorded as unconfirmed in the reference,
and the aspect states the rule that copes with it.

A story's design is written on the story issue and approved there;
what is built that differs from or adds to it is commented on the story first,
and a decision not on the story is not approved.
The change that builds a decision moves it into its aspect with its reasons,
rewrites the decisions it changes where they stand,
and fixes, by meaning, every sentence it makes wrong,
so that a document never describes what is not built.
When the story is done, a comment on it names the heading each decision now stands under.

Code cites a decision by document and heading,
`(doc/design/views.md, "The ghost")`,
and `TestDesignCitations` checks that every citation resolves.
A decision with its own reasoning and citations, its own "Not X",
or code relying on it alone has its own heading;
old decision ids stay out of code and of these documents.
Moving or removing design text repoints, in the same change,
whatever cited it: code, tasks and issue comments
(an issue comment is repointed by adding a comment, never by rewriting one).
The full rules are in [`AGENTS.md`](../../AGENTS.md), "Design documents".
