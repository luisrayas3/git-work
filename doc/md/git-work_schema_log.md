## git-work schema log

Print the schema's history

### Synopsis

Print the schema's history: every operation of every type and field entity,
one JSON object per line, oldest first within each entity. With a KEY, only
the operations of every entity that ever held it, archived included, the
current first and the rest by creation: one in the ordinary case, and two when
two clones defined the key before exchanging.
With --id ID, a full id or a unique prefix, only that entity's, archived or
not; an ID is never read as a key.

This is what says who added a status and when. KEY is a type key or a field
key, <type>/<field>.

```
git-work schema log [KEY | --id ID] [flags]
```

### Examples

```
git work schema log
git work schema log task/status
git work schema log --id db9cdb7
```

### Options

```
      --id string       name the entity by its id or a unique prefix of it, archived or not, instead of by KEY
  -f, --format string   Select the output formatting style. Valid values are [json,text] (default "json")
  -h, --help            help for log
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

