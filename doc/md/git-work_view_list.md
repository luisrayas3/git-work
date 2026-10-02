## git-work view list

Draw a list

### Synopsis

Draw a list.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query          query                            defaulted          the jq program the issues come from; the default is every unarchived issue, last edited first
  fields         field keys                       defaulted ["type","title"] the fields shown as columns, in order
  details        field keys                       optional           the fields shown on a dim second line under each row
  group_by       field key                        optional           the field whose value starts a new section; the rows with no value at all are the last section, (none)
  expand         relation or layer                optional           the relation nested under a row: "children", or a layer {"relation":…,"query":…,"fields":…,"details":…,"group_by":…,"rank":…,"expand":…}; a relation is a stored one (parent) or the inverse name of one (children), a layer's query runs over that row's own unarchived children, the keys it leaves out are the layer above's, and its expand is the level below ("self" repeats the layer)
  rank           field key                        defaulted "rank"   the rank field rows are ordered and dragged by

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view list [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for list
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

