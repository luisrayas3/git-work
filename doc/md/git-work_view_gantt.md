## git-work view gantt

Draw a gantt

### Synopsis

Draw a gantt.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query          query                            defaulted          the jq program the issues come from; the default is every unarchived issue, last edited first
  start          field key                        required           the date field a bar starts at
  stop           field key                        required           the date field a bar ends at
  label          field key                        defaulted "title"  the field shown on a bar
  scale          enum day, week, month, quarter   defaulted "week"   how wide one column of the chart is
  from           string                           defaulted          the first date shown; the data's own extent by default
  to             string                           defaulted          the last date shown; the data's own extent by default
  progress       field key                        optional           the number field, 0 to 1, a bar is filled to
  group_by       field key                        optional           the field whose value starts a new row group; the rows with no value at all are the last group, (none)
  expand         relation or layer                optional           the relation nested under a row: "children", or a layer {"relation":…,"query":…,"fields":…,"details":…,"group_by":…,"rank":…,"expand":…}; a relation is a stored one (parent) or the inverse name of one (children), a layer's query runs over that row's own unarchived children, the keys it leaves out are the layer above's, and its expand is the level below ("self" repeats the layer)
  rank           field key                        defaulted "rank"   the rank field rows are ordered and dragged by

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view gantt [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for gantt
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

