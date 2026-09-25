## git-work view board

Draw a board

### Synopsis

Draw a board.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query      query                            defaulted          the jq program the issues come from; the default is every unarchived issue, last edited first
  columns    field key                        required           the field whose values are the columns
  values     strings                          defaulted          the column values, in order; the field's schema order by default, which is resolved at render time
  card       field keys                       defaulted ["title"] the fields shown on a card
  group_by   field key                        feature            the field whose value starts a new swimlane
  rank       field key                        feature            the rank field cards are ordered and dragged by

A `feature` argument is in the table and not drawn yet.


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

