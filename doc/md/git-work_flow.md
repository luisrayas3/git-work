## git-work flow

List the flows

### Synopsis

List the flows: their names, descriptions and arguments.

A flow is one Starlark function stored in refs/work-flows. Its name is the
flow's name, its docstring the description and its parameters the arguments
`git work flow run` takes.

--format text prints one line per flow, the name and its description.

```
git-work flow [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for flow
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work flow archive](git-work_flow_archive.md)	 - Archive a flow
* [git-work flow export](git-work_flow_export.md)	 - Print a flow's script, or write every flow to a directory
* [git-work flow import](git-work_flow_import.md)	 - Import flows from Starlark files
* [git-work flow log](git-work_flow_log.md)	 - Print the flows' history
* [git-work flow rm](git-work_flow_rm.md)	 - Remove a flow from the local repository
* [git-work flow run](git-work_flow_run.md)	 - Run a flow

