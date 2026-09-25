## git-work schema rm

Remove a type or a field from the local repository

### Synopsis

Remove a type's or a field's local ref. This is local: the entity comes back on
the next pull. The replicated removal is archive.

KEY is a type key or a field key, <type>/<field>.

```
git-work schema rm KEY [flags]
```

### Examples

```
git work schema rm task/estimate
```

### Options

```
  -h, --help   help for rm
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

