## git-work push

Push updates to a git remote

### Synopsis

Push the tracker's work-* namespaces to a remote, and update
refs/remotes/<REMOTE>/work-* to match.

The remote is the argument, else the git-work.remote config, else origin. It
must be a configured remote's name: the remote-tracking refs are named after
it, so a URL is refused before anything is pushed. git remote add NAME URL
gives a URL a name.

```
git-work push [REMOTE] [flags]
```

### Options

```
  -h, --help   help for push
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

