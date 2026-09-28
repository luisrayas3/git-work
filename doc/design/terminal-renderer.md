# The terminal renderer (`84dfbde`)

**Outcome:** `git work view list`, `git work view show`,
and any flow that calls `work.view.*`,
draw the issues in the terminal,
let a human navigate them with the keys they already know,
edit a field under the cursor,
reorder and re-bucket by grabbing an item,
and stay correct while agents write from other processes —
without a second query language, a second data path, or a second lock.

**Serves:** `17e1d0a` (workflow 4, whose human face this is),
`3df330f` (saved views are flows that call views),
`8b06191` (the HTML backend of the same calls),
`9a24c8e` (`flow pick`),
`ca81145` (the chat pin, folded in here).

**Status:** decided 2026-09-24 (Luis); keys and `show` revised 2026-09-27 (Luis).
List and show are being built now;
board, gantt and nesting come after the migration (`bf6f392`).
This document revises `config-entity.md` E9,
which is now the short form and points here.

## There is no `tui` command

The terminal renderer is not a surface with a command of its own.
It sits behind `git work view KIND [KWARGS] [--gui]`
and behind `git work flow run NAME` whenever the flow calls `work.view.*`,
which is the same rule the GUI follows (`8b06191`):
two backends of one capability,
never a capability on one surface and not the other.

The framework is **Bubble Tea v2**,
`charm.land/bubbletea/v2` with `bubbles/v2` and `lipgloss/v2`,
Go only, like every other surface (`867db1a`).

The gocui `termui`, and gocui itself,
are deleted by the migration (`bf6f392`), not before.
The order matters: `termui` is the only interactive surface that exists today,
so it stays until the list renderer can replace it.

## The command is the spec

A view's input is **one KWARGS object**,
exactly the object `work.view.KIND(**kwargs)` takes.
`git work view board '{"columns":"status"}'` and
`work.view.board(columns="status")` are the same call spelled two ways,
and there is nothing left to serialize:
the input already *is* the serialization.

So the spec is gone.
Nothing reads items from standard input,
nothing prints `{"view", "bindings", "items"}`,
`git work view` has no `--format`,
and `Spec` and `IsSpec` leave package `view`.
What stays in package `view` is the kinds table,
which is the argument contract three consumers read:
the view functions validate against it,
a backend knows what it may be handed,
and the command line's help is generated from it.

`--gui` posts the same KWARGS object to the `gui` process (`8b06191`);
until that process exists it is an error.
No TTY and no `--gui` is an error too.
An agent that wants the data does not open a view —
it runs `git work issue PROGRAM`, which is where the data lives.
`flow run` keeps its `--format`, for what the flow itself returns.

This revises the 2026-09-24 morning decision,
which kept the spec as a "headless serialization"
printed when a call could not render where it ran.
Printing a spec was a way for an agent to get items out of a view call,
and an agent has a better one.

## The first line is the call

Every view's first line is **the call that drew it**:
the kind, then its arguments —
`list  group_by=status  map(select(…))`,
`show abc1234`.
The command is the spec, so the one line that answers
"what am I looking at?" is the call itself.
Two arguments are not spelled on it, because the screen already says them
(revised 2026-09-28, Luis):
`fields` is the columns of a list and the rows of a show's table,
and the query goes **last, without its name** —
it is on every kind but `show`, it is the answer to "why these issues?",
and it is the one argument that runs long,
so it is what a narrow window cuts.
An argument left at its default is dim, so the ones somebody chose stand out.
The status line at the bottom keeps the last message and the count,
and no longer carries the query (revised 2026-09-27, Luis).

## What a view call is

A view call **blocks on the script's thread**
and returns the user's answer,
which is `None` for every kind today.
Starlark is therefore paused inside the call,
there is no concurrency in the language,
and a script that ends while its view is open is not a state that can arise.

Underneath, the Bubble Tea event loop runs on **its own goroutine**.
The script's goroutine, blocked in the call, is a worker:
it takes work from a queue the event loop feeds.
Nothing in the first cut puts anything on that queue —
the renderer's own edits go straight to the host —
but the layout is built now because of what it buys:
navigation and redraw never wait on a script,
and a second edit queues behind a running one
instead of racing it.
The queue's clients are the deferred features below.

## Arguments, per kind

Every argument falls in one of three tiers:

