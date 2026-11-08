# The terminal app

The terminal renderer is git-work's only interactive surface:
a stack of pages, one keymap, a palette of commands,
pickers, the grab, and the refresh that keeps a page true
while agents write from other processes.
This document is that shell, the part every page shares.
What each kind of page draws and the arguments it takes are [views](views.md);
what `git work` opens on, and how a flow runs, are [flows](flows.md);
the query a page runs is [query](query.md);
the command line a page is a call of is [cli](cli.md).
The code is `tui/`.

## The renderer

### There is no tui command

The renderer sits behind `git work view KIND [KWARGS]`,
and behind any flow that calls `work.view.*`,
both reaching it through `host.View`.
It is not a surface with a command of its own:
a view is the command.
It is the one backend;
there is no GUI, and no `--gui`.

`git work termui`, git-bug's gocui interface,
is one of the frozen legacy packages and is not this renderer.

### Bubble Tea, Go only

The framework is Bubble Tea v2
(`charm.land/bubbletea/v2`, with `bubbles/v2` and `lipgloss/v2`),
Go only, like every other surface.

### The call is the page

A page's input is one KWARGS object,
exactly the object `work.view.KIND(**kwargs)` takes,
so `git work view board '{"columns":"status"}'` and `work.view.board(columns="status")`
are one call spelled two ways,
and there is nothing left to serialize.
There is no spec:
nothing reads items from standard input,
and `git work view` has no `--format`.
Package `view` keeps the kinds table,
which the view functions validate against,
the renderer reads,
and the help is generated from.

No terminal is an error.
An agent that wants the data does not open a view:
it runs `git work issue list PROGRAM`, which is where the data is.

### The first line is the call

