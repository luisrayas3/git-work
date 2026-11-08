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

A story's design is written on the story issue and approved there.
The change that builds it rewrites the decisions it changes, where they stand,
so that a document never describes what is not built.
Code cites a decision by document and heading,
`(doc/design/views.md, "The ghost")`,
and `TestDesignCitations` checks that every citation resolves.
Moving or removing design text repoints, in the same change,
whatever cited it: code, tasks and issue comments
(an issue comment is repointed by adding a comment, never by rewriting one).
A decision with its own reasoning and citations has its own heading;
old decision ids stay out of code and of these documents.
A rejected alternative is argued in the conditional,
and what a document deliberately does not do is listed
under "Deliberately absent", each entry stating its standing reason.
What Jira is inferred, not confirmed, to do is recorded as unconfirmed in the reference,
and the aspect states the rule that copes with it.
The full rules are in [`AGENTS.md`](../../AGENTS.md), Working conventions.
