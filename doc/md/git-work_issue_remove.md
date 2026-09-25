## git-work issue remove

Remove items from list-valued fields of an issue

### Synopsis

Remove items from list-valued fields from a JSON object of lists, given as
the argument or on standard input: one RemoveValue operation per item, one commit.

  git work issue remove 2f15 '{"labels":["prio:high"]}'

Removing an item that is not there changes nothing.
ID is an id prefix or an alias.

```
git-work issue remove ID ITEMS|- [flags]
```

### Options

```
      --dry-run   Print the operations that would be committed, and write nothing
  -h, --help      help for remove
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

