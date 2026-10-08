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
`Tab` and `↓` reach Create.
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
