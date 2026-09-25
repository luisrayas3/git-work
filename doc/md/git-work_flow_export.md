## git-work flow export

Print a flow's script, or write every flow to a directory

### Synopsis

Print a flow's script on standard output, verbatim, so that a redirection
writes the file an import takes back unchanged.

--all writes one <name>.star per unarchived flow into the directory, which is
created if it is missing, and prints nothing.

```
git-work flow export NAME|--all DIR [flags]
```

### Examples

```
git work flow export board > flows/board.star
git work flow export --all flows/
```

### Options

```
      --all    Write every flow into the directory given as the argument
  -h, --help   help for export
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows

