## git-work flow list

List the flows, JSON out

### Synopsis

List the flows: their names, descriptions and arguments, as JSON.

A flow is one Starlark function stored in refs/work-flows. Its name is the
flow's name, its docstring the description and its parameters the arguments
`git work flow run` takes.

--format text prints one line per flow, the name and its summary line, which
is what the bare `git work flow` prints.

```
git-work flow list [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for list
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows, one line each

