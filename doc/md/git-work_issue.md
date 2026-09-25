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

```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for issue
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work issue add](git-work_issue_add.md)	 - Add items to list-valued fields of an issue
* [git-work issue archive](git-work_issue_archive.md)	 - Archive an issue
* [git-work issue comment](git-work_issue_comment.md)	 - Write an issue's comments
* [git-work issue get](git-work_issue_get.md)	 - Print one issue whole
* [git-work issue log](git-work_issue_log.md)	 - Print an issue's history
* [git-work issue new](git-work_issue_new.md)	 - Create a new issue from a JSON document
* [git-work issue remove](git-work_issue_remove.md)	 - Remove items from list-valued fields of an issue
* [git-work issue rm](git-work_issue_rm.md)	 - Remove an issue from the local repository
* [git-work issue set](git-work_issue_set.md)	 - Set fields of an issue

