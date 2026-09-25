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

```
git-work schema archive KEY [flags]
```

### Examples

```
git work schema archive task/estimate
```

### Options

```
  -h, --help   help for archive
```

### SEE ALSO

* [git-work schema](git-work_schema.md)	 - Show the schema

