## git-work schema log

Print the schema's history

### Synopsis

Print the schema's history: every operation of every type and field entity,
one JSON object per line, oldest first within each entity. With a KEY, only
that entity's.

This is what says who added a status and when. KEY is a type key or a field
key, <type>/<field>.

```
git-work schema log [KEY] [flags]
```

### Examples

```
git work schema log
git work schema log task/status
```

### Options

```
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for log
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

