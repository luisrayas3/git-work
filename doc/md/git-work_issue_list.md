## git-work issue list

Run a jq program over the issues, JSON out

### Synopsis

Run a jq program over the issues and print what it emits.

The program's input is the array of unarchived issue excerpts, the same JSON
`git work issue list` prints: one object per issue, with an id, times, an
author and a fields map. --include-archive brings the archived back into the
input. With no program, the list is every issue of the input, last edited first.

--format json prints each emitted value as JSON, one per line when there are
several. --format text prints one line per issue when the program returned
issues, and falls back to JSON when it returned anything else.

--at TIME runs the program over the issues as they stood at that moment,
replayed from their operations: TIME is a date (2026-09-21), an RFC 3339 time, or a duration back from now (7d, 2w, 12h).
An issue created after TIME is absent, and archived is the value that stood
then, so the input leaves out what was archived at the time.

```
git-work issue list [PROGRAM] [flags]
```

### Examples

```
Every issue, in the input's own order:
git work issue list .

The titles of the issues of one epic:
git work issue list 'map(select(.fields.parent == "6a1b2c3")) | map(.fields.title)'

What was open a week ago:
git work issue list 'map(select(.fields.status != "done"))' --at 7d

The archived issues:
git work issue list 'map(select(.fields.archived))' --include-archive

```

### Options

```
      --at string         the issues as they stood at TIME
      --include-archive   include the archived issues in the program's input
  -f, --format string     Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help              help for list
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues, one line each