- **required** — the call has no meaning without it;
- **defaulted** — the view always has one, and the default is usually right;
- **feature** — off entirely unless named.

`query` is on every kind but `show`:
a jq program, defaulting to the list's default program
(unarchived, last edited first).

| Kind | Required | Defaulted | Feature |
| --- | --- | --- | --- |
| `list` | — | `fields` (`["type","title"]`) | `details`, `group_by`, `expand`, `depth`, `rank` |
| `board` | `columns` | `values` (the field's schema order), `card` (`["title"]`) | `group_by`, `rank` |
| `gantt` | `start`, `stop` | `label` (title), `scale` (`week`), `from`, `to` (the data's extent) | `progress`, `group_by`, `expand`, `depth`, `rank` |
| `show` | `id` | `fields` (the type's fields, schema order) | — |

`fields` on a list is an ordered list of field keys,
shown as columns and **editable in place**;
the id is always the first column, and is not named in `fields`.
`details` is the read-only keys drawn under the title.
`card` is the keys on a board card.
`values` on a board is the ordered column values;
a value the data has and `values` does not
gets a trailing column of its own,
because a board that silently drops issues is worse than a board with a ragged edge.
`scale` is `day`, `week`, `month` or `quarter`.

`sort_by` and `card_title` are gone.
Order is the query's order — jq sorts, and it sorts better than a binding would —
except under `rank`, below.
`card_title` was `card` with one element before it had a name.
Gantt's `end` is renamed `stop` (Luis, 2026-09-24).

## Navigation and editing

Standard keys, vim and emacs are all read, at once.
There is no mode and no configuration:
whoever sits down already knows one of the three.
`?` shows them as three tabs, one per family,
each tab the whole set a person of that family needs
(revised 2026-09-27, Luis; the single table it replaces
put three spellings in every cell, which read as noise to all three).

| Action | Standard | vim | emacs |
| --- | --- | --- | --- |
| up | `↑` | `k` | `C-p` |
| down | `↓` | `j` | `C-n` |
| left | `←` | `h` | `C-b` |
| right | `→` | `l` | `C-f` |
| page up / down | `PgUp` `PgDn` | `C-u` `C-d` | `M-v` `C-v` |
| start / end | `Home` `End` | `g` `G` | `M-<` `M->` |
| next / previous stop (show) | `Tab` `S-Tab` | | |
| edit the field under the cursor | `C-Enter` `⌘-Enter` `F2` | | |
| copy the cell under the cursor | `C-c`, and `⌘-c` `C-S-c` where the terminal hands them over | `y` | `M-w` |
| copy the issue id, from any column | `M-c` | `Y` | |
| paste into the field under the cursor | the terminal's paste (`⌘-v`, `C-S-v`) | `p` | `C-y` |
| next / previous tab (show) | `→` `←`, `C-PgDn` `C-PgUp` | `l` `h`, `gt` `gT` | `C-f` `C-b` |
| narrow the rows by text | `/` | `/` | `C-s` |
| back; twice from the first view, quit | `Esc` | `Esc` `q` | `Esc` `C-g` |
| quit, at once | `C-q` | | |

A blank cell is the standard key, which every family also reads.
`Enter` opens, `Space` grabs and `?` is the help in every family.

**`C-c` copies; it no longer quits.**
It is the key a standard user copies with,
and in a raw-mode terminal it is a keystroke, not a signal,
so the renderer is free to give it that meaning.
`C-q` is the quit that nothing swallows —
grabbed, in a text box, in a picker —
which is the guarantee `C-c` used to carry.

**Back is always back** (revised 2026-09-28, Luis).
`Esc`, vim's `q` and emacs's `C-g` leave whatever is open —
a filter, a picker, an issue —
and from the first view they leave the program,
but only on the second press:
the first says *back again quits*,
and any other key in between disarms it.
Back from the first view is the one back that loses the view,
and a stray `Esc` must not end a session.
`q` is therefore not a quit key any more, and there is no single-key quit
except `C-q`.

**`C-Enter` edits; `e` is gone.**
A letter that edits is a letter that cannot be typed,
and the comment box on `show` is where a user types.
`C-Enter` and `⌘-Enter` exist only on a terminal
that speaks the kitty keyboard protocol
(kitty, Ghostty, WezTerm, foot, iTerm2 with CSI u);
elsewhere `C-Enter` arrives as `Enter` and opens the issue,
and `⌘` never arrives at all, because the terminal keeps it.
`F2` is the edit key every terminal sends,
and `y`/`M-w` the copy, so no terminal is left without either.
A field that cannot be edited — the id, a set-valued field,
a field the schema does not know — **rings the bell**,
the terminal's own blink, and says why in the status line.

**Copy and paste are the terminal's first** (revised again 2026-09-27, Luis).
`⌘-c` and `⌘-v` on a Mac, `C-S-c` and `C-S-v` in a Linux terminal,
are the terminal's own copy and paste, and they stay its own:
the renderer defines none of the keys a terminal already means something by.
Where a terminal hands one over instead of keeping it,
it means the same thing here:
a copy key copies the cell under the cursor, a paste key reads the clipboard.
`C-c` is the copy key the renderer does define,
because no terminal keeps it.
**Copy is the cell, over OSC 52**, as `y` was:
the escape sequence puts it on the clipboard of whatever terminal is in front,
including one on the other end of ssh.
The id is a column the cursor can stand on, and **the cursor starts there**,
so copying on arrival is copying the id,
which is the chat pin `ca81145` asked for;
`M-c` and `Y` copy the id from any column.
`C-S-c` was that key for an afternoon,
until it was pointed out that it is every Linux terminal's copy.
Paste arrives as a bracketed paste,
and it **opens the editor** on the field under the cursor with the text in it —
an input line holding it, or a value list on the value it names —
so `Enter` writes it and nothing is written by a paste alone.
`p` and `C-y` ask the terminal for its clipboard over OSC 52 instead,
which a terminal may refuse; then nothing happens.
`C-v` stays emacs's page down:
a standard user's `C-v` is the terminal's paste, and never reaches the program.

What a direction means is the kind's business:

- **list** — up and down move between items *at the current nesting depth*,
  left and right between the columns: the id, then the `fields`.
- **board** — up and down within a column, left and right between columns.
- **gantt** — up and down between rows,
  left and right between time periods:
  the cursor is a cell, and the window scrolls to keep it visible.

Nesting adds `Tab` into the first child, `Shift-Tab` to the parent,
and `z` to fold and unfold — when nesting is built.

**Grab replaces every drag binding.**
A terminal has no drag, and a modifier-plus-arrow vocabulary
collides with everything the three key families already claim.
So: `Space` grabs the item under the cursor, whose border blinks;
the direction keys move it;
`Space` or `Enter` drops it;
`Esc` puts it back where it was.
One drop is **one commit**, however many moves it took to get there.

What the directions do while grabbed:

- **list** — up and down change rank, within the group and the level.
- **board** — left and right set the `columns` field to the neighbouring value,
  up and down change rank.
- **gantt** — left and right shift by one period of the current `scale`:
  on the bar's first cell only `start` moves,
  on its last cell only `stop`,
  anywhere between them both move and the bar keeps its length.
  Up and down change rank.

Reparenting a grabbed list item — writing the `expand` relation by moving it —
is deliberately left out of the first cut.

The rest is the same on every kind:

| Key | Action |
| --- | --- |
| `Enter` | open `show` for the issue under the cursor, or for the one a link cell names; `Esc` returns to the view where it was |
| edit | edit the field under the cursor in place, or ask which field on a card or a bar |
| copy | copy the cell under the cursor to the clipboard over OSC 52 |
| `/` `C-s` | narrow the visible rows by text, locally |
| `?` | list the keys |
| back | back; twice from the first view, quit |

On a list, the row under the cursor has **a light wash across the window**,
a shade off the terminal's background, which the renderer asks the terminal for,
and the cell under the column cursor is reversed within it:
the wash says which issue, the cell says which field.

**A relation is a link** (2026-09-28, Luis).
A field of kind `relation` or `multi-relation` holds the other issue's whole id,
and nobody reads a 64-character hash,
so it is drawn as the issue it names: short id and title, underlined.
`Enter` on it opens that issue rather than the row's;
a cell that names several asks which.
Copy on it copies the id, which is what another command takes.
A link to an issue the store does not have yet is its short id alone.

There is no comment key on a list.
A comment is written on `show`, where the issue it is about is on the screen
(revised 2026-09-27, Luis).

Edit picks its widget from the schema kind:
a value list for an enum, a toggle for a bool,
an input line for text, number and date,
an identity list for an identity.
Editing a relation waits for questions to the user (choose), below.

`/` is a local narrowing of what is drawn.
It never writes, and it never changes the query —
the query is the view's input, and changing it is re-running the call.

**What the renderer says is terse** (2026-09-28, Luis):
a status line, a hint, an empty tab and a log line are the fewest words that
carry them — *copied status*, *id not editable*, *back again to quit*,
*(no comments)*, *created* — with no pronouns, no instructions the help
already gives, and the reasoning left to this document.

Every write goes through package `host`,
so the renderer gets the schema check and the write lock for free
and there is nothing for two implementations to disagree about.
A refusal — a schema violation, a lost race — shows in the status line
and nothing on screen moves.
## Refresh

The view runs its own `query`.
It re-runs it when the ref watcher (`63c68d1`) reports a change,
and after each of its own writes,
and it keeps the cursor on the same **id** across the re-run
rather than on the same row,
because the row under a moving cursor is not what the user was looking at.

There is no provider function and no static list.
Both were artefacts of the E9 text written earlier the same day,
not decisions anyone made:
once the view owns the query,
a callable that returns items and a list that does not
are two spellings of something the view already does.

jq is enough because it is the program `git work issue` takes,
`.` is the whole array,
and a view is then exactly a saved `git work issue` invocation with a drawing attached.
What is given up is rows that are not issues —
a view cannot show a synthetic total row or a schema entity.
That is acceptable: every target workflow is a workflow over issues.

## Rank

The `rank` binding names a field of kind `rank` (`441dcbb`),
and giving it **changes the order rule**:
within each group, column or nesting level
the view sorts by `(rank, id)`,
issues without a rank last,
and the query then only selects.

Without `rank` the query's order stands,
and grabbing an item moves it nowhere vertically —
a view whose order is `sort_by(.fields.due)` cannot honour a drag,
and pretending otherwise is the bug.

A drop writes **one** midpoint key to **one** issue,
which is the whole point of a fractional index:
two people dragging at once both keep their drag.

## Show

`show` is a view kind like the others,
not a mode of the list,
because `git work view show '{"id":"abc1234"}'` is a thing to want on its own
and because `Enter` from any kind opens it.
It takes `id`, and `fields` to narrow and order what it prints;
by default it prints the type's fields in schema order.

The page is **four stops**, top to bottom (revised 2026-09-28, Luis):

1. the **title**;
2. the **comment box**, immediately below it, with its buttons under it;
3. the **fields table**;
4. the **tabs**: comments, description and log.

The title is a heading.
A terminal has one size of text,
so it is made to read as one the other ways:
bold, in the accent colour, over a rule as long as it is,
with a blank line on either side, the type dim beside it.

The cursor opens **in the comment box**,
because opening an issue to say something about it is the common case,
and the box is where the typing goes.
It opens two lines tall and grows with what is typed, up to a paragraph.
The textarea's highlighted cursor line is turned off:
its shade is the colour of the text on the wrong kind of terminal,
and what was typed disappeared into it.
`Tab` and `S-Tab` move between stops from anywhere, the box included,
and step through the box's buttons on the way,
because a button that `Tab` cannot reach is one a terminal without `C-Enter`
cannot press.
Outside the box's text the directions work within a stop first
and move to the next stop at its edge:
in the table, up and down walk the rows,
and past the last row is the tab strip;
on the tabs, up and down scroll.
Edit and copy work on the title and on each row as on a list cell;
copy on the description copies the description.

The fields table is two columns, the key dim and the value,
with no header row: a key and its value need no caption.
A relation is a link here as on a list,
and a `multi-relation` is a line per issue it names,
so that each is a link the cursor can stand on and `Enter` can follow.

The tabs are drawn **as tabs**, under the fields table:
boxes on a rule, the one drawn open into what is under it.
**Left and right switch them**, from anywhere but the box's text —
the tab keys of this page, since nothing else on it goes sideways —
and `C-PgDn` and `C-PgUp` from anywhere, the box included,
which is the tab key of every browser and editor;
vim's `gt` and `gT` work outside the box.
`t` is gone: a letter is a bad universal key on a page that opens in a text box.

- **comments**, the default, is what was said: every comment after the body, in full, as it reads now.
- **description** is the issue's body, its first comment.
- **log** is what was done, an operation to a line, in the order it was done —
  *set status to in-progress*, *added cli to area*, *commented: …*.

Comments and the log were one timeline for a day;
they are two tabs again because reading a discussion
and reading what changed are two different reasons to open an issue.

The box submits with `C-Enter`,
or with `Tab` to a button and `Enter`, which every terminal can send.
`Enter` in the box is a newline:
a comment is prose, and sending half of one is worse than a second key.
`Esc` in an empty box goes back to the view that opened the issue;
with a draft in it, `Esc` leaves the box for the buttons and keeps the draft,
and going back with a draft asks for a second `Esc`,
because a draft is the one thing on the page the store does not have.

The buttons are **Submit comment** alone, today.
They are where actions injected into views (deferred, below) land on `show`:
a view invocation that names, say, *Comment and close*
gets a second button beside the first,
running the comment and then its action.

## Nesting

Designed now, built after the migration,
because the relation fields it walks arrive with the new entity.

`expand` names a relation field, and `depth` how far to follow it —
default 1, unlimited allowed, cycles cut at the repeat.
The **query selects the roots**.
A matched issue that is another matched issue's child
shows nested under it, once, not twice;
a child the query did not match still shows under its parent,
because a parent's children are the reason to expand a parent.

Every level uses the same `fields`, `details` and `group_by`.
Uniform levels are what makes the columns line up,
and a per-level projection is a feature nobody has asked for.

Gantt nests as **rows under rows**, not blocks within a row:
children within a parent's row overlap as soon as two of them share a week,
and a chart that overlaps is not a chart.
A folded parent draws its own bar,
or, when it has no dates of its own, the envelope of its children's.

## Deferred

Not decided, and grouped here because they are one conversation:

- **Actions injected into views.**
  `actions={"Start": start_fn}` becoming buttons on rows, cards and the show pane,
  plus a global bar for text, inputs and flow-wide actions.
  This subsumes the `on_change` / `on_select` callback sketch that E9 carried:
  a callback is an action the view invents a name for,
  and an action is the same thing the flow names.
  This is the first client of the worker goroutine.
  On `show` they join *Submit comment* in the button row under the comment box,
  which is where *Comment and close* comes from (2026-09-27).
- **`work.view.split`**, two panes as one composite view.
- **Questions to the user** — choose, confirm, ask, form —
  which is how a flow asks something without a view.
- **Reparenting by grab**, above.

`pick=True` on a list is **removed**, not deferred.
`flow pick ID` (`9a24c8e`) takes its id from the command line,
which is what an agent needs and what a human typing a command already has;
a flow that wants the human to choose can open a list
once questions to the user exist.

## Concurrency

The renderer **holds no lock** while it is open.
Reads never lock,
and each write takes the short write lock and releases it (`d35de2e`).
This is the whole reason the concurrency work came before the surfaces:
the gocui `termui` holds the store for as long as it is on screen,
which is what made an open UI and a working agent mutually exclusive.

The ref watcher is started when a view opens and stopped when it closes.

## Sequencing

1. **List and show, without nesting**, tested against scratch repositories.
   Being built now, in parallel with this document.
2. **The migration** (`bf6f392`),
   which deletes `entities/bug`, `commands/bug`, `termui` and gocui.
3. **Board, then gantt, then nesting.**

Board before gantt because workflow 3 is the in-person kanban
and a board is a list with a second axis;
nesting last because it touches all three kinds
and its relation fields only exist after step 2.

## What this replaces

The model this replaces is the one recorded on `84dfbde` comment #5,
`3df330f` comment #3 and `config-entity.md` E9,
all written earlier on 2026-09-24:
a view call rendered and blocked — that stands —
but `items` arrived as a list or a provider function,
a list with `pick=True` returned the chosen item,
a spec `{"view", "bindings", "items"}` was the headless serialization
printed when the call could not render where it ran,
and callbacks were the sketched future.
Items are now a jq query the view owns and re-runs,
the command's KWARGS object is the only serialization there is,
`pick` is gone,
and actions injected into views are the sketched future instead.
Older still, and also gone:
`git work tui` as a command (`8b06191`),
and a flow that returns a spec for `flow run` to hand to a renderer (`3df330f`).
