## git-work issue add

Add items to list-valued fields of an issue

### Synopsis

Add items to list-valued fields from a JSON object of lists, given as the
argument or on standard input: one AddValue operation per item, one commit.

  git work issue add 2f15 '{"labels":["area:core","prio:high"]}'

Items have set semantics, so two people adding two items concurrently both
win. A many-cardinality relation is a list field too, of the other issues'
ids; an item that is the unambiguous prefix of one issue is taken as its id.
ID is an id prefix or an alias.

```
git-work issue add ID ITEMS|- [flags]
```

### Options

```
      --dry-run   Print the operations that would be committed, and write nothing
  -h, --help      help for add
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

