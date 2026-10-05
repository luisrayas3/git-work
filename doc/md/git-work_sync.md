## git-work sync

Pull from a git remote, then push back to it

### Synopsis

Run git work pull, then git work push, against one remote: the whole store in
and the whole store out, in the one order that works, since pushing over a
tracker that was never pulled is rejected anyway. The remote is the argument,
else the git-work.remote config, else origin.

With --jira, git work jira sync runs between the two, so one run takes Jira's
changes in and publishes the store with them. The Jira step is opt-in: without
the flag nothing Jira-related is read or checked. When it fails the push still
happens — the tracker is published whatever Jira did — and the command exits 1.
A pull that fails stops the command.

The output is the pull's, then the Jira sync's one JSON object per line
(--format text for a human), then the push's. Only the Jira step is
dry-runnable, so --dry-run needs --jira.

```
git-work sync [REMOTE] [flags]
```

### Examples

```
git work sync
git work sync --jira
git work sync upstream
```

### Options

```
      --jira            Sync with the bound Jira project between the pull and the push
      --dry-run         With --jira, read both sides of Jira and write neither; the pull and the push still run
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for sync
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

