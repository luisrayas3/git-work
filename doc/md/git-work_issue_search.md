## git-work issue search

Find the issues whose id, alias, title or description contain TEXT

### Synopsis

Print the issues whose id, alias, title or description contain TEXT, as one
array of the excerpts `git work issue list` prints.

The description is the issue's first comment; the other comments are not
searched. The match ignores case. An id matches by prefix; an alias, the
title and the description match anywhere in them.

The issues an id or an alias names come first, then those whose title
matches, then those that match in the description alone; within each, the
last edited comes first. Empty TEXT matches every issue.

The input is the unarchived issues; --include-archive brings the archived
back. --format text prints one line per issue (id, status, title).

```
git-work issue search TEXT [flags]
```

### Examples

```
The issues that mention a word:
git work issue search 'rate limit'

By Jira key, for a human:
git work issue search PROJ-12 --format text

```

### Options

```
      --include-archive   search the archived issues too
  -f, --format string     Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help              help for search
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues, one line each

