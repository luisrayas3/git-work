## git-work view list

Draw a list

### Synopsis

Draw a list.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  query            query                            defaulted          the jq program the rows come from, over every unarchived issue; the default is all of them, last edited first; a row may add a key, its identity on screen (unique, not starting with +), with or without the id of the issue it acts on, and children, the ids or rows nested under it
  include_archive  bool                             defaulted          include the archived issues in the input the query runs over; false by default
  fields           field keys                       defaulted ["type","title"] the fields shown as columns, in order
  details          field keys                       optional           the fields shown on a dim second line under each row
  group_by         field key                        optional           the field whose value starts a new section, in the value's own order (an enum's schema order, a relation's issues by rank); the rows with no value at all are the last section, (none)
  expand           relation or layer                optional           the relation nested under a row: "children", or a layer {"relation":…,"query":…,"include_archive":…,"fields":…,"details":…,"group_by":…,"expand":…}; a relation is a stored one (parent) or the inverse name of one (children), a row's own top-level children (ids or rows) replace it for that row, so a layer needs one only where a row above lists none; a layer's query runs over that row's own unarchived children (the archived too with include_archive, which a layer inherits from the call or the layer above unless it names its own), the keys it leaves out are the layer above's, and its expand is the level below: a layer, or a number of further levels this same layer draws, 0 for every one; links is refused, being several relations, a show's side tables
  open             bool or levels                   optional           how unfolded a nested view opens: true every parent, a number that many levels from the roots, false or absent folded
  shows            type to spec                     optional           how each issue type is shown, on the page Enter opens and every page opened from there: {"story":{"fields":["status","links"],"expand":["children","links"]}} maps a type key to a spec, what show takes less the id, checked against that type; fields are the rows of show's fields table, in order, the type's in schema order when absent, its link fields folded into one links row, the count of what the issue is linked to by relation, at the first one's place: naming a link field draws it as a row, and naming links places that row, which a list of fields without it leaves out; expand is the side tables beside the fields (under them in a narrow window), a relation name ("children", "blocks") or a layer {"relation":…,"query":…,"include_archive":…,"fields":…}, or a list of them, one table per element, a relation stored or the inverse name the schema gives one, the query over the unarchived issues it reaches, fields the columns after id and title; links is a table per link relation with entries, stored then inverse; a type the call leaves out is shown by its show in the schema, else by {"expand":["links"]}, whole, never merged

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view list [KWARGS|-] [flags]
```

### Options

```
  -h, --help   help for list
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

