## git-work view list

Draw a list

### Synopsis

Draw a list.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query      query                            defaulted          the jq program the issues come from; the default is every unarchived issue, last edited first
  fields     field keys                       defaulted ["type","title"] the fields shown as columns, in order
  details    field keys                       feature            the fields shown on a dim second line under each row
  group_by   field key                        feature            the field whose value starts a new section
  expand     field key                        feature            the relation whose targets are nested under a row
  depth      int                              feature            how many levels of nesting to expand
  rank       field key                        feature            the rank field rows are ordered and dragged by

A `feature` argument is in the table and not drawn yet.


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

