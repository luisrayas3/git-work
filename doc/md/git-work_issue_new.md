## git-work issue new

Create a new issue from a JSON document

### Synopsis

Create an issue from a JSON document, given as the argument or on standard input.

  {"fields": {"title": "…", "type": "task", "status": "open"},
   "body": "the first comment",
   "aliases": {"jira": "PROJ-12"}}

A title is required and lives in fields, like every other property of an issue.
An alias is an external id, immutable, accepted wherever an id is.
The new issue's id is printed, and nothing else.

```
git-work issue new DOC|- [flags]
```

### Examples

```
git work issue new '{"fields":{"title":"Task: rework the CLI","type":"task"}}'
echo "$doc" | git work issue new -
```

### Options

```
  -h, --help   help for new
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

