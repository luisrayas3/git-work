## git-work bug

List issues (old format)

### Synopsis

Display a summary of each issue.

You can pass an additional query to filter and order the list. This query can be expressed either with a simple query language, flags, a natural language full text search, or a combination of the aforementioned.

```
git-work bug [QUERY] [flags]
```

### Examples

```
List open issues sorted by last edition with a query:
git work bug status:open sort:edit-desc

List closed issues sorted by creation with flags:
git work bug --status closed --by creation

Do a full text search of all issues:
git work bug "foo bar" baz

Use queries, flags, and full text search:
git work bug status:open --by creation "foo bar" baz

```

### Options

```
  -s, --status strings        Filter by status. Valid values are [open,closed]
  -a, --author strings        Filter by author
  -m, --metadata strings      Filter by metadata. Example: github-url=URL
  -p, --participant strings   Filter by participant
  -A, --actor strings         Filter by actor
  -l, --label strings         Filter by label
  -t, --title strings         Filter by title
  -n, --no strings            Filter by absence of something. Valid values are [label]
  -b, --by string             Sort the results by a characteristic. Valid values are [id,creation,edit] (default "creation")
  -d, --direction string      Select the sorting direction. Valid values are [asc,desc] (default "asc")
  -f, --format string         Select the output formatting style. Valid values are [default,plain,id,json,org-mode] (default "default")
  -h, --help                  help for bug
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git
* [git-work bug comment](git-work_bug_comment.md)	 - List an issue's comments
* [git-work bug deselect](git-work_bug_deselect.md)	 - Clear the implicitly selected issue
* [git-work bug label](git-work_bug_label.md)	 - Display labels of an issue
* [git-work bug new](git-work_bug_new.md)	 - Create a new issue
* [git-work bug rm](git-work_bug_rm.md)	 - Remove an existing issue
* [git-work bug select](git-work_bug_select.md)	 - Select an issue for implicit use in future commands
* [git-work bug show](git-work_bug_show.md)	 - Display the details of an issue
* [git-work bug status](git-work_bug_status.md)	 - Display the status of an issue
* [git-work bug title](git-work_bug_title.md)	 - Display the title of an issue