Every page's first line is the call that drew it:
the kind, a lead in place of the argument it names (`show 3a2279e`),
then the arguments,
because the one line that answers "what am I looking at?" is the call.
`fields` and `card` are left off, since the columns and the cards already say them,
and the query goes last without its name:
it answers "why these issues?", and it is the one argument that runs long,
so it is what a narrow window cuts.
An argument at its default is dim, so the ones somebody chose stand out.
The whole call, uncut, is on [the palette](#where-you-are).

### Pages are a stack

`Enter` on an issue pushes its page;
back pops it, revealing the one under it where it was.
Every page in the stack is told every resize and every refresh,
so the one underneath is current the moment the top one closes.

A flow's view call made while the program draws
is a page pushed onto the same stack, not a second program:
the flow blocks until that page leaves the stack,
then gets the page's answer and goes on, as it would from the shell.
Flows run on the worker,
the goroutine `Render` was called on,
which is the one a Starlark thread may be re-entered on;
the program runs on its own,
so navigation and redraw never wait on a script
(`tui/home.go`, `tui/tui.go`).
A flow that prints gets its output on a page of its own,
and a failure whose reason is a backtrace does too.

## Keys

### Core keys, and the rest on the palette

The keys a person needs with no menu are the ones every program has:
the arrows, `enter`, `space`,
`tab` and `shift+tab` (the next and previous stop),
page up and down, home and end.
Every other command is on [the palette](#the-palette), under a letter.

### One keymap

The standard keys and emacs's `ctrl` and `alt` keys are read, and nothing else:

| Action | Keys |
| --- | --- |
| up, down | `↑` `↓`, `ctrl+p` `ctrl+n` |
| left, right (column, tab) | `←` `→`, `ctrl+b` `ctrl+f` |
| page up, page down | `pgup` `pgdown`, `alt+v` `ctrl+v` |
| first, last | `home` `end`, `alt+<` `alt+>` |
| next, previous tab | `ctrl+pgdown` `ctrl+pgup` |
| filter | `/`, `ctrl+s`, or any character typed |
| back | `esc`, `ctrl+g` |
| quit now | `ctrl+q` |

Emacs's keys stay because they never collide with typed text.
vim's single letters (`hjkl`, `g`, `G`, `y`, `p`, `q`, `gt`) are not read,
nor its `ctrl+u` and `ctrl+d`,
because a letter at the top level is worth more as text.
Not three families with a tab each in the help:
three spellings of every key read as noise to all three,
and vim's letters are the ones typing uses.

### `esc x` is `alt+x`

A terminal sends `alt+x` as `ESC x`,
which is also what `esc` then `x` typed quickly sends.
So the two are one key, by definition, on every terminal:
`alt+s` is search, as `esc s` is,
and the palette's letters agree with emacs's `alt` keys wherever emacs has one,
being the same keys.
Bubble Tea v2 already asks every terminal for the kitty protocol's disambiguation,
which removes the wait after a lone `ESC` and tells `shift+enter` from `enter`;
no key means something different because of it.

### Typing filters

A printable key on a list, a board or a gantt starts the filter with that character.
`/` starts it empty, and `?` is the help; neither starts a filter.
`enter` keeps the narrowing, `esc` drops it,
and typing over a kept filter adds to it.
The filter narrows what is drawn, locally:
it never writes and never changes the query,
which is the page's input (see [Editing the query](#editing-the-query)).

### Enter opens, space edits

`enter` goes somewhere and never changes anything:
on a row or a cell it opens the row's issue,
on a link the issue the link names, at once,
on a card or a bar its issue,
and in a text box it sends.
`space` changes what is under the cursor:
on a cell it edits it,
on a nested row's arrow it folds,
and on what has no cell to edit (a list's id, a card, a bar) it grabs.

Two keys, one to go and one to change, is the split every file manager has.
Not one action key that opens, edits or follows by where it lands:
it would read as one key meaning three things
and make a link take two presses to follow.
Not `ctrl+enter`:
without the kitty protocol a terminal sends the same byte for it as for `enter`,
so it would be one key on one machine and another elsewhere,
and a key that is one key here and another there is not a key the renderer reads.
Not `F2` either: a function key is not where a hand is.
Every action is the direction keys, `enter` and `space`,
which every terminal has had since the VT100.

### `ctrl+q` quits, `ctrl+c` copies

`ctrl+q` quits at once, from anywhere:
grabbed, in a text box, in a picker, on the palette.
The stack reads it before any page does,
because anything that can swallow the quit can strand the user.
`ctrl+c` copies:
in a raw-mode terminal it is a keystroke, not a signal,
and it is the key a standard user copies with.

### Back is two presses

`esc` on a page at rest opens the palette,
and a second `esc` is back:
out of a kept filter, out of an issue,
and from the first page out of the program.
`esc esc` typed quickly arrives as `alt+esc`, and is back too.
So one stray `esc` cannot end a session,
and the place it lands instead is the palette,
which is a place worth standing on.
A page holding a draft says *back again drops draft* on the first back.

While a page is editing, filtering, grabbed or showing the help,
`esc` is that state's own and cancels it, and nothing else.

### The help is the keymap

`?` lists every action,
one row each: its keys, its `alt` key, its letter on the palette, and what it does,
under dim column headers that say which is which (*keys*, *alt*, *palette*, *action*).
It is generated from the keymap in `tui/keys.go`,
so it cannot fall out of date.
It has the bottom line every page has, reading *esc: close*,
and no line of its own explaining the table:
a sentence there would read as a hint,
and `esc` closes the help on its first press, so a hint past it would never be read
([Key hints are on the bottom line only](#key-hints-are-on-the-bottom-line-only)).
Where the window is too short for the table, the table is cut and the bottom line stays.

### What has nothing to do rings

A key with nothing to do where the cursor is rings the bell,
the terminal's own blink,
and the status line says why.
A refusal from a write (a schema violation, a lost race)
does the same, and nothing on screen moves.

## The palette

### The palette is a box over the page

The palette is a bordered box in the middle of the page,
at most 80 columns inside and shrinking to the window,
with the page still drawn around it
and the palette's own status line under both.
Not a panel at the foot:
there it read as the page's last lines changing,
not as a menu opened over the page,
and it hid the rows nearest the status line,
where the eye already is.
The search, the flow picker and the query editor are drawn in the same box.

### Where you are

The box opens with where you are.
The stack is a breadcrumb (`home › overview › show 3a2279e`),
ending in the top page's kind and lead,
so the call under it starts with the arguments:
one a line, the name in the left column and the value beside it,
a default dim,
and a long value wrapped rather than cut,
since the palette is where a call is read in full.
The query is a section of its own, labeled `query`,
formatted by `jq.Format` the way gofmt would print it:
a top-level pipe one stage per line,
and anything else broken at its own seam where it does not fit,
because a jq program folded onto one line is unreadable.
A program that does not parse is cut at its top-level pipes instead.
The section is at most twelve lines, the twelfth saying how many more there are,
and it gives up lines first where the window is shorter.

Then this page's commands, and everywhere's.

### A letter means one thing on every page

Each command is drawn with its letter,
and a letter means one thing on every page,
so the menu teaches the keys it lists.

| Letter | Command | Where |
| --- | --- | --- |
| `f` | filter | list, board, gantt, matrix, a show's side table |
| `z` | fold all, or unfold all | a nested list or gantt |
| `Z` | open every row, leaves too | a nested list or gantt |
| `w` | copy the cell | list, matrix, show, new |
| `c` | copy the id; on a matrix, the command | list, board, gantt, matrix, show |
| `e` | change what is expanded | show |
| `l` `u` | link, unlink | show |
| `d` | delete the comment under the cursor | show's comments tab |
| `j` | edit the query | every kind that takes one |
| `J` | reset the query | once it was edited |
| `s` | search | everywhere |
| `n` | new issue | everywhere |
| `h` | home | everywhere |
| `r` | run a flow | everywhere |
| `?` | help | everywhere |
| `q` | quit | everywhere |
| `g` | close the palette | everywhere |

The letters emacs gives `alt` keep emacs's meaning
(`w` copies, `v` pages up, `<` and `>` go to the first and the last);
the rest take letters emacs leaves free.
`v`, `<` and `>` are not listed, since page up, home and end are core keys,
but they work all the same, being `alt` keys.
A shifted letter is the further step of its own:
`Z` opens further than `z`, `J` undoes `j`.
`z` and `Z` have no key on the page, where they are text.
On macOS `alt` reaches the program only when the terminal sends Option as Meta;
the palette works either way.

### Keys on the palette

A letter on the palette is handed to the page as `alt+letter`,
so `esc w` typed slowly and typed quickly are one code path.
A printable key that is no command rings and leaves the palette open.
A direction, `enter` or `tab` only closes it;
any other key closes it and then means what it means on the page.
`g` closes it and does nothing else, as emacs's `C-g` quits what is half typed;
`esc g` typed quickly is `alt+g`, which nothing on a page binds.
`ctrl+c` copies the top page's call as the shell command that draws it,
`git work view KIND '{…}'`,
so the command behind the screen is one keystroke from the shell.

### Editing the query

`j` makes the palette's query section a text box, the query formatted in it.
`enter` draws the page again from its call with that query in place,
`alt+enter` is a newline,
and `esc` leaves it as it was.
An empty query is the default.
A query that does not parse, or that the page refuses,
keeps the box open with the reason under it,
so nothing typed is lost to a typo.

The page is rebuilt from the call, in the same place in the stack:
the call is the page,
so a page with another query is another page,
and its cursor, folds and filter start over.
The stack keeps the call the page was opened with, by place,
so `J` puts it back on every kind without the kind knowing,
and a flow waiting on the page still gets its answer when it leaves.
Only the top-level `query` is edited; a layer's is still the call's.

### Linking from the palette

On a show page,
`l` picks a link relation by typing (`blo` is `blocks`),
then its target in [the finder](#the-finder);
`u` picks one of the issue's links to remove,
one choice per link, the issue drawn and its relation dim beside it.
`l` is the `links` row's picker reached from anywhere on the page,
not a second link path,
so it offers the link relations only, stored and inverse,
and choosing an issue already linked removes it, as on the row;
every other relation already has its own way in
(see [views](views.md) for the `links` row).

Inverses are offered:
adding `is-blocked-by X` writes `X.blocks += this`,
one commit to one entity, the other one,
so nothing has to be atomic.
The finder narrows to the issues whose type has the stored field,
the write is checked against that type's schema,
and the status line names the issue written.

### Changing what is expanded

`e` on a show page is a picker that toggles:
the type's relations, stored and inverse, then `links`,
the ones the page's spec expands marked `● expanded`.
`enter` toggles the one under the cursor and the page is redrawn at once,
the picker staying open, so several are toggled in one go.
The spec the page was drawn by becomes the call's `shows[type]`, whole,
with the relation taken out of `expand` or added at its end,
so the first line still spells the call that drew the page
and `ctrl+c` on the palette copies it.
Nothing is written, not the store and not the schema:
the changed `shows` is the page's, carried to the pages it opens,
and going back is going back to the spec the page under it had.

### Deleting a comment

On show's comments tab, `esc d` deletes the comment under the cursor.
A delete is final and replicated
([issue](issue.md)),
so it is a chord, never one stray key;
it asks nothing first, there being no confirmation in the renderer.
The log tab draws the operation as *deleted comment*.

## Pickers

### Pickers are typed into

Every picker opens with its input focused:
typing narrows, the arrows move, `pgup` and `pgdown` move ten,
`enter` chooses,
the first `esc` clears the text and the second cancels.
`/` is text in a picker.
A picker draws ten choices at a time around the cursor,
which starts on the current value, marked *● current*,
goes to the first match as the text changes,
and returns to the current value when the text is cleared.
A static picker (an enum, the people) narrows on the label,
the dim text beside it, and the start of the value;
a person matches on name, login and email, the login not drawn.

A paste that names a choice outright (a value, a label, a person)
puts the cursor on it with the text left empty, so `esc` still cancels;
any other paste is typed in, and refused if it keeps nothing.

### The finder

The issue picker is the finder,
one widget in three places:
a relation field's picker,
the target of a link,
and the palette's search.
Its choices are `host.SearchExcerpts`'s answer to the text,
asked again on every change,
so it matches and orders as `git work issue search` does
([query](query.md)).
It hands back the first 200 matches and counts the rest
(*… N more: type to narrow*):
the search is a scan, and drawing thousands of rows on every key is what would cost.
A relation's finder is narrowed to the field's `target_types`,
leaves out the issue itself and the archived,
lists the current value whatever its type or archived state,
and ends in *(none)*, which clears.

### The search

`s` opens the finder over every issue, in the palette's box,
as tall with one match as with many so it does not move under the text.
`enter` pushes the issue's show,
opened by the page's own `shows` as `enter` on one of its rows would;
its first `esc` clears, its second closes it with nothing pushed.
`r` is the same box over the stored flows, and runs the one chosen,
its page over this one.
`h` goes back down the stack to the home page,
or runs the home flow over this page where the stack has none.

## Cells

### Edit picks its widget from the schema

The field's kind, from the live schema, picks the widget:
a value list for an enum,
the people for an identity,
the finder for a relation,
an input line for text, a number and a date,
and a bool flips at once.
`enter` in the widget writes through `host`, one commit.
An empty input clears the field, except the title, which no issue can lack.

A cell that cannot be edited rings, saying why:
a column that is not a field of the row's type,
a set-valued field, which is `git work issue add` and `remove`.
A `multi-relation` has no picker:
a checkbox list would commit the difference as several adds and removes,
and one edit must be one commit.

### A relation is a link

A `relation` or `multi-relation` cell holds the other issue's whole id,
and nobody reads a 64-character hash,
so it is drawn as the issue it names: its id and title, underlined.
`enter` on it follows it, at once
(on a list cell of a `multi-relation`, the first);
`space` opens its picker on the current value.
An empty relation has nothing to go to:
`enter` opens the row's issue on a list, and rings on show.
A link to an issue the store does not have yet is its short id alone.

Wherever an issue's id is drawn (a link, an id column, a card, a picker, a message)
it is its `human_id`, which is an alias where `git-work.display.id` names one
([cli](cli.md)).

### A person is a name

An `identity` cell holds an identity's whole id,
and is drawn as what it names:
its name, else its login, else the short id (`host.UserName`).
Everywhere a value is drawn it is the name,
a group's, a lane's or a column's header included,
and the filter matches on it.
It is not a link, because a person has no page:
`space` edits it, a drop into a person's column writes the id,
and copy copies the id.

### Copy and paste are the terminal's first

`cmd+c` and `cmd+v`, `ctrl+shift+c` and `ctrl+shift+v`,
are the terminal's own copy and paste, and stay its own:
the renderer defines none of the keys a terminal already means something by.
Where a terminal hands one over instead of keeping it,
it means the same thing here.
`ctrl+c` is the copy key the renderer does define, because no terminal keeps it,
and it copies the cell under the cursor;
`esc c` copies the issue's id from any column.
The cursor starts on the id, so copying on arrival copies the id.

A copy goes out over OSC 52,
which reaches the clipboard of whatever terminal is in front, across ssh,
and also through the machine's clipboard tool where there is one and a display
(`wl-copy`, `xclip`, `xsel`, `pbcopy`),
because not every terminal honours OSC 52.
What is copied is what is shown:
an alias as drawn, a hash whole, a link the ids it holds.
The message names the short id, since a hash right-aligned is a hash with its head cut off.

A paste arrives as a bracketed paste
and opens the editor on the field under the cursor with the text in it,
so `enter` writes it and nothing is written by a paste alone.
`ctrl+y`, and a paste key the terminal hands over,
ask the terminal for its clipboard over OSC 52, which it may refuse.
`ctrl+v` stays emacs's page down:
a standard user's `ctrl+v` is the terminal's paste and never reaches the program.

### The cursor's row

The row under the cursor has a light wash across the window,
a shade off the terminal's background, which the renderer asks the terminal for,
and the cell under the column cursor is reversed within it:
the wash says which issue, the cell which field.

### The current group's header stays on top

Where the window opens on a line under a group's header,
its first line is that header, and the rows start one line lower.
A header scrolled off the top is unreachable,
since the scroll follows the cursor and the cursor never stands on a header,
and the rows under it lose the only thing that says which group they are in.
The sticky line is the header as the page drew it, whatever it carries.
One helper does it for every kind (`window`, `tui/list_view.go`).

## The status line

### The bottom line is two places

The hints are on the left, always.
The last action's message is right-aligned,
with the count beside it where a kind has one (*moved · 12 issues*),
and stays until the next action replaces it.
Not the message over the whole line:
then the one moment a user has just done something new
would be the one moment the keys are hidden.
Where the window cannot hold both, the hints win whole
and the right side is cut from its left,
because a count and the end of a refusal are what is read at that edge.
One helper builds the line for every kind (`bottomLine`, `tui/hints.go`).

### Key hints are on the bottom line only

No box, picker, editor or overlay draws its own keys:
they are on the bottom line's left, and nowhere else.
Not a footer under the widget as well:
two copies of one list drift
(a footer saying *enter saves* over a line saying *enter: write*),
and the bottom line is the copy every state already has.
So an editor draws the field's name and its input or choices,
and its keys are `editor.hints` on the line;
the search, flow and query boxes draw theirs the same way (`withBox`, `tui/palette.go`);
the tab strip draws no `←→`;
and the help's own keys are its bottom line ([The help is the keymap](#the-help-is-the-keymap)).
The palette's letters are its content, not hints,
and a placeholder's words (*type to narrow*) name no key.

### The hints name the keys that act under the cursor

What `space` and `enter` do changes with the cell,
and a user who has to open the help each time the cursor moves
is paying for the generality with what it was meant to buy.
So the hint is `enter` and `space` first, in that order where both act,
then the two or three more that matter in that exact state,
and `? keys` last, the one pair on every line.
Keys are spelled as the standard keys, lower case.
Every kind builds its line from one helper over `(key, action)` pairs
(`hints`, `tui/hints.go`), so the wording cannot drift between kinds.

A hint never promises a key the page does not answer there:
where a key rings, its pair is left out,
so the line is shorter exactly where there is less to do.
`editHint` mirrors `editable`, which keeps that true as the widgets grow.

### Terse words

A status line, a hint, an empty tab and a log line
are the fewest words that carry them
(*copied status*, *id not editable*, *created*),
with no pronouns, no instructions the help already gives,
and the reasoning left to this document.

## The grab

### Grab replaces dragging

A terminal has no drag,
and a modifier-plus-arrow vocabulary collides with every key already claimed.
So `space` grabs the item under the cursor, which blinks;
the directions move it;
`space` or `enter` drops it;
`esc` puts it back where it was.
One drop is one commit for the issue that moved, however many moves it took.

While grabbed:

- **list**: up and down change rank, within the group and the level.
- **board**: left and right set the `columns` field to the neighbouring value,
  up and down change rank.
- **gantt**: left and right shift by one period of the `scale`
  (on the bar's first cell only `start` moves, on its last only `stop`,
  between them both),
  up and down change rank.

### Every order is the built-in rank

Every list, board, gantt and side table orders by,
and every drop writes, the built-in `rank` every type carries
([issue](issue.md)),
so every one of them is draggable with nothing bound.
`rank` is internal: no kind takes it as an argument,
and naming it is refused like any unknown name,
because an order is an implementation detail of a drop,
not something a view describes.

Within each group, column or nesting level the order is `(rank, id)`:
the ranked first, in that order,
then the unranked, in the query's own order.
The tie-break by id is what lets two concurrent drops into one gap both survive.
A rank is null until a drop writes one,
so a store nobody has dragged in reads exactly as its query asked.

### A drop reads as it was drawn

On a scope nothing is ranked in,
the first drop would carry its issue to the top,
it being the only issue with a key.
So a drop first gives a rank to every unranked row
drawn above the drop point in the same ordering scope
(the same group, the same level under the same parent, on a board the same lane's column),
in the order they are drawn,
and only then computes the dropped row's key between its neighbours.
The rows below are left unranked:
they already sort after everything ranked, in the query's order, which is where they are drawn.

The dropped row's write is one `set`:
its midpoint key, and the group or column field where it crossed,
so two people dragging at once both keep their drag.
The keys filled in above are one `set` each, one issue each,
there being no multi-entity commit;
the status line counts them (*rank set · 2 ranked*),
so a drop that wrote three issues does not read like one that wrote one.

### Carried past its group, a row enters the next

A grabbed row moved up past its group's first row enters the group above, at its end;
moved down past its last, the group below, at its start.
The drop writes that group's value and the rank in one `set`,
as a board does when a card is carried into another column:
a group is a column drawn the other way.
It holds on a list, on a gantt and across a board's swimlanes.
What it writes is the value a row already in that group holds,
and `(none)` writes null.

Some crossings ring, the row staying at its group's edge:

- a `group_by` of `type`, which decides which fields an issue has,
  so a move does not get to change it;
- a set-valued `group_by`, since a row is grouped by its whole set
  and a drag cannot say which item it meant;
- a keyed row, which stands for a share of an issue.

Only a root crosses:
a nested child stays among its siblings, its place being under its parent.

### A keyed row reorders for the view only

A row whose `key` is not its `id`, or that has no `id`
([query](query.md)),
stands for a share of an issue,
so writing the issue's rank would reorder every row standing for it.
It drags as any row does, and the drop writes nothing:
the page holds the scope's order by key until it is quit,
across refreshes,
and says *order kept for this view*.
A level is all keyed rows or none,
so a scope is the page's to order or the ranks', never both.

## Refresh and concurrency

### The page re-runs its query

A page runs its own query,
and runs it again when the store changes under it and after each of its own writes.
The cursor comes back on the same key, which is the id unless the query gave one,
not on the same row:
the row under a moving cursor is not what the user was looking at.

### The ref watcher drives the refresh

`Render` starts the cache's ref watcher and observes the issues and the schema;
every event collapses into one refresh,
forwarded on a goroutine of its own with a buffer of one,
so a slow redraw does not stall the watcher and a burst is one redraw.
A watcher that cannot start is a page that does not update itself,
not a page that fails to open.

### The renderer holds no lock

Reads take no lock,
and each write takes the short write lock and releases it,
so an open page and a working agent are never exclusive.
Every write goes through package `host`,
so the renderer gets the schema check and the write lock
and there is nothing for two implementations to disagree about.

## Deliberately absent

- **The mouse.**
  A click would edit, follow or switch, and the wheel scroll;
  what it costs is the terminal's own drag-select,
  which a terminal keeps behind `shift` once a program asks for the mouse.
- **A confirmation.**
  The renderer asks no questions;
  what is final is a chord instead.
- **Reparenting by grab**:
  moving a row under another would write a relation,
  and a grab moves within a scope.
- **A list that returns a choice** (`pick`).
  A flow that needs an issue takes its id on the command line,
  which is what an agent needs and what a human typing a command already has.
