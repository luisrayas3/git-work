# The app: one keymap, a palette, a finder, and a home

**Outcome:** git-work in a terminal is an application rather than a set of flows.
`git work` opens a home page;
from any page an issue is one search away,
and opens expanded the way its type says;
every command a page has is listed on a menu one key away;
and every picker narrows as it is typed into.

**Serves:** story `087decb`.
It revises `terminal-renderer.md` (There is no `tui` command; Navigation and editing),
`show-from-a-view.md` (V1, V3, and Out of scope, a default per type in the schema),
`show-side-table.md` (S1, show's `expand`),
and takes in `link-fields.md`, revised the same day to fit.

**Status:** approved 2026-10-15.

## The problem

A flow is the only way onto a page.
What `Enter` opens is whatever the flow's `show` map says,
so a story the flow did not plan for opens bare,
and there is no way from one page to an issue it does not link to.
There is no search at all:
`/` narrows the rows a page already has.

The keys are three families, standard, vim and emacs,
and most commands are a letter that only `?` lists,
which is a tool for whoever already knows it.

Pickers narrow only after `/`,
so linking to an issue, or choosing a person, among thousands,
means knowing to press it first.

## A1 — Core keys, and the rest on a palette

The keys a person needs with no menu are the ones every program has:
arrows, `enter` (open, follow), `space` (edit, grab),
`tab` and `shift+tab` (next and previous stop),
page up and down, home and end.
Every other command is on the **palette**.

**`esc` opens the palette**, a panel at the foot of the page:

- **Where you are**: the stack as a breadcrumb
  (`home › overview › show 3a2279e`),
  then the top page's whole call, query included —
  what the first line cuts in a narrow window, here in full.
- **This page's commands**: filter, fold all (and open every row, leaves too), copy cell, copy id,
  change what is expanded, link and unlink (A6), edit the query.
- **Everywhere's commands**: search (A3), new, home, run a flow, help, quit.

Each command is drawn with its letter,
and **a letter means one thing on every page** (`s` is always search, `n` always new),
so the menu teaches the keys it lists.
The precedent is Helix's space menu and Emacs's which-key.

**A second `esc` goes back**: out of the palette and off the page.
From the first page it is quit,
so one stray `esc` still cannot end a session,
which is what the call line's double press was for (`terminal-renderer.md`, 2026-09-28);
the call line becomes the palette's first section.

**While editing**, `esc` cancels the edit and nothing else,
and the editor's own keys are on the status line, as `hints.go` draws them now.

## A2 — `esc x` is `alt+x`

A terminal sends `alt+x` as the bytes `ESC x`,
which is also what `esc` then `x` typed quickly sends,
and a lone `esc` is `ESC` with nothing after it.
Without the kitty keyboard protocol the program cannot tell them apart,
and Bubble Tea decides by waiting a moment after `ESC`.

So the two are **one key, by definition, on every terminal**:
`alt+s` is search, as `esc s` is.
The kitty protocol is requested where the terminal has it,
which removes the wait and tells `shift+enter` from `enter`,
but no key means something different because of it.

This is why the palette's letters agree with emacs's `alt` keys (A4):
they are the same keys.

## A3 — One keymap; vim's modal keys are gone

The three families become one.
**Emacs's `ctrl` and `alt` keys stay**,
because they never collide with typed text;
**vim's single letters go** (`hjkl`, `g`, `G`, `y`, `Y`, `p`, `q`, `gt`, `gT`),
because a letter at the top level is now worth more as text:

**Typing on a page filters it.**
A printable key on a list, a board or a gantt starts the filter with that character,
so `/` is no longer needed to start one (it stays on the palette as `f`, and as itself).

## A4 — The palette's letters

The letters emacs gives `alt` keep emacs's meaning (A2):

| key | command |
| --- | --- |
| `w` | copy cell |
| `c` | copy id |
| `v` | page up |
| `<` `>` | first, last |

The rest take letters emacs leaves free:
`s` search, `n` new, `h` home, `r` run a flow, `f` filter,
`e` change what is expanded, `l` link, `u` unlink,
`q` quit, `?` help.
The full table is the help's, generated from the keymap as it is now.

On macOS `alt` reaches the program only when the terminal sends Option as Meta;
the palette works either way.

### Settled in building A1–A4 (2026-10-15)

Where the sections above are silent, `8a9ea75` and `72285a6` chose:

- **A letter on the palette is handed to the page as `alt+letter`**,
  so `esc w` typed slowly and typed quickly are one code path, not two.
  A printable key that is no command rings and leaves the palette open;
  a direction, `enter` or `tab` only closes it;
  any other key closes it and then means what it means on the page.
  `esc esc` typed quickly, which arrives as `alt+esc`, is back.
- **Fold all is `esc z`** (`alt+z`), and nothing else: `Z` was its key, and is text now.
  It has no key of its own on the page (Luis, 2026-10-15).
- **`?` and `/` stay keys of their own** and do not start a filter:
  every status line promises `? keys`, and `/` is the empty filter.
  Typing over a filter kept with `enter` adds to it, as `/` reopens it.
- **vim's `ctrl+u` and `ctrl+d` go with its letters**:
  the one keymap is the standard keys and emacs's.
- **`v`, `<` and `>` are not listed on the palette**:
  page up, home and end are core keys,
  and the letters work all the same, being alt keys.
- **`ctrl+c` on the palette copies the call as its shell command**,
  which the call line did, so the palette's first section keeps it.
- **`n` opens the creator on an empty draft** over any page, on its type cell,
  with the page's `show` map.
- Search, home, run a flow, link, unlink, change what is expanded
  and edit the query are left off until their features exist;
  each is an entry in `pageCommands` or `everywhere` (`tui/palette.go`).
- A draft on show or new warns *back again drops draft* on the first back,
  since back is now two presses.
- The matrix's `c` keeps what `alt+c` did there, copying the command.
- Bubble Tea v2 already requests the kitty protocol's disambiguation on every terminal,
  so nothing is added for A2.

### Two folds (2026-10-15, Luis)

`create.md` C8 (`b9a9b62`) draws a fold cell on a leaf, `▸ 0`,
which unfolds to an empty child table holding only its "(new)" row.
So the palette has **two** fold commands on a nested list or gantt:

- **`z`, fold all**: opens every row that has children, or folds them all, as today.
- **`Z`, open everything**: opens every row, the leaves included,
  so every level's "(new)" row is on screen.

Both are palette commands only, `esc z` and `esc Z` (`alt+z`, `alt+Z`, A2);
neither has a key of its own on the page, where `z` and `Z` are typed text.
It lands with C8, which is what gives a leaf something to open.

## A5 — Pickers are typed into, and the issue picker is the finder

Every picker opens **with its input focused**:
typing narrows, arrows move, `enter` chooses,
the first `esc` clears the text and the second cancels.

- **People** match on name, login and email.
- **Issues** match on id prefix, alias (a Jira key), title,
  and the description once A8 lands.

The issue picker is **the finder**,
and it is one widget in three places:
a relation field's picker,
the target of a link (A6),
and the palette's search,
where choosing an issue pushes its show (A7).
A relation's picker keeps its narrowing to the field's target types.

### Settled in building A5 (2026-10-15)

Where the section above is silent, `59bae59` chose:

- **The finder is a picker whose choices are the search's**:
  `host.SearchExcerpts` asked again on every change of the text,
  so it matches and orders as `git work issue search` does,
  description included (A9 landed first).
  A static picker (an enum, the people) narrows its own choices
  on label, the dim text beside it, and the start of the value;
  a person's login is matched without being drawn.
- **The cursor goes to the first match as the text changes**, the best one,
  and back to the current value when the text is cleared.
- **The finder hands back the first 200 matches** and counts the rest
  (`… N more: type to narrow`): the search is a scan,
  and it is drawing thousands of rows on every key that would cost.
  The current value is listed even past that.
- **A relation's picker searches the archive only for its current value**,
  which it lists whatever its type or archived state, as before;
  (none) stays last, and is narrowed as a static choice is.
- **A paste that names a choice outright** (an enum's value or label, a person)
  puts the cursor on it with the text left empty, so `esc` still cancels;
  any other paste is typed in, and refused if it keeps nothing.
- **The search (`s`) is a panel at the foot of the page**, drawn as the palette is,
  not a page of its own: it opens over any page,
  and `enter` pushes the issue's show opened by the page's own show map
  (`showOf`, `showsOf`), as `enter` on a row of that page would.
  Its first `esc` clears, its second closes it, with nothing pushed.
- **`/` is text in a picker**, no longer its narrowing key;
  `pgup` and `pgdown` move ten choices.

## A6 — Linking from the palette, inverses included

`l` on a show page picks a relation by typing (`blo` is `blocks`),
then its target in the finder;
`u` picks one of the issue's links to remove.

**Inverses are offered.**
Adding `is-blocked-by X` writes `X.blocks += this`:
one commit to one entity, the other one,
and nothing on this side is stored, so nothing has to be atomic.
That revises `link-fields.md` L5, which refused them
on the grounds that it is a second entity;
it is a second entity, but it is the only one written
("a reverse relation is just a single relation write, just on another issue", Luis).
There is no verb for it: it is `issue add` on the other issue,
and `create.md` C8 writes a child stored on its parent the same way.
The finder narrows to issues whose type has the stored field,
the write is checked against that type's schema as any write is,
and the status line says which issue was written.

## A7 — `shows`: how each type is shown, one map for every view

Every view kind's call has two parts.
The **core** is what this view is:
`query`, `fields`, `group_by`, `columns`, `start`, `open` and the rest of its table.
**`shows`** is how each issue *type* is shown, whichever view meets it:

```python
work.view.list(
    query = OPEN,
    fields = ["type", "status", "title"],
    shows = {
        "story": {"expand": [{"relation": "children", "fields": ["status", "title"]}, "links"]},
        "task":  {"fields": ["status", "priority", "parent"]},
    },
)
```

A **spec**, the value under a type, is **exactly what show takes**, less the issue:
`fields`, the rows of its fields table,
and `expand`, the related issues drawn with it,
each element a relation name or a flat layer
(`relation`, `query`, `include_archive`, `fields`, `details`, `group_by`).
`show` itself takes `id` and `shows`, and nothing else:
it draws the issue by its type's spec,
and every page it opens — a link followed, a side-table row entered —
is `show` with that issue's id and the same `shows`.

It replaces two things, and leaves a third alone:

- **`show`, the map `Enter` opens by (`show-from-a-view.md` V1), becomes `shows`**,
  on list, gantt, board, matrix and show,
  and what V3 carried from page to page is simply the same map, passed on.
- **`fields` and `expand` on show** move into the spec of the issue's type.
- **`expand` on list and gantt stays**, as it is (`terminal-renderer.md`, Nesting).
  Nesting is part of what a list *is* —
  its rows, and what folds under them —
  so it is the core's, and a spec says nothing about it.
  A spec's `expand` is the side tables on a show page, and only that.

So a story is shown one way across a flow:
its page opened from a list, from a board, from search,
or from a link on another page, is all its spec.

**Where a type's spec comes from**, most specific first,
**the whole spec, not merged key by key**:

1. the call's `shows[type]`;
2. the type's `show` in the schema;
3. the built-in spec, `{"expand": ["links"]}`, every field in schema order.

```yaml
types:
  story:
    show:
      expand: [children, links]
```

Whole rather than merged because a spec is read as one sentence
("a story is drawn with its children and its links");
a call that says `{"fields":[…]}` and silently inherits the schema's `expand`
is drawing something it never said.
A call that wants the schema's and one change spells the whole spec.

A spec is checked as show's own call is (`show-from-a-view.md` V2),
against the type it is keyed under,
and a key that is not a type is refused naming the types.

The type's `show` in the schema
reverses `show-from-a-view.md`'s Out of scope,
"A default per type in the schema.
`show-side-table.md` already places a type's usual tables with the flow."
That held while every show was opened by a flow.
Search (A5) and `git work view show '{"id":…}'` open pages no flow called,
and they need a default as much as a flow does.
The precedent is `link-fields.md` L1:
a schema attribute that the views alone read,
which changes no storage, no write and no query.

A spec that came from the schema or the built-in is **drawn dim on the first line**,
as a defaulted argument is, so the first line is still the call that drew the page.

`e` on a show page changes the spec of its issue's type,
for this page and the pages it opens,
from the relations the schema gives the type, stored and inverse, and `links`.

## A8 — `git work` is `git work flow run home`

Bare `git work` runs the flow named `home`.
With no `home` flow, or no terminal, it prints the help, as today.

`home` calls a new view kind, **`home`**:

```python
def home():
    work.view.home(flows=["overview", "board", "report"])
```

It draws the flows it names, each with its docstring's first line,
and `enter` on one runs it, the view it opens pushed onto the same stack.
`work.view.home(flows=…)` is `git work view home '{"flows":[…]}'`,
with an argument table like any kind.

**Flows call views; views never call flows**, and this one does not:
`enter` hands the flow's name to the host, which runs it,
as `enter` on a row hands an id to the host, which opens it.
The view names a call; it does not make one.
`r` on the palette offers every flow, not only home's.

### Settled in building A8 (2026-10-15)

Where the section above is silent, `1298303` chose:

- **How the name reaches the host.**
  `enter` answers a message naming the flow;
  the stack hands it to the renderer,
  which runs it on its worker — the goroutine `Render` was called on,
  the one loop `tui.go` kept for script work —
  through `flow/run`'s `Flow`, the function `git work flow run` calls,
  with the renderer itself as the flow's renderer.
  The flow's `work.view.*` call reaches `host.View` and then `Render`,
  which, finding its own program drawing,
  pushes the page onto that stack instead of starting a second program,
  and blocks the flow until the page leaves the stack
  (with the page's answer, so a flow's `new` still returns its id).
  The worker keeps serving while a flow is blocked,
  so a flow run from a page a flow drew works too.
  Quitting ends a blocked flow with an error nobody is shown.
- **`tui` imports `flow/run`**, which is no cycle:
  `flow/run` knows only the `view.Renderer` interface.
  Injecting a runner from each command instead
  was three call sites to keep in step for nothing.
- **A flow runs with its defaults.**
  One with a required argument fails at `enter`, saying which;
  home has no form for arguments.
- **A name the store lacks is refused at the call**, in `host.View`,
  naming the flows there are, as an unknown field is;
  `flows` is required and may not be empty.
  One archived after the call is drawn *(no such flow)* and rings on `enter`.
- **A flow that prints** has its output — and its returned value, as JSON —
  on a page of its own, `flow run NAME`, scrolled, `w` copying it,
  whether or not it also drew.
  One that neither prints nor draws says so on the status line;
  one that fails says why there,
  or on the output page when the reason is a Starlark backtrace.
- **`h`** goes back down to the lowest home page on the stack;
  where there is none — the stack began at `git work view list` —
  it runs the `home` flow, its page pushed over this one.
  On home already it rings.
- **`r`** is A5's picker over every stored flow, each with its first line,
  drawn over the page in the panel the search is drawn in:
  typed into at once, the first `esc` clears the text and the second cancels,
  and the flow chosen runs, its page pushed over this one.
- **Home's rows are a picker's too**, the same widget:
  typing narrows by name or first line with no `/` needed,
  and `esc` drops the text before it is the palette.

## A9 — Search over descriptions, sized for thousands

`git work issue search TEXT` and `work.issue.search(text)`:
the issues whose id, alias, title or description contain TEXT,
as excerpts, as `issue list` prints them.
The finder calls the same function.

The description is comment 0, and the excerpt carries only a comment count,
so the excerpt gains the **description's text**.
A few thousand descriptions of a few KB each is a few MB held in memory,
a substring scan over it is milliseconds,
and the excerpt is already rebuilt when its issue changes,
so there is no second structure to keep coherent.
It stays out of the JSON `issue list` prints:
a program that wants it reads the issue.

Comments stay unsearched.
bleve, kept behind the pristine `RepoIndex` (`0578918`),
is the way to them, and to ranking or stemming,
when a workflow asks.

## Order

The palette and keymap first (A1–A4): every later decision is an entry on it.
Then the finder (A5), then linking (A6), then `shows` and home (A7, A8),
the link fields' L1–L3 before A7, since the built-in spec names `links`.
Search over descriptions (A9) is last.

## Out of scope

- **The mouse**, still deferred (`terminal-renderer.md`).
- **Actions a flow injects** into a page, still deferred;
  the palette is where they would appear.
- **Searching comments** (A9).
- **A recent-issues list** in the finder;
  wanted once the finder is used enough to show what it misses.
