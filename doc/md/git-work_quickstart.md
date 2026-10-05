## git-work quickstart

Print how to use git-work, and this repository's types

### Synopsis

Print a short guide to git-work, as markdown, for an agent.

The guide is the model — how issues are stored, what the commands are, where
everything this leaves out can be found — followed by the types and fields
this repository actually has, read from the store. One call is enough to write
a `git work issue new` document the schema check accepts.

It is `work.quickstart()` in a flow's script, the same text.

```
git-work quickstart [flags]
```

### Options

```
  -h, --help   help for quickstart
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

