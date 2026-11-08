# Views

A view is a kind of page a flow or a shell opens on the issues:
`list`, `board`, `gantt`, `matrix`, `show`, `new` and `home`.
This document is what each kind takes, what it draws,
and the edits each one makes.
The argument table in package `view` (`view/kinds.go`) is the authority on the arguments;
`tui/` is the authority on what is drawn.

Its neighbours own what it touches:
the keys, the palette, the stack of pages, the first line, refresh,
the rank a drag writes and the renderer's locking are the app shell's, in [tui](tui.md);
`query`, `include_archive` and rows that are not issues are [query](query.md)'s;
how a flow calls a view, and `home`, are [flows](flows.md)'s;
the `link` mark on a field and a type's `show` attribute are stored by the [schema](schema.md);
the writes a view makes are the operations of the [issue](issue.md).

## The call

### A view call blocks

A view call **blocks on the caller's thread**
and returns the person's answer:
the id `new` created, or nothing, and nothing for every other kind.
Starlark is paused inside the call,
so there is no concurrency in the language,
and a script that ends while its view is open is not a state that can arise.

The Bubble Tea event loop runs on its own goroutine,
and the caller's goroutine, blocked in the call, is a worker fed from a queue.
A flow run from a page is a job on it (`tui/home.go`),
and the view that flow calls is pushed onto the same stack
rather than starting a second program.
So navigation and redraw never wait on a script,
and a second job queues behind a running one instead of racing it.

### One table of arguments

Every kind's arguments are one row each in `view.Kinds`,
read by four consumers:
`view.Parse` validates against it,
`--help` is generated from it,
the renderer reads what it was handed,
and Starlark takes the same keywords the shell does.
A call's whole input is one JSON object of those keywords,
`git work view board '{"columns":"status"}'` and `work.view.board(columns="status")` alike,
both through `host.View`.

What the table cannot check alone — the relations and fields `expand` names, the specs in `shows`,
and `draft` as `issue new` would read it —
is checked in `host.View` against the live schema, before anything draws,
so every surface refuses a bad call with the same words.

### Three tiers

Every argument is in one of three tiers:

- **required**: the call has no meaning without it;
- **defaulted**: absent, it takes the table's default, or one the renderer computes;
- **optional**: no default; absent, the view does not do that thing.

A tier is what a call must say, never how far the renderer has got.
Not `feature` for the third: a help that footnoted it as "not drawn yet"
would read as unimplemented once everything is drawn,
and send an agent looking for another way to do what `group_by` and `expand` do.

### The kinds and their arguments

`query` is a jq program over the unarchived issues,
and `include_archive` puts the archived back in its input ([query](query.md)).

