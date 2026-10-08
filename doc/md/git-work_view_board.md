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
  group_by         field key                        optional           the field whose value starts a new swimlane; the cards with no value at all are the last swimlane, (none)
  show             type to show arguments           optional           what Enter opens per type: {"epic":{"expand":"children"}} maps a type key to show's arguments without id, checked as show's own call; the type is the stored issue's, an unlisted one opens a bare show, and every page opened from there opens by the same map

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view board [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for board
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

