## git-work view show

Draw a show

### Synopsis

Draw a show.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  id               id                               required           the issue to show, by id prefix or alias
  fields           field keys                       defaulted          the fields shown, in order; the type's fields in schema order by default
  children         child relations                  optional           the issues pointing at this one, a section each, as [{"type":"task","relation":"parent","fields":["status"]}]; relation may be the inverse name instead, and type and fields may be left out

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view show [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for show
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

