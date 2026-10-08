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

**Status:** decided 2026-09-24 (Luis); keys and `show` revised 2026-09-27 (Luis);
the board decided and built 2026-09-28 (Luis);
the gantt and nesting decided and built 2026-09-28 (`565d57a`, the gantt story `00a63d9`);
the matrix decided and built 2026-10-02 (the allocations story `3289ec1`,
its reasoning in `doc/design/allocations.md`).
List and show landed 2026-09-24, the board, the gantt and nesting 2026-09-28,
the matrix 2026-10-02;
every kind in the table is drawn.
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
on its right, beside the hints (Navigation and editing),
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
- **optional** — no default; off entirely unless named.

The third tier was called **feature** until 2026-10-02,
and the help footnoted it as "in the table and not drawn yet" —
true before any kind was drawn, and left standing after they all were,
so an agent reading `view list --help` took `group_by` and `expand`
for sketches and went looking for another way (`f9c991e`).
A tier is what a call must say, not how far the renderer has got;
`optional` says it, and the footnote now says what it means.

`query` is on every kind but `show`:
a jq program over the unarchived issues, defaulting to the list's default program
(all of them, last edited first);
`include_archive`, false by default, is on every kind that takes `query`
and puts the archived back in its input (`df6ff51`, `include-archive.md`).

