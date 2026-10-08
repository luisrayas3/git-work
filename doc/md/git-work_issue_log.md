## git-work issue log

Print the history of one issue or of many

### Synopsis

Print operations, oldest first, in the shape the store holds it. Each entry
names the issue it belongs to. This is what a status report is generated from.

The argument is one issue — an id prefix or an alias — or a jq program over
the same array the list runs on, the unarchived issues, in which case the
selected issues' operations all come back, ordered by time. With no argument
the selection is the list's default: every unarchived issue.
--include-archive brings the archived back into the program's input; an id
names its issue archived or not, with or without it.

An id is tried first, because no id prefix is a valid jq program; what does not
resolve is compiled as one, and if that fails too the error names both.

--from TIME and --to TIME select the operations written in the half-open
window [from, to), so back-to-back windows neither drop an operation nor count
it twice. TIME is a date (2026-09-21), an RFC 3339 time, or a duration back from now (7d, 2w, 12h). The cut is each operation's own wall-clock
time, so an operation pulled late still lands in the window it was written in.

```
git-work issue log [ID|PROGRAM] [flags]
```

### Examples

```
What one issue has been through:
git work issue log 6a1b2c3

Everything that happened this past week:
git work issue log --from 7d --format text

```

### Options

```
      --from string       only operations at or after TIME
      --to string         only operations before TIME
      --include-archive   include the archived issues in the program's input
  -f, --format string     Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help              help for log
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