| Kind | Required | Defaulted | Optional |
| --- | --- | --- | --- |
| `list` | — | `query`, `include_archive` (false), `fields` (`["type","title"]`) | `details`, `group_by`, `expand`, `open`, `draft`, `shows` |
| `board` | `columns` | `query`, `include_archive`, `values` (the field's schema order), `card` (`["title"]`), `column_width` (32, at least 10), `empty_groups` (false) | `group_by`, `draft`, `shows` |
| `gantt` | `start`, `stop` | `query`, `include_archive`, `label` (`title`), `scale` (`week`), `from`, `to` (the data's extent) | `progress`, `group_by`, `expand`, `open`, `draft`, `shows` |
| `matrix` | `rows`, `columns` | `query`, `include_archive`, `row_values`, `column_values` (each axis's own order) | `value`, `group_by`, `shows` |
| `show` | `id` | — | `shows` |
| `new` | — | `fields` (the type's, in schema order) | `draft` |
| `home` | `flows` | — | — |

`fields` on a list are the columns, in order, **editable in place**;
the id is always the first column and is not named.
`details` are read-only keys on a dim line under the row.
`card` is the keys on a board card.
`scale` is `day`, `week`, `month` or `quarter`.
`home` is the page `git work` opens on, a stored flow per row ([flows](flows.md)).

### Order is the query's

No kind takes a sort argument.
Order is the query's order — jq sorts, and better than a binding would —
among the issues the built-in `rank` does not place ([tui](tui.md)).
No call names `rank`: it is an order the drags write.

### Group order

A `group_by`'s sections, lanes or blocks are in **the values' own order**, on every kind:
an enum's schema order (over the types on the view; an unlisted value after the listed ones,
a `multi-enum` compared item by item),
`false` then `true`,
a relation's issues by `(rank, …)` with the unranked after (a `multi-relation` by the first issue it names),
numbers and dates ascending,
text and people by the drawn label,
`type` by the schema's type order,
and `(none)` last.
Where two tie, the order the query first put a row in them breaks the tie,
so the order is stable across a refresh.

Not first-seen order, because a group's place would then depend on where the query happened to put a card,
and drawing an empty group ([Empty swimlanes](#empty-swimlanes)) would move the others.

## Nesting

### `expand` is a layer

`expand`, on the list and the gantt, is a **layer**, or a relation name as the shorthand for one:

```json
{"relation": "children",
 "query": "map(select(.fields.status != \"done\"))",
 "fields": ["status", "title"], "details": [], "group_by": "",
 "include_archive": false,
 "expand": {"relation": "blocks"}}
```

The keys besides `relation` and `expand` are the list's own arguments for that layer's rows;
`expand` is the level below, and a layer with none is leaves.
`relation` is needed only where some row above lists no `children` of its own ([query](query.md)).
`"expand": "children"` is one layer with every default.

Not `depth`: a number says how far to walk and nothing about what is down there,
so every level would draw the first one's columns.
A layer is the description, and the shorthand costs nothing.

### A number repeats a layer

A **number** as a layer's `expand` is that same layer again, that many more levels down,
and `0` as far as the relation goes:
`"expand": 1` under `children` is tasks, then subtasks.
It is a number and not a name because every string in that slot is a relation,
and a repeated layer is one layer drawn at every level, not a level inheriting the one above.
A number at the top is refused: there is nothing there to repeat.

### A layer's query selects that row's children

The call's `query` selects the roots.
A layer's runs over the array of **that row's own candidate children** —
every unarchived issue the relation reaches from it,
the archived too where `include_archive` says so, which a layer inherits from the layer above unless it names its own —
so it is written exactly like a view's query, over a smaller array.
Without one every child shows.

A matched issue that is another matched issue's child shows once, nested under it;
a child shows under its parent whether or not the root query matched it,
because a parent's children are the reason to expand a parent.
A row's key shows once, its id unless the query gave it one ([query](query.md)).
Cycles are cut at the repeat.

### Either side of a relation

A relation name is **either side** of a relation:
a stored relation field (`blocks`, `parent`),
or the inverse name the schema declares on one (`children`, read off every `parent` that names the row, in the store's order).
The inverse is never stored ([schema](schema.md)).
A layer's rows are ordered `(rank, id)`.
`links` is refused in a list's or gantt's `expand`, being several relations ([The links row](#the-links-row)).
A layer's relation and every field key it names are checked against the schema before anything draws.

### A nested layer is its own table

The rows under an opened parent are **a table of their own**:
indented as one unit, two cells a level,
with its own header line,
its own columns in its own widths,
and its id column first and flush within it,
so a child table reads by id as the top one does.
`fields` and `details` a layer leaves out are the layer above's,
so the shorthand is a tree of like tables,
and a story's tasks can show `status` and `assignee` where the stories show `type` and `priority`.
A layer's widths, and the tree cell's, are measured over **every row of that layer**, hidden under a fold or not,
so folding or unfolding moves nothing beside it.
When the cursor's table has its header scrolled off the top,
the header is kept on the first body line, as a group header is.
A layer's `group_by` sections each of its child tables.
A detail line is indented with its table.

Not one table with the indent inside the tree cell:
it would be one table by its header and many by its widths,
so a child's cells would sit under its parent's header and not under its own columns.

### The fold arrow is a cell

The id column stays first and flush;
the **tree cell comes after it**:
`▾` open, `▸` folded with the count of the row's own children hidden under it (`▸ 3`), nothing on a row that can have none.
It is as wide as the largest count, so it never moves.
It is a cursor stop:
`Space` there folds and unfolds, `Enter` opens the row as it does on the id.
The whole tree at once is a palette command ([tui](tui.md)).

### A nested view opens folded

Every parent starts shut, so the first screen is the roots and their counts.
`open` says otherwise:
`true` opens every parent, a number that many levels from the roots,
and it is spent on the first tree, so a refresh keeps the person's folds.
Folds are kept by key across a refresh,
and a refresh that would hide the cursor's row opens its ancestors:
a cursor is never moved by somebody else's write.
The filter keeps a row whose descendant matches.

### Tab goes down a level

`Tab` goes into the first child, unfolding on the way, and lands on its id;
`Shift-Tab` goes up to the parent's fold arrow, where `Space` folds back what `Tab` opened.
On a list `↑` and `↓` move among the rows at the cursor's own level,
so a level reads as the list it is;
on a gantt they move through every row, because a chart reads top to bottom.

### Gantt nests rows under rows

A gantt nests as **rows under rows**, not bars within a row:
children in a parent's row overlap as soon as two share a period.
It keeps one header, the chart's periods, and the indent in its label column,
because its rows share one time axis.
`start`, `stop`, `label` and `progress` are the chart's, not a layer's;
a layer's `query` and `group_by` apply as on a list.

## Board

### The only edits a board makes are moves

`board` is a list with a second axis:
a column per value of `columns`, a card per issue,
and a swimlane per value of `group_by`.
`Enter` on a card opens its issue in show ([Shows](#shows)),
and nothing on a card is edited in place:
a card is a summary, and the page where a field is edited is the one with the field on it.
So a card has no cell cursor, copy copies its id,
and a paste has nowhere to go and says so.

### Moving a card

`Space` grabs, always.
`←` and `→` carry the card into the neighbouring column, empty or not,
and the drop writes the `columns` field, `null` in `(none)`.
`↑` and `↓` reorder by the built-in rank;
past the end of a stack they carry the card into the swimlane above or below,
and the drop writes `group_by` too.
The drop is one `set`, one commit:
the field when the column or lane changed, and the rank whenever the card moved,
because a card in a new column has new neighbours.
A card dropped where it was picked up writes nothing,
a drop the schema refuses is a status line,
`type` as the `group_by` is never written by a drop,
and a refresh while a card is grabbed lets it go.

### The columns

The columns are `values` when given, in that order,
else the field's schema order, read off the types of the cards on the board —
every type owns its own field, so an iteration's statuses are not columns of a board of tasks —
and off every type when there are no cards.
Then, trailing, every value the data has that is not listed,
and `(none)` for the cards with no value,
because a board that silently drops issues is worse than one with a ragged edge.
A listed column is drawn when empty; a trailing one only while a card is in it.
The header is each column's name and its count across every lane.

### A card

A card is the short id, dim,
the title wrapped to the column's width,
then each other `card` field on a line of its own as `key: value`,
a relation drawn as the issue it names,
and a blank line before the next.
The card under the cursor is washed whole with its id reversed.

### Board directions

`↑` and `↓` walk the column and past its end into the same column of the next lane that has a card;
`←` and `→` go to the nearest column with a card in this lane,
since an empty column is nothing to stand on —
unless a card is grabbed, when it is exactly where the card goes.

### Column width

Columns keep `column_width`, 32 cells by default.
When they all fit they share the window;
when they do not, the board scrolls sideways by whole columns to keep the cursor's on screen,
`‹` and `›` at the header's ends saying there is more.
It scrolls vertically as one canvas, so the lanes stay aligned.

Not squeezing every column to fit, because a column four cells wide is not a column;
and not a constant, because how wide a column should be depends on the board
— two columns of long titles, or eight of short ones —
which the renderer cannot tell from the data.
32 is a title line that reads as a line; 20 would shred titles to a word a line.
Below 10 is refused at parse time (`Min` on the row), narrower than a card's id line.
A `column_width` left at its default is not spelled on the first line.

### Empty swimlanes

`empty_groups`, with `group_by`, adds a lane for every value the field could hold here that no card has,
each with a ghost in every column,
so a lane is somewhere to put work, not only a heading over work that exists.
A bool, not a list of values, because the case is "show me the rest";
`values` is the model if an explicit list is ever wanted.

The values, by the field's kind:
every value an enum, ordinal enum or `multi-enum` lists, over the types on the board;
`false` and `true` for a bool;
for a relation, the siblings of the lanes present ([below](#a-relations-empty-lanes-are-siblings));
nothing for `text`, `number`, `date` and `identity`, which have no set of values to draw.
`(none)` is still drawn only when a card has no value.
A lane carries the value it stands for as stored, the enum id or the issue's full id,
so a drop into an empty lane, and its ghost, write it with no card to read it from.
`type` as the `group_by` gets no empty lanes.
The counts are of cards, a filter hides the empty lanes,
and a refresh recomputes them, so a lane fills in place and one that empties stays.

### A relation's empty lanes are siblings

A relation's possible values are the whole store,
which is wrong the moment the query narrows the board,
and the query cannot be read for its scope.
So the scope is read from the lanes it produced:
each present lane is an issue with a value of its own for the same key, `group_by` —
an epic's `parent` is its initiative —
and those values are the anchors.
The empty lanes are the other issues whose `group_by` value is an anchor,
of a type the field may target from a type on the board,
unarchived unless `include_archive` says otherwise,
matched by full id.
So tasks under initiative X grouped by `parent` draw X's other epics, never Y's.
It reaches one level only, and names no field, so it keeps the no-field-roles rule.

A present lane with no value of its own anchors on no value,
so grouped by stories that have no parent it is every unarchived story, done ones included.
That is the rule applied consistently; `empty_groups` being opt-in, it is accepted.

## Gantt

### A bar is moved, not edited

`gantt` is a list with a chart beside it:
a row per issue, a bar from `start` to `stop` over a column per period of `scale`.
Its only edits are moves, as on a board:
`Enter` opens the issue in show, copy copies the id, a paste says it has nowhere to go.

### The gantt cursor is a cell

The cursor is a row and a period,
drawn as a shade a step past the row wash in the terminal's own foreground,
not reversed, which on a light terminal blacked out what it covered.
`↑` and `↓` keep the period, because the date is what the eye is on;
`←` and `→` move a period and scroll the chart by whole periods, `‹` and `›` saying there is more.
It opens on the first row and on today's period, the left-most drawn,
because a plan is read from now on;
with `from` or `to` bound, on the period the first row's bar starts in.
The cursor's period is washed down every row and on its label, a crosshair with the row's.
With `expand`, `←` from the first period is the fold arrow's cell.

### Dragging a bar

`Space` grabs, always.
`←` and `→` shift the bar a period:
on its first cell only `start` moves, on its last only `stop`, between them both, keeping its length.
A one-cell bar grows rather than shifts — `←` its start, `→` its stop —
because it is on both edges at once and growing is what it needs most.
A start never passes its stop, and the cursor stays on the edge it drags.
`↑` and `↓` reorder by the built-in rank.
The drop is one `set`, one commit: each date that moved, and the rank when the row moved.
Dropped where picked up writes nothing; `Esc` puts it back;
a refresh while grabbed lets it go;
and the chart grows with a bar dragged past the edge of a chart sized to the data.

### A shift is from the stored date

The drag counts periods and the drop adds that many to the date as stored,
so a bar dragged away and back lands on the day it left,
a month added to the 31st normalizes once, at the write,
a day stays a day and a time keeps its clock.

### A milestone

A row with one date is a milestone, a trail six cells long, and a grab moves the date it has.
A start begins on its period's first cell and fades right, `▓▓▒▒░░`;
a stop ends on its last and fades left, `░░▒▒▓▓`.

### A band begins at today

A row with no start has not started, and could start any time from now,
so its dateless band runs **from today**:
to its stop, or with no dates at all, to the chart's end.
Not from the chart's left edge, which drew weeks already gone as weeks the work might be done in.
A row with a start and no stop fades from its start and runs on to the chart's edge.
A band is its group's tint blended a third of the way to the background,
so the row reads as unplanned rather than missing.
The chart's extent counts a missing start as today for the same reason.

### An overdue stop

A stop that has gone by keeps its marker,
and the band from it to today is the **overdue tint**, red,
which outranks the group's tint: a missed date is what the eye is looking for.
Nothing is drawn past a stop.

### A parent draws its children's envelope

A parent with no dates of its own draws the envelope of its children's, folded or not, in a glyph of its own,
because an empty row would say the story has no plan when its tasks are the plan.
A parent with dates draws them: they are its own claim.

### The label column

`label` is a column left of the chart, with the id, sized to the labels up to two fifths of the window.
Not text on the bar, where a title in a two-week bar is three letters and an ellipsis.

### The chart's extent

The chart runs from `from` to `to` when given,
else over the dates on the chart and today's period;
with neither bound it runs on to fill the window, so today can be the left-most.
Today's period is marked on the rule under the header.
A `from` or `to` that is not a date is refused when the view opens.

### Progress

`progress` names a number, 0 to 1, that fills that fraction of the bar with the done glyph;
without it the whole bar is done.

### Periods are whole

The chart is aligned to the period — a week starts on Monday, a quarter on its first month —
and a bar covers every period it touches, its stop inclusive.
A day or a week is three cells wide, a month or a quarter four.
The header is the periods' own labels (day of month, ISO week, month, quarter),
over them, dim, the coarse ones where they change.

### Gantt groups are colors

`group_by` makes a section per value of the root rows, headed by the value as a cell draws it.
Each group's bars and header take a colour of their own in the groups' order,
yellow left to the grab and the ungrouped plain.

## Matrix

### An allocation is an issue type

*How much of whom goes to which work in which iteration* is an issue of a type
whose fields are relations to its axes and a number;
in this repository, `allocation`, with `iteration`, `work`, `assignee` and `points`, and no status.
Making it an issue buys ids, comments, the CRDT, the lock, the CLI, jq and every view, at no cost,
adds no storage semantics, and leaves the axes to the team.
It has no status because it is a plan, not work: it is true or it is archived,
and a status would put it in every "open work" listing.
Its two relations need two inverse names, `allocations` and `staffing`,
an inverse name being unique within the type that holds it.

Not a map field on the parent, `{"luis": {"sprint-12": 3}}`:
a new value kind, last-writer-wins over the whole map, and unqueryable.
Not a new config-entity shape: it is data, not configuration.

### The presets ship no allocation

`schema init jira` and `init linear` create no `allocation` type.
The presets are those trackers' native models, and neither has one;
nothing is lost, because the matrix needs no particular type.

### A matrix sums any number over any issues

`matrix` is the two-axis summary:
a row per value of `rows`, a column per value of `columns`,
and in each cell the sum of the number field `value` over the issues that have both,
or their count with no `value`.
So `points` per sprint per story over allocations
and `estimate` per sprint per story over tasks are the same call:

```sh
git work view matrix '{"rows":"work","columns":"iteration","value":"points",
  "query":"map(select(.fields.type == \"allocation\"))"}'
```

`value` is optional, not defaulted, because its absence is a different view, a count.
There is no totals switch: totals are always drawn.

### The order of an axis

Each axis is ordered as a board's columns are:
`row_values` / `column_values` when given;
otherwise an enum's schema order over the types on the matrix;
then every value the data has that was not placed;
then `(none)`, last.
The values not placed, and every axis that is not an enum,
are ordered by **the label the axis draws, then the id**:
a relation by the issue's title, a person by name,
a date lexically, and two numbers as numbers.

Not the query's order, because the query returns allocations, not sprints,
and jq can reach only the allocation's own fields, which hold the sprint's hash.
Explicit ids in `row_values` are the escape hatch.

### A multi-valued axis double-counts

An issue whose `area` is `["cli","tui"]` is in both rows, and the totals count it twice.
That is the honest reading of *area by sprint*;
dividing the number between the values would invent data.

### A cell is a sum, or a count

A cell draws its sum plainly (`3`, `2.5`), right-aligned.
A `value` that is not a number contributes nothing rather than an error,
because one bad cell must not take the matrix down.
**A cell with no issues is blank; a cell that sums to zero is `0`**:
nothing allocated, against allocated and spent.

### Totals are of what is drawn

A dim totals row along the bottom, a totals column down the right, and the grand total in the corner,
dim because they are derived.
With `group_by`, each block ends in a subtotal row and the last row is the grand total.
Totals are of what is drawn, so a filtered matrix totals what it shows:
a total that did not add up would be the one way a matrix can lie.

### Blocks of rows, one header

`group_by` splits the rows into blocks, each under a group line, `(none)` last,
sharing **one column header and one layout**.
Not a matrix per group, because the columns are one axis
and two matrices of drifted widths cannot be compared down the screen.

### A cell is opened, never edited

The cursor is a cell, starting on the first data cell; totals are cells like any other.
`Enter` **drills down**: it pushes an ordinary list
whose query is the matrix's own piped into a `select` on the cell's axis values
(`== null` for `(none)`, an `index` test for a multi-valued axis),
the predicate and not today's ids, so the list re-runs to the same cell after a write.
A total drops one clause; the corner drops both.
Its columns are the title, the two axes and `value`,
it carries the matrix's `include_archive` and `shows` (which the matrix uses for nothing else),
and it is a legal `git work view list` call, as its first line says.
Its `draft` holds the cell ([The matrix cell](#the-matrix-cell)).
A sum you cannot open is a number you have to trust.

`Space` rings: a sum is not a value, and an allocation is edited on its show page.
Copy copies the cell's number as drawn; copy id copies the cell's drill-down command,
since a cell has no id and that query is what identifies it.

### The matrix filter narrows the axes

The filter drops the rows and columns whose label does not match.
Not the issues, which would change every sum while the totals still read as totals.

### Sticky row labels

Columns keep a minimum width and scroll sideways by whole columns, as a board's do.
The row-label column does not scroll, and the totals column stays pinned at the right,
because a grid whose labels scrolled off is a grid of numbers nobody can read.
The corner reads `rows \ columns`, so both axes are named on the screen.

### A matrix keeps its cursor by value

A refresh keeps the cursor on the same row and column **values**, not indexes,
so a column appearing for a new sprint does not move what you were about to open.

## Show

### Show is a kind

`show` is a kind, not a mode of the list,
because `git work view show '{"id":"abc1234"}'` is a thing to want on its own,
and `Enter` from every kind opens it.
It is where the fields a board and a gantt do not edit are edited.
It takes `id` and `shows`; how it draws the issue is its type's spec ([Shows](#shows)).

### The show page

The page is four stops, top to bottom, with the side tables a fifth beside the fields ([Side tables](#side-tables)):

1. the **header**: the type, dim; the title, bold over a rule as long as it is;
   and `[x] archived` in the warning colour, or `[ ] archived` dim, always drawn,
   because the toggle has to be in sight;
2. the **fields table**;
3. the **comment box**;
4. the **tabs**: description, comments, log.

`title`, `type` and `archived` are the header, never rows.
`rank` is **not drawn at all**, even when `fields` names it:
it is an order the drags write, a fractional index nobody reads.
In the header, `Space` on the type opens the schema's types, on the title an input line,
and on archived flips it, both ways: the same press undoes an archive.

### Show opens on the fields

The cursor opens on the first row of the fields table,
because every other view opens on a cell and the fields are what an issue is opened to read;
a table with no rows opens on the box.
Not on the comment box, which would leave the cursor below what the page is opened to read.

### The comment box

The box is **one stop, typed in once entered**.
On it, `Space` puts the cursor in the text and `Enter` does nothing;
in the text every key is text, `Enter` sends, `M-Enter` (or `S-Enter` where the terminal tells it) is a newline,
`Esc` opens the palette over the text, which stays focused underneath,
and a second `Esc` leaves the text keeping the draft.
This is the page's own rule, one `Esc` the palette and two back, one level in,
so `Esc` means the same in the text and outside it;
from a text the palette offers only the text's commands
([the palette over a text](tui.md#the-palette-over-a-text)).
Not typing on arrival, because a page whose every letter is text has no keys of its own.
Not `Enter` as newline, because once typing is entered with `Space`, `Enter` sending is what every chat box does.
On the box the cursor washes it whole, every line, and marks it `›` as a row is, on its first line only;
in the text the textarea's cursor is drawn instead, so on and in are two looks.
Not a `›` on every line: a marker repeated down the margin would read as a list, not as one place.
The box always has a one-column gutter on its left, where that `›` is drawn,
so its text is in the same column whether the cursor is elsewhere, on the box, or in its text.
This holds wherever the box is drawn: show's comment box, the description editor, and the creator page's description box.
Its keys are on the bottom line, as every cell's are.
It opens two lines tall and grows with the text, its highlighted cursor line turned off.
No arrow enters or leaves it: it is a stop of `Tab` and `S-Tab` alone,
because the row drawn beside the box is not where the person came from.
There is no submit button: it would be a second way to do one thing.
Going back with a draft asks for a second `Esc`, a draft being the one thing on the page the store does not have.

`esc e` (`M-e`), on the box or in its text, writes the draft in an external editor.
The editor is git's, in git's order — `GIT_EDITOR`, `core.editor`, `VISUAL`, `EDITOR`, then the first common one on the `PATH` —
because a tracker kept in git uses the editor git does.
It is a shell command, as git's is, so one with arguments (`code --wait`) runs.
The draft goes to a temporary `.md` file and the screen is suspended while the editor runs;
on its exit the file's text, less its trailing whitespace, is the draft, and the cursor is in the text.
`Enter` still sends: not *saving sends*, because on new `Enter` accepts the draft rather than creating,
so one exit would mean two things.
The editor failing, or the file left as it was, leaves the draft as it was, a failure said in the status line.
`M-e` is no textarea key, so it is read before the text gets it,
and from the text `Esc` then `e` typed slowly is the palette's `e`, the same key.
Anywhere on show or new but a text box it rings.

### The tabs

The tabs are drawn as tabs under the box.
`←` and `→` switch them from everywhere but the header and the box's text,
and `C-PgDn`/`C-PgUp` from everywhere, the box included.

- **description**, the first and the one the page opens on, is the issue's first comment, wrapped to the window;
- **comments** is every comment after it, in full, as it reads now;
- **log** is what was done, an operation to a line.

Comments and the log run **newest first**, by the operation log's causal order, never the wall clock,
because an issue is opened to see what changed since last time.
They are two tabs, not one timeline, because reading a discussion and reading what changed are two reasons to open an issue.

### Editing the description

`Space` on the description tab edits it:
the comment box over the body's current text, with the box's keys,
and `Enter` writes an edit of the first comment.
An empty body is refused with the bell: the model has no issue without one.
The editor stands where the text was read and stays, blurred, while it holds an unwritten draft;
`Esc` opens the palette over it, as in the box, and a second `Esc` leaves it;
leaving the page with a draft asks for a second `Esc`, and an editor left untouched keeps nothing.
Where the author and time stand over the description,
a dim line over the editor says what it holds,
*being rewritten* while the cursor is in it and *a draft, not written* after it is left,
so a draft never reads as the description written.
Its keys are on the bottom line, as the box's are
([key hints are on the bottom line only](tui.md#key-hints-are-on-the-bottom-line-only)).
`esc e` on the tab or in its editor writes the text in the external editor, as on the comment box:
the editor's draft, or the body the store has when none is open;
on exit the editor is open on what came back, and `Enter` writes it.

### The fields table

Two columns, the key dim and the value, with no header row.
A relation is a link the cursor stands on, and `Enter` follows it;
a `multi-relation` is a line per issue it names.
A stored relation drawn as a side table is left out of the table, unless the spec's `fields` names it:
one issue's `blocks` drawn twice is noise.
`Space` edits the value under the cursor, by the field's kind
([edit picks its widget from the schema](tui.md#edit-picks-its-widget-from-the-schema)).

### The links row

The fields marked `link` ([schema](schema.md)) are **not rows**.
In their place is **one row, `links`**, at the first link field's place in schema order:
the count, then each relation with any entry and its count, stored first in schema order, then inverses,
`3 · blocks 1 · implements 1 · is-blocked-by 1`, or `—` with none.
A Jira site carries a dozen link types, mapped onto every type and empty on most issues;
drawn as rows they would be half the table.
A spec's `fields` naming a link field draws it as a row and leaves it out of the count;
naming `links` places the row, and a `fields` without it drops it.
`links` is a word of the views, not a field: the schema refuses it as a key.

### Editing links from the row

`Enter` on the `links` row moves to the first link table, the row being their table of contents.
`Space` opens a picker of the link relations, **stored and inverse**, each with its count;
choosing one opens the finder over its membership, and a write there is one `add` or `remove`:
to this issue's field for a stored relation,
to the other issue's stored field for an inverse —
adding `is-blocked-by X` is `issue add X '{"blocks":[this]}'`,
one commit to the one entity written, checked against its type,
the finder offering only issues whose type has the stored field.
The palette's link and unlink are this picker from anywhere on the page ([tui](tui.md)).

## Shows

### `shows` maps a type to a spec

Every kind that opens an issue takes **`shows`**, and so does show:
a type key mapped to a **spec**, which is what show takes less the id:
`fields`, the rows of its fields table,
and `expand`, its side tables.

```python
work.view.list(
    query = OPEN,
    shows = {
        "story": {"expand": [{"relation": "children", "fields": ["status", "title"]}, "links"]},
        "task":  {"fields": ["status", "priority", "parent"]},
    },
)
```

`Enter` on a row, a card or a bar opens show with the row's id and the view's `shows`,
and show draws the issue by its type's spec.
So a story is drawn one way across a flow, opened from a list, a board, search or a link.
A list's and a gantt's `expand` is their nesting, the core's, and is not in a spec.

### A spec is whole, never merged

A type's spec is, most specific first:

1. the call's `shows[type]`;
2. the type's `show` in the schema;
3. the built-in spec, `{"expand":["links"]}`, every field in schema order.

The first that exists is used **whole**, never merged key by key,
because a spec reads as one sentence,
and a call that said `fields` and silently inherited the schema's `expand` would draw something it never said.
The schema's default exists because search and a direct `git work view show` open pages no flow called.
A schema spec that no longer reads falls to the built-in.

### The type is the stored issue's

The spec is picked by **the issue's type as stored**,
never a row's `fields.type`, which a query may shape:
the page shown is the issue's.
It applies at every nesting level; the type picks the spec, not the level.
A row with no id rings.

### Every page carries the map

The page `Enter` opens carries the view's `shows`, and so does every page it opens:
a side table's row, a relation cell, the creator a ghost opens and the page after Create.
So a drill-down from an epic to a story to its tasks reads the same at every step,
and `Esc` back up the stack returns to pages that still have their tables.

### A spec is checked as show's call is

Each spec is checked in `host.View`, against the type it is keyed under, before anything draws:
a key that is not a type is refused naming the types;
a field the type lacks, a relation it does not have stored or inverse,
and anything a side table refuses, naming the type.
The schema's own `show` gets the same check at import.

### A defaulted spec is drawn dim

A spec from the schema or the built-in is drawn dim on the first line, `shows.story={…}`,
as a defaulted argument is, so the first line is still the call that drew the page.

### Changing the spec from the page

A show page's spec can be changed in place, a relation's table at a time,
and the changed spec becomes the call's `shows[type]`, whole, for this page and the pages it opens;
nothing is written ([changing what is expanded](tui.md#changing-what-is-expanded)).

## Side tables

### Side tables are `expand`

A spec's `expand` is **what the list's `expand` takes, or a list of it**,
one table per element, in order, drawn beside the fields:

```json
{"story": {"expand": [{"relation": "children", "fields": ["status", "assignee"]}, "blocks"]}}
```

It is the list's argument because it is the list's object,
the issues one relation reaches from one row, narrowed and drawn as a table:
a nested list draws it under a parent, show beside the one issue.
So the parser and the schema check are the same,
a relation name is either side of a relation,
and narrowing to one type is the layer's `query`.
`links` is a table per link relation with any entry, stored then inverse, each headed by its name;
an issue with none draws none and no `(none)`, the links row already saying so.

### A side table is flat

A side table takes `relation`, which it needs, `query`, `include_archive` and `fields`;
`details`, `group_by` and a nested `expand` are **refused by name**,
because a side column is too narrow for a second line, a section or a tree.
`fields` are the columns after `id` and `title`, defaulting to nothing more, there being no field roles.

### Side layout

Each table has a heading, the relation as the spec gave it,
then a dim header, `id`, `title` and the `fields`,
columns measured over every row, the title taking the slack,
a person by name and a relation as the issue it names.
Tables stack with a blank line between; a table with no row reads `(none)`.

The fields block is two columns divided by a dim `│`:
on the left the 16-cell key and the values, as wide as the widest value up to 36 cells;
on the right the tables, **beside** when the rest of the window holds every table at its natural width
with the title at least 20 cells, otherwise **under** the fields, full width, before the box.
Width alone decides, since which side a table is on says nothing about it and the keys are the same both ways.
The side column's first line is the fields table's first line.
It is at most the taller of the fields table and 12 lines,
past that scrolling within itself, the cursor's table heading and header kept on top,
a dim last line saying how much is out of sight,
so a story with forty tasks does not push the box a screen away.

### Side tables are a stop

The side tables are one stop between the fields and the box,
reached and left by `Tab` and `S-Tab` only, as the box is.
`↑` and `↓` walk every row of every table, ghosts included,
and ring at either end: `↑` from the first row and `↓` from the last.
Not `↑` from the first row to the header:
under the fields, in the narrow drawing, it would jump over the whole fields table,
and even side by side the header is not where the person came from.
Not arrows that depend on the layout: the keys are the same both ways, the width alone deciding the layout.
`Enter` on the fields' `links` row still moves into the link tables, not being an arrow, and `S-Tab` is the way back.
`←` and `→` stay the tab keys, so no stop answers them two ways depending on the call.
**A row is one issue, and the cursor is a row**:
`Enter` opens it by the map, and `Esc` there comes back to the same row;
`Space` grabs it to move within its table, the drop writing the child's rank, the order it has on every list too;
a row cannot leave its table, because which relation reaches a row is not an order;
copy copies its id;
the filter narrows the side rows by id, title and drawn fields.
A child's own fields are edited on its page, one `Enter` away.

### Side tables are live

The tables are recomputed on every load, the ref watcher's and the page's own writes alike,
and the cursor is kept by the row's id.
A query that fails on the data is said in the status line and leaves its table unnarrowed.
`new` draws no side table: a draft has no id, so nothing can point at it.

## Creating

### `new` is a view kind

`git work view new [KWARGS]` and `work.view.new(**kwargs)` are the interactive `issue new`.
It takes `draft`, the document `issue new` takes — `{"fields": {…}, "body": "…", "aliases": {…}}` —
with what is already decided filled in, and `fields`, as show's.
`work.view.new(draft=d)` is `work.issue.new(d)` with a person in the loop.
It is a kind and not a mode of show for the reason show is not a mode of the list:
`git work view new '{"draft":{"fields":{"type":"task","parent":"abc1234"}}}'` is a thing to want on its own.
`draft` is checked at the call as `issue new` would check it (`host.IssueDraft`),
so a flow learns at once rather than after the person has typed.
Not `doc`: the name `draft` says what it is on `new` and on every kind with a ghost alike.

### Nothing is written until Create

The page holds a draft, and **Create commits it as `issue new` would**, one operation through `host.IssueNew`;
the page has no write path of its own.
Not a placeholder issue opened in show, because the title cannot be cleared,
so a change of mind is an archive and a tombstone;
the Jira sync would export the placeholder on its next run, and a local archive does not delete it there;
and the create's one commit would become a create and a trail of sets.
With a draft, the sync first sees the issue in its final form, and its create path needs no special case.

### The creator page

Show's stops, less the tabs and plus one:
the header (type and title, no archived), the fields table, the description box, and **Create**, a button.
The type picker is the schema's types; with no type the table is empty, the fields being the type's.
The fields table has show's editors and the `links` row over the draft's stored link fields,
offering no inverse, since the draft has no id to write into another issue.
The description box is the comment box over the body; `Enter` accepts the text into the draft, and an empty one is allowed.
`Esc` in it opens the palette over the text and a second leaves it, keeping the text, as in show's box.
`esc e` writes it in the external editor as on show's box, what comes back being the draft's body; nothing is created on the editor's exit.
Create is the only way to commit, and `Enter` elsewhere opens or does nothing, so nothing creates by accident.

The page opens **on the title, typing**, when the type is set, and on the type cell when it is not:
a ghost's `Enter` means *I want to add one*, and the title is what is typed next.
`Tab` reaches Create; the arrows do not cross the box.
Changing the type keeps the values the new type also has and drops the rest, with a count in the status line.
`Esc` with anything typed asks for a second `Esc`.
Create refuses what the planner refuses — a missing type, an empty title — and puts the cursor on the cell it names.

### What Create does next

Opened from a view, Create **pops back**: the view reloads, and the cursor lands on the new issue,
the proof that it appeared where the ghost was.
An issue the view's query filters out is named instead: *created abc1234, not in this view*.
Run standalone, Create replaces the page with show on the new issue,
so a first view stays a view, worth standing on.

### `new` prints the id

`git work view new` prints the created id, or nothing when left without creating,
and `work.view.new` returns it, or `None`.
It is the one view that prints, because it is a writer, and a writer prints the id it created or nothing ([cli](cli.md));
a flow that goes on to set a field on what the person made needs the id.

### The form invents no default

A field left empty is **null in the commit**, never a first value or a guess:
locally it is a field unset,
and toward Jira a key held null takes Jira's value after the create ([jira](jira.md)),
so a priority Jira fills on create is imported rather than overwritten by one the form made up.
The draft starts as `draft` and the ghost's prefill, and nothing else.

### The ghost

Every view that lists issues has a place to add one: the **ghost**,
a dim `+ (new)` row at the foot of each scope of a list and a gantt,
a `+ (new)` card at the foot of each column of each lane on a board,
and a row at the foot of each table on show.
The cursor reaches it with the arrows and `Tab`;
`Enter` opens `new` with the scope's prefill;
`Space` rings, there being nothing to edit or grab; copy copies nothing.
It is **never a rank sibling**: a grab skips it, a drop above it is a drop at the foot, the rank fills do not count it,
and the issue it creates gets no rank, a null rank already sorting last, where the ghost stood.
A filter hides it, having no text to match.
There is no new-issue key on the page: the ghost is where the prefill comes from.

### One ghost per scope

A **scope** is a set of siblings and a group among them:
the roots, or one opened parent's drawn children,
and, where that level's layer groups, one group of them.
Every scope ends in a ghost, at every level of a nested list and a gantt,
by one walk over the drawn tree, the roots being the scope whose parent is no row.
At the foot of an opened parent's child table, the ghost also divides that subtree from the next root,
so a tree opened in several places reads as blocks.
A folded table's ghost is hidden with it, and a fold's count never counts a ghost.
A child scope with no rows in a level that groups has one ghost, in `(none)`, as the roots do.

### A leaf unfolds to an empty table

A row whose type could hold the layer below — it stores the relation, or a type that stores the inverse may target it —
draws a fold cell even with no children, `▸ 0`, faint,
and opens to an empty child table: its header, then its ghost,
so the first child is added where every other is.
Faint, so a column of leaves does not read as a column of parents;
the digit kept, so the tree cell's width is measured alike for every row.
The cost on a tree of leaves is nothing until a fold opens.
`open`, and folding every row, open the rows with children only;
opening the leaves too is its own palette command ([tui](tui.md)).

### No ghost where there is nothing to write

A parent with no id (a keyed row), a layer whose rows above list their own `children`,
and a relation whose write set cannot be named have no ghost:
a `+` that cannot put the issue where it stands would break the one promise it makes.

### The prefill is the drop's write set

A ghost's draft is what a drop at its place would write:
the group's stored value for `group_by` (`(none)` leaving it null, `type` as the `group_by` prefilling the type),
on a board the column's value and the lane's,
and under a parent the relation to it ([below](#a-relation-stored-on-the-parent)).
`type` is the one every row of the scope shares;
a jq query cannot be inverted, and the rows it produced are the best witness of what it selects.
Where they share none, it is the one type the schema says can hold the relation, else none,
and the creator opens on the type cell.
An empty lane, with no cards, takes the type every card on the board shares.
Every prefilled cell stays editable: the prefill is a hint, never a constraint.

### `draft` on a view

The list, the gantt and the board take **`draft`**: what an issue created in this view starts with.
A ghost knows only what its view arranges — its group, its layer's relation, its rows' type —
never the query, so a list over sprint 41 would make issues that fall out of it the moment they exist.
The caller that wrote the predicate knows, and `draft` is where it says so.
It is not sugar: it is information the view has no other way to get, one value in the one KWARGS object,
checked at the call as `new`'s is.
All three kinds take it, because all three draw ghosts that cannot read the query.

### One construction, every ghost

Every ghost's draft is built in one order:

1. the call's `draft`;
2. the type the scope's rows share, where `draft` names none;
3. the group's value, which wins on its key, the ghost standing in that group;
4. under a parent, the relation write.

A child scope inherits `draft`, since an issue made under a parent is made in this view:
sprint 41's stories expanded into tasks make tasks in sprint 41.
Then what the scope's issue cannot hold is dropped, as a change of type drops it:
a `draft` type that cannot hold the child relation gives way to the scope's own type,
and a field the resulting type lacks is dropped.
A child stored in a set joins the parent's id to what `draft` gave the key.

### A relation stored on the parent

Which side the relation is stored on is decided per parent type:
the parent's own relation field of that name, else the inverse, stored on the child.

- **On the child** (`children` through `parent`):
  the draft holds the stored field set to the parent's id, or a set holding it;
  Create commits it in the issue's one operation, and nothing else is written.
- **On the parent** (its own field lists its children):
  once Create has committed the issue, the opener writes the parent's field to hold the new id,
  `issue add` for a `multi-relation`, whose set semantics merge with a concurrent add,
  `issue set` for a single `relation`.

The parent-side write is **an ordinary relation write** that lands on another issue,
one commit on one entity, as linking an inverse is ([Editing links from the row](#editing-links-from-the-row)).
Nothing batches it: there is no atomic multi-entity commit, and a relation between issues is eventually consistent, like Jira.
No verb does create-and-link, and `new` takes no argument naming a second write:
it is two calls that exist on both sides, `work.view.new(draft=d)` then `work.issue.add(parent, …)`,
and the ghost makes them on the person's behalf.
So show's tables over a stored relation of the shown issue have a ghost too.
An abandoned draft writes nothing, link included.

### A single relation on the parent replaces

While the parent's single relation is empty its ghost reads `(new)`;
while it is set, `(new, replacing abc1234)`,
because a `+` that silently swapped a link would do more than it says.
The issue replaced is not touched; only the parent's field changes.

### When the parent's write fails

When the parent refuses the new issue's type, or is gone, **the issue stays created**:
it is complete on its own, and undoing it would leave a tombstone for a link that failed.
The status line says both halves, *created abc1234; not added to def5678's blocks: …*,
and the cursor goes to the new issue where the view shows it, else stays on the ghost.

### The matrix cell

The matrix draws no ghost.
**Every cell drills down**, an empty one included,
and the list it pushes carries the cell in its `draft`:
each axis field with the cell's value (`(none)` left out, a multi-valued axis as a set of the one value),
the block's `group_by` value,
and the type every issue on the matrix shares, so an empty cell's list still knows it —
never `value`, since the form invents no number.
So a cell gets its next allocation through its list's ghost,
and a blank cell stays the matrix's *nothing here*, one meaning under `Enter`, not two by its contents.

## Deliberately absent

- **Editing in the matrix.** A cell's number edited in place would be a write the matrix invents.
- **A wildcard spec** for every type not listed: the schema's `show` and the built-in cover them.
- **Editing a side table's cells**: `←`/`→` are the tab keys, and the child's page is one `Enter` away.
- **Storing links differently**, as one field of `{type, target}` pairs:
  every query would grow longer, and `blocks` would stop being a field a flow can name.
- **The external editor on the command line.**
  `issue comment new` stays plumbing, its body an argument or `-`:
  an agent calls it, and an editor prompt would block it.
