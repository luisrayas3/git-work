## git-work schema archive

Archive a type or a field

### Synopsis

Archive a type or a field: the replicated removal, the one that reaches every
clone. A ref cannot be deleted across clones — it comes back on the next pull —
so removing a config entity is an operation like any other, and setting the
flag back undoes it.

An archived field stops being settable; it does not vanish from the issues that
have it, because a schema says what may be written now, never what was written
before. KEY is a type key or a field key, <type>/<field>.

A KEY two entities hold, two clones having defined it before exchanging, is
refused, naming both ids: name the one to archive with --id ID, a full id or a
unique prefix, archived or not, which is never read as a key.

```
git-work schema archive KEY | --id ID [flags]
```

### Examples

```
git work schema archive task/estimate
git work schema archive --id db9cdb7
```

### Options

```
  -h, --help        help for archive
      --id string   name the entity by its id or a unique prefix of it, archived or not, instead of by KEY
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

