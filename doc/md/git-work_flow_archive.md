## git-work flow archive

Archive a flow

### Synopsis

Archive a flow: it leaves every listing and stops being importable over.

Archiving is an operation, so it reaches every clone, which is what makes it
the removal a team can rely on. A ref cannot be deleted across clones; rm
deletes the local one and the flow comes back on the next pull.

```
git-work flow archive NAME [flags]
```

### Options

```
  -h, --help   help for archive
```

### SEE ALSO

* [git-work flow](git-work_flow.md)	 - List the flows

