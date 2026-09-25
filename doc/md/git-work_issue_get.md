## git-work issue get

Print one issue whole

### Synopsis

Print the whole issue: its fields, its people and its comments.

get pairs with set at the document level, so there is no per-field getter:
a field is `git work issue get ID | jq .fields.status`.
ID is an id prefix or an alias.

```
git-work issue get ID [flags]
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for get
```

### SEE ALSO

* [git-work issue](git-work_issue.md)	 - List issues

