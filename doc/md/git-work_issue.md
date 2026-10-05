## git-work issue

List issues

### Synopsis

Run a jq program over the issues and print what it emits.

The program's input is the array of issue excerpts, the same JSON this command
prints: one object per issue, with an id, times, an author and a fields map.
With no program, the list is every unarchived issue, last edited first.

Each emitted value is printed as JSON, one per line when there are several.
--format text prints one line per issue when the program returned issues, and
falls back to JSON when it returned anything else.

--at TIME runs the program over the issues as they stood at that moment,
replayed from their operations: TIME is a date (2026-09-21), an RFC 3339 time, or a duration back from now (7d, 2w, 12h).
An issue created after TIME is absent, and archived is the value that stood
then, so the default program hides what was archived at the time.

```
git-work issue [PROGRAM] [flags]
```

### Examples

```
Every issue, in the input's own order:
git work issue .

The titles of the issues of one epic:
git work issue 'map(select(.fields.parent == "6a1b2c3")) | map(.fields.title)'

A kanban of what is not done:
git work view board '{"query":"map(select(.fields.status != \"done\"))","columns":"status"}'

What was open a week ago:
git work issue 'map(select(.fields.status != "done"))' --at 7d

```

### Options

```
      --at string       the issues as they stood at TIME
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for issue
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work issue add](git-work_issue_add.md)	 - Add items to list-valued fields of an issue
* [git-work issue archive](git-work_issue_archive.md)	 - Archive an issue
* [git-work issue comment](git-work_issue_comment.md)	 - Write an issue's comments
* [git-work issue get](git-work_issue_get.md)	 - Print one issue whole
* [git-work issue log](git-work_issue_log.md)	 - Print the history of one issue or of many
* [git-work issue new](git-work_issue_new.md)	 - Create a new issue from a JSON document
* [git-work issue remove](git-work_issue_remove.md)	 - Remove items from list-valued fields of an issue
* [git-work issue rm](git-work_issue_rm.md)	 - Remove an issue from the local repository
* [git-work issue set](git-work_issue_set.md)	 - Set fields of an issue

