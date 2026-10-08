## git-work schema rm

Remove a type or a field from the local repository

### Synopsis

Remove a type's or a field's local ref. This is local: the entity comes back on
the next pull. The replicated removal is archive.

KEY is a type key or a field key, <type>/<field>. A KEY two entities hold is
refused, naming both ids: name one with --id ID, a full id or a unique prefix,
archived or not, which is never read as a key.

```
git-work schema rm KEY | --id ID [flags]
```

### Examples

```
git work schema rm task/estimate
git work schema rm --id db9cdb7
```

### Options

```
  -h, --help        help for rm
      --id string   name the entity by its id or a unique prefix of it, archived or not, instead of by KEY
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