| Kind | Required | Defaulted | Optional |
| --- | --- | --- | --- |
| `list` | — | `fields` (`["type","title"]`), `rank` (`rank`), `include_archive` (false) | `details`, `group_by`, `expand` |
| `board` | `columns` | `values` (the field's schema order), `card` (`["title"]`), `rank` (`rank`), `include_archive` (false) | `group_by` |
| `gantt` | `start`, `stop` | `label` (title), `scale` (`week`), `from`, `to` (the data's extent), `rank` (`rank`), `include_archive` (false) | `progress`, `group_by`, `expand` |
| `matrix` | `rows`, `columns` | `row_values`, `column_values` (each axis's own order), `include_archive` (false) | `value`, `group_by` |
| `show` | `id` | `fields` (the type's fields, schema order) | `children` |

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
`rows` and `columns` on a matrix are the two fields it splits the issues by
and `value` the number it sums in each cell;
`row_values` and `column_values` order each axis
the way `values` orders a board's columns.

`sort_by` and `card_title` are gone.
Order is the query's order — jq sorts, and it sorts better than a binding would —
among the issues that have no rank; see `rank`, below.
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
| copy the cell under the cursor | `C-c`, and `⌘-c` `C-S-c` where the terminal hands them over | `y` | `M-w` |
| copy the issue id, from any column | `M-c` | `Y` | |
| paste into the field under the cursor | the terminal's paste (`⌘-v`, `C-S-v`) | `p` | `C-y` |
| next / previous tab (show) | `→` `←`, `C-PgDn` `C-PgUp` | `l` `h`, `gt` `gT` | `C-f` `C-b` |
| narrow the rows by text | `/` | `/` | `C-s` |
| back; twice from the first view, quit | `Esc` | `Esc` `q` | `Esc` `C-g` |
| quit, at once | `C-q` | | |

A blank cell is the standard key, which every family also reads.
`Enter` opens, `Space` edits — or grabs, where there is no cell — and `?` is the help in every family.

**`C-c` copies; it no longer quits.**
It is the key a standard user copies with,
and in a raw-mode terminal it is a keystroke, not a signal,
so the renderer is free to give it that meaning.
`C-q` is the quit that nothing swallows —
grabbed, in a text box, in a picker —
which is the guarantee `C-c` used to carry.

**Back is always back** (revised again 2026-09-28, Luis).
`Esc`, vim's `q` and emacs's `C-g` leave whatever is open —
a filter, a picker, an issue —
and from the first view they leave the program,
but only on the second press.
The first press moves the cursor **onto the call line**,
which stays the call line — the kind and the named arguments —
and unfolds the query under it, **formatted**:
`jq.Format` in `query/jq` parses it with gojq, the evaluator,
and prints the tree back the way gofmt would —
every node on one line where it fits,
a top-level pipe one stage per line whether it fits or not,
and what does not fit broken at its own seam,
a call one argument per line, an object one key per line,
`if`/`then`/`else`, `try`/`catch`, `reduce` and `foreach`
in the shape their keywords give them —
because a jq program folded onto one line, or shown as a JSON string,
is a readability nightmare (Luis, 2026-09-28).
The parser drops comments and the author's spelling,
so the formatter is for reading a program and never for rewriting typed text:
when the call line becomes editable, the text edited is the text typed,
and formatting is an action on it.
A program that does not parse, which a half-typed one is,
falls back to a scanner cutting at top-level pipes.
It is not editable yet, but copy copies the whole call
as the shell command, `git work view list '{…}'`,
so the command that drew the screen is one keystroke from the shell.
Back from there leaves the program;
`Enter`, `Tab` or a direction key goes back into the view,
and any other key does too and then means what it means there.
Back from the first view is the one back that loses the view,
and a stray `Esc` must not end a session;
the call line is where it lands instead of a warning,
because the place where the second back quits from
is a place worth standing on.
`q` is therefore not a quit key any more, and there is no single-key quit
except `C-q`.

**`Space` edits, `Enter` opens** (revised 2026-10-02, Luis).
`Enter` goes somewhere and never changes anything:
on a list's id, or any cell that is not a link, it opens the row's issue,
on a link it opens the issue the link names, at once
(A relation is a link, below),
on a board's card and a gantt's bar it opens the issue,
and in the comment box's text, once typing, it sends.
`Space` changes what is under the cursor:
on a cell it edits it —
a value list for an enum or a person,
an issue list for a relation,
an input line for text, a number or a date,
and a bool flips at once —
on the comment box it puts the cursor in the text,
and on what has no cell to edit — a list's id, a card, a bar — it grabs (below).
A cell that is drawn and never written —
a list column that is not a field of the row's type, a show's child row —
rings the bell under `Space`.
For four days `Enter` was the one action key,
opening on an id, editing on a cell, and on a link a picker headed by *go to*,
so that `Enter`, `Enter` followed;
it read as one key meaning three things,
and a link took two presses to follow, which is the thing a link is for.
Two keys, one to go and one to change, is the split every file manager has,
and `Space` was already the key that changed a card's place on a board.
`C-Enter` was the edit key and the send key for a week, and it is banned:
it exists only on a terminal that speaks the kitty keyboard protocol
(kitty, Ghostty, WezTerm, foot, iTerm2 with CSI u),
and everywhere else — gnome-terminal, Terminal.app, Windows Terminal, xterm,
tmux without extended keys —
the terminal sends the same byte for `Enter` and `C-Enter`,
so the program cannot tell them apart
and one key did two things on two machines.
A key that is one key here and another there is not a key the renderer reads.
`F2` was the fallback every terminal sends, and it goes too:
a function key is not where a hand is,
and with `Space` editing nothing needs it.
Every action is direction keys, `Enter` and `Space`,
which every terminal has had since the VT100.
`e` stays gone: a letter that edits is a letter that cannot be typed,
and the comment box on `show` is where a user types.
A cell that cannot be edited — a set-valued field,
a field the schema does not know — **rings the bell**,
the terminal's own blink, and says why in the status line.

**The status line names the keys that act under the cursor**
(2026-10-02, Luis).
Two keys do everything, and what each of them does changes with the cell:
`Space` edits a status, flips a bool, opens a picker, grabs a card, or rings;
`Enter` opens the row, follows a link, or sends a comment.
A user who has to open the help to learn which is which, each time the cursor
moves, is paying for the generality with the thing it was meant to buy.
So the line the status line draws is a hint:
`enter` and `space` first, in that order where both act, then at most two or
three more that matter in that exact state — the tree's `z` and `tab`,
a grab's directions, a filter's `esc` — and `? keys` last, which is the only
pair on every line. It replaces the one-off hints that preceded it:
the relation cell's, and the matrix's *enter: the cell's issues*.

**The bottom line is two places** (revised 2026-10-02, Luis).
The hints are on the left, always;
the last action's message is right-aligned,
with the count beside it where a kind has one — *moved · 12 issues* —
and it stays there until the next action replaces it.
The message had the whole line until then,
so a field change or a move took the hints over
and only the next key gave them back:
the one moment a user has just done something new
was the one moment the keys were hidden.
The two never fight for the room:
where the window cannot hold both, the hints win whole
and the right side is cut **from its left**,
because a count and the end of a refusal are what is read at that edge.
One helper builds the line for every kind
(`bottomLine` in `tui/hints.go`),
so a message cannot land differently on a board than on a list.
A copy names the **short** id in its message, the clipboard still taking the whole one:
a 64-character hash right-aligned is a hash with its head cut off.
Every kind builds its line from one helper over `(key, action)` pairs
(`hints` in `tui/hints.go`), so the wording cannot drift between kinds,
and the description editor's footer draws its pairs from the same list.
The keys are spelled the way the **standard family** spells them, lower case:
the help is where the vim and the emacs spellings live, and three spellings in
a one-line hint read as noise to all three.
**A hint never promises a key the page does not answer there.**
Where a key rings the bell its pair is left out rather than listed, so the line
is shorter exactly where there is less to do: `space` is missing on a column
that is not a field of the row's type, on a set-valued field, on a matrix's
sum, because none of those has anything for it to grab or edit. One helper mirrors `editable`
(`editHint`), which keeps that promise true as the widgets grow.

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
Not every terminal honours it —
the VTE 0.76 one this was found on, under GNOME, did not —
so the same text also goes through the machine's clipboard tool
where there is one and a display to own the clipboard
(`wl-copy`, `xclip`, `xsel`, `pbcopy`; 2026-09-28, Luis):
both land the same text, and a terminal that honours OSC 52 copies once.
The id is a column the cursor can stand on, and **the cursor starts there**,
so copying on arrival is copying the id,
which is the chat pin `ca81145` asked for;
`M-c` and `Y` copy the id from any column.
`C-S-c` was that key for an afternoon,
until it was pointed out that it is every Linux terminal's copy.
Paste arrives as a bracketed paste,
and it **opens the editor** on the field under the cursor with the text in it —
an input line holding it, or a value list on the value it names —
so `Enter` in the editor writes it and nothing is written by a paste alone.
`p` and `C-y` ask the terminal for its clipboard over OSC 52 instead,
which a terminal may refuse; then nothing happens.
`C-v` stays emacs's page down:
a standard user's `C-v` is the terminal's paste, and never reaches the program.

What a direction means is the kind's business:

- **list** — up and down move between items *at the current nesting depth*,
  left and right between the columns: the id, the tree's arrow cell where
  anything nests, then the `fields` of the cursor's own layer.
- **board** — up and down within a column, left and right between columns.
- **gantt** — up and down between rows,
  left and right between time periods:
  the cursor is a cell, and the window scrolls to keep it visible.

Nesting adds `Tab` into the first child, `Shift-Tab` to the parent,
and `z` to fold and unfold (built 2026-09-28, Nesting below).

**A grouped view keeps the current group's header on the top line**
(2026-10-02, Luis).
Every kind draws its body as one canvas,
a `group_by` header before each group's rows,
and the window scrolls over that canvas to follow the cursor.
Moving up onto a group's first row therefore used to put that row on the top line
and its header one line above the window:
unreachable — nothing scrolls to it,
since the scroll follows the cursor and the cursor cannot stand on a header —
and the rows at the top of the screen lost the only thing
that said which group they were in.
So when the window opens on a line that is not itself a header,
its first line is the header of the group that line belongs to,
and the rows start one line lower, the cursor's among them;
a window opening on a header is unchanged.
The sticky line is the header as the page drew it,
so what it carries comes with it:
the gantt's crosshair and its group tint, the board's per-lane line.
One helper does this for the three kinds — `window` in `tui/list_view.go` —
each page saying, alongside its lines,
which header line every one of them sits under.

**Grab replaces every drag binding.**
A terminal has no drag, and a modifier-plus-arrow vocabulary
collides with everything the three key families already claim.
So: `Space` grabs the item under the cursor, whose border blinks —
on a list from the id column only, because on a field `Space` edits it;
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
| `Enter` | open `show` for the issue under the cursor; on a link cell, the issue it names; `Esc` returns to the view where it was |
| `Space` | edit the field under the cursor in place; a card and a bar have no cell, and `Space` grabs them (Board, Gantt) |
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
**`Enter` on it follows it, and `Space` changes it** (revised 2026-10-02, Luis).
A link is also a value, and a cell that only followed could never be edited;
with `Space` the edit key, the two are two keys and neither waits on the other.
`Enter` opens the issue it names at once, with no picker;
on a list cell of a `multi-relation`, which holds several, the first,
and on `show`, where each is a line of its own, the line's.
`Space` opens the relation's picker (below) on the current value.
From 2026-09-29 to 2026-10-02 the first `Enter` opened the picker
headed by a **go to** entry, the cursor on it, so `Enter`, `Enter` followed;
with `Enter` no longer an edit key, the entry has nothing left to do, and it is gone.
An empty relation has nothing to go to: `Enter` on it opens the row's issue on a list,
and on `show`, where the row is the page already, rings the bell.
The status line on a relation cell says what the keys do: *enter: go to · space: change*, or *space: change* on an empty one, which is the general rule below.
Board cards and gantt bars are not cells, and `Enter` opens them.
Copy on it copies the id, which is what another command takes.
A link to an issue the store does not have yet is its short id alone.

**A person is a name** (2026-09-28).
A field of kind `identity` — `assignee`, a Jira `user` field — holds an identity's whole id
for the same reason a relation does, and is drawn the same way, as what it names:
the identity's name, else its login, else the short id when the store has no such identity
(`host.UserName`, which `git work issue get --format text` uses too).
A Jira account is such an identity named as Jira names it (JS16, `doc/design/jira-sync.md`),
so an imported assignee reads as Jira shows it.
Everywhere a value is drawn it is the name:
a cell, a card, a gantt label, a group or swimlane header, a board column's header,
and `/` filters on it.
It is not a link, because a person has no page:
`Space` edits it, the identity list shows the same names and writes the id,
a board drop into a person's column writes the id,
and copy copies the id.
JSON and the `query` a view runs keep the id, because the id is the value.

There is no comment key on a list.
A comment is written on `show`, where the issue it is about is on the screen
(revised 2026-09-27, Luis).

Edit picks its widget from the schema kind:
a value list for an enum, a toggle for a bool,
an input line for text, number and date,
an identity list for an identity,
an issue list for a relation.
The issue list is the same value list:
the issues of the field's `target_types`, or every issue where it names none,
last edited first as a list is, each drawn as a link is, short id and title,
the issue itself and archived issues left out, and `(none)` last, which clears it;
the cursor opens on the current value, marked *● current* among them,
and a paste of an id, whole or short, lands on that issue.
**`/` narrows any value list** as it narrows a list:
typed text keeps the choices whose label or id it is in,
`Enter` keeps the narrowing and goes back to choosing, `Esc` drops it,
and a picker draws ten choices at a time around the cursor,
because an issue list is longer than a status list.
`Enter` in it writes the id through `host` like any other edit, one commit,
and the schema check still has the last word on `target_types`.
A `multi-relation` has no picker yet: `Space` on it rings the bell, naming `git work issue add/remove`,
and `Enter` goes to the issue under the cursor as on any link:
a list of checkboxes would commit the difference as adds and removes,
and there is no one host call that commits both as one commit yet (2026-09-29).

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
What was given up here was rows that are not issues,
on the grounds that every target workflow is a workflow over issues.
Reversed 2026-10-08 (`query-rows.md`):
a sprint page by person is over issues and still needs rows that are not,
so on a list and a gantt a row may carry a `key`, its identity on the screen,
apart from the `id` of the issue it acts on, which may be left out,
and a `children` listing what nests under it.
The cursor comes back across a re-run on the same **key**, which is the id unless the query gave one.

## Rank

The `rank` binding names a field of kind `rank` (`441dcbb`),
and it **defaults to the built-in `rank`**
every type carries (`configurable-schema.md` D8, 2026-10-02),
so every list, board and gantt is draggable with nothing bound at all.
It stays an argument because a second ordering field is a field like any other.

The order rule, within each group, column or nesting level,
is `(rank, id)`: the issues that have a rank first, in that order,
then the issues that have none, in the query's own order.
A rank is null until a drop writes one, so a store nobody has dragged
anything in reads exactly as its query asked for,
and the query only selects once ranks exist.
**A drop reads as it was drawn** (2026-10-02).
On a view where nothing is ranked the first drop would otherwise
carry its issue to the top of its scope, it being the only issue with a key,
whatever position it was dropped at.
So a drop that writes a rank first gives one to every row
**drawn above the drop point in the same ordering scope** —
the same group, the same nesting level under the same parent, and on a
board the same stack, which is one lane's one column —
that has none, in the order they are drawn,
and only then computes the dropped row's own key between its neighbours.
The rows below the drop point are left unranked:
they already sort after everything ranked, in the query's own order,
which is where they are drawn.

A drop therefore writes **one** midpoint key to the issue that moved —
that key, and the `group_by` or `columns` field where the row crossed,
in one `set`, which is one commit, which is the whole point of a
fractional index: two people dragging at once both keep their drag.
The keys it fills in above are one `set` each, one issue each,
because there is no multi-entity commit (`AGENTS.md`);
the status line names them, `rank set · 2 ranked`,
so that a drop that wrote three issues does not read like a drop that
wrote one. A drop on a view already ranked fills nothing in and is the
single write it has always been.

**A grabbed row carried past the edge of its group enters the next one**
(2026-10-02): the group above when it is moved up, at that group's end,
and the group below when it is moved down, at its start.
The drop then writes the `group_by` field to that group's value
*and* the rank, in one `set`, which is one commit —
exactly what a board has always done when a card is carried into
another column, and a group is a column drawn the other way.
It holds on a list, on a gantt, and across a board's swimlanes.

What a group is on the screen is the cell its rows draw,
so what the drop writes is the value a row already in that group holds:
an enum's value id, a relation's or an identity's id, a bool.
`(none)` is the group of the rows with no value at all, and writes null.
Nothing moves in the drawing order when a row crosses:
the boundary between two groups is one point,
and which group a row is in is which header it is drawn under.

Two crossings are refused, with the bell and a status line,
the row staying at its group's edge.
**`group_by` of `type`** — the type decides which fields an issue has,
so a move does not get to change it.
**A set-valued `group_by`** (`multi-enum`, `multi-identity`,
`multi-relation`) — a row is grouped by its whole set, so a drag cannot
say which item it meant; that is `issue add`/`remove`,
the same answer `Space` gives on such a cell.
A nested child stays among its siblings:
only a root crosses, because a child's place is under its parent.

## Board

`board` is a list with a second axis:
a column per value of one field, `columns`,
a card per issue, and a swimlane per value of `group_by` when it is bound.
Decided with the list's keys in hand, on 2026-09-28 (Luis),
and built the same day.

**The only edits a board makes are moves.**
`Enter` on a card opens the issue in `show`,
and nothing on a card is edited in place:
a card is a summary,
the page where a field is edited is the one with the field on it,
and what a board is for is moving cards.
So a card has no cell cursor inside it,
copy in every spelling copies the id,
and a paste has nowhere to go and says so.
This supersedes the earlier "ask which field on a card",
written when `e` was still the edit key.

**Grab needs no binding on a board.**
Moving a card to another column is the board's reason to exist,
so `Space` always grabs;
`←` and `→` carry the card into the neighbouring column, empty or not,
and the drop writes the `columns` field — `null` when dropped in `(none)`.
`↑` and `↓` need an order to write and always have one,
the built-in `rank` being the argument's default (Rank, above);
past the end of a stack they carry the card into the swimlane above or
below, in the column it is in, and the drop writes `group_by` too.
The drop is one `set` with up to two keys, which is one commit:
the field when the column changed,
and the rank whenever the card moved at all,
because a card in a new column has new neighbours.
A card dropped where it was picked up writes nothing.
A drop the schema refuses — a task dragged into a column
only an iteration's status has —
is a status line and nothing else, as every refusal is.
A refresh while a card is grabbed lets it go,
because the board under it is no longer the one it was picked up from.

**The columns** are `values` when given, in that order,
else the field's schema order,
read off the types the cards on the board have —
every type owns its own field (`e7e58f2`),
and an iteration's statuses are not columns of a board of tasks —
and off every type when there are no cards at all.
Then, trailing, every value the data has that is not listed,
and `(none)` for the cards with no value,
because a board that silently drops issues
is worse than a board with a ragged edge.
A listed column is drawn when it is empty;
the trailing ones exist only while a card is in them.
The header is each column's name and count;
the count is across every lane.

**A card** is the short id, dim,
the title wrapped to the column's width,
then each other `card` field on a line of its own as `key: value`,
the key dim, a relation drawn as the issue it names as everywhere else,
and a blank line before the next card.
The card under the cursor has the wash over its whole width
and its id reversed, as a list row and its cell do,
and the grabbed card carries the blinking markers on its id line.

**Directions.**
`↑` and `↓` walk the column, and past its end
into the same column of the next lane that has a card there;
`←` and `→` go to the nearest column with a card in this lane,
because an empty column is nothing to stand on —
unless a card is grabbed, when it is exactly where the card goes.
The page keys move within the column; `Home` and `End` are its ends.

**Columns keep `column_width`**, thirty-two cells by default,
and when they do not all fit
the board scrolls sideways by whole columns
to keep the cursor's column on screen,
the sideways twin of the vertical scroll;
`‹` and `›` at the ends of the header say there is more.
When they do fit, they share the window.
Squeezing every column to fit was the alternative,
and a column four characters wide is not a column.
The board scrolls vertically as one canvas,
so that the lanes stay aligned across the columns.

The width was a constant of twenty until 2026-10-02 (`10f676e`),
and twenty is where a title is shredded into one word a line:
six statuses in a 140-wide terminal were twenty cells each,
and the board read as a column of fragments rather than a column of work.
Thirty-two is a title line that is a line.
It is an argument rather than a new constant
because how wide a column should be
is a question about the board somebody is drawing —
two columns of long titles, or eight of short ones —
and the terminal renderer cannot answer it from the data.
Below ten it is refused at parse time, where every other argument is checked,
because a column narrower than a card's id line
is something the board would scroll sideways past rather than read;
the floor is `Min` on the argument's row of the table,
so the help prints it and `Parse` enforces it from the one place.

`card` is not spelled on the call line, as `fields` is not:
the cards are on the screen.
A `column_width` left at its default is not spelled there either,
for the same reason the built-in `rank` is not —
the board as drawn already says it —
and one somebody chose shows like any other argument.

## Gantt

`gantt` is a list with a chart beside it:
a row per issue, a bar from its `start` to its `stop`
over a column per period of `scale` —
a day, a week, a month or a quarter —
and, with `expand`, rows under rows.
Decided with the board's calls in hand, on 2026-09-28 (Luis),
and built the same day (`565d57a`).

**The only edits a gantt makes are moves**, as on a board.
`Enter` on a row opens the issue in `show`,
copy in every spelling copies the id,
and a paste has nowhere to go and says so.
A bar has no cell inside it any more than a card has;
what a gantt is for is moving bars.

**The cursor is a cell**: a row and a period.
It is drawn as a shade of the background a step past the row wash,
in the terminal's own foreground, not reversed as a list's cell is:
a reversed shade glyph, or an empty period, is a block of the foreground,
which on a light terminal is black and hid what it covered (2026-09-29).
`↑` and `↓` move between rows, every level of the tree,
and keep the period, because a column is a date and the date is what
the eye is on;
`←` and `→` move a period, and the chart scrolls sideways
by whole periods to keep the cursor's on screen,
`‹` and `›` at the ends of the header saying there is more.
It opens on the first row and on today's period, the left-most drawn,
because a plan is read from now on and the past is a scroll to the left
(2026-09-29).
With `from` or `to` bound it opens instead on the period the first row's
bar starts in, which is where a grab would move its start.
With `expand` bound, `←` from the first period is the **arrow cell**,
the one a list draws after the id (Nesting):
`Space` there folds the row and `Enter` opens it,
and `→` is back into the chart.

**Grab needs no binding.** `Space` always grabs;
`←` and `→` shift the bar by one period:
on its first cell only `start` moves,
on its last cell only `stop`,
anywhere between them both, and the bar keeps its length.
A bar of one cell grows rather than shifts —
`←` moves its start, `→` its stop —
because a one-cell bar is on its first cell and its last at once,
and growing is the move a one-period bar needs most:
shifting it is two moves, one on each edge.
A start never passes its stop.
The cursor moves with what moved, so it stays on the edge it is dragging.
`↑` and `↓` reorder, as on a board, with the same always-bound rank.
The drop is one `set` with up to three keys, which is one commit:
each date that moved, and the rank when the row moved.
A bar dropped where it was picked up writes nothing;
`Esc` puts it back on its stored dates;
and a refresh while a bar is grabbed lets it go,
because the chart under it is no longer the one it was picked up from.

**A shift is from the stored date, not from its period**:
the drag counts periods, and the drop adds that many to the date
as it was stored, so a bar dragged and dragged back lands on the day it
left, and a month added to the 31st normalizes once, at the write.
A day stays a day and a time keeps its clock:
`2026-09-08T10:00:00Z` moved a week is `2026-09-15T10:00:00Z`.

**A row with one date is a milestone**, a trail six cells long,
and a grab moves the date it has.
The trail says which date it is:
a start begins on its period's first cell and fades to the right, `▓▓▒▒░░`,
a stop ends on its last and fades to the left, `░░▒▒▓▓`,
the fade running into the neighboring periods,
and past it the side the missing date leaves open is the dateless band.
It had a diamond on the date too, dropped on 2026-09-29:
the trail's dense end already marks it.

**A band begins at today** (2026-10-02, `04248c5`).
A row with no start has not started,
and it could start at any point from now on,
so what the chart has to say about it is *from today*:
the band of a row with a stop and no start runs from today's period to that
stop, and the band of a row with no dates at all from today to the chart's
end. Before this both ran from the chart's left edge,
which drew weeks that have already gone by as weeks the work might be done
in — the one thing about an unplanned row that is certainly false.
A row with a start and no stop is unchanged:
it fades from its start to the right and runs on as the band to the chart's
edge, because work with no end date has not been given one.
**A stop that has gone by** keeps its marker, and the band runs back from it
to today in an **overdue tint**, red: a date already missed is what the eye
is looking for on a chart, and red outranks the group's tint there, the
group being readable from the row's neighbours anyway.
Nothing is drawn past such a stop: a stop is a stop.
The chart's own extent counts a missing start as today for the same reason,
so a row that is only a stop sizes the chart to today-to-stop
rather than to the point its stop is.

A band is drawn in its group's tint blended a third of the way to the
background — the terminal's faint washed the hue out, so a group could not
be told — so the row reads as unplanned rather than as missing;
a row with no dates has nothing to move, and says so.

**A parent with no dates of its own draws the envelope of its children's**,
folded or not, in a glyph of its own,
because a parent's row with nothing on it would say the story has no
plan when its tasks are the plan.
A parent with dates draws them: they are its own claim.

**`label` is the row's label column**, not text on the bar.
A title inside a two-week bar is three letters and an ellipsis,
so the label sits to the left of the chart with the id,
in a column sized to the labels up to two fifths of the window.

**The chart's extent** is `from` to `to` when given,
else the dates on the chart — the earliest start, or stop, to the latest —
and today's period, which is where the chart opens;
a chart of nothing is not nothing to stand on.
With neither bound the chart also runs on past its last date
to fill the window from its first period drawn,
so that today can be the left-most whatever the data's end.
Today's period is marked on the rule under the header.
While a bar is dragged past the edge of a chart sized to the data,
the chart grows with it.
A `from` or `to` that is not a date is refused when the view opens,
the way the schema refuses one on a write.

**`progress`** names a number, 0 to 1, and fills that fraction of the
bar's cells with the done glyph and the rest with the other;
without it the whole bar is done, which is to say it is a bar.

**Periods are whole**: the chart is aligned to the period —
a week starts on Monday, a quarter on its first month —
and a bar covers every period it touches, its stop inclusive.
A day or a week is three cells wide, a month or a quarter four:
room for a day of the month, a month's name or a quarter's.
The header is the periods' own labels —
the day of the month, the ISO week number, the month's name, the quarter —
and over them, dim, the coarse ones where they change —
the month over days and weeks (a week's is its Monday's),
the year over months and quarters —
the first said whole and a change after it short,
unless the year changed too.
The cursor's period has the row wash down every row and on its label,
a crosshair with the cursor row's,
and through a `group_by` header too, so the line is unbroken.

**`group_by` makes a section per value of the root rows**,
headed by the value as a cell draws it,
so a relation's group is the issue it names, `13e21c6 north`.
Each group's bars and header take a color of their own,
given in the order the groups are stored so a filter keeps them,
yellow left to the grab and the ungrouped left plain.

## Matrix

`matrix` is the two-axis summary:
a row per value of `rows`, a column per value of `columns`,
and in each cell the sum of the number field `value` names —
or a count of the issues when none is named.
Decided and built 2026-10-02 (Luis), for the allocations story (`3289ec1`);
**its reasoning lives in `doc/design/allocations.md`**,
because the kind and the allocation type were decided together
and the kind is the half that belongs to the renderer.

The short of it, for a reader who stops here:
the axes order themselves the way a board's columns do —
`row_values` / `column_values`, else an enum's schema order,
then the values the data has, then `(none)` —
and a relation axis is the issue it names, ordered by title;
the cursor is a cell, `Enter` opens the issues summed into it
as an ordinary list whose query selects them,
`Space` rings because a sum is not a value,
`/` narrows the axes rather than the issues,
the totals row and column are dim and are cells like any other,
`group_by` is blocks of rows under one column header,
and the row labels stay put while the columns scroll sideways by whole columns.

## Show

`show` is a view kind like the others,
not a mode of the list,
because `git work view show '{"id":"abc1234"}'` is a thing to want on its own
and because `Enter` from any kind opens it, and the edits a board and a gantt do not make are made here.
It takes `id`, and `fields` to narrow and order what it prints;
by default it prints the type's fields in schema order.
`children` adds the issues that point at it (Children, below).

The page is **four stops**, top to bottom (revised again 2026-09-28, Luis):

1. the **header**: the type, the title, and whether the issue is archived;
2. the **fields table**;
3. the **comment box**;
4. the **tabs**: description, comments and log.

The header is three of the four built-in fields,
which are on every type and are not rows of the table
(`rank` is the fourth, and is **not drawn at all**, 2026-10-08, Luis:
it is an order the drags write, a fractional index nobody reads,
so it is no row either, even when `fields` names it;
`git work issue` still reads and sets it, and every drag still writes it):
the type first, dim, because the list shows it left of the title too;
the title bold, in the terminal's own foreground —
a terminal has one size of text, so the title reads as a heading
by being bold, over a rule as long as it is,
with a blank line on either side —
and archived as a checkbox, `[x] archived` in the warning colour when the issue is
and `[ ] archived` dim when it is not,
always drawn, because the toggle `Space` flips has to be in sight
(it was invisible until true for an hour, and that read as missing).
Left and right walk the three cells as they walk a list row,
and `Space` on the type opens the schema's types,
on the title an input line,
and on archived flips it, both ways, in one press:
an archive is an operation like any other, and the same press undoes it.

The cursor opens **on the first row of the fields table** (revised 2026-10-08, Luis),
the value cell under it as a list's cell is,
because every other view opens on a cell
and the fields are what an opened issue is read for;
a table with no rows opens on the box.
It opened on the comment box from 2026-09-28 to 2026-10-08,
on the reasoning that opening an issue to say something about it is the common case;
in use it was a page whose cursor sat below what it was opened to read.
It was never typing on arrival (2026-10-02):
a page whose every letter is text on arrival has no keys of its own,
and `Space`, the edit key, is what starts it, as it starts every other edit.
It opens two lines tall and grows with what is typed, up to a paragraph.
The textarea's highlighted cursor line is turned off:
its shade is the colour of the text on the wrong kind of terminal,
and what was typed disappeared into it.
The box is **one stop, typed in once entered**:
on it, the directions leave it as from any stop, `Enter` does nothing,
and `Space` puts the cursor in the text;
in the text every key is text, `Space` a space, the directions the text's own,
`Enter` sends, and `Esc` leaves the text with the draft kept,
the cursor back on the box.
A newline is `M-Enter`, which every terminal sends as escape, enter,
and `S-Enter` where the terminal tells it from `Enter`.
`Tab` and `S-Tab` move between stops, out of the text too,
and land on the box, never in it.
On the box, the cursor **washes it whole**, every line, marked `›` as a row is,
so the box reads as one cell under the cursor;
in the text the wash goes and the textarea's own cursor is drawn,
so on and in are two looks, not one look and a caption.
Its keys are on the bottom line's left, as every cell's are —
*space: type · tab: skip* on the box,
*enter: send · alt+enter: newline · esc: leave* in it.
A footer line under the box repeated them until 2026-10-08, Luis:
the same keys twice, one of them a line between the box and the tabs.
It held a **Submit comment** button until 2026-10-02,
reached by `Down` from the text's last line and pressed with `Enter`;
with `Enter` sending from the text, the button was a second way to do one thing.
Outside the box's text the directions work within a stop first
and move to the next stop at its edge:
in the table, up and down walk the rows,
past the last row is the box, and past the box is the tab strip;
on the tabs, up and down scroll.
Copy works on each header cell and on each row as on a list cell;
copy on the description copies the description.

The fields table is two columns, the key dim and the value,
with no header row: a key and its value need no caption.
A relation is a link here as on a list,
and a `multi-relation` is a line per issue it names,
so that each is a link the cursor can stand on;
`Enter` goes to that line's issue.

The tabs are drawn **as tabs**, under the comment box:
boxes on a rule, the one drawn open into what is under it.
**Left and right switch them**, from anywhere but the box's text —
the tab keys of this page, since nothing else on it goes sideways —
and `C-PgDn` and `C-PgUp` from anywhere, the box included,
which is the tab key of every browser and editor;
vim's `gt` and `gT` work outside the box.
`t` is gone: a letter is a bad universal key on a page with a text box in it.

- **description**, the first tab and the one the page opens on
  (2026-09-28, Luis), is the issue's body, its first comment,
  as Jira's page leads with it; the text wraps to the window,
  because a description is read, not scanned, and a cut line is a sentence lost.
  **`Space` on it edits it**
  (2026-10-02, Luis, asked as "how do I edit the description?"):
  the hint on that tab reads *space: edit · ←→: tab*,
  and the editor is the comment box over the body's current text,
  with the box's keys — `Enter` writes, `M-Enter` is a newline,
  `Esc` leaves it with the draft kept, which is a draft of its own,
  never the comment's — and the write is an edit of that first comment,
  after which the tab reads the store again.
  It was reachable only as `git work issue comment edit <comment-id>` until then,
  which is an id to look up to change the one text an issue opens with.
  An **empty body is refused**, with the bell:
  the first comment is the description and the model has no issue without one,
  so there is nothing for an emptied box to write.
  The editor stands in the tab's place while it is open,
  and stays there, blurred, while it holds an unwritten draft,
  so that the text being rewritten is where the text being read was;
  leaving the page with one asks for a second `Esc`, as a comment draft does.
  An editor opened and left untouched keeps nothing:
  the tab goes back to reading the store,
  because an unchanged text is not a draft.
- **comments** is what was said: every comment after the body, in full,
  as it reads now, newest first, wrapped the same way.
- **log** is what was done, an operation to a line, latest first —
  *set status to in-progress*, *added cli to area*, *commented: …*.

Both run **newest first** (2026-09-28, Luis):
an issue is opened to see what changed since last time,
and the latest is what that is.
Newest is the end of the operation log, its causal order,
never the wall-clock times, which are for display only.
Comments and the log were one timeline for a day;
they are two tabs again because reading a discussion
and reading what changed are two different reasons to open an issue.

`Enter` in the text was a newline until 2026-10-02,
because sending half a comment is worse than a second key;
once typing is a mode `Space` enters, `Enter` sending is what every chat box does,
and the newline is the second key instead.
`Esc` in the text only leaves it;
`Esc` on the box goes back to the view that opened the issue,
and going back with a draft asks for a second `Esc`,
because a draft is the one thing on the page the store does not have.

Actions injected into views (deferred, below) lost their place on `show` with the button:
a view invocation that names, say, *Comment and close*
needs a key or a row of its own, decided with them.

### Children

Asked for 2026-09-29 (Luis): show can list the issues that point **at** the shown one —
a story's tasks, whose `parent` names it —
given "a type and a relation to find the shown item".
The relation is stored on the child, never on the shown issue
(`schema.yaml`, D4), so the shown issue's fields cannot say it;
the call has to name the child's side.

```json
{"id": "abc1234",
 "children": [{"type": "task", "relation": "parent", "fields": ["status"]},
              {"relation": "blocked_by"}]}
```

`children` is a **list**, because a story has tasks *and* subtasks,
and an issue is blocked by some and blocks others:
one entry is one section, drawn in the list's order.
An entry is an object rather than a `type/relation` string,
because it has three parts and a string would grow a grammar:

- **`relation`** (required) is the relation field on the child
  whose value is the shown issue's id.
  It may instead be the name the schema's `inverse` gives the other side —
  `children` for `parent`, `blocked_by` for `blocks` —
  which is how `expand` reads a derived side (Nesting, below),
  so the word a list's `expand` takes is a word show takes too.
  A key that is a field of the type is that field, and has to be a relation;
  only a key that is not is read as an inverse.
- **`type`** narrows the children to one type.
  Left out, every type whose relation matches counts:
  `{"relation":"children"}` alone is everything whose `parent` is this issue.
  It is optional because the explicit form is the one asked for
  and the other costs nothing: the lookup is the one `expand` already does.
- **`fields`** are drawn after each child's title, joined by `·`,
  a person by name and a relation as the issue it names.
  Status is what one wants there, and it is a key the call names,
  not a default, because there are **no field roles** (`d56e6f1`):
  nothing can tell which field is the status.

The shape is checked by the table (`view.Parse`), which has no store;
the names are checked against the live schema in `host.View`,
before a renderer is chosen, so the command, a flow and the gui refuse
the same call with the same words, and before anything is drawn.
An unknown type names the types; a relation no type has, or a field that is not one,
names every relation of the types in question with its inverse;
a `fields` key no child type has is refused too.

**A section is rows of the fields table**, after the stored fields,
not a fourth tab and not a panel of its own.
The other side of a relation is drawn the way a relation is:
the key column holds the section's name, on its first line,
and every child is a line of its own, its short id and title, a link.
That makes each child a place the cursor stands, `Enter` follows,
and copy copies the id, through the table's own handling —
a tab is a scrolled text the cursor does not stand in,
and links in it would need a second cursor.
It is also where Jira puts *Child issues*: in the issue's body, with its fields,
not behind a tab.
The rows are derived, never stored, so they are **not a field**:
nothing edits them: `Space` on them rings the bell, and so does `Enter` on an empty section.
To reparent a task, open it and edit its `parent`.

A section is named by the relation from the shown issue's side:
the inverse, when the schema gives one — a story's tasks are its `children` —
else the stored key behind an arrow (`← parent`);
a named type follows it (`children · task`),
so two sections of one relation and two types read apart.
A section with no child is drawn as `(none)`,
because a section that is not drawn reads as one that was never asked for.
Children come in the store's order and leave out the archived,
as `expand` has them.

It is **live** as the rest of the page is:
the sections are recomputed on every load,
which the ref watcher's refresh and the page's own writes both do,
so a task created elsewhere with this story as its parent appears.

Only the call carries `children`.
`Enter` on a list row, and following a link, open a bare show, as before:
the options are the call's, and a page reached by a link was not called with any.

## Nesting

Designed 2026-09-24, built 2026-09-28 on the list and the gantt
(`565d57a`), after the migration brought the relation fields it walks,
and made a **layer spec** on 2026-10-02 (Luis, `f4426ff`).

**`expand` is a layer, or a relation name as the shorthand for one.**
A layer is an object:

```json
{"relation": "children",
 "query": "map(select(.fields.status != \"done\"))",
 "fields": ["status", "title"], "details": [], "group_by": "", "rank": "rank",
 "expand": {"relation": "blocks"}}
```

`relation` is the only required key,
and since 2026-10-08 only where some row above lists no `children` of its own (`query-rows.md`, R3),
and the rest are the list's own arguments *for that layer's rows*:
the level below is its own `expand`,
and a layer with none is leaves.
`"expand": "children"` is one layer with every default,
and a **number** as a layer's `expand` is that same layer again,
that many more levels down — `"expand": 1` under `children` is tasks, then subtasks —
and `0` as far down as the relation goes.
It is a number and not a name because every string in that slot is a relation,
and because a repeated layer is not a level below inheriting the one above —
an odd idea across types that do not share fields —
but one layer drawn at every level, which a count says plainly.

**`depth` is gone.** A number said how far to walk and nothing about what
was down there, so every level drew the columns of the first one
and a second level nobody could describe was all it could give:
`depth: 2` on a tracker meant "tasks and whatever is under them",
with the tasks' columns and the roots' query.
A layer is the description, and it costs nothing to leave out:
the shorthand is the old behaviour, one level deep.
A number belongs on the layer it repeats, where it says how far *that* layer goes,
never at the top, where there is nothing to repeat.

**The query selects the roots; a layer's query selects that row's children.**
The root `query` runs over the array of every unarchived issue, as every program does.
A layer's runs over the array of *that row's own candidate children* —
every unarchived issue the relation reaches from it,
the archived too under `"include_archive": true`,
which a layer inherits from the layer above, the first from the call,
unless it names its own (`include-archive.md`, I5) —
so it is written exactly like a view's query and reads as one:
`map(select(…))` over a smaller array.
Without one, every unarchived child shows,
which is what "do the children obey the query?" had to be answered with
before the spec (it was no, and nothing said so: `f9c991e`).
A matched issue that is another matched issue's child
shows nested under it, once, not twice —
since 2026-10-08 a *key* shows once, and a row's key is its id unless the query gave one (`query-rows.md`);
a child shows under its parent whether or not the root query matched it,
because a parent's children are the reason to expand a parent.
Cycles are cut at the repeat.

**Either side of a relation is a name `expand` takes**,
and the derived side of a stored relation resolves through it
(2026-09-28, Luis): the inverse is never stored (`schema.yaml`, D4),
so `expand=children` reads every issue whose `parent` names the row,
off the whole store and in the store's order,
and `expand=blocks` the targets of the row's own `blocks`.
A rank orders a layer's rows by `(rank, id)`.
A layer's relation, and every field key it names, are checked against the
schema before anything draws, the way show's `children` are.

**A nested layer is its own table** (Luis, 2026-10-07).
The rows under an opened parent are drawn as a table of their own:
the whole of it indented as one unit, two cells a level,
with its own header line above its first row,
its own columns in its own widths,
and its id column first and flush *within the table*,
so a child table reads and sorts by id the way the top one does.
`fields` and `details` a layer leaves out are the layer above's,
so the shorthand is a tree of like tables,
and a layer that names them draws them:
a story's tasks can show `status` and `assignee`
where the stories show `type` and `priority`.
A layer's widths are measured over every row of that layer,
drawn or hidden under a fold,
so every table of one layer has the same columns at the same widths,
and folding or unfolding moves nothing beside it.
The top header describes the roots and never changes;
each child table's header describes its layer,
and when the cursor is in a table whose header has scrolled off the top
that header is kept on the first body line, as a group header is,
because a row read without its header is a row of numbers.
A `group_by` is the layer's own: the roots' sections head the list,
and a layer that names one sections each child table, its group lines indented with it.
A group is where a row is drawn, so a rank moves a row among its siblings
in its group only, its subtree with it,
and a story dragged past another carries its tasks.
Only a root crosses into another group (Rank):
a child's place is under its parent, wherever the parent goes.
A detail line is indented with its table.
The keys do not change: `Tab` lands on the first child's id,
`←`/`→` walk the child's own cells, and the cursor cell is reversed as before.
A folded parent has no table under it, so the headers cost lines only where a tree is open.
The gantt is not a table of tables:
its rows share one time axis, so it keeps the indent in its label column
and one header, the chart's periods.

This replaces the first drawing of nesting, where every level started at the
same column with the indent inside the tree cell, each layer in its own widths,
and one header following the cursor's level.
It was two things at once: one table by its header, many by its widths,
so a child's cells sat under its parent's header and not under its columns.
It was also unstable: the tree cell was measured over the rows being drawn,
despite meaning to be measured over all of them, so unfolding a parent
deepened the tree, widened the cell and shifted every column to the right,
and a repeated layer's widths were measured the same way, so a parent's columns
moved when its children appeared.
Measuring every row is the rule both for the tree cell and for the columns.

**The fold arrow is a cell, and it comes after the id** (2026-10-02).
The id column stays first and flush, so a list reads and sorts by id
whether or not anything nests — before this the indent pushed the id
sideways by level, and the first column of a tree was ragged.
The tree cell carries the arrow — `▾` open, `▸` folded, nothing on a leaf —
and, folded, the count of the rows hidden under it: `▸ 3`;
the indent is the child table's, not the cell's (2026-10-07),
and the cell is as wide as the largest count on any row, so it never moves.
The count is the row's own children, which is what unfolding reveals.
It is a cursor stop like any other cell:
the cursor still starts on the id, `→` reaches the arrow,
`Space` there folds and unfolds, `Enter` opens the row as it does on the id,
and `Space` on a leaf's tree cell rings.
On a gantt the arrow cell is the one `←` reaches from the first period,
the cursor's other way out of the chart.
`z` is gone with it: a key for what a cell already does is a key to remember.
**`Z` folds or unfolds every parent** — fold where any is open, unfold where
none is — because a tree opened all over has no other way back.

**A nested view opens folded.** Every parent starts shut, so the first screen
is the roots and their counts, which is a summary;
`Space`, `Tab` or `Z` opens what the eye wants.
Fold state is kept by id across a refresh,
and a refresh that would leave the cursor's row hidden opens its ancestors,
the way the filter keeps a match's ancestors on the screen:
a cursor is never moved by somebody else's write.

`Tab` goes into the first child, unfolding on the way,
and `Shift-Tab` up to the parent.
Whatever cell the cursor was on, `Tab` lands on the child's id,
where a row is read from and `Space` grabs it,
and `Shift-Tab` on the parent's fold arrow,
where `Space` folds back what `Tab` opened (2026-10-07).
The gantt has no id cell, so its `Tab` keeps the period
and its `Shift-Tab` lands on the arrow.
On a list `↑` and `↓` move between the rows at the cursor's own level,
so a level reads as the list it is and `Tab` is the way down;
on a gantt they move between every row, because the chart reads top to
bottom. The filter keeps a row whose descendant matches,
because a match needs its parent on the screen to be under.

Gantt nests as **rows under rows**, not blocks within a row:
children within a parent's row overlap as soon as two of them share a week,
and a chart that overlaps is not a chart.
A parent draws its own bar,
or, when it has no dates of its own, the envelope of its children's,
folded or not (Gantt above).
The chart's own arguments — `start`, `stop`, `label`, `progress` — are the
chart's, not a layer's: one chart has one time axis,
and a level that measured something else would not be the same chart.
A layer's `query`, `rank` and `group_by` apply there as they do on a list.

Not now, and listed so the next reader does not take them for oversights:
reparenting a row by dragging it out of its parent,
numbers rolled up from children onto a folded parent,
and creating a child from the tree (`111e8e9`; create in general is the `new` kind,
decided 2026-10-07 in `doc/design/create.md`, and the child ghost is `b9a9b62` there).

## Creating from a view

Decided 2026-10-07 in `doc/design/create.md`, and only pointed at from here:
`new` is a view kind, show's page over a draft that Create commits as `issue new` would;
the ghost is a `+ (new)` row at the foot of each group, column and lane that opens it
prefilled with the fields a drop there would write;
and a ghost is never a rank sibling, so the Rank section above holds with one more row on the screen.

## Deferred

Not decided, and grouped here because they are one conversation:

- **Actions injected into views.**
  `actions={"Start": start_fn}` becoming buttons on rows, cards and the show pane,
  plus a global bar for text, inputs and flow-wide actions.
  This subsumes the `on_change` / `on_select` callback sketch that E9 carried:
  a callback is an action the view invents a name for,
  and an action is the same thing the flow names.
  This is the first client of the worker goroutine.
  On `show` they were to join *Submit comment* in a button row under the comment box,
  which is where *Comment and close* came from (2026-09-27);
  the button went on 2026-10-02, and where they land there is open again.
- **`work.view.split`**, two panes as one composite view.
- **Questions to the user** — choose, confirm, ask, form —
  which is how a flow asks something without a view.
- **Reparenting by grab**, above.
- **The mouse** (postponed 2026-09-28, Luis, after the keys were settled).
  A click on a cell edits it, on a link follows it, on a tab switches to it,
  and the wheel scrolls:
  Bubble Tea reports clicks, and the page knows which line each thing is on.
  What it costs is the terminal's own drag-select,
  which every terminal keeps behind `Shift` once a program asks for the mouse,
  and that is worth writing down before it is turned on.

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
3. **Board**, then **gantt**, then **nesting** — all landed 2026-09-28.

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
