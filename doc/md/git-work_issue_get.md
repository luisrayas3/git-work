## git-work issue get

Print one issue whole

### Synopsis

Print the whole issue: its fields, its people and its comments.

get pairs with set at the document level, so there is no per-field getter:
a field is `git work issue get ID | jq .fields.status`.
ID is an id prefix or an alias.

--at TIME prints the issue as it stood at that moment, replayed from its
operations: TIME is a date (2026-09-21), an RFC 3339 time, or a duration back from now (7d, 2w, 12h).
An issue created after TIME did not exist yet, and that is an error.

```
git-work issue get ID [flags]
```

### Options

```
      --at string       the issue as it stood at TIME
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for get
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

