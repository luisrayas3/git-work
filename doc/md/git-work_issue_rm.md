## git-work issue rm

Remove an issue from the local repository

### Synopsis

Remove an issue's local ref. This is local: the issue comes back on the next
pull, and removing one that came from a bridge does not remove it on the remote.
The replicated removal is archive.
ID is an id prefix or an alias.

```
git-work issue rm ID [flags]
```

### Options

```
  -h, --help   help for rm
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

