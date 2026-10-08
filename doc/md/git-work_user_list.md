## git-work user list

List identities, JSON out

### Synopsis

List the identities this repository knows about, as JSON.

JSON out by default, like every other reader; --format text prints one line
per identity, the id and the display name, which is what the bare
`git work user` prints.

```
git-work user list [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for list
```

### SEE ALSO

* [git-work user](git-work_user.md)	 - List identities, one line each

