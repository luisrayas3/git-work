## git-work issue set

Set fields of an issue

### Synopsis

Set fields of an issue from a JSON object, given as the argument or on
standard input: one SetField operation per key, all of them in one commit.

  git work issue set 2f15 '{"status":"done","estimate":3}'

A null clears a field. Setting a field replaces it whole, last writer wins,
which is what a cardinality-one relation wants too: {"parent":"6a1b2c3"}.
For the items of a list-valued field, use add and remove instead.
ID is an id prefix or an alias.

```
git-work issue set ID FIELDS|- [flags]
```

### Options

```
      --dry-run   Print the operations that would be committed, and write nothing
  -h, --help      help for set
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

