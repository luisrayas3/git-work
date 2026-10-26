# Creating issues from a view: the `new` kind and the ghost (`111e8e9`)

**Outcome:** every view that lists issues has a place to add one,
and the issue it adds is already where it was added.
A ghost at the foot of each group, column and lane opens a page
that is show's page over an issue that does not exist yet;
Create commits it, in one operation, exactly as `git work issue new` would,
and the view the ghost stood on reloads with the cursor on the new issue.

**Serves:** `111e8e9` (the story),
`3111def` (the kind and the page), `414417b` (the ghost),
`cdffc6c` (the multi-value picker), `b9a9b62` (the child ghost and the matrix cell),
`2298f37` and `00a63d9` (the board and the gantt, which get the ghost),
`e8d6426` (the plumbing it mirrors).
Required fields are `8cf1a78`, outside this scope.

**Status:** proposed 2026-10-07, approved the same day (Luis),
with required fields cut from the scope and filed on their own.

## C1 — `new` is a view kind: the interactive `issue new`

`git work view new [KWARGS]` and `work.view.new(**kwargs)`.
It takes `doc`, the document `git work issue new` takes —
`{"fields": {…}, "body": "…", "aliases": {…}}` — with what is already decided filled in,
and `fields`, the keys to draw and their order, as show takes them.
Both are optional: `git work view new` alone is an empty form.

It is a kind and not a mode of show, for the reason show is a kind and not a mode of the list:
`git work view new '{"doc":{"fields":{"type":"task","parent":"abc1234"}}}'`
is a thing to want on its own, from a shell or from a flow,
and whatever shape wins is a view, so flows call it and the GUI draws it (`111e8e9`).

`doc` is the name, not `fields`, because `fields` on show is a list of keys
and one name must not mean two things across kinds;
and because the pair then reads as what it is —
`work.view.new(doc=d)` is `work.issue.new(d)` with a person in the loop.
What Create commits is the draft, as that document, through `host.IssueNew`:
the page has no write path of its own.

`doc` is checked at open as `children` and `expand` are,
before anything draws: its keys must be fields of its type when a type is given,
and its values must fit their kinds (the planner's `CheckNew`, minus nothing it does not yet check).
A document the planner would refuse is refused at the call,
so a flow learns at once rather than after the person has typed.

## C2 — Nothing is written until Create

The story put two shapes on the table.
The other, create a placeholder then open show on it, was rejected:

- the title cannot be cleared, so the undo of an abandoned creation is an archive,
  and the store keeps a tombstone for every change of mind;
- every unarchived issue of a mapped type is exported by the Jira sync (JS15),
  so a placeholder reaches Jira on whichever sync runs next,
  and a local archive does not delete it there;
- the schema check runs before a title was typed,
  and the create's one commit becomes a create and a trail of sets.

With a draft, the sync first sees the issue in its final form, one operation,
the same operation `issue new` writes,
so the create path — the `jira-create` marker, the create base, the refusal journal —
needs no change and gets no special case.
What a draft costs is a page that holds one, and show already holds two.

## C3 — The page is show's page over a draft

The same stops, top to bottom, and one more:

1. the **header**: the type and the title, each a cell `Space` edits.
   There is no archived cell: a draft is not archived.
   The type's picker is the schema's types, as on show;
   with no type the fields table is empty, because the fields are the type's.
2. the **fields table**: the type's fields in schema order, or `fields`,
   the draft's value in each, the same editors and pickers as show's —
   a value list for an enum or a person, an issue list for a relation,
   an input line for text, a number or a date, a bool flipping at once.
   A `multi-relation` and a `multi-enum` ring for now, as on show
   (a picker that toggles is the first follow-on, below).
3. the **description box**, the comment box over the body:
   `Space` enters it, `Enter` in the text accepts it into the draft
   and leaves it, `M-Enter` is a newline, `Esc` leaves it keeping the text.
   Washed whole while the cursor is on it, its keys on the bottom line alone,
   as show's box is (2026-10-08).
   An empty description is allowed on a draft, because `issue new` allows one.
4. **Create**, the last stop, a button:
   `Enter` on it commits the draft.
   Show's button went because it was a second way to do one thing;
   here it is the only way, and `Enter` elsewhere on the page opens or does nothing,
   as it does on show, so that nothing creates by accident.

The page opens **on the title cell, typing**, when the type is set,
and on the type cell when it is not:
a ghost's `Enter` means *I want to add one*, and the title is what is typed next.
Accepting the title with `Enter` leaves the cursor on it;
`Tab` reaches Create.
The arrows do not, since 2026-10-08: the description box is a stop of `Tab` and `S-Tab` alone,
as show's comment box is, so no arrow enters or leaves it,
and `↓` from the last row, `↑`/`↓` on the box and `↑` on Create ring.
The hint line names it: *tab: create*.

Changing the type keeps the values of the fields the new type also has
and drops the rest, with a count in the status line, because the draft is the person's typing
and a type picked wrong is one key from right.

`Esc` with anything typed asks for a second `Esc`, as show's drafts do.
Back from a draft drops it; there is nothing to keep.

Create refuses what the planner refuses, naming it in the status line
and putting the cursor on the cell it names:
today that is a missing type or an empty title.
Required fields beyond those are not in the schema and not in this scope
(the ticket, below).

## C4 — What Create does next, and what `new` prints

Opened from a view, Create **pops the page**:
the view underneath reloads, as it does after any write,
and the cursor lands on the new id.
The ghost's promise is that the new issue appears where the ghost was,
and standing on it is the proof.
An issue the view's query then filters out —
a prefilled value edited away, or a type the query excludes —
is named in the status line instead: *created abc1234, not in this view*.

Run standalone, from the shell or a flow, Create **replaces the page with show**
on the created issue, so that a first view stays a view
and a second `Esc` quits from a place worth standing on.

`git work view new` **prints the created id**, or nothing when the page is left without creating,
and `work.view.new(...)` returns it, or `None`.
This bends the rule that a view prints nothing.
It bends because `new` is a writer, and a writer prints the id it created or nothing at all (`e8d6426`);
a flow that goes on to set a field on what the person just made needs the id,
and nothing else about the view rule — one KWARGS object, nothing on standard input, `--gui` posts it — changes.

## C5 — The form invents no default

A field the person leaves empty is **null in the commit**, never a first value or a guess.
Locally a null is a field unset, which every reader already handles.
Toward Jira it is the create base's rule (JS15):
a key local holds null takes Jira's value after the `POST`,
so a priority or a status Jira fills on create imports
rather than being overwritten by a default the form made up.
The draft starts as `doc` and the ghost's prefill and nothing else.

## C6 — The prefill is the drop's write set

Every page already knows, for the cursor's position,
the fields a drop would write to put an issue there:
the list and the gantt know the group's stored value and the nested parent through the layer's relation,
the board knows the column's value and the lane's.
That set is the prefill, and the ghost is the drop's twin:
a page's `draft()` returns the `issue new` document for where the cursor stands,
and the ghost's `Enter` opens `new` with it.

- `group_by` prefills its field with the group's stored value, `(none)` leaving it null.
  `type` as the `group_by` prefills the type.
- A board prefills the `columns` field with the column's value, `(none)` leaving it null,
  and the `group_by` field with the lane's.
- A nested layer prefills the stored side of its relation with the parent's id —
  `children` is read through `parent`, as `childrenIndex` reads it —
  which is the follow-on for the child tables (the story's comment of 2026-10-02).
- `type` is prefilled when every row in the ghost's scope has the same one;
  otherwise it is empty and the page opens on the type cell.
  A query is a jq program and cannot be inverted;
  the rows it produced are the best witness of what it selects.
- **No rank.** A ghost stands at the foot of its scope,
  and a null rank already sorts after every set rank,
  so the issue lands where the ghost was without a rank write.
- Every prefilled cell stays editable: the prefill is a hint, never a constraint.

## C7 — The ghost

A dim `+` row at the foot of each group on a list and a gantt,
one at the foot of the whole view when `group_by` is not bound,
and a dim `+` card at the foot of each column on a board,
of each lane's column when `group_by` is bound.
It is a row the cursor reaches with `↓`,
`Enter` opens the creator with the scope's prefill,
`Space` rings, because there is nothing to edit and nothing to grab,
copy copies nothing,
and it is **never a rank sibling**: a grab skips it, a drop above it is a drop at the foot,
and the rank fills do not count it.
The filter hides it with the rows it narrows away.
On a nested list the ghost is the roots' only, for now;
each child table gets its own with the child follow-on.

There is no `n` key: a letter is a bad universal key on pages that open in a text box,
and the ghost is where the prefill comes from.

## C8 — The child ghost and the matrix cell (`b9a9b62`, 2026-10-15)

Decided 2026-10-15 (Luis): all of `b9a9b62`.
It takes C7's *the roots' only, for now* away,
and settles two items of Deferred below, the child ghost and the matrix cell —
the cell not as Deferred words it, by a creating cell, but through its drill-down and the `draft` argument (C9).
It also overturns one sentence of `show-side-table.md` S6: why, below.
Revised the same day with Luis's answers on the matrix, leaves, a single relation on the parent, and show (`app.md` A7).

### One ghost per scope, at every level

A **scope** is a set of siblings and a group among them:
the roots, or one opened parent's drawn children,
and, where that level's layer has a `group_by`, one group of them.
Every scope ends in a ghost, at every level of a nested list and of a gantt,
so an opened parent's child table ends in one, and so does each group of it.
A level that groups gets one ghost per group, exactly as the roots do:
a child table is a list, and every level is arranged the same way (`arrange`).

There is **one placement path**, and the roots are not a special case of it.
Today's `addGhosts` reads layer 0 and `rootGroups`;
it becomes one walk over the drawn tree that yields every scope — level, parent, group —
and one draft per scope, the roots' scope being the one whose parent is no row.
The reason is visual as much as structural:
the ghost at the foot of an opened parent's child table,
drawn at the children's indent,
is also what divides that parent's subtree from the next root,
so a tree opened in several places reads as blocks,
and it must look and behave the same at every depth.

- **Folded, no ghost.** A child table folded away is hidden with its ghost;
  the fold's count counts issues, never the ghost,
  and a gantt's folded envelope is drawn from bars, which a ghost has not.
- **A leaf unfolds to an empty child table** (Luis, 2026-10-15),
  so the first child is added where every other is.
  A row draws a fold cell when the layer below it exists
  and its write set (below) can be named for the row's type —
  its type stores the relation, or a type that stores the inverse may target it —
  because that is exactly when a ghost could stand under it.
  A row that cannot have children of that layer draws no fold cell, as today.
  - Folded, a leaf's cell reads `▸ 0`, faint:
    the count is still *what folding hides*, and zero is the truth;
    faint so that a column of leaves does not read as a column of parents,
    and the digit kept so the tree column's width (`treeWidth`) is measured the same way for every row.
    Opened, it reads `▾`, as a parent's does.
    The count never counts the ghost, at any level.
  - The empty table **draws its column header**, above its ghost,
    as every child table draws one above its first row:
    the ghost is that table's first row, and the header says what the new child's row will show.
    One rule for every table is worth the line.
  - **The cost** on a tree of leaves is nothing until a fold opens: folded is the default (`nest`),
    and a folded leaf is the one line it already was.
    **Two palette commands** open many at once (`app.md` A4, *Two folds*, 2026-10-15, Luis),
    both palette-only, `esc z` / `alt+z` and `esc Z` / `alt+Z`,
    with no key on the page, where `z` and `Z` are typed text (A3):
    *fold all* opens every row that has children, or folds them all, and leaves leaves folded,
    because it means *show me the tree*, and an empty table under every leaf is not the tree;
    *open everything* opens the leaves too, so every level's ghost is on the screen,
    for the person who is about to add under many of them.
    The second lands with this section's code, in `pageCommands` beside the first,
    and only where something nests.
  - The call's **`open`** (`query-rows.md` R4) keeps its meaning, the rows with children:
    it is the state a view opens in, which is a summary,
    and nothing a flow opens a view for needs an empty table under every leaf;
    a person who wants them is one palette command away.
  - A single leaf is opened by its own fold cell, `Space`, or by `Tab` into it, which lands on the ghost.
- **The filter** hides a child ghost as it hides the roots':
  a ghost has no text, so a non-empty filter narrows it away.
- **Never a rank sibling**, at any level (C7):
  a grabbed child carried down stops above its table's ghost,
  and a carry across a child table's groups steps over the ghost between them as a root's does.
- **No ghost where there is nothing to write.**
  A parent with no id (a keyed row, `query-rows.md` R1),
  a layer whose rows above list their own `children` (R3),
  and a relation whose write set below cannot be named
  have no ghost: a `+` that cannot put the issue where it stands would break the one promise it makes.
- **`Enter`, `Space`, `Tab` and the arrows, nothing else.** A ghost is reached with the arrows and `Tab`,
  and opened with `Enter`; `Space` on a fold cell folds.
  Neither a ghost nor a fold cell has a letter key or depends on a meaning of `Esc`,
  because `app.md` A1–A4 give letters to the filter and `Esc` to the palette.

### The prefill is the drop's write set, per scope

C6 holds at every level, with the parent as one more coordinate.
A child scope's draft is:

- **the relation**, written on the side it is stored on (below);
- **the group**: the scope's layer's `group_by`, prefilled as the roots' is (`groupPrefill`),
  read off the layer and not the call, since a level groups because it was asked to;
- **the type**: the type every row of the scope shares, when it is one that can hold the relation;
  otherwise the one type the schema says can, as S6 reads it;
  otherwise none, and the page opens on the type cell.
  The rows are still the best witness (C6),
  and the schema is the fallback for a scope whose rows disagree;
- **no rank**, and nothing a gantt would read off the cursor's column:
  the ghost has no bar, and a null rank sorts last, where it stood.

Which side the relation is stored on is decided per parent type,
the way `view.InverseSources` already decides it for show:
when the parent's type has a stored relation field of that name, it is that field;
otherwise it is the inverse, stored on the child.

- **Stored on the child** (`children` read through `parent`, as `childrenIndex` reads it):
  the draft holds the stored field set to the parent's id,
  or a set holding it for a `multi-relation` (`blocks` under `blocked_by`).
  Create commits it in the issue's one operation, and nothing else is written.
  Where the types that hold the inverse disagree on its key,
  the scope's shared type picks the key; with no shared type there is no ghost (S6).
- **Stored on the parent** (the parent's own field lists its children):
  the draft holds nothing of the relation,
  and once Create has committed the issue,
  **the parent's field is written to hold the new id**:
  `issue add` for a `multi-relation`,
  whose set semantics merge with anyone else's concurrent add
  where a set of the whole list would lose it;
  `issue set` for a single `relation`, whose ghost is always drawn (Luis, 2026-10-15).

**A single relation on the parent replaces.**
While the field is empty its ghost reads `(new)`, as every ghost does.
While it is set, the ghost reads `(new, replacing abc1234)`,
naming by short id the issue that will leave the field,
because a `+` that silently swapped a link would be the one ghost that does more than it says.
Where the first field's cell is too narrow, it is cut from the right with `…`,
as every cell is (`fit`), so `(new, repl…` still says it is not a plain add;
below `(new…` it is the cell's width, as any cell.
Create then sets the parent's field to the new id.
The issue replaced is not archived, edited or otherwise touched:
only the parent's field changes, and the old issue stays where any other view finds it.

### The parent-side write is an ordinary write

It is not a second mechanism, and not *two writes* of one thing.
A relation stored on the parent is a single relation write
that happens to land on another issue:
the same `issue add` (or `set`) a person would type,
through `host` and `cache/` like every edit,
one commit on one entity, after the create's one commit on another.
Nothing batches them: there is no atomic multi-entity commit, and none is added.
It is the principle of `link-fields.md` L5 as revised the same day, and of `app.md` A6:
linking `is-blocked-by X` from show on Y is `issue add X '{"blocks":[Y]}'`,
one commit to X, checked against X's type.
The inverse is written as the other issue's stored field, in one commit,
and the parent-side write here is that write with the new issue as Y:
`issue add P '{"<field>":["<new id>"]}'`.

S6 gave a stored relation of the shown issue no ghost
because *the new issue would have to be added to this issue's field, a second commit on a second entity*.
That reason is withdrawn by the same principle:
a commit on the other entity is what writing a relation already is,
eventually consistent, like Jira (AGENTS.md).
So show's tables over a stored relation of the shown issue get the ghost too (confirmed, Luis, 2026-10-15),
through the same write set, and S6's ghosts become this section's.

On show, a side table is **one `expand` element of the shown type's spec**:
since `app.md` A7 (committed 2026-10-15) show takes `id` and `shows`,
and the spec under the shown issue's type carries `fields` and `expand`,
each element a relation name or a flat layer (`relation`, `query`, `include_archive`, `fields`, `details`, `group_by`).
The ghost hangs off one such element: its relation names the write set, as a nested layer's does,
its `group_by` gives one ghost per group, and its rows are the type witness.
Where the element came from — show's own `expand` before A7, a spec in `shows` after it — changes nothing here.
List and gantt nesting is the core's `expand` and is untouched by A7.

**One API.** No verb does create-and-link, and none is added.
The writers are the existing `host.IssueAdd` (and `host.IssueSet` for a single `relation`),
which `git work issue add`/`set` and `work.issue.add`/`set` already are,
and the composition is two calls that exist on both sides:

```python
id = work.view.new(draft=d)  # `doc` before C9
if id:
    work.issue.add(parent, {"blocks": [id]})
```

and the same two commands in a shell.
The ghost is the renderer making those two calls on the person's behalf, as a drop makes its writes.
`new` takes no argument naming a second write:
the page has no write path of its own (C1), a view that wrote a second entity would be one,
and an argument that is two existing calls is sugar (`cli-convention.md`).
The link is the opener's, not the creator's:
the page the ghost stood on writes it on `createdMsg`, before it reloads,
so the reload already has the new row under its parent.
An abandoned draft writes nothing, link included.

**When the parent's write fails** — the parent's field refuses the type the person picked
(its `target_types`), or the parent is gone — the issue stays created:
it is complete and correct on its own, the commit is not undone,
and an archive would leave a tombstone for a link that failed (C2's reason against placeholders).
The status line says both halves:
*created abc1234; not added to def5678's blocks: <the refusal>*,
and the cursor goes to the new issue where the view still shows it — among the roots, say — and otherwise stays on the ghost.
That is C4's rule, *the issue appears where the ghost was, or is named*, applied to one more way of not being there:
the failure is said where the person is looking, and the fix is one edit on the parent.
Between the two commits another reader can see the issue unlinked;
the next refresh shows it linked, as any two edits are seen.

### The matrix cell: the drill-down carries it

**Every cell follows one rule**, empty or not (Luis, 2026-10-15):
`Enter` drills down, as A9 has it, and the list it pushes ends in a ghost
that already holds the cell.
So a cell that holds allocations gets one more —
two people on one story in one sprint, the usual case —
and an empty cell gets its first, each one step away, through its drill-down's ghost.
The matrix itself draws no ghost and creates nothing:
a blank cell stays the matrix's *nothing here* against a `0` (A6),
and a cell has one meaning under `Enter`, not two by its contents.
A totals cell drills down the same way and carries only the axes it names.

The list learns the cell the way any list learns what its ghosts cannot see:
from the call's **`draft`** (C9).
The matrix is one caller of it, not the reason it exists:
the drill-down is an ordinary list call, built exactly as a hand-written one would be.
What the matrix puts in `draft`, for the cell under the cursor, is the cell's write set:

- **each axis field** with the cell's row value and column value, as a group prefills (`groupPrefill`),
  `(none)` leaving it out;
  a multi-valued axis gets a set of the one value, which unlike a drop is not ambiguous,
  since a draft has nothing to replace;
- **the block's `group_by` value** the same way;
- **the type** every issue on the matrix shares, the matrix's own rows being C6's witness,
  so an empty cell's drill-down, with no rows of its own, still knows it;
- **no `value`**: the form invents no number (C5), and the person types the points.

The pushed list's call line carries the `draft`, so copying it there still gives a legal `git work view list` call (A9).
Create pops back to the drill-down, the cursor on the new row;
going back to the matrix finds the cell's number changed, the cursor kept by axis values (A11).

This takes up the item `allocations.md` defers,
which said a cell with none *could create the allocation the axes describe*
and left it to *actions injected into views*.
It is not an injected action: it is the list's ghost, the one creation path every kind has,
told where it stands. Editing a cell's number in place stays deferred there.

## C9 — `draft`: what an issue created in a view starts with (2026-10-15)

Decided 2026-10-15 (Luis), with C8.
Every kind that draws a ghost takes **`draft`**, and `new`'s `doc` is renamed `draft`.

| Argument | Kinds | Tier | Kind | Meaning |
| --- | --- | --- | --- | --- |
| `draft` | `list`, `gantt`, `board` | feature | issue document | partial; the base every ghost's draft starts from |
| `draft` | `new` | optional | issue document | the draft to open on (C1's `doc`, renamed) |

The value is an `issue new` document, `{"fields": {…}, "body": "…", "aliases": {…}}`,
with what is already decided filled in.

### Why a view needs it

A ghost knows only what its view **arranges**:
the group it stands in, the relation of the layer it is under, the type its scope's rows share.
It never reads the view's query, because a jq program cannot be inverted (C6).
So a hand-written list over sprint 41,
`map(select(.fields.iteration == "<41>"))`,
has the same gap the matrix's drill-down has:
its ghost cannot know that what it makes belongs to sprint 41,
and the issue it makes falls out of the view the moment it is created (C4's *not in this view*).
The caller that wrote the predicate knows; `draft` is where it says so.

**It is not sugar** (`cli-convention.md`).
Sugar is a second spelling of something already expressible;
this is information the view has no other way to get.
It is plumbing: one JSON value in the one KWARGS object,
the same on the command line and as `work.view.list(draft=…)`,
parsed against the argument table in package `view`,
so a flow, a shell and the matrix pass it the same way.
It is checked at open as `new`'s is (C1), through the same `host.IssueDraft`,
so a call carrying a document `new` would refuse is refused before anything draws.

**List, gantt and board take it now**, not the list alone:
all three draw ghosts, and on all three the ghost cannot read the query.
It is one argument with one meaning on every kind that has something to create,
and leaving two of them without it would make the same flow work on a list and silently prefill less on a board.

### One construction, every ghost

Every ghost's draft is built in one order, roots and child scopes alike:

1. the call's **`draft`**;
2. the **type** its scope's rows share, where `draft` names none (C6);
3. the **group** value, where the scope's layer groups (on a board, the column's and the lane's), which wins on its key,
   because the ghost stands in that group;
4. for a child scope, the **relation write** (C8): the stored key on the child, or the link on the parent.

**A child scope inherits `draft`**, because `draft` is *what an issue created in this view starts with*,
and an issue made under a parent is made in this view.
A list of sprint 41's stories, expanded into their tasks, makes a task in sprint 41 too,
which is what a person standing there means.
One step keeps the construction honest where a child is a different kind of issue:
after the four steps, **what the scope's issue cannot hold is dropped**, as C3 drops it on a change of type.
A `draft` type that cannot hold the child relation's stored key —
or that the parent's field does not admit, for a relation stored on the parent —
gives way to the scope's own type (step 2's, or C8's schema fallback);
then a field the resulting type does not have is dropped.
At the roots nothing is ever dropped, because `draft` was checked at open against its own type,
so this is not a case for children but the same rule finding nothing to do.

The person edits any of it in the creator, as every prefill (C6).

### `new`'s `doc` is renamed `draft`

C1 named `new`'s argument `doc`, so that it would not mean two things across kinds.
With a second kind taking the same document for the same purpose,
the name that says what it is is `draft`:
the document a creation starts from, whether the creation is the page (`new`)
or each ghost that opens it (`list`, `gantt`, `board`).
`doc` would read on a list as the list's own document;
`draft` is already the page's word for what it holds (C2, C3, C5).
This supersedes C1's name; C1's reasons for a document, rather than `fields`, stand.

The rename lands with the code, and touches:

- `view/kinds.go` (the `new` kind's argument, and `draft` on list, gantt and board);
- `host/view.go` (the open-time check, `call.Has("doc")`);
- `tui/ghost.go` (`openNew` builds `{"doc": …}`), and through it `tui/palette.go`'s `n`;
- `tui/new.go` (`call.Raw("doc")`, twice);
- the tests that spell it: `tui/new_test.go`, `tui/show_map_test.go`, `view/view_test.go`;
- `host/quickstart.md` (`git work view new '{"doc":{…}}'`);
- the generated `doc/md/git-work_view_new.md`, `doc/man/git-work-view-new.1`
  and the completions, by `go generate`.

No flow in `flows/` passes `doc=`.
Earlier sections of this document keep the name of their day.

### Settled in building C8 and C9 (2026-10-26)

Where C8 and C9 are silent, or a word of theirs had gone stale, the code chose:

- **`draft`'s tier is `optional`.** C9's table says *feature*,
  the tier's name until `f9c991e` renamed it `optional` (`view/kinds.go`, `Tier`).
- **A child scope with no rows in a level that groups** has one ghost, in `(none)`,
  under a `(none)` group line, as the roots with no rows already had:
  one rule for every scope, the roots included.
- **A child stored in a set** (`blocks` under `blocked_by`) joins the parent's id
  to whatever set `draft` already gave the key, rather than replacing it.
- **The fold cell's width and the gantt's indent** count a leaf that could have children
  one level deeper, so opening a leaf to its empty table moves no column.
- **Show's side ghost takes the rows' shared type**, as C8 says every scope does,
  where S6 filled the type only when one type held the stored field;
  that remains the fallback.
- **The matrix's cell type** is the type every issue the matrix's query returned shares;
  a number axis is written as a number, a bool as a bool, a set as a set of the one value.
- **A grabbed child still cannot cross its table's groups** (`crossGroup`, roots only),
  so C8's *steps over the ghost between them* has nothing to do yet.


## Deferred

- **A multi-value picker**: a picker whose `Space` toggles a mark and whose `Enter` accepts the set.
  It is first because `area` and `labels` are on every type here and a create without them is a step back from the CLI;
  once it exists show gets it too, and *rings for now* goes from both.
- **The child ghost**: one per child table on a nested list and gantt,
  and one per `children` section on show, prefilling the stored side of the relation.
- **The matrix cell**: an empty cell creating the allocation its axes name (`doc/design/allocations.md`, Deferred).
- **Required fields**: a `required` attribute on the field entity,
  enforced by `CheckNew` and `CheckFields`, skipped by the pull,
  derived by `git work jira schema` from the create screen's `required` without a default,
  marked on this page and refused by Create.
  Filed as `8cf1a78`; nothing here depends on it.
- **`issue new --dry-run`**, the one writer without it.
