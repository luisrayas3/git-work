## git-work flow import

Import flows from Starlark files

### Synopsis

Import flows from files, directories of *.star files, or standard input.

A file is one flow: exactly one top-level def, whose name is the flow's name,
whose docstring is the description and whose parameters are its arguments. The
file's name and location never matter, so a scratch file anywhere imports the
same as one under the repository.

Import is an upsert keyed on the def's name: a flow that is not there is
created and its id printed, one whose script or description differs is
updated, and one that is unchanged emits nothing. --prune additionally
archives every flow the inputs do not mention, which is the only way an import
removes anything.

Every input is parsed before anything is written, so a file that does not
parse aborts the whole import.

```
git-work flow import FILE|DIR|-... [flags]
```

### Examples

```
git work flow import flows/
git work flow import flows/board.star flows/report.star
git work flow export board | git work flow import -
```

### Options

```
      --prune     Archive every flow the inputs do not mention
      --dry-run   Print what would be done, and write nothing
  -h, --help      help for import
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows

