## git-work migrate

Migrate the store once from git-bug's format to the owned model

### Synopsis

Migrate the store once from git-bug's format to the owned model (bf6f392).

Every issue under refs/issues/* is replayed under refs/work-issues/*,
one commit per original commit, with its id, its comment ids and its
lamport times unchanged, and the label taxonomy becomes fields;
the identities are copied to refs/work-users/*. The old refs are kept.

The schema has to be in place first: git work schema import schema.yaml.
The command refuses when refs/work-issues/* holds anything.
doc/design/store-migration.md is the design.

```
git-work migrate [--dry-run] [flags]
```

### Options

```
      --dry-run   Print what would be written and write nothing but the identity copy
  -h, --help      help for migrate
```

### SEE ALSO

* [git-work](git-work.md)	 - A project tracker embedded in Git

