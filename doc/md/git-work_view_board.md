## git-work view board

Draw a board

### Synopsis

Draw a board.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query            query                            defaulted          the jq program the issues come from, over every unarchived issue; the default is all of them, last edited first
  include_archive  bool                             defaulted          include the archived issues in the input the query runs over; false by default
  columns          field key                        required           the field whose values are the columns
  values           strings                          defaulted          the column values, in order; the field's schema order by default, which is resolved at render time
  card             field keys                       defaulted ["title"] the fields shown on a card
  column_width     int, at least 10                 defaulted 32       the narrowest a column goes before the board scrolls sideways; when every column fits they share the width
  group_by         field key                        optional           the field whose value starts a new swimlane, in the value's own order (an enum's schema order, a relation's issues by rank); the cards with no value at all are the last swimlane, (none)
  empty_groups     bool                             defaulted          with group_by, add the swimlanes no card falls in, by the field's kind: every value an enum lists, false and true for a bool, and for a relation the siblings of the lanes the cards are in, the other issues whose own group_by holds what theirs holds (an epic's fellow epics under its initiative); text, number, date and identity get none, and (none) is still drawn only when a card has no value; false by default
  draft            issue document                   optional           what an issue created in this view starts with, as issue new takes it: {"fields":{"iteration":"abc1234"},"body":"…"}; every ghost's draft starts from it, then takes the type its scope's rows share where it names none, its group's value, and under a parent the relation to it, dropping what the resulting type has no field for; a child scope inherits it, and every value stays editable
  shows            type to spec                     optional           how each issue type is shown, on the page Enter opens and every page opened from there: {"story":{"fields":["status","links"],"expand":["children","links"]}} maps a type key to a spec, what show takes less the id, checked against that type; fields are the rows of show's fields table, in order, the type's in schema order when absent, its link fields folded into one links row, the count of what the issue is linked to by relation, at the first one's place: naming a link field draws it as a row, and naming links places that row, which a list of fields without it leaves out; expand is the side tables beside the fields (under them in a narrow window), a relation name ("children", "blocks") or a layer {"relation":…,"query":…,"include_archive":…,"fields":…}, or a list of them, one table per element, a relation stored or the inverse name the schema gives one, the query over the unarchived issues it reaches, fields the columns after id and title; links is a table per link relation with entries, stored then inverse; a type the call leaves out is shown by its show in the schema, else by {"expand":["links"]}, whole, never merged

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view board [KWARGS|-] [flags]
```

### Options

```
  -h, --help   help for board
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

