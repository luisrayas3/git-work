## git-work view show

Draw a show

### Synopsis

Draw a show.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  id               id                               required           the issue to show, by id prefix or alias
  fields           field keys                       defaulted          the fields shown, in order; the type's fields in schema order by default
  expand           relations or layers              optional           the issues a relation reaches from this one, a table each beside the fields (under them in a narrow window): what a list's expand takes, a relation name ("children", "blocks") or a layer {"relation":…,"query":…,"include_archive":…,"fields":…}, or a list of them, one table per element; a relation is a stored one or the inverse name the schema gives one, the query runs over the unarchived issues it reaches, fields are the columns after id and title, and details, group_by and expand are refused, a side table being flat
  show             type to show arguments           optional           what the pages this one opens are opened by, never this issue: a side table's row, a relation cell, the page after Create; a type key mapped to show's arguments without id, as a list takes it

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view show [KWARGS|-] [flags]
```

### Options

```
  -h, --help   help for show
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

