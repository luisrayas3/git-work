## git-work view new

Create an issue in a form: show's page over a draft, written on Create

### Synopsis

Create an issue in a form: show's page over an issue that does not exist yet.
Nothing is written until Create, which commits the draft as `issue new` would,
one operation; the created id is printed, or nothing when the form is left.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  doc              issue document                   optional           the draft to open on, as issue new takes it: {"fields":{"type":"task","parent":"abc1234"},"body":"…"}; every value stays editable
  fields           field keys                       defaulted          the fields shown, in order; the type's fields in schema order by default

An `optional` argument has no default: name it and the view does that
thing, leave it out and it does not.


```
git-work view new [KWARGS|-] [flags]
```

### Options

```
      --gui    Draw the view in the browser
  -h, --help   help for new
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

