## git-work pull

Pull updates from a git remote

### Synopsis

Fetch the tracker's work-* namespaces from a remote into
refs/remotes/<REMOTE>/work-*, then merge them into the local tracker.

The remote is the argument, else the git-work.remote config, else origin. It
must be a configured remote's name: the remote-tracking refs are named after
it, so a URL is refused before anything is fetched. git remote add NAME URL
gives a URL a name.

```
git-work pull [REMOTE] [flags]
```

### Options

```
  -h, --help   help for pull
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

