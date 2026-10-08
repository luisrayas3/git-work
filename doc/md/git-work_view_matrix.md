## git-work view matrix

Draw a matrix

### Synopsis

Draw a matrix.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query            query                            defaulted          the jq program the issues come from, over every unarchived issue; the default is all of them, last edited first
  include_archive  bool                             defaulted          include the archived issues in the input the query runs over; false by default
  rows             field key                        required           the field whose values are the rows
  columns          field key                        required           the field whose values are the columns
  value            field key                        optional           the number field summed in a cell; with none, a cell counts its issues
  row_values       strings                          defaulted          the row values, in order; the axis's own order by default, which is resolved at render time
  column_values    strings                          defaulted          the column values, in order; the axis's own order by default
  group_by         field key                        optional           the field whose value starts a new block of rows; the rows with no value at all are the last block, (none)

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view matrix [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for matrix
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

