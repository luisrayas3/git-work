## git-work view home

Draw the home page: flows by name, Enter runs one

### Synopsis

Draw the home page: the flows it names, each with its docstring's first line.
Enter runs one, as `git work flow run NAME` does, and the view it opens goes
on the same stack; a flow that prints shows its output on a page. Bare
`git work` runs the flow named home, which calls this.

KWARGS is a JSON object of this view's arguments, read from standard
input when it is "-":
  flows            strings                          required           the flows drawn, in order, each by name with its docstring's first line, every one a stored flow; Enter runs one with its defaults, and the view it opens goes on the same stack


```
git-work view home [KWARGS|-] [flags]
```

### Options

```
  -h, --help   help for home
```

### SEE ALSO

* [git-work view](git-work_view.md)	 - Draw the issues

