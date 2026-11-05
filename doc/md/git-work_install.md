## git-work install

Add the tracker's fetch refspecs to a remote's configuration

### Synopsis

Add one fetch refspec per tracker namespace to remote.<REMOTE>.fetch:
+refs/<ns>/*:refs/remotes/<REMOTE>/<ns>/* for each of the work-* namespaces.

The tracker's remote-tracking refs live under refs/remotes/<REMOTE>/work-*/.
Under the default refspec alone git reads them as branches, so git remote
prune and git fetch --prune delete them all, and the next pull fetches the
whole tracker again. With these refspecs a prune drops only what the remote
deleted.

Run it once per clone. The remote is the argument, else the git-work.remote
config, else origin, and it must be a configured remote. Only the missing lines
are added, with git config --add; the others are left as they are, so running
it again changes nothing. It prints nothing.

```
git-work install [REMOTE] [flags]
```

### Examples

```
git work install
git work install upstream
```

### Options

```
  -h, --help   help for install
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

